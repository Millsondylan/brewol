package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FSListTool lists files in a directory
type FSListTool struct {
	root string
}

type fsListArgs struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

func (t *FSListTool) Name() string { return "fs_list" }

func (t *FSListTool) Description() string {
	return "List files and directories in a path. Returns file names, sizes, and modification times."
}

func (t *FSListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path relative to workspace root to list",
			},
			"depth": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum depth to recurse (0 = current dir only, -1 = unlimited)",
				"default":     1,
			},
		},
		"required": []string{"path"},
	}
}

func (t *FSListTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a fsListArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	if a.Depth == 0 {
		a.Depth = 1
	}

	// Resolve and validate path
	targetPath, err := resolvePath(t.root, a.Path)
	if err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	var output strings.Builder
	currentDepth := 0

	err = filepath.WalkDir(targetPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, _ := filepath.Rel(targetPath, path)
		depth := strings.Count(relPath, string(os.PathSeparator))

		if a.Depth >= 0 && depth > a.Depth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if currentDepth != depth {
			currentDepth = depth
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		prefix := strings.Repeat("  ", depth)
		if d.IsDir() {
			fmt.Fprintf(&output, "%s%s/\n", prefix, d.Name())
		} else {
			fmt.Fprintf(&output, "%s%s (%d bytes)\n", prefix, d.Name(), info.Size())
		}

		return nil
	})

	if err != nil && err != context.Canceled {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output.String(),
		Duration: time.Since(start).Seconds(),
	}, nil
}

// FSReadTool reads file contents
type FSReadTool struct {
	root string
}

type fsReadArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func (t *FSReadTool) Name() string { return "fs_read" }

func (t *FSReadTool) Description() string {
	return "Read the contents of a file. Can optionally specify line range."
}

func (t *FSReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path relative to workspace root to read",
			},
			"start_line": map[string]interface{}{
				"type":        "integer",
				"description": "Starting line number (1-indexed, 0 for beginning)",
				"default":     0,
			},
			"end_line": map[string]interface{}{
				"type":        "integer",
				"description": "Ending line number (0 for end of file)",
				"default":     0,
			},
		},
		"required": []string{"path"},
	}
}

func (t *FSReadTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a fsReadArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	targetPath, err := resolvePath(t.root, a.Path)
	if err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	file, err := os.Open(targetPath)
	if err != nil {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}
	defer file.Close()

	var output strings.Builder
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	lineNum := 0
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return &ToolResult{Name: t.Name(), Error: ctx.Err(), Duration: time.Since(start).Seconds()}, ctx.Err()
		default:
		}

		lineNum++

		if a.StartLine > 0 && lineNum < a.StartLine {
			continue
		}
		if a.EndLine > 0 && lineNum > a.EndLine {
			break
		}

		fmt.Fprintf(&output, "%4d: %s\n", lineNum, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   output.String(),
		Duration: time.Since(start).Seconds(),
	}, nil
}

// FSWriteTool writes file contents
type FSWriteTool struct {
	root string
}

type fsWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *FSWriteTool) Name() string { return "fs_write" }

func (t *FSWriteTool) Description() string {
	return "Write content to a file. Creates parent directories if needed. Use fs_patch for large files."
}

func (t *FSWriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path relative to workspace root to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *FSWriteTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a fsWriteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	targetPath, err := resolvePath(t.root, a.Path)
	if err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	if err := os.WriteFile(targetPath, []byte(a.Content), 0644); err != nil {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	relPath, _ := filepath.Rel(t.root, targetPath)
	return &ToolResult{
		Name:     t.Name(),
		Output:   fmt.Sprintf("Written %d bytes to %s", len(a.Content), relPath),
		Duration: time.Since(start).Seconds(),
	}, nil
}

// FSPatchTool applies a unified diff to files
type FSPatchTool struct {
	root string
}

type fsPatchArgs struct {
	Diff string `json:"diff"`
}

func (t *FSPatchTool) Name() string { return "fs_patch" }

func (t *FSPatchTool) Description() string {
	return "Apply a unified diff patch to files. Preferred for modifying existing files."
}

func (t *FSPatchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"diff": map[string]interface{}{
				"type":        "string",
				"description": "Unified diff to apply",
			},
		},
		"required": []string{"diff"},
	}
}

