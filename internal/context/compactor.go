package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CompactorConfig holds configuration for the compactor
type CompactorConfig struct {
	WorkspaceRoot       string
	MaxToolOutputLines  int    // Max lines to keep from tool output (head + tail)
	ToolOutputHeadLines int    // Lines to keep from start of tool output
	ToolOutputTailLines int    // Lines to keep from end of tool output
	MaxTranscriptTurns  int    // Max conversation turns to keep
	EnableLogStorage    bool   // Whether to store full outputs to disk
	LogDir              string // Directory for storing full logs
}

// DefaultCompactorConfig returns default configuration
func DefaultCompactorConfig(workspaceRoot string) CompactorConfig {
	return CompactorConfig{
		WorkspaceRoot:       workspaceRoot,
		MaxToolOutputLines:  20,
		ToolOutputHeadLines: 10,
		ToolOutputTailLines: 10,
		MaxTranscriptTurns:  5,
		EnableLogStorage:    true,
		LogDir:              filepath.Join(workspaceRoot, ".brewol", "logs", "tool_outputs"),
	}
}

// Message represents a conversation message (simplified version)
type Message struct {
	Role    string
	Content string
}

// ToolOutput represents a tool execution output
type ToolOutput struct {
	Name      string
	Command   string
	ExitCode  int
	Output    string
	Error     string
	Duration  float64
	Timestamp time.Time
	LogPath   string // Path to full log on disk
}

// Compactor handles compaction of tool outputs and conversation history
type Compactor struct {
	config        CompactorConfig
	budgetManager *BudgetManager
	logCounter    int64
	mu            sync.Mutex
}

// NewCompactor creates a new compactor
func NewCompactor(cfg CompactorConfig, budget *BudgetManager) (*Compactor, error) {
	if cfg.EnableLogStorage {
		if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	return &Compactor{
		config:        cfg,
		budgetManager: budget,
	}, nil
}

// CompactToolOutput compacts a tool output, storing full output to disk if needed
func (c *Compactor) CompactToolOutput(output *ToolOutput) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	lines := strings.Split(output.Output, "\n")
	totalLines := len(lines)

	// If output is small enough, return as-is
	maxLines := c.config.MaxToolOutputLines
	if totalLines <= maxLines {
		return c.formatToolOutput(output, output.Output, ""), nil
	}

	// Store full output to disk
	var logPath string
	if c.config.EnableLogStorage {
		c.logCounter++
		filename := fmt.Sprintf("%s_%d_%d.log", output.Name, output.Timestamp.Unix(), c.logCounter)
		logPath = filepath.Join(c.config.LogDir, filename)

		if err := os.WriteFile(logPath, []byte(output.Output), 0644); err != nil {
			// Log error but continue with compaction
			logPath = fmt.Sprintf("(failed to save: %v)", err)
		}
		output.LogPath = logPath
	}

	// Keep head + tail lines
	headLines := c.config.ToolOutputHeadLines
	tailLines := c.config.ToolOutputTailLines

	if headLines+tailLines >= totalLines {
		// If head + tail would be everything, just use maxLines
		if totalLines > maxLines {
			headLines = maxLines / 2
			tailLines = maxLines - headLines
		} else {
			return c.formatToolOutput(output, output.Output, logPath), nil
		}
	}

	// Build compacted output
	var compacted strings.Builder

	// Head lines
	for i := 0; i < headLines && i < totalLines; i++ {
		compacted.WriteString(lines[i])
		compacted.WriteString("\n")
	}

	// Omission indicator
	omitted := totalLines - headLines - tailLines
	if omitted > 0 {
		compacted.WriteString(fmt.Sprintf("\n... [%d lines omitted] ...\n\n", omitted))
	}

	// Tail lines
	startTail := totalLines - tailLines
	if startTail < headLines {
		startTail = headLines
	}
	for i := startTail; i < totalLines; i++ {
		compacted.WriteString(lines[i])
		if i < totalLines-1 {
			compacted.WriteString("\n")
		}
	}

	return c.formatToolOutput(output, compacted.String(), logPath), nil
}

// formatToolOutput formats a tool output for model context
func (c *Compactor) formatToolOutput(output *ToolOutput, content string, logPath string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("### Tool: %s\n", output.Name))
	if output.Command != "" {
		sb.WriteString(fmt.Sprintf("Command: `%s`\n", truncateStr(output.Command, 100)))
	}
	sb.WriteString(fmt.Sprintf("Exit Code: %d | Duration: %.2fs\n", output.ExitCode, output.Duration))

	if output.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", truncateStr(output.Error, 200)))
	}

	sb.WriteString("```\n")
	sb.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n")

	if logPath != "" {
		sb.WriteString(fmt.Sprintf("Full log: %s\n", logPath))
	}

	return sb.String()
}

