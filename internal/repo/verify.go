package repo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// VerificationResult represents the result of a verification run
type VerificationResult struct {
	Command  string
	Success  bool
	Output   string
	Duration time.Duration
	ExitCode int
}

// Verifier runs verification commands for a project
type Verifier struct {
	project *Project
}

// NewVerifier creates a new Verifier for the given project
func NewVerifier(project *Project) *Verifier {
	return &Verifier{project: project}
}

// RunTests runs the project's test suite
func (v *Verifier) RunTests(ctx context.Context) *VerificationResult {
	if v.project.TestCommand == "" {
		return &VerificationResult{
			Command: "",
			Success: true,
			Output:  "No test command configured for this project type",
		}
	}

	return v.runCommand(ctx, v.project.TestCommand)
}

// RunBuild runs the project's build command
func (v *Verifier) RunBuild(ctx context.Context) *VerificationResult {
	if v.project.BuildCommand == "" {
		return &VerificationResult{
			Command: "",
			Success: true,
			Output:  "No build command configured for this project type",
		}
	}

	return v.runCommand(ctx, v.project.BuildCommand)
}

// RunLint runs the project's linter
func (v *Verifier) RunLint(ctx context.Context) *VerificationResult {
	if v.project.LintCommand == "" {
		return &VerificationResult{
			Command: "",
			Success: true,
			Output:  "No lint command configured for this project type",
		}
	}

	return v.runCommand(ctx, v.project.LintCommand)
}

// RunFormat runs the project's formatter
func (v *Verifier) RunFormat(ctx context.Context) *VerificationResult {
	if v.project.FormatCommand == "" {
		return &VerificationResult{
			Command: "",
			Success: true,
			Output:  "No format command configured for this project type",
		}
	}

	return v.runCommand(ctx, v.project.FormatCommand)
}

// RunAll runs all verification commands
func (v *Verifier) RunAll(ctx context.Context) []*VerificationResult {
	var results []*VerificationResult

	// Order: format -> lint -> build -> test
	if v.project.FormatCommand != "" {
		results = append(results, v.RunFormat(ctx))
	}

	if v.project.LintCommand != "" {
		results = append(results, v.RunLint(ctx))
	}

	if v.project.BuildCommand != "" {
		results = append(results, v.RunBuild(ctx))
	}

	if v.project.TestCommand != "" {
		results = append(results, v.RunTests(ctx))
	}

	return results
}

// runCommand executes a shell command and returns the result
func (v *Verifier) runCommand(ctx context.Context, command string) *VerificationResult {
	start := time.Now()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = v.project.Root
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"CI=true", // Many tools behave better in CI mode
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	success := true
	if err != nil {
		success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n--- stderr ---\n"
		}
		output += stderr.String()
	}

	// Truncate very long outputs
	const maxOutput = 20000
	if len(output) > maxOutput {
		output = output[:maxOutput] + "\n... (output truncated)"
	}

	return &VerificationResult{
		Command:  command,
		Success:  success,
		Output:   output,
		Duration: duration,
		ExitCode: exitCode,
	}
}

// QuickCheck performs a quick verification suitable for each iteration
func (v *Verifier) QuickCheck(ctx context.Context) *VerificationResult {
	// For Go: just run tests
	// For Node: run build + test
	// For Python: run pytest
	// For Rust: run cargo check

	switch v.project.Type {
	case ProjectTypeGo:
		return v.runCommand(ctx, "go build ./... && go test ./...")
	case ProjectTypeNode:
		if v.project.PackageManager != "" {
			return v.runCommand(ctx, fmt.Sprintf("%s run build && %s test", v.project.PackageManager, v.project.PackageManager))
		}
		return v.runCommand(ctx, "npm run build && npm test")
	case ProjectTypePython:
		return v.runCommand(ctx, "pytest -x --tb=short")
	case ProjectTypeRust:
		return v.runCommand(ctx, "cargo check && cargo test")
	case ProjectTypeMake:
		if v.project.TestCommand != "" {
			return v.runCommand(ctx, v.project.TestCommand)
		}
		return &VerificationResult{Success: true, Output: "No quick check available"}
	default:
		return &VerificationResult{Success: true, Output: "No quick check available for unknown project type"}
	}
}

// ScanForIssues scans the codebase for common issues
type Issue struct {
	Type     string
	File     string
	Line     int
	Message  string
	Priority int // 1 = critical, 2 = high, 3 = medium, 4 = low
}

// ScanForTODOs finds TODO/FIXME/HACK comments in the codebase
func ScanForTODOs(root string) ([]Issue, error) {
	patterns := []struct {
		pattern  string
		priority int
	}{
		{"FIXME", 2},
		{"HACK", 2},
		{"TODO", 3},
		{"XXX", 3},
	}

	var issues []Issue

	for _, p := range patterns {
		cmd := exec.Command("rg", "--line-number", "--no-heading", p.pattern, root)
		output, err := cmd.Output()
		if err != nil {
			// rg returns 1 for no matches
			continue
		}

		for _, line := range strings.Split(string(output), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, ":", 3)
			if len(parts) >= 3 {
				issues = append(issues, Issue{
					Type:     p.pattern,
					File:     parts[0],
					Message:  strings.TrimSpace(parts[2]),
					Priority: p.priority,
				})
			}
		}
	}

	return issues, nil
}

// GetFailingTests extracts failing test names from test output
func GetFailingTests(output string, projectType ProjectType) []string {
	var failing []string

	switch projectType {
	case ProjectTypeGo:
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "--- FAIL:") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					failing = append(failing, parts[2])
				}
			}
		}
	case ProjectTypeNode:
		// Jest/Vitest style
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "FAIL ") {
				failing = append(failing, strings.TrimPrefix(line, "FAIL "))
			}
			if strings.Contains(line, "✕") || strings.Contains(line, "✗") {
				failing = append(failing, line)
			}
		}
	case ProjectTypePython:
		// pytest style
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "FAILED ") {
				parts := strings.SplitN(line, " - ", 2)
				if len(parts) >= 1 {
					failing = append(failing, strings.TrimPrefix(parts[0], "FAILED "))
				}
			}
		}
	case ProjectTypeRust:
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "test ") && strings.Contains(line, " FAILED") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					failing = append(failing, parts[1])
				}
			}
		}
	}

	return failing
}
