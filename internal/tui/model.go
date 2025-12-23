// Package tui provides the terminal user interface using Bubble Tea.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	ctxmgr "github.com/ai/brewol/internal/context"
	"github.com/ai/brewol/internal/engine"
	"github.com/ai/brewol/internal/ollama"
	"github.com/ai/brewol/internal/tools"
)

// KeyMap defines the keybindings
type KeyMap struct {
	Escape         key.Binding
	CommandMode    key.Binding
	ModelPicker    key.Binding
	ToggleDiff     key.Binding
	ToggleThinking key.Binding
	OpenLogs       key.Binding
	Help           key.Binding
	ScrollUp       key.Binding
	ScrollDown     key.Binding
	PageUp         key.Binding
	PageDown       key.Binding
}

// DefaultKeyMap returns the default keybindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel/exit"),
		),
		CommandMode: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "command palette"),
		),
		ModelPicker: key.NewBinding(
			key.WithKeys("ctrl+m"),
			key.WithHelp("ctrl+m", "model picker"),
		),
		ToggleDiff: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "toggle diff"),
		),
		ToggleThinking: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "toggle thinking"),
		),
		OpenLogs: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "open logs"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn", "page down"),
		),
	}
}

// ShortHelp returns keybindings to be shown in the mini help view
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Escape, k.CommandMode, k.Help}
}

// FullHelp returns keybindings for the expanded help view
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Escape, k.CommandMode, k.ModelPicker},
		{k.ToggleDiff, k.ToggleThinking, k.OpenLogs, k.Help},
		{k.ScrollUp, k.ScrollDown, k.PageUp, k.PageDown},
	}
}

// Model is the main TUI model
type Model struct {
	engine          *engine.Engine
	keyMap          KeyMap
	help            help.Model
	spinner         spinner.Model
	streamView      viewport.Model
	toolLogView     viewport.Model
	backlogView     viewport.Model
	thinkingView    viewport.Model
	commandInput    textinput.Model
	promptInput     textinput.Model
	width           int
	height          int
	streamContent   string
	thinkingContent string
	thinkingBuffer  []string // Rolling buffer for display (capped)
	fullThinking    string   // Full thinking content for logging
	isThinking      bool     // Currently in thinking phase
	toolLogs        []ToolLogEntry
	suggestions     []engine.Suggestion
	showHelp        bool
	showDiff        bool
	showCommand     bool
	showModels      bool
	showThinking    bool // Show/hide thinking pane (Ctrl+T toggle)
	hideThinkingUI  bool // /hidethinking setting - UI-only toggle
	inputFocused    bool
	showCmdList     bool
	filteredCmds    []Command
	selectedCmd     int
	models          []ollama.ModelInfo
	selectedModel   int
	tokensPerSec    float64
	lastExitCode    int
	lastEscTime     time.Time
	quitting        bool
	err             error
}

// ToolLogEntry represents a tool execution log entry
type ToolLogEntry struct {
	Name     string
	Duration float64
	ExitCode int
	Output   string
	Error    error
	Time     time.Time
}

// Command represents an available command
type Command struct {
	Name        string
	Description string
	NeedsArg    bool
}

// AvailableCommands returns all available slash commands
func AvailableCommands() []Command {
	return []Command{
		{Name: "/goal", Description: "Set a new goal for the agent", NeedsArg: true},
		{Name: "/model", Description: "Switch to a different model", NeedsArg: false},
		{Name: "/models", Description: "Open model picker", NeedsArg: false},
		{Name: "/test", Description: "Test current model connection", NeedsArg: false},
		{Name: "/status", Description: "Show current status", NeedsArg: false},
		{Name: "/checkpoint", Description: "Create a git checkpoint", NeedsArg: false},
		{Name: "/rollback", Description: "Rollback to last checkpoint", NeedsArg: false},
		{Name: "/speed", Description: "Set throttle speed (0=none)", NeedsArg: true},
		{Name: "/help", Description: "Show help", NeedsArg: false},
		{Name: "/clear", Description: "Clear the output", NeedsArg: false},
		{Name: "/pause", Description: "Pause the agent", NeedsArg: false},
		{Name: "/resume", Description: "Resume the agent", NeedsArg: false},
		// System instructions commands
		{Name: "/system", Description: "System prompt: show|set|load|reset|save", NeedsArg: true},
		// Memory commands
		{Name: "/summary", Description: "Show operational summary", NeedsArg: false},
		{Name: "/memory", Description: "Show/reset rolling memory", NeedsArg: false},
		// Context commands
		{Name: "/context", Description: "Context: show|set <num>|compact", NeedsArg: false},
		{Name: "/tasks", Description: "Show task list or compact", NeedsArg: false},
		// Thinking commands
		{Name: "/think", Description: "Think: show|on|off|auto|low|medium|high", NeedsArg: false},
		{Name: "/hidethinking", Description: "Hide thinking panel: on|off", NeedsArg: false},
	}
}

// New creates a new TUI model
func New(eng *engine.Engine) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ti := textinput.New()
	ti.Placeholder = "Enter command..."
	ti.CharLimit = 256

	pi := textinput.New()
	pi.Placeholder = "Enter goal or prompt... (press Enter to send, Tab to unfocus)"
	pi.CharLimit = 1024
	pi.Focus()

	// Check if thinking should be visible by default
	thinkMode := eng.Client().GetThinkMode()
	showThinking := thinkMode != ollama.ThinkModeOff

	return Model{
		engine:         eng,
		keyMap:         DefaultKeyMap(),
		help:           help.New(),
		spinner:        s,
		commandInput:   ti,
		promptInput:    pi,
		inputFocused:   true,
		showThinking:   showThinking,
		thinkingBuffer: make([]string, 0, 100), // Cap at 100 lines for display
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.listenForUpdates(),
		m.fetchModels(),
		textinput.Blink,
	)
}

// engineUpdateMsg carries an update from the engine
type engineUpdateMsg struct {
	update engine.CycleUpdate
}

