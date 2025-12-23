// Package main provides the entry point for the brewol autonomous coding agent.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ai/brewol/internal/engine"
	"github.com/ai/brewol/internal/tui"
)

// Version information (set by goreleaser)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		workspace   string
		goal        string
		model       string
		showVersion bool
		testMode    bool
		maxCycles   int
	)

	flag.StringVar(&workspace, "workspace", "", "Workspace root directory (default: current directory)")
	flag.StringVar(&workspace, "w", "", "Workspace root directory (shorthand)")
	flag.StringVar(&goal, "goal", "", "Initial goal for the agent")
	flag.StringVar(&goal, "g", "", "Initial goal for the agent (shorthand)")
	flag.StringVar(&model, "model", "", "Ollama model to use (overrides OLLAMA_MODEL)")
	flag.StringVar(&model, "m", "", "Ollama model to use (shorthand)")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&showVersion, "v", false, "Show version information (shorthand)")
	flag.BoolVar(&testMode, "test-mode", false, "Enable test mode (exit after max-cycles)")
	flag.IntVar(&maxCycles, "max-cycles", 1, "Maximum cycles to run in test mode (default: 1)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `brewol - Autonomous Coding Agent

An autonomous terminal-based coding agent that continuously works on your codebase.
It never stops (only ESC ESC exits) and never asks for permission.

Usage:
  brewol [flags]

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Environment Variables:
  OLLAMA_HOST       Ollama API base URL (default: http://localhost:11434)
  OLLAMA_MODEL      Default model to use
  OLLAMA_API_KEY    API key for cloud Ollama endpoint
  OLLAMA_KEEP_ALIVE Model keep-alive duration

Keybindings:
  ESC               Cancel current operation
  ESC ESC           Exit (double-press within 600ms)
  Ctrl+K            Command palette
  Ctrl+M            Model picker
  Ctrl+D            Toggle diff panel
  Ctrl+L            Show logs path
  ?                 Toggle help

Commands (via Ctrl+K):
  /goal <text>      Set the current goal
  /model <name>     Switch model
  /models           Show model picker
  /status           Show current status
  /checkpoint       Create a checkpoint
  /rollback         Rollback to last checkpoint
  /speed <n>        Set throttle (0 = no throttle)

Examples:
  brewol                              Start in current directory
  brewol -w /path/to/project          Start in specified directory
  brewol -g "Fix all failing tests"   Start with a specific goal
  brewol -m codellama                 Use codellama model

For more information: https://github.com/ai/brewol
`)
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("brewol %s\ncommit: %s\nbuilt: %s\n", version, commit, date)
		os.Exit(0)
	}

	// Determine workspace
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
			os.Exit(1)
		}
	}

	// Verify workspace exists
	info, err := os.Stat(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: workspace not found: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: workspace is not a directory: %s\n", workspace)
		os.Exit(1)
	}

	// Set model from flag if provided
	if model != "" {
		os.Setenv("OLLAMA_MODEL", model)
	}

	// Check if model is set
	if os.Getenv("OLLAMA_MODEL") == "" {
		fmt.Fprintf(os.Stderr, "Warning: No model specified. Use -m flag or set OLLAMA_MODEL environment variable.\n")
		fmt.Fprintf(os.Stderr, "         Will attempt to use first available model from Ollama.\n\n")
	}

	// Create engine
	eng, err := engine.NewEngine(engine.Config{
		WorkspaceRoot: workspace,
		Goal:          goal,
		TestMode:      testMode,
		MaxCycles:     maxCycles,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create engine: %v\n", err)
		os.Exit(1)
	}

	// Check Ollama availability
	ctx := context.Background()
	if !eng.Client().IsAvailable(ctx) {
		fmt.Fprintf(os.Stderr, "Warning: Ollama is not available at %s\n", eng.Client().BaseURL())
		fmt.Fprintf(os.Stderr, "         Make sure Ollama is running: ollama serve\n\n")
	} else {
		// Try to auto-select a model if none specified
		if eng.Client().GetModel() == "" {
			models, err := eng.Client().ListModels(ctx)
			if err == nil && len(models) > 0 {
				eng.Client().SetModel(models[0].Name)
				eng.SyncContextSize() // Update context size for the selected model
				fmt.Fprintf(os.Stderr, "Auto-selected model: %s\n\n", models[0].Name)
			}
		} else {
			// Model was set from environment or flag - sync context size
			eng.SyncContextSize()
		}
	}

	// Start the engine
	eng.Start(ctx)

	// Create TUI
	m := tui.New(eng)

	// Create program with options for full-screen mode
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Handle OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		eng.Stop()
		p.Quit()
	}()

	// Run the TUI
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Session logs saved to:", eng.Session().Path())
}
