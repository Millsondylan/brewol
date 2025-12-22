package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ExecTool executes shell commands
type ExecTool struct {
	root string
}

type execArgs struct {
	Cmd        string            `json:"cmd"`
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env"`
	TimeoutSec int               `json:"timeout_sec"`
}

func (t *ExecTool) Name() string { return "exec" }

func (t *ExecTool) Description() string {
	return "Execute a shell command. Commands run within the workspace root by default. Returns stdout, stderr, and exit code."
}

func (t *ExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"cmd": map[string]interface{}{
				"type":        "string",
				"description": "Command to execute",
			},
			"cwd": map[string]interface{}{
				"type":        "string",
				"description": "Working directory (relative to workspace root)",
				"default":     "",
			},
			"env": map[string]interface{}{
				"type":        "object",
				"description": "Additional environment variables",
			},
			"timeout_sec": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (default 120)",
				"default":     120,
			},
		},
		"required": []string{"cmd"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a execArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	if a.TimeoutSec == 0 {
		a.TimeoutSec = 120
	}

	// Determine working directory
	workDir := t.root
	if a.Cwd != "" {
		resolved, err := resolvePath(t.root, a.Cwd)
		if err != nil {
			return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
		}
		workDir = resolved
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(a.TimeoutSec)*time.Second)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(execCtx, "sh", "-c", a.Cmd)
	cmd.Dir = workDir

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range a.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Ensure non-interactive
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, "GIT_ASKPASS=")
	cmd.Env = append(cmd.Env, "SSH_ASKPASS=")

	// Set process group for clean termination
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			return &ToolResult{
				Name:     t.Name(),
				Output:   fmt.Sprintf("Command timed out after %d seconds\nstdout:\n%s\nstderr:\n%s", a.TimeoutSec, stdout.String(), stderr.String()),
				Error:    fmt.Errorf("timeout"),
				Duration: time.Since(start).Seconds(),
				ExitCode: -1,
			}, nil
		} else if execCtx.Err() == context.Canceled {
			return &ToolResult{
				Name:     t.Name(),
				Output:   fmt.Sprintf("Command cancelled\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String()),
				Error:    ctx.Err(),
				Duration: time.Since(start).Seconds(),
				ExitCode: -1,
			}, nil
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
	const maxOutput = 50000
	if len(output) > maxOutput {
		output = output[:maxOutput] + "\n... (output truncated)"
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output,
		Duration: time.Since(start).Seconds(),
		ExitCode: exitCode,
	}, nil
}

// KillProcess attempts to kill a process and its children
func KillProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	// Try to kill the process group
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		// Send SIGINT first for graceful shutdown
		syscall.Kill(-pgid, syscall.SIGINT)

		// Give it a moment
		time.Sleep(100 * time.Millisecond)

		// Force kill if still running
		syscall.Kill(-pgid, syscall.SIGKILL)
	}

	return cmd.Process.Kill()
}

// ValidatePathContainment checks if a path is within the workspace root
func ValidatePathContainment(root, path string) error {
	if filepath.IsAbs(path) {
		cleanPath := filepath.Clean(path)
		cleanRoot := filepath.Clean(root)
		if !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) && cleanPath != cleanRoot {
			return fmt.Errorf("path traversal blocked: %s is outside workspace root %s", path, root)
		}
		return nil
	}

	joined := filepath.Join(root, path)
	cleanPath := filepath.Clean(joined)
	cleanRoot := filepath.Clean(root)

	if !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) && cleanPath != cleanRoot {
		return fmt.Errorf("path traversal blocked: %s resolves outside workspace root %s", path, root)
	}

	return nil
}