// modelsLoadedMsg carries loaded models
type modelsLoadedMsg struct {
	models []ollama.ModelInfo
	err    error
}

// modelTestMsg carries the result of a model test
type modelTestMsg struct {
	model   string
	success bool
	message string
	err     error
}

func (m Model) listenForUpdates() tea.Cmd {
	return func() tea.Msg {
		update, ok := <-m.engine.Updates()
		if !ok {
			return nil
		}
		return engineUpdateMsg{update: update}
	}
}

func (m Model) fetchModels() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		models, err := m.engine.Client().ListModels(ctx)
		return modelsLoadedMsg{models: models, err: err}
	}
}

func (m Model) testModel() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		model := m.engine.Client().GetModel()
		messages := []ollama.Message{
			{Role: "user", Content: "Say 'Hello' in one word."},
		}

		resp, err := m.engine.Client().Chat(ctx, messages, nil)
		if err != nil {
			return modelTestMsg{
				model:   model,
				success: false,
				err:     err,
			}
		}

		return modelTestMsg{
			model:   model,
			success: true,
			message: resp.Message.Content,
		}
	}
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateViewports()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case engineUpdateMsg:
		m = m.handleEngineUpdate(msg.update)
		return m, m.listenForUpdates()

	case modelsLoadedMsg:
		if msg.err == nil {
			m.models = msg.models
		} else {
			m.streamContent += fmt.Sprintf("\n[ERROR loading models: %v]\n", msg.err)
			m.streamView.SetContent(m.streamContent)
		}
		return m, nil

	case modelTestMsg:
		if msg.success {
			m.streamContent += fmt.Sprintf("\n[Model %s OK! Response: %s]\n", msg.model, strings.TrimSpace(msg.message))
		} else {
			m.streamContent += fmt.Sprintf("\n[ERROR: Model %s failed: %v]\n", msg.model, msg.err)
		}
		m.streamView.SetContent(m.streamContent)
		m.streamView.GotoBottom()
		return m, nil
	}

	// Update viewports
	if !m.showCommand {
		var cmd tea.Cmd
		m.streamView, cmd = m.streamView.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle command input mode
	if m.showCommand {
		switch msg.String() {
		case "esc":
			m.showCommand = false
			m.commandInput.Reset()
			return m, nil
		case "enter":
			cmd := m.commandInput.Value()
			m.showCommand = false
			m.commandInput.Reset()
			return m.executeCommand(cmd)
		default:
			var cmd tea.Cmd
			m.commandInput, cmd = m.commandInput.Update(msg)
			return m, cmd
		}
	}

	// Handle model picker mode
	if m.showModels {
		switch msg.String() {
		case "esc":
			m.showModels = false
			m.inputFocused = true
			m.promptInput.Focus()
			m.streamContent += "[Model picker cancelled]\n"
			m.streamView.SetContent(m.streamContent)
			return m, textinput.Blink
		case "enter":
			if m.selectedModel < len(m.models) {
				selectedName := m.models[m.selectedModel].Name
				m.engine.Client().SetModel(selectedName)
				m.engine.SyncContextSize() // Update context size for new model
				ctxSize := m.engine.GetNumCtx()
				m.streamContent += fmt.Sprintf("[Model changed to: %s (context: %dk)]\n", selectedName, ctxSize/1024)
				m.streamView.SetContent(m.streamContent)
			}
			m.showModels = false
			m.inputFocused = true
			m.promptInput.Focus()
			return m, textinput.Blink
		case "up", "k":
			if m.selectedModel > 0 {
				m.selectedModel--
			}
			return m, nil
		case "down", "j":
			if m.selectedModel < len(m.models)-1 {
				m.selectedModel++
			}
			return m, nil
		}
		return m, nil
	}

	// Handle prompt input when focused
	if m.inputFocused {
		input := m.promptInput.Value()

		// Check if we should show command list
		if strings.HasPrefix(input, "/") {
			m.showCmdList = true
			m.filteredCmds = m.filterCommands(input)
		} else {
			m.showCmdList = false
			m.selectedCmd = 0
		}

		switch msg.String() {
		case "esc":
			m.inputFocused = false
			m.showCmdList = false
			m.promptInput.Blur()
			return m, nil
		case "tab":
			// Autocomplete selected command
			if m.showCmdList && len(m.filteredCmds) > 0 {
				cmd := m.filteredCmds[m.selectedCmd]
				if cmd.NeedsArg {
					m.promptInput.SetValue(cmd.Name + " ")
					m.promptInput.SetCursor(len(cmd.Name) + 1)
				} else {
					m.promptInput.SetValue(cmd.Name)
					m.promptInput.SetCursor(len(cmd.Name))
				}
				m.filteredCmds = m.filterCommands(m.promptInput.Value())
				return m, nil
			}
			m.inputFocused = false
			m.showCmdList = false
			m.promptInput.Blur()
			return m, nil
		case "up":
			if m.showCmdList && len(m.filteredCmds) > 0 {
				if m.selectedCmd > 0 {
					m.selectedCmd--
				} else {
					m.selectedCmd = len(m.filteredCmds) - 1
				}
				return m, nil
			}
			return m, nil
		case "down":
			if m.showCmdList && len(m.filteredCmds) > 0 {
				if m.selectedCmd < len(m.filteredCmds)-1 {
					m.selectedCmd++
				} else {
					m.selectedCmd = 0
				}
				return m, nil
			}
			return m, nil
		case "enter":
			// If command list is showing, execute the SELECTED command
			if m.showCmdList && len(m.filteredCmds) > 0 {
				selectedCmd := m.filteredCmds[m.selectedCmd]
				m.promptInput.Reset()
				m.showCmdList = false
				m.selectedCmd = 0
				m.inputFocused = false
				m.promptInput.Blur()
				return m.executeCommand(selectedCmd.Name)
			}

			// Otherwise use input value
			inputVal := m.promptInput.Value()
			m.promptInput.Reset()
			m.showCmdList = false
			m.selectedCmd = 0

			if strings.HasPrefix(inputVal, "/") {
				m.inputFocused = false
				m.promptInput.Blur()
				return m.executeCommand(inputVal)
			} else if inputVal != "" {
				// Set as goal
				m.engine.SetGoal(inputVal)
				m.streamContent += fmt.Sprintf("\n[Goal set: %s]\n", inputVal)
				m.streamView.SetContent(m.streamContent)
				m.streamView.GotoBottom()
			}
			return m, nil
		default:
			var cmd tea.Cmd
			m.promptInput, cmd = m.promptInput.Update(msg)
			// Update filtered commands after input changes
			newInput := m.promptInput.Value()
			if strings.HasPrefix(newInput, "/") {
				m.showCmdList = true
				m.filteredCmds = m.filterCommands(newInput)
				m.selectedCmd = 0
			} else {
				m.showCmdList = false
			}
			return m, cmd
		}
	}

	// Normal mode keybindings
	switch {
	case key.Matches(msg, m.keyMap.Escape):
		return m.handleEscape()

	case key.Matches(msg, m.keyMap.CommandMode):
		m.showCommand = true
		m.commandInput.Focus()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.ModelPicker):
		m.showModels = true
		return m, m.fetchModels()

	case key.Matches(msg, m.keyMap.ToggleDiff):
		m.showDiff = !m.showDiff
		m.updateViewports()
		return m, nil

	case key.Matches(msg, m.keyMap.ToggleThinking):
		m.showThinking = !m.showThinking
		m.updateViewports()
		status := "hidden"
		if m.showThinking {
			status = "visible"
		}
		m.streamContent += fmt.Sprintf("\n[Thinking panel %s]\n", status)
		m.streamView.SetContent(m.streamContent)
		m.streamView.GotoBottom()
		return m, nil

	case key.Matches(msg, m.keyMap.OpenLogs):
		// Display log path
		m.streamContent += fmt.Sprintf("\n[Logs: %s]\n", m.engine.Session().Path())
		m.streamView.SetContent(m.streamContent)
		m.streamView.GotoBottom()
		return m, nil

	case key.Matches(msg, m.keyMap.Help):
		m.showHelp = !m.showHelp
		if !m.showHelp {
			m.inputFocused = true
			m.promptInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case key.Matches(msg, m.keyMap.ScrollUp):
		m.streamView.LineUp(1)
		return m, nil

	case key.Matches(msg, m.keyMap.ScrollDown):
		m.streamView.LineDown(1)
		return m, nil

	case key.Matches(msg, m.keyMap.PageUp):
		m.streamView.HalfViewUp()
		return m, nil

	case key.Matches(msg, m.keyMap.PageDown):
		m.streamView.HalfViewDown()
		return m, nil
	}

	// If any other key is pressed, focus the input
	m.inputFocused = true
	m.promptInput.Focus()
	var cmd tea.Cmd
	m.promptInput, cmd = m.promptInput.Update(msg)
	return m, tea.Batch(cmd, textinput.Blink)
}