// CompactTranscript compacts conversation transcript, keeping only recent turns
func (c *Compactor) CompactTranscript(messages []Message, keepSystemMsg bool) ([]Message, string) {
	if len(messages) == 0 {
		return messages, ""
	}

	maxTurns := c.config.MaxTranscriptTurns
	if c.budgetManager != nil {
		maxTurns = c.budgetManager.GetMaxTranscriptTurns()
	}

	// Calculate how many messages to keep
	// A "turn" is typically user + assistant
	maxMessages := maxTurns * 2

	startIdx := 0
	if keepSystemMsg && len(messages) > 0 && messages[0].Role == "system" {
		startIdx = 1
		maxMessages++ // Account for system message
	}

	if len(messages) <= maxMessages {
		return messages, "" // No compaction needed
	}

	// Build summary of removed messages
	var summary strings.Builder
	removedCount := len(messages) - maxMessages
	summary.WriteString(fmt.Sprintf("[Transcript compacted: %d earlier messages removed]\n", removedCount))

	// Extract key points from removed messages
	for i := startIdx; i < len(messages)-maxMessages+startIdx; i++ {
		msg := messages[i]
		if msg.Role == "assistant" {
			// Try to extract key actions from assistant messages
			if action := extractKeyAction(msg.Content); action != "" {
				summary.WriteString(fmt.Sprintf("- %s\n", action))
			}
		}
	}

	// Keep system message (if any) + last N messages
	compacted := make([]Message, 0, maxMessages)
	if keepSystemMsg && startIdx == 1 {
		compacted = append(compacted, messages[0])
	}
	compacted = append(compacted, messages[len(messages)-maxMessages+startIdx:]...)

	return compacted, summary.String()
}

// extractKeyAction extracts a key action summary from an assistant message
func extractKeyAction(content string) string {
	// Look for common patterns
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for action indicators
		if strings.HasPrefix(line, "Objective:") ||
			strings.HasPrefix(line, "Goal:") ||
			strings.HasPrefix(line, "RUN:") ||
			strings.HasPrefix(line, "EXECUTING:") {
			return truncateStr(line, 80)
		}
	}
	return ""
}

// RollingMemoryUpdate represents an update to rolling memory
type RollingMemoryUpdate struct {
	GitBranch         string
	GitDiff           string
	LastTestCommand   string
	LastTestResult    string
	CurrentObjective  string
	TaskBrief         string
	CompactionSummary string
	Timestamp         time.Time
}

// BuildRollingMemory builds a rolling memory text from current state
func (c *Compactor) BuildRollingMemory(update RollingMemoryUpdate) string {
	var sb strings.Builder

	sb.WriteString("## ROLLING MEMORY\n\n")

	if update.CurrentObjective != "" {
		sb.WriteString(fmt.Sprintf("**Current Objective:** %s\n\n", update.CurrentObjective))
	}

	if update.GitBranch != "" {
		sb.WriteString(fmt.Sprintf("**Git Branch:** %s\n", update.GitBranch))
	}

	if update.GitDiff != "" {
		sb.WriteString("**Uncommitted Changes:**\n```diff\n")
		sb.WriteString(truncateStr(update.GitDiff, 500))
		sb.WriteString("\n```\n\n")
	}

	if update.LastTestCommand != "" {
		sb.WriteString(fmt.Sprintf("**Last Test:** `%s`\n", update.LastTestCommand))
		if update.LastTestResult != "" {
			sb.WriteString(fmt.Sprintf("**Result:** %s\n", truncateStr(update.LastTestResult, 100)))
		}
		sb.WriteString("\n")
	}

	if update.TaskBrief != "" {
		sb.WriteString(update.TaskBrief)
		sb.WriteString("\n")
	}

	if update.CompactionSummary != "" {
		sb.WriteString("**Compaction Summary:**\n")
		sb.WriteString(update.CompactionSummary)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\n*Memory updated: %s*\n", update.Timestamp.Format("15:04:05")))

	return sb.String()
}

// CompactMessage compacts a single message if it's too long
func (c *Compactor) CompactMessage(msg Message, maxLength int) Message {
	if len(msg.Content) <= maxLength {
		return msg
	}

	// For assistant messages, try to keep structure
	if msg.Role == "assistant" {
		content := compactAssistantMessage(msg.Content, maxLength)
		return Message{Role: msg.Role, Content: content}
	}

	// For other messages, just truncate
	return Message{
		Role:    msg.Role,
		Content: truncateStr(msg.Content, maxLength),
	}
}

// compactAssistantMessage compacts an assistant message while preserving key sections
func compactAssistantMessage(content string, maxLength int) string {
	lines := strings.Split(content, "\n")

	// Prioritize sections
	var priorityLines []string
	var otherLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "Objective:") ||
			strings.HasPrefix(trimmed, "Goal:") ||
			strings.HasPrefix(trimmed, "RUN:") ||
			strings.HasPrefix(trimmed, "```") {
			priorityLines = append(priorityLines, line)
		} else if trimmed != "" {
			otherLines = append(otherLines, line)
		}
	}

	// Build result with priority lines first
	var result strings.Builder
	remaining := maxLength

	for _, line := range priorityLines {
		if remaining <= 0 {
			break
		}
		if len(line)+1 <= remaining {
			result.WriteString(line)
			result.WriteString("\n")
			remaining -= len(line) + 1
		}
	}

	// Add other lines if space remains
	for _, line := range otherLines {
		if remaining <= 0 {
			break
		}
		if len(line)+1 <= remaining {
			result.WriteString(line)
			result.WriteString("\n")
			remaining -= len(line) + 1
		}
	}

	output := result.String()
	if len(output) < len(content) {
		output += "\n[Content truncated]"
	}

	return output
}

// EstimateTokens provides a rough token estimate for a string
func EstimateTokens(s string) int {
	// Rough estimate: ~4 characters per token
	return len(s) / 4
}

// GetLogDir returns the log directory path
func (c *Compactor) GetLogDir() string {
	return c.config.LogDir
}

// CleanOldLogs removes log files older than the specified duration
func (c *Compactor) CleanOldLogs(maxAge time.Duration) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	entries, err := os.ReadDir(c.config.LogDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(c.config.LogDir, entry.Name())
			if err := os.Remove(path); err == nil {
				removed++
			}
		}
	}

	return removed, nil
}
