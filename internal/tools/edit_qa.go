// Package tools provides the tool registry and implementations for the autonomous agent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EditQAResult contains the results of post-edit verification
type EditQAResult struct {
	ToolResult    *ToolResult // The original tool result
	Verified      bool        // Whether verification passed
	ReadBackDiff  string      // Diff between expected and actual (if any)
	FilesModified []string    // List of files modified
	GitDiffStat   string      // Output of git diff --stat
	GitDiffSnippet string     // Bounded git diff snippet
}

// ExecuteWithQA executes a tool with post-edit verification for fs_write and fs_patch
func (r *Registry) ExecuteWithQA(ctx context.Context, name string, args json.RawMessage) (*EditQAResult, error) {
	// Execute the tool
	result, err := r.Execute(ctx, name, args)
	if err != nil {
		return &EditQAResult{ToolResult: result, Verified: false}, err
	}

	qaResult := &EditQAResult{
		ToolResult: result,
		Verified:   true,
	}

	// For fs_write and fs_patch, perform post-edit verification
	if name == "fs_write" || name == "fs_patch" {
		qaResult.FilesModified = extractModifiedFiles(name, args, result)

		// Re-read the modified files to verify
		for _, file := range qaResult.FilesModified {
			absPath := file
			if !filepath.IsAbs(file) {
				absPath = filepath.Join(r.workspaceRoot, file)
			}

			// Verify file exists and is readable
			content, err := os.ReadFile(absPath)
			if err != nil {
				qaResult.Verified = false
				qaResult.ReadBackDiff = fmt.Sprintf("Failed to read back %s: %v", file, err)
				break
			}

			// Attach readback evidence to the result
			if result.Output != "" {
				result.Output += "\n\n"
			}
			result.Output += fmt.Sprintf("--- POST-EDIT VERIFICATION ---\nFile: %s\nSize: %d bytes\nFirst 500 chars:\n%s\n",
				file, len(content), truncateForDisplay(string(content), 500))
		}

		// Get git diff for context
		if IsGitRepo(r.workspaceRoot) {
			gitDiffStatResult, _ := r.Execute(ctx, "git_diff", json.RawMessage(`{"staged": false}`))
			if gitDiffStatResult != nil {
				// Parse just the stat portion
				qaResult.GitDiffStat = extractDiffStat(gitDiffStatResult.Output)
				qaResult.GitDiffSnippet = truncateForDisplay(gitDiffStatResult.Output, 1000)
			}
		}
	}

	return qaResult, nil
}

// VerifyBeforeCommit checks that verification passed before allowing commit
func (r *Registry) VerifyBeforeCommit(ctx context.Context, forceAnyway bool) (*ToolResult, error) {
	if !IsGitRepo(r.workspaceRoot) {
		return nil, fmt.Errorf("not a git repository")
	}

	// Get dirty files
	dirtyFiles := GetDirtyFiles(r.workspaceRoot)
	if len(dirtyFiles) == 0 {
		return &ToolResult{
			Name:   "verify",
			Output: "No changes to commit",
		}, nil
	}

	// Run verification (build/test)
	verifyResult, err := r.runVerification(ctx)
	if err != nil && !forceAnyway {
		return verifyResult, fmt.Errorf("verification failed: %w", err)
	}

	if verifyResult != nil && verifyResult.ExitCode != 0 && !forceAnyway {
		return verifyResult, fmt.Errorf("verification failed with exit code %d", verifyResult.ExitCode)
	}

	return verifyResult, nil
}

// runVerification runs the appropriate verification commands
func (r *Registry) runVerification(ctx context.Context) (*ToolResult, error) {
	// Try to detect project type and run appropriate verification
	// For now, try common patterns

	// Check for Go project
	if fileExists(filepath.Join(r.workspaceRoot, "go.mod")) {
		// Run go build and go test
		buildResult, _ := r.Execute(ctx, "shell", json.RawMessage(`{"command": "go build ./..."}`))
		if buildResult != nil && buildResult.ExitCode != 0 {
			return buildResult, fmt.Errorf("go build failed")
		}

		testResult, _ := r.Execute(ctx, "shell", json.RawMessage(`{"command": "go test ./..."}`))
		return testResult, nil
	}

	// Check for Node project
	if fileExists(filepath.Join(r.workspaceRoot, "package.json")) {
		testResult, _ := r.Execute(ctx, "shell", json.RawMessage(`{"command": "npm test"}`))
		return testResult, nil
	}

	// Check for Python project
	if fileExists(filepath.Join(r.workspaceRoot, "pyproject.toml")) || fileExists(filepath.Join(r.workspaceRoot, "pytest.ini")) {
		testResult, _ := r.Execute(ctx, "shell", json.RawMessage(`{"command": "pytest"}`))
		return testResult, nil
	}

	// Check for Makefile with test target
	if fileExists(filepath.Join(r.workspaceRoot, "Makefile")) {
		content, _ := os.ReadFile(filepath.Join(r.workspaceRoot, "Makefile"))
		if strings.Contains(string(content), "test:") {
			testResult, _ := r.Execute(ctx, "shell", json.RawMessage(`{"command": "make test"}`))
			return testResult, nil
		}
	}

	// No verification available
	return &ToolResult{
		Name:   "verify",
		Output: "No verification command found for this project type",
	}, nil
}

// extractModifiedFiles extracts the list of modified files from tool args/result
func extractModifiedFiles(name string, args json.RawMessage, result *ToolResult) []string {
	var files []string

	switch name {
	case "fs_write":
		var writeArgs struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &writeArgs); err == nil && writeArgs.Path != "" {
			files = append(files, writeArgs.Path)
		}

	case "fs_patch":
		// Parse the diff to find target files
		var patchArgs struct {
			Diff string `json:"diff"`
		}
		if err := json.Unmarshal(args, &patchArgs); err == nil {
			for _, line := range strings.Split(patchArgs.Diff, "\n") {
				if strings.HasPrefix(line, "+++ ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						file := strings.TrimPrefix(parts[1], "b/")
						files = append(files, file)
					}
				}
			}
		}
	}

	return files
}

// extractDiffStat extracts the --stat portion from git diff output
func extractDiffStat(diffOutput string) string {
	lines := strings.Split(diffOutput, "\n")
	var statLines []string

	// Look for lines that look like stat output (file | changes)
	for _, line := range lines {
		if strings.Contains(line, "|") && (strings.Contains(line, "+") || strings.Contains(line, "-")) {
			statLines = append(statLines, line)
		}
		// Also capture the summary line
		if strings.Contains(line, "file changed") || strings.Contains(line, "files changed") {
			statLines = append(statLines, line)
		}
	}

	if len(statLines) > 0 {
		return strings.Join(statLines, "\n")
	}

	return "[no stat available]"
}

// truncateForDisplay truncates content for display
func truncateForDisplay(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... [truncated]"
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// NewExecTool creates a new exec tool for a workspace
func NewExecTool(root string) *ExecTool {
	return &ExecTool{root: root}
}