func (m Model) handleEscape() (tea.Model, tea.Cmd) {
	now := time.Now()

	// Check for double ESC (within 600ms)
	if now.Sub(m.lastEscTime) < 600*time.Millisecond {
		m.quitting = true
		m.engine.Stop()
		return m, tea.Quit
	}

	m.lastEscTime = now

	// Single ESC: cancel current operation
	m.engine.CancelCurrent()
	m.streamContent += "\n[Operation cancelled]\n"
	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()

	return m, nil
}

func (m Model) executeCommand(cmd string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return m, nil
	}

	switch parts[0] {
	case "/goal", "/g":
		if len(parts) > 1 {
			goal := strings.Join(parts[1:], " ")
			m.engine.SetGoal(goal)
			m.streamContent += fmt.Sprintf("\n[Goal set: %s]\n", goal)
		} else {
			m.streamContent += "\n[Usage: /goal <your goal>]\n"
		}

	case "/model", "/m", "/models":
		// Always open model picker - user can select from list
		m.showModels = true
		m.streamContent += "\n[Opening model picker...]\n"
		m.streamView.SetContent(m.streamContent)
		m.streamView.GotoBottom()
		return m, m.fetchModels()

	case "/test":
		model := m.engine.Client().GetModel()
		if model == "" {
			m.streamContent += "\n[ERROR: No model selected. Use /model first]\n"
		} else {
			m.streamContent += fmt.Sprintf("\n[Testing model: %s...]\n", model)
			m.streamView.SetContent(m.streamContent)
			return m, m.testModel()
		}

	case "/status", "/s":
		state := m.engine.GetState()
		objective := m.engine.GetObjective()
		model := m.engine.Client().GetModel()
		paused := ""
		if m.engine.IsPaused() {
			paused = " (PAUSED)"
		}
		m.streamContent += fmt.Sprintf("\n[Status: %s%s | Model: %s | Objective: %s]\n", state, paused, model, objective)

	case "/checkpoint", "/cp":
		go m.engine.Checkpoint(context.Background())
		m.streamContent += "\n[Creating checkpoint...]\n"

	case "/rollback", "/rb":
		go m.engine.Rollback(context.Background())
		m.streamContent += "\n[Rolling back...]\n"

	case "/speed":
		if len(parts) > 1 {
			var speed int
			fmt.Sscanf(parts[1], "%d", &speed)
			m.engine.SetSpeed(speed)
			m.streamContent += fmt.Sprintf("\n[Speed set to: %d]\n", speed)
		} else {
			m.streamContent += "\n[Usage: /speed <0-10> (0=no throttle)]\n"
		}

	case "/help", "/h", "/?":
		m.showHelp = true

	case "/clear":
		m.streamContent = ""
		m.toolLogs = nil

	case "/pause":
		m.engine.Pause()
		m.streamContent += "\n[Agent paused]\n"

	case "/resume":
		m.engine.Resume()
		m.streamContent += "\n[Agent resumed]\n"

	case "/system":
		return m.handleSystemCommand(parts)

	case "/summary":
		return m.handleSummaryCommand()

	case "/memory":
		return m.handleMemoryCommand(parts)

	case "/context":
		return m.handleContextCommand(parts)

	case "/tasks":
		return m.handleTasksCommand(parts)

	case "/think":
		return m.handleThinkCommand(parts)

	case "/hidethinking":
		return m.handleHideThinkingCommand(parts)

	default:
		m.streamContent += fmt.Sprintf("\n[Unknown command: %s]\n", parts[0])
	}

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()

	return m, nil
}

