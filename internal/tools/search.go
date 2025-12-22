package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RgSearchTool searches files using ripgrep or fallback
type RgSearchTool struct {
	root string
}

type rgSearchArgs struct {
	Query      string `json:"query"`
	Glob       string `json:"glob"`
	MaxResults int    `json:"max_results"`
}

func (t *RgSearchTool) Name() string { return "rg_search" }

func (t *RgSearchTool) Description() string {
	return "Search for patterns in files using ripgrep (rg). Falls back to Go implementation if rg is not available."
}

func (t *RgSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search pattern (regex supported)",
			},
			"glob": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g., '*.go', '**/*.ts')",
				"default":     "",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return",
				"default":     100,
			},
		},
		"required": []string{"query"},
	}
}

func (t *RgSearchTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a rgSearchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	if a.MaxResults == 0 {
		a.MaxResults = 100
	}

	// Try ripgrep first
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return t.executeRg(ctx, rgPath, a, start)
	}

	// Fallback to Go implementation
	return t.executeGoSearch(ctx, a, start)
}

func (t *RgSearchTool) executeRg(ctx context.Context, rgPath string, a rgSearchArgs, start time.Time) (*ToolResult, error) {
	cmdArgs := []string{
		"--line-number",
		"--color=never",
		"--no-heading",
		fmt.Sprintf("--max-count=%d", a.MaxResults),
	}

	if a.Glob != "" {
		cmdArgs = append(cmdArgs, "--glob", a.Glob)
	}

	cmdArgs = append(cmdArgs, a.Query, t.root)

	cmd := exec.CommandContext(ctx, rgPath, cmdArgs...)
	cmd.Dir = t.root

	output, err := cmd.Output()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			// rg returns 1 for no matches, which is not an error
			if exitCode == 1 {
				return &ToolResult{
					Name:     t.Name(),
					Output:   "No matches found",
					Duration: time.Since(start).Seconds(),
					ExitCode: exitCode,
				}, nil
			}
		} else {
			return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
		}
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   string(output),
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

func (t *RgSearchTool) executeGoSearch(ctx context.Context, a rgSearchArgs, start time.Time) (*ToolResult, error) {
	pattern, err := regexp.Compile(a.Query)
	if err != nil {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	var output strings.Builder
	resultCount := 0

	// Walk directory and search files
	err = filepath.Walk(t.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Skip common non-source directories
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check glob match
		relPath, _ := filepath.Rel(t.root, path)
		if a.Glob != "" {
			matched, _ := filepath.Match(a.Glob, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		// Search file
		matches, err := searchFileForPattern(path, pattern, a.MaxResults-resultCount)
		if err != nil {
			return nil
		}

		for _, match := range matches {
			fmt.Fprintf(&output, "%s:%d:%s\n", relPath, match.line, match.text)
			resultCount++
			if resultCount >= a.MaxResults {
				return filepath.SkipAll
			}
		}

		return nil
	})

	if err != nil && err != context.Canceled && err != filepath.SkipAll {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	if resultCount == 0 {
		return &ToolResult{
			Name:     t.Name(),
			Output:   "No matches found",
			Duration: time.Since(start).Seconds(),
		}, nil
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output.String(),
		Duration: time.Since(start).Seconds(),
	}, nil
}

type searchMatch struct {
	line int
	text string
}

func searchFileForPattern(path string, pattern *regexp.Regexp, maxMatches int) ([]searchMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []searchMatch
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() && len(matches) < maxMatches {
		lineNum++
		line := scanner.Text()
		if pattern.MatchString(line) {
			matches = append(matches, searchMatch{
				line: lineNum,
				text: truncateLine(line, 200),
			})
		}
	}

	return matches, scanner.Err()
}

func truncateLine(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