func (t *FSPatchTool) Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	start := time.Now()

	var a fsPatchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return &ToolResult{Name: t.Name(), Error: err}, err
	}

	// Parse and apply the diff
	// This is a simplified implementation - a production version would use a proper diff library
	result, err := applyUnifiedDiff(t.root, a.Diff)
	if err != nil {
		return &ToolResult{Name: t.Name(), Error: err, Duration: time.Since(start).Seconds()}, err
	}

	return &ToolResult{
		Name:     t.Name(),
		Output:   result,
		Duration: time.Since(start).Seconds(),
	}, nil
}

// resolvePath resolves a path relative to root and validates it's within the root
func resolvePath(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		// Check if absolute path is within root
		cleanPath := filepath.Clean(path)
		cleanRoot := filepath.Clean(root)
		if !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) && cleanPath != cleanRoot {
			return "", fmt.Errorf("path traversal blocked: %s is outside workspace root", path)
		}
		return cleanPath, nil
	}

	// Join relative path with root
	joined := filepath.Join(root, path)
	cleanPath := filepath.Clean(joined)
	cleanRoot := filepath.Clean(root)

	// Verify the resolved path is within root
	if !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) && cleanPath != cleanRoot {
		return "", fmt.Errorf("path traversal blocked: %s resolves outside workspace root", path)
	}

	return cleanPath, nil
}

// applyUnifiedDiff applies a unified diff to files
func applyUnifiedDiff(root, diff string) (string, error) {
	// Parse diff header to find target file
	lines := strings.Split(diff, "\n")
	var targetFile string
	var hunks []string
	var currentHunk []string
	inHunk := false

	for _, line := range lines {
		if strings.HasPrefix(line, "--- ") {
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			// Extract filename (handle a/, b/ prefixes)
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				targetFile = strings.TrimPrefix(parts[1], "b/")
			}
			continue
		}
		if strings.HasPrefix(line, "@@") {
			if inHunk && len(currentHunk) > 0 {
				hunks = append(hunks, strings.Join(currentHunk, "\n"))
			}
			currentHunk = []string{line}
			inHunk = true
			continue
		}
		if inHunk {
			currentHunk = append(currentHunk, line)
		}
	}

	if len(currentHunk) > 0 {
		hunks = append(hunks, strings.Join(currentHunk, "\n"))
	}

	if targetFile == "" {
		return "", fmt.Errorf("could not parse target file from diff")
	}

	targetPath, err := resolvePath(root, targetFile)
	if err != nil {
		return "", err
	}

	// Read existing file
	content, err := os.ReadFile(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Apply hunks (simplified - a production version would use proper patch logic)
	result := string(content)
	for _, hunk := range hunks {
		result, err = applyHunk(result, hunk)
		if err != nil {
			return "", fmt.Errorf("failed to apply hunk: %w", err)
		}
	}

	// Write result
	if err := os.WriteFile(targetPath, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Applied patch to %s", targetFile), nil
}

// applyHunk applies a single hunk to content
func applyHunk(content, hunk string) (string, error) {
	lines := strings.Split(hunk, "\n")
	if len(lines) == 0 {
		return content, nil
	}

	// Parse @@ header
	header := lines[0]
	if !strings.HasPrefix(header, "@@") {
		return content, nil
	}

	contentLines := strings.Split(content, "\n")
	var result []string
	var contextBefore []string
	var additions []string
	var removals []string

	// Parse hunk lines
	for _, line := range lines[1:] {
		if len(line) == 0 {
			contextBefore = append(contextBefore, "")
			continue
		}

		switch line[0] {
		case ' ':
			contextBefore = append(contextBefore, line[1:])
		case '+':
			additions = append(additions, line[1:])
		case '-':
			removals = append(removals, line[1:])
		}
	}

	// Find location to apply hunk by matching context
	if len(contextBefore) > 0 || len(removals) > 0 {
		searchLines := removals
		if len(searchLines) == 0 {
			searchLines = contextBefore
		}

		found := false
		for i := 0; i <= len(contentLines)-len(searchLines); i++ {
			match := true
			for j, searchLine := range searchLines {
				if i+j >= len(contentLines) || contentLines[i+j] != searchLine {
					match = false
					break
				}
			}
			if match {
				// Apply changes at this location
				result = append(result, contentLines[:i]...)
				result = append(result, additions...)
				result = append(result, contentLines[i+len(removals):]...)
				found = true
				break
			}
		}

		if !found && len(removals) > 0 {
			return content, fmt.Errorf("could not find context to apply hunk")
		}
		if !found {
			// Just append additions if no removals
			result = append(contentLines, additions...)
		}
	} else {
		// No context, just append
		result = append(contentLines, additions...)
	}

	return strings.Join(result, "\n"), nil
}