func (m Model) handleEngineUpdate(update engine.CycleUpdate) Model {
	// Update state display
	if update.Message != "" {
		m.streamContent += fmt.Sprintf("[%s] %s\n", update.State, update.Message)
	}

	// Handle thinking content (displayed in thinking pane, NOT added to conversation)
	if update.ThinkingContent != "" {
		m.isThinking = update.IsThinking
		m.fullThinking += update.ThinkingContent
		m.thinkingContent += update.ThinkingContent

		// Keep display buffer capped at ~100 lines
		lines := strings.Split(m.thinkingContent, "\n")
		if len(lines) > 100 {
			m.thinkingContent = strings.Join(lines[len(lines)-100:], "\n")
		}

		// Update thinking view if visible
		if m.showThinking && !m.hideThinkingUI {
			m.thinkingView.SetContent(m.thinkingContent)
			m.thinkingView.GotoBottom()
		}
	}

	// Track when we transition out of thinking phase
	if !update.IsThinking && m.isThinking {
		m.isThinking = false
		// Clear thinking buffer for next response (full log preserved on disk)
		m.thinkingContent = ""
		m.fullThinking = ""
	}

	// Handle streaming tokens (answer content)
	if update.TokenContent != "" {
		m.streamContent += update.TokenContent
		m.tokensPerSec = update.TokensPerSec
	}

	// Handle tool results
	if update.ToolResult != nil {
		m.lastExitCode = update.ToolResult.ExitCode
		m.toolLogs = append(m.toolLogs, ToolLogEntry{
			Name:     update.ToolResult.Name,
			Duration: update.ToolResult.Duration,
			ExitCode: update.ToolResult.ExitCode,
			Output:   update.ToolResult.Output,
			Error:    update.ToolResult.Error,
			Time:     time.Now(),
		})

		// Keep only last 100 tool logs
		if len(m.toolLogs) > 100 {
			m.toolLogs = m.toolLogs[len(m.toolLogs)-100:]
		}

		m.updateToolLogView()
	}

	// Update suggestions
	if len(update.Suggestions) > 0 {
		m.suggestions = update.Suggestions
	}

	// Handle errors
	if update.Error != nil {
		m.err = update.Error
		m.streamContent += fmt.Sprintf("\n[ERROR] %v\n", update.Error)
	}

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()

	return m
}

func (m *Model) updateViewports() {
	headerHeight := 3
	footerHeight := 5 // Prompt input + status line
	contentHeight := m.height - headerHeight - footerHeight

	// Calculate widths
	mainWidth := m.width
	sideWidth := 0
	if m.showDiff && m.width > 120 {
		sideWidth = m.width / 3
		mainWidth = m.width - sideWidth - 1
	}

	// Calculate heights - reserve space for thinking pane if visible
	thinkingHeight := 0
	if m.showThinking && !m.hideThinkingUI && m.width > 80 {
		thinkingHeight = 10 // Fixed height for thinking pane
	}

	// Stream view (main content)
	m.streamView = viewport.New(mainWidth, contentHeight-10-thinkingHeight)
	m.streamView.SetContent(m.streamContent)

	// Tool log view
	m.toolLogView = viewport.New(mainWidth, 8)
	m.updateToolLogView()

	// Thinking view (if visible)
	if thinkingHeight > 0 {
		m.thinkingView = viewport.New(mainWidth, thinkingHeight-2) // -2 for border
		m.thinkingView.SetContent(m.thinkingContent)
	}

	// Backlog view (if showing diff/side panel)
	if sideWidth > 0 {
		m.backlogView = viewport.New(sideWidth, contentHeight)
		m.updateBacklogView()
	}
}

func (m *Model) updateToolLogView() {
	var content strings.Builder
	for i := len(m.toolLogs) - 1; i >= 0 && i >= len(m.toolLogs)-5; i-- {
		log := m.toolLogs[i]
		status := "OK"
		if log.ExitCode != 0 {
			status = fmt.Sprintf("EXIT:%d", log.ExitCode)
		}
		if log.Error != nil {
			status = "ERR"
		}
		content.WriteString(fmt.Sprintf("[%s] %s (%.2fs) %s\n",
			log.Time.Format("15:04:05"),
			log.Name,
			log.Duration,
			status,
		))
	}
	m.toolLogView.SetContent(content.String())
}

func (m *Model) updateBacklogView() {
	backlog := m.engine.GetBacklog()
	var content strings.Builder
	content.WriteString("BACKLOG\n")
	content.WriteString(strings.Repeat("─", 30) + "\n")
	for i, item := range backlog {
		if i >= 10 {
			content.WriteString(fmt.Sprintf("... and %d more\n", len(backlog)-10))
			break
		}
		content.WriteString(fmt.Sprintf("P%d %s\n", item.Priority, truncate(item.Description, 25)))
	}
	m.backlogView.SetContent(content.String())
}

