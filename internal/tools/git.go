package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GitStatusTool runs git status
type GitStatusTool struct {
	root string
}

func (t *GitStatusTool) Name() string { return "git_status" }

func (t *GitStatusTool) Description() string {
	return "Get the current git status including branch, staged/unstaged changes, and untracked files."
}

func (t *GitStatusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *GitStatusTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v2", "--branch")
	cmd.Dir = t.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.Output()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			return &ToolResult{
				Name:     t.Name(),
				Output:   string(exitErr.Stderr),
				Error:    err,
				Duration: time.Since(start).Seconds(),
				ExitCode: exitCode,
			}, nil
		}
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   string(output),
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

// GitDiffTool runs git diff
type GitDiffTool struct {
	root string
}

type gitDiffArgs struct {
	Staged bool   `json:"staged"`
	Ref    string `json:"ref"`
}

func (t *GitDiffTool) Name() string { return "git_diff" }

func (t *GitDiffTool) Description() string {
	return "Get git diff. Can show staged changes, unstaged changes, or diff against a specific ref."
}

func (t *GitDiffTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"staged": map[string]interface{}{
				"type":        "boolean",
				"description": "Show staged changes only",
				"default":     false,
			},
			"ref": map[string]interface{}{
				"type":        "string",
				"description": "Compare against a specific ref (branch, commit, tag)",
				"default":     "",
			},
		},
	}
}

func (t *GitDiffTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a gitDiffArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	cmdArgs := []string{"diff", "--stat", "--patch"}
	if a.Staged {
		cmdArgs = append(cmdArgs, "--staged")
	}
	if a.Ref != "" {
		cmdArgs = append(cmdArgs, a.Ref)
	}

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Dir = t.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.Output()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
		}
	}

	result := string(output)
	if result == "" {
		result = "No changes"
	}

	// Truncate very long diffs
	const maxDiff = 30000
	if len(result) > maxDiff {
		result = result[:maxDiff] + "\n... (diff truncated)"
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   result,
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

// GitCheckoutTool runs git checkout
type GitCheckoutTool struct {
	root string
}

type gitCheckoutArgs struct {
	Ref string `json:"ref"`
}

func (t *GitCheckoutTool) Name() string { return "git_checkout" }

func (t *GitCheckoutTool) Description() string {
	return "Checkout a branch, tag, or commit."
}

func (t *GitCheckoutTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ref": map[string]interface{}{
				"type":        "string",
				"description": "Branch, tag, or commit to checkout",
			},
		},
		"required": []string{"ref"},
	}
}

func (t *GitCheckoutTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a gitCheckoutArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	cmd := exec.CommandContext(ctx, "git", "checkout", a.Ref)
	cmd.Dir = t.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output,
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

// GitCreateBranchTool creates a new branch
type GitCreateBranchTool struct {
	root string
}

type gitCreateBranchArgs struct {
	Name string `json:"name"`
}

func (t *GitCreateBranchTool) Name() string { return "git_create_branch" }

func (t *GitCreateBranchTool) Description() string {
	return "Create and checkout a new branch."
}

func (t *GitCreateBranchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Name for the new branch",
			},
		},
		"required": []string{"name"},
	}
}

func (t *GitCreateBranchTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a gitCreateBranchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", a.Name)
	cmd.Dir = t.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output,
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

// GitCommitTool creates a commit
type GitCommitTool struct {
	root string
}

type gitCommitArgs struct {
	Message string `json:"message"`
}

func (t *GitCommitTool) Name() string { return "git_commit" }

func (t *GitCommitTool) Description() string {
	return "Stage all changes and create a commit with the given message."
}

func (t *GitCommitTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Commit message",
			},
		},
		"required": []string{"message"},
	}
}

func (t *GitCommitTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a gitCommitArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	// Stage all changes first
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = t.root
	addCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	if err := addCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ToolResult{
				Name:     t.Name(),
				Output:   "Failed to stage changes: " + string(exitErr.Stderr),
				Error:    err,
				Duration: time.Since(start).Seconds(),
				ExitCode: exitErr.ExitCode(),
			}, nil
		}
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	// Create commit
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", a.Message)
	commitCmd.Dir = t.root
	commitCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")

	var stdout, stderr bytes.Buffer
	commitCmd.Stdout = &stdout
	commitCmd.Stderr = &stderr

	err := commitCmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output,
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

// GitResetHardTool performs a hard reset (for recovery only)
type GitResetHardTool struct {
	root string
}

type gitResetHardArgs struct {
	Ref string `json:"ref"`
}

func (t *GitResetHardTool) Name() string { return "git_reset_hard" }

func (t *GitResetHardTool) Description() string {
	return "Perform a hard reset to a specific ref. WARNING: This discards uncommitted changes. Use only for recovery."
}

func (t *GitResetHardTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ref": map[string]interface{}{
				"type":        "string",
				"description": "Ref to reset to (branch, tag, commit SHA)",
			},
		},
		"required": []string{"ref"},
	}
}

func (t *GitResetHardTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a gitResetHardArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	cmd := exec.CommandContext(ctx, "git", "reset", "--hard", a.Ref)
	cmd.Dir = t.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output,
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

// GetCurrentBranch returns the current git branch
func GetCurrentBranch(root string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// IsGitRepo checks if the directory is a git repository
func IsGitRepo(root string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = root
	return cmd.Run() == nil
}

// GetDirtyFiles returns a list of modified/untracked files
func GetDirtyFiles(root string) []string {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var files []string
	for _, line := range strings.Split(string(output), "\n") {
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[3:]))
		}
	}
	return files
}