// View implements tea.Model
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Render header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Render main content
	if m.showHelp {
		b.WriteString(m.renderHelp())
	} else if m.showModels {
		b.WriteString(m.renderModelPicker())
	} else if m.showCommand {
		b.WriteString(m.renderCommandInput())
	} else {
		b.WriteString(m.renderMainContent())
	}

	// Render footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m Model) renderHeader() string {
	state := m.engine.GetState()
	model := m.engine.Client().GetModel()
	project := m.engine.Project()
	branch := tools.GetCurrentBranch(project.Root)

	headerStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	modeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("205")).
		Foreground(lipgloss.Color("230")).
		Bold(true).
		Padding(0, 1)

	stateStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("33")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	// Build header parts
	mode := modeStyle.Render("AUTOPILOT")
	stateStr := stateStyle.Render(state.String())
	modelStr := headerStyle.Render(fmt.Sprintf("Model: %s", model))
	branchStr := headerStyle.Render(fmt.Sprintf("Branch: %s", branch))
	projectStr := headerStyle.Render(fmt.Sprintf("Project: %s", project.Type))

	// Build think mode indicator
	thinkMode := m.engine.Client().GetThinkMode()
	thinkStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("99")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)
	thinkStr := thinkStyle.Render(fmt.Sprintf("Think: %s", thinkMode))

	// Build context meter
	ctxState := m.engine.GetContextState()
	ctxMeter := m.renderContextMeter(ctxState)

	stats := statsStyle.Render(fmt.Sprintf("%.1f tok/s | exit: %d", m.tokensPerSec, m.lastExitCode))

	left := lipgloss.JoinHorizontal(lipgloss.Left, mode, " ", stateStr, " ", m.spinner.View())
	right := lipgloss.JoinHorizontal(lipgloss.Right, thinkStr, " ", ctxMeter, " ", modelStr, " ", branchStr, " ", projectStr, " ", stats)

	// Calculate spacing
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	spacing := m.width - leftWidth - rightWidth
	if spacing < 1 {
		spacing = 1
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, left, strings.Repeat(" ", spacing), right)
}

// renderContextMeter renders the context usage meter
func (m Model) renderContextMeter(state ctxmgr.BudgetState) string {
	if state.NumCtx == 0 {
		return ""
	}

	// Calculate percentage
	pct := int(state.UsageRatio * 100)
	if pct > 100 {
		pct = 100
	}

	// Choose color based on usage
	var color lipgloss.Color
	switch {
	case state.UsageRatio >= 0.80:
		color = lipgloss.Color("196") // Red
	case state.UsageRatio >= 0.60:
		color = lipgloss.Color("214") // Orange
	default:
		color = lipgloss.Color("82") // Green
	}

	meterStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(color).
		Padding(0, 1)

	// Format: "CTX: 4.2k/8k (52%)"
	promptK := float64(state.LastPromptTokens) / 1000
	ctxK := float64(state.NumCtx) / 1000

	meterText := fmt.Sprintf("CTX:%.1fk/%.0fk(%d%%)", promptK, ctxK, pct)
	return meterStyle.Render(meterText)
}

func (m Model) renderMainContent() string {
	var b strings.Builder

	// Suggestions panel
	if len(m.suggestions) > 0 {
		sugStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1).
			Width(m.width - 4)

		var sugContent strings.Builder
		sugContent.WriteString("SUGGESTIONS:\n")
		for _, s := range m.suggestions {
			icon := "○"
			switch s.Status {
			case "EXECUTING":
				icon = "●"
			case "SKIPPED":
				icon = "⊘"
			}
			sugContent.WriteString(fmt.Sprintf(" %s %s — %s", icon, s.Item, s.Status))
			if s.Reason != "" {
				sugContent.WriteString(fmt.Sprintf(" (%s)", s.Reason))
			}
			sugContent.WriteString("\n")
		}

		b.WriteString(sugStyle.Render(sugContent.String()))
		b.WriteString("\n")
	}

	// Thinking pane (if visible and not hidden)
	if m.showThinking && !m.hideThinkingUI {
		thinkingStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(0, 1).
			Width(m.width - 4)

		thinkingHeader := "THINKING"
		if m.isThinking {
			thinkingHeader = "THINKING..."
		}

		var thinkingPanel strings.Builder
		thinkingPanel.WriteString(thinkingHeader + "\n")
		if m.thinkingContent != "" {
			thinkingPanel.WriteString(m.thinkingView.View())
		} else {
			thinkingPanel.WriteString("  [Waiting for model thinking trace...]")
		}

		b.WriteString(thinkingStyle.Render(thinkingPanel.String()))
		b.WriteString("\n")
	}

	// Stream view
	streamStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))

	b.WriteString(streamStyle.Render(m.streamView.View()))
	b.WriteString("\n")

	// Tool log view
	toolLogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderTop(true)

	b.WriteString(toolLogStyle.Render(m.toolLogView.View()))

	return b.String()
}

func (m Model) renderHelp() string {
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(m.width - 4)

	content := `KEYBINDINGS:
  ESC           Cancel current operation
  ESC ESC       Exit (within 600ms)
  Ctrl+K        Command palette
  Ctrl+M        Model picker
  Ctrl+D        Toggle diff panel
  Ctrl+L        Show logs path
  ?             Toggle help

COMMANDS:
  /goal <text>  Set the current goal
  /model <name> Switch model
  /models       Show model picker
  /status       Show current status
  /checkpoint   Create a checkpoint
  /rollback     Rollback to last checkpoint
  /speed <n>    Set throttle (0 = no throttle)

SCROLLING:
  ↑/k           Scroll up
  ↓/j           Scroll down
  PgUp/Ctrl+U   Page up
  PgDn/Ctrl+D   Page down

Press ? to close this help`

	return helpStyle.Render(content)
}

func (m Model) renderModelPicker() string {
	pickerStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("205")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(m.width - 4)

	var content strings.Builder
	content.WriteString("╔══════════════════════════════════════╗\n")
	content.WriteString("║         SELECT OLLAMA MODEL          ║\n")
	content.WriteString("╚══════════════════════════════════════╝\n\n")

	if len(m.models) == 0 {
		content.WriteString("  Loading models...\n")
		content.WriteString("  (Make sure Ollama is running)\n")
	} else {
		currentModel := m.engine.Client().GetModel()

		for i, model := range m.models {
			cursor := "  "
			if i == m.selectedModel {
				cursor = "> "
			}

			current := ""
			if model.Name == currentModel {
				current = " ← current"
			}

			content.WriteString(fmt.Sprintf("%s%s%s\n", cursor, model.Name, current))
		}
	}

	content.WriteString("\n↑/↓ navigate | Enter select | ESC cancel")

	return pickerStyle.Render(content.String())
}

func (m Model) renderCommandInput() string {
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Padding(0, 1).
		Width(m.width - 4)

	return inputStyle.Render(fmt.Sprintf("Command: %s", m.commandInput.View()))
}

func (m Model) renderFooter() string {
	var b strings.Builder

	// Command autocomplete dropdown (above input)
	if m.showCmdList && len(m.filteredCmds) > 0 {
		cmdListStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1).
			Width(m.width - 4)

		var cmdContent strings.Builder
		for i, cmd := range m.filteredCmds {
			cursor := "  "
			if i == m.selectedCmd {
				cursor = "> "
			}
			cmdContent.WriteString(fmt.Sprintf("%s%s  %s\n", cursor, cmd.Name, lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(cmd.Description)))
		}
		cmdContent.WriteString("\n↑/↓ navigate | Tab autocomplete | Enter execute")

		b.WriteString(cmdListStyle.Render(cmdContent.String()))
		b.WriteString("\n")
	}

	// Prompt input field
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Padding(0, 1).
		Width(m.width - 4)

	if m.inputFocused {
		inputStyle = inputStyle.BorderForeground(lipgloss.Color("205"))
	} else {
		inputStyle = inputStyle.BorderForeground(lipgloss.Color("240"))
	}

	b.WriteString(inputStyle.Render(m.promptInput.View()))
	b.WriteString("\n")

	// Status line
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	objective := m.engine.GetObjective()
	if objective == "" {
		objective = "No current objective"
	}

	// Show paused state if applicable
	status := ""
	if m.engine.IsPaused() {
		status = "[PAUSED] "
	}

	left := fmt.Sprintf("%sObjective: %s", status, truncate(objective, 50))
	right := "ESC ESC to exit | Ctrl+M models | ? help"

	leftWidth := len(left)
	rightWidth := len(right)
	spacing := m.width - leftWidth - rightWidth
	if spacing < 1 {
		spacing = 1
	}

	b.WriteString(footerStyle.Render(left + strings.Repeat(" ", spacing) + right))

	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// filterCommands filters commands based on input prefix
func (m Model) filterCommands(input string) []Command {
	var filtered []Command
	input = strings.ToLower(input)

	for _, cmd := range AvailableCommands() {
		if strings.HasPrefix(strings.ToLower(cmd.Name), input) {
			filtered = append(filtered, cmd)
		}
	}

	// If no matches, return all commands
	if len(filtered) == 0 && input == "/" {
		return AvailableCommands()
	}

	return filtered
}

// handleSystemCommand handles /system subcommands
func (m Model) handleSystemCommand(parts []string) (tea.Model, tea.Cmd) {
	if len(parts) < 2 {
		m.streamContent += "\n[Usage: /system show|set <text>|load <path>|reset|save]\n"
		m.streamView.SetContent(m.streamContent)
		m.streamView.GotoBottom()
		return m, nil
	}

	subCmd := parts[1]
	switch subCmd {
	case "show":
		prompt := m.engine.GetEffectiveSystemPrompt()
		m.streamContent += "\n╔══════════════════════════════════════════════════════════════╗\n"
		m.streamContent += "║               EFFECTIVE SYSTEM PROMPT                         ║\n"
		m.streamContent += "╚══════════════════════════════════════════════════════════════╝\n\n"
		// Truncate if too long for display
		if len(prompt) > 2000 {
			m.streamContent += prompt[:2000] + "\n... [truncated, " + fmt.Sprintf("%d", len(prompt)-2000) + " more chars]\n"
		} else {
			m.streamContent += prompt + "\n"
		}
		m.streamContent += "\n═══════════════════════════════════════════════════════════════\n"

	case "set":
		if len(parts) < 3 {
			m.streamContent += "\n[Usage: /system set <instructions text>]\n"
		} else {
			text := strings.Join(parts[2:], " ")
			m.engine.SetSessionInstructions(text)
			m.streamContent += fmt.Sprintf("\n[Session instructions set (%d chars). Takes effect on next cycle.]\n", len(text))
		}

	case "load":
		if len(parts) < 3 {
			m.streamContent += "\n[Usage: /system load <path>]\n"
		} else {
			path := parts[2]
			if err := m.engine.LoadInstructionsFromFile(path); err != nil {
				m.streamContent += fmt.Sprintf("\n[ERROR: %v]\n", err)
			} else {
				m.streamContent += fmt.Sprintf("\n[Loaded instructions from %s]\n", path)
			}
		}

	case "reset":
		m.engine.ClearSessionInstructions()
		m.streamContent += "\n[Session instructions cleared. Reverted to base+repo+user layers.]\n"

	case "save":
		if err := m.engine.SaveSessionInstructions(); err != nil {
			m.streamContent += fmt.Sprintf("\n[ERROR: %v]\n", err)
		} else {
			path := m.engine.PromptManager().UserConfigPath()
			m.streamContent += fmt.Sprintf("\n[Session instructions saved to %s]\n", path)
		}

	default:
		m.streamContent += fmt.Sprintf("\n[Unknown /system subcommand: %s. Use: show|set|load|reset|save]\n", subCmd)
	}

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()
	return m, nil
}

// handleSummaryCommand handles /summary command
func (m Model) handleSummaryCommand() (tea.Model, tea.Cmd) {
	summary := m.engine.GetSummary()

	m.streamContent += "\n╔══════════════════════════════════════════════════════════════╗\n"
	m.streamContent += "║                   OPERATIONAL SUMMARY                        ║\n"
	m.streamContent += "╚══════════════════════════════════════════════════════════════╝\n\n"

	m.streamContent += fmt.Sprintf("  Goal:        %s\n", summary.CurrentGoal)
	m.streamContent += fmt.Sprintf("  Objective:   %s\n", summary.CurrentObjective)
	m.streamContent += fmt.Sprintf("  State:       %s\n", summary.CurrentState)
	m.streamContent += fmt.Sprintf("  Cycle:       %d\n", summary.CycleCount)
	m.streamContent += fmt.Sprintf("  Branch:      %s\n", summary.CurrentBranch)

	if summary.IsPaused {
		m.streamContent += "  Status:      PAUSED\n"
	} else {
		m.streamContent += "  Status:      RUNNING\n"
	}

	if summary.LastVerificationOK {
		m.streamContent += "  Last Verify: PASSED\n"
	} else {
		m.streamContent += "  Last Verify: FAILED\n"
	}

	if summary.ErrorCount > 0 {
		m.streamContent += fmt.Sprintf("  Errors:      %d (last: %s)\n", summary.ErrorCount, truncate(summary.LastError, 40))
	}

	if len(summary.DirtyFiles) > 0 {
		m.streamContent += fmt.Sprintf("\n  Dirty Files: %d\n", len(summary.DirtyFiles))
		for i, f := range summary.DirtyFiles {
			if i >= 5 {
				m.streamContent += fmt.Sprintf("    ... and %d more\n", len(summary.DirtyFiles)-5)
				break
			}
			m.streamContent += fmt.Sprintf("    - %s\n", f)
		}
	}

	if len(summary.BacklogItems) > 0 {
		m.streamContent += "\n  Backlog:\n"
		for _, item := range summary.BacklogItems {
			m.streamContent += fmt.Sprintf("    - %s\n", truncate(item, 50))
		}
	}

	m.streamContent += "\n═══════════════════════════════════════════════════════════════\n"

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()
	return m, nil
}

// handleMemoryCommand handles /memory command
func (m Model) handleMemoryCommand(parts []string) (tea.Model, tea.Cmd) {
	if len(parts) >= 2 && parts[1] == "reset" {
		m.engine.ResetMemory()
		m.streamContent += "\n[Working memory reset. Logs preserved on disk.]\n"
		m.streamView.SetContent(m.streamContent)
		m.streamView.GotoBottom()
		return m, nil
	}

	// Show current memory
	memText := m.engine.GetWorkingMemory()

	m.streamContent += "\n╔══════════════════════════════════════════════════════════════╗\n"
	m.streamContent += "║                   WORKING MEMORY                             ║\n"
	m.streamContent += "╚══════════════════════════════════════════════════════════════╝\n\n"

	if memText == "" {
		m.streamContent += "  [No working memory yet - will populate after a few cycles]\n"
	} else {
		m.streamContent += memText + "\n"
	}

	m.streamContent += "\n  Use /memory reset to clear (logs preserved on disk)\n"
	m.streamContent += "═══════════════════════════════════════════════════════════════\n"

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()
	return m, nil
}

// handleContextCommand handles /context command
func (m Model) handleContextCommand(parts []string) (tea.Model, tea.Cmd) {
	subCmd := "show"
	if len(parts) >= 2 {
		subCmd = parts[1]
	}

	switch subCmd {
	case "show":
		state := m.engine.GetContextState()
		m.streamContent += "\n╔══════════════════════════════════════════════════════════════╗\n"
		m.streamContent += "║                   CONTEXT BUDGET                             ║\n"
		m.streamContent += "╚══════════════════════════════════════════════════════════════╝\n\n"

		m.streamContent += fmt.Sprintf("  Context Window:     %d tokens\n", state.NumCtx)
		m.streamContent += fmt.Sprintf("  High Watermark:     %d tokens (%.0f%%)\n", state.HighWatermark, 80.0)
		m.streamContent += fmt.Sprintf("  Low Watermark:      %d tokens (%.0f%%)\n", state.LowWatermark, 60.0)
		m.streamContent += fmt.Sprintf("\n  Last Prompt Tokens: %d\n", state.LastPromptTokens)
		m.streamContent += fmt.Sprintf("  Last Eval Tokens:   %d\n", state.LastEvalTokens)
		m.streamContent += fmt.Sprintf("  Usage:              %.1f%%\n", state.UsageRatio*100)
		m.streamContent += fmt.Sprintf("  Available:          %d tokens\n", state.AvailableTokens)

		if state.NeedsCompaction {
			m.streamContent += "\n  ⚠️  COMPACTION NEEDED\n"
		}

		// Show last compaction event
		if event := m.engine.BudgetManager().GetLastCompactionEvent(); event != nil {
			m.streamContent += fmt.Sprintf("\n  Last Compaction: %s\n", event.CompactedItems)
			m.streamContent += fmt.Sprintf("  Tokens: %d → %d\n", event.TokensBefore, event.TokensAfter)
		}

		m.streamContent += "\n═══════════════════════════════════════════════════════════════\n"

	case "set":
		if len(parts) < 3 {
			m.streamContent += "\n[Usage: /context set <num_ctx>]\n"
		} else {
			var numCtx int
			fmt.Sscanf(parts[2], "%d", &numCtx)
			if numCtx < 1024 {
				m.streamContent += "\n[Error: num_ctx must be at least 1024]\n"
			} else {
				m.engine.SetNumCtx(numCtx)
				m.streamContent += fmt.Sprintf("\n[Context window set to: %d tokens]\n", numCtx)
			}
		}

	case "compact":
		m.engine.ForceCompact()
		m.streamContent += "\n[Forcing context compaction...]\n"

	default:
		m.streamContent += "\n[Usage: /context show|set <num>|compact]\n"
	}

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()
	return m, nil
}

// handleTasksCommand handles /tasks command
func (m Model) handleTasksCommand(parts []string) (tea.Model, tea.Cmd) {
	subCmd := "show"
	if len(parts) >= 2 {
		subCmd = parts[1]
	}

	switch subCmd {
	case "show":
		tasks := m.engine.TaskStore().GetAllTasks()
		m.streamContent += "\n╔══════════════════════════════════════════════════════════════╗\n"
		m.streamContent += "║                   TASK LIST                                  ║\n"
		m.streamContent += "╚══════════════════════════════════════════════════════════════╝\n\n"

		if len(tasks) == 0 {
			m.streamContent += "  [No tasks in store]\n"
		} else {
			// Show current task
			current := m.engine.TaskStore().GetCurrentTask()
			if current != nil {
				m.streamContent += fmt.Sprintf("  ► CURRENT: [P%d] %s\n", current.Priority, truncate(current.Title, 50))
				if current.NextAction != "" {
					m.streamContent += fmt.Sprintf("    Next: %s\n", truncate(current.NextAction, 45))
				}
				m.streamContent += "\n"
			}

			// Show pending tasks
			pending := m.engine.TaskStore().GetPendingTasks()
			if len(pending) > 0 {
				m.streamContent += "  PENDING:\n"
				for i, task := range pending {
					if i >= 10 {
						m.streamContent += fmt.Sprintf("    ... and %d more\n", len(pending)-10)
						break
					}
					m.streamContent += fmt.Sprintf("    %d. [P%d/%s] %s\n", i+1, task.Priority, task.Category, truncate(task.Title, 45))
				}
				m.streamContent += "\n"
			}

			// Show counts
			catCounts := m.engine.TaskStore().GetCategoryCounts()
			if len(catCounts) > 0 {
				m.streamContent += "  BY CATEGORY:\n"
				for cat, count := range catCounts {
					m.streamContent += fmt.Sprintf("    %s: %d\n", cat, count)
				}
			}
		}

		m.streamContent += "\n═══════════════════════════════════════════════════════════════\n"

	case "compact":
		brief := m.engine.GetTaskBrief(ctxmgr.TaskBriefCompact)
		m.streamContent += "\n[Task Brief (Compact Mode)]\n"
		m.streamContent += brief.FormatCompact()
		m.streamContent += "\n"

	case "clear":
		m.engine.TaskStore().ClearCompleted()
		m.streamContent += "\n[Completed tasks cleared]\n"

	default:
		m.streamContent += "\n[Usage: /tasks show|compact|clear]\n"
	}

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()
	return m, nil
}

// handleThinkCommand handles /think command
func (m Model) handleThinkCommand(parts []string) (tea.Model, tea.Cmd) {
	subCmd := "show"
	if len(parts) >= 2 {
		subCmd = parts[1]
	}

	switch subCmd {
	case "show":
		mode := m.engine.Client().GetThinkMode()
		isCapable := m.engine.Client().IsThinkingCapable()
		panelVisible := m.showThinking && !m.hideThinkingUI

		m.streamContent += "\n╔══════════════════════════════════════════════════════════════╗\n"
		m.streamContent += "║                   THINKING STATUS                            ║\n"
		m.streamContent += "╚══════════════════════════════════════════════════════════════╝\n\n"

		m.streamContent += fmt.Sprintf("  Think Mode:     %s\n", mode)
		m.streamContent += fmt.Sprintf("  Model Capable:  %v\n", isCapable)
		m.streamContent += fmt.Sprintf("  Panel Visible:  %v\n", panelVisible)
		m.streamContent += fmt.Sprintf("  Hide UI:        %v\n", m.hideThinkingUI)

		if m.isThinking {
			m.streamContent += "\n  Currently thinking...\n"
		}

		m.streamContent += "\n  Use: /think on|off|auto|low|medium|high\n"
		m.streamContent += "  Use: /hidethinking on|off (UI-only toggle)\n"
		m.streamContent += "  Use: Ctrl+T to toggle thinking panel\n"
		m.streamContent += "\n═══════════════════════════════════════════════════════════════\n"

	case "on":
		m.engine.Client().SetThinkMode(ollama.ThinkModeOn)
		m.showThinking = true
		m.streamContent += "\n[Thinking mode: ON - model will include reasoning trace]\n"

	case "off":
		m.engine.Client().SetThinkMode(ollama.ThinkModeOff)
		m.showThinking = false
		m.streamContent += "\n[Thinking mode: OFF - no reasoning trace]\n"

	case "auto":
		m.engine.Client().SetThinkMode(ollama.ThinkModeAuto)
		m.showThinking = m.engine.Client().IsThinkingCapable()
		m.streamContent += "\n[Thinking mode: AUTO - enabled for thinking-capable models]\n"

	case "low":
		m.engine.Client().SetThinkMode(ollama.ThinkModeLow)
		m.showThinking = true
		m.streamContent += "\n[Thinking mode: LOW - minimal reasoning trace]\n"

	case "medium":
		m.engine.Client().SetThinkMode(ollama.ThinkModeMedium)
		m.showThinking = true
		m.streamContent += "\n[Thinking mode: MEDIUM - balanced reasoning trace]\n"

	case "high":
		m.engine.Client().SetThinkMode(ollama.ThinkModeHigh)
		m.showThinking = true
		m.streamContent += "\n[Thinking mode: HIGH - extensive reasoning trace]\n"

	default:
		m.streamContent += "\n[Usage: /think show|on|off|auto|low|medium|high]\n"
	}

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()
	return m, nil
}

// handleHideThinkingCommand handles /hidethinking command
func (m Model) handleHideThinkingCommand(parts []string) (tea.Model, tea.Cmd) {
	subCmd := "show"
	if len(parts) >= 2 {
		subCmd = parts[1]
	}

	switch subCmd {
	case "on":
		m.hideThinkingUI = true
		m.streamContent += "\n[Thinking panel hidden (still requesting thinking from model)]\n"

	case "off":
		m.hideThinkingUI = false
		m.streamContent += "\n[Thinking panel visible]\n"

	default:
		status := "visible"
		if m.hideThinkingUI {
			status = "hidden"
		}
		m.streamContent += fmt.Sprintf("\n[Thinking panel: %s. Use: /hidethinking on|off]\n", status)
	}

	m.streamView.SetContent(m.streamContent)
	m.streamView.GotoBottom()
	return m, nil
}
