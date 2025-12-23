package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompactToolOutputSmall(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	bm := NewBudgetManager(DefaultBudgetConfig())
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	// Small output should not be truncated
	output := &ToolOutput{
		Name:      "shell",
		Command:   "ls -la",
		ExitCode:  0,
		Output:    "file1.txt\nfile2.txt\nfile3.txt",
		Duration:  0.5,
		Timestamp: time.Now(),
	}

	result, err := c.CompactToolOutput(output)
	if err != nil {
		t.Fatalf("Failed to compact tool output: %v", err)
	}

	// Should contain the full output
	if !strings.Contains(result, "file1.txt") {
		t.Error("Result should contain full output")
	}
	if !strings.Contains(result, "file3.txt") {
		t.Error("Result should contain full output")
	}
}

func TestCompactToolOutputLarge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	cfg.MaxToolOutputLines = 10
	cfg.ToolOutputHeadLines = 5
	cfg.ToolOutputTailLines = 5
	bm := NewBudgetManager(DefaultBudgetConfig())
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	// Create large output (50 lines)
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("x", 80))
	}
	largeOutput := strings.Join(lines, "\n")

	output := &ToolOutput{
		Name:      "shell",
		Command:   "cat large_file.txt",
		ExitCode:  0,
		Output:    largeOutput,
		Duration:  1.0,
		Timestamp: time.Now(),
	}

	result, err := c.CompactToolOutput(output)
	if err != nil {
		t.Fatalf("Failed to compact tool output: %v", err)
	}

	// Should contain omission indicator
	if !strings.Contains(result, "lines omitted") {
		t.Error("Large output should contain omission indicator")
	}

	// Should have log path
	if !strings.Contains(result, "Full log:") {
		t.Error("Large output should have full log path")
	}

	// Check log file was created
	logFiles, _ := os.ReadDir(c.GetLogDir())
	if len(logFiles) == 0 {
		t.Error("Log file should have been created")
	}
}

func TestCompactTranscript(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	cfg.MaxTranscriptTurns = 3

	bm := NewBudgetManager(BudgetConfig{
		NumCtx:              8192,
		HighWatermarkRatio:  0.8,
		LowWatermarkRatio:   0.6,
		ReserveOutputTokens: 2048,
		MaxTranscriptTurns:  3,
	})
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	// Create transcript with many messages
	messages := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Message 1"},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Message 2"},
		{Role: "assistant", Content: "Response 2"},
		{Role: "user", Content: "Message 3"},
		{Role: "assistant", Content: "Response 3"},
		{Role: "user", Content: "Message 4"},
		{Role: "assistant", Content: "Response 4"},
		{Role: "user", Content: "Message 5"},
		{Role: "assistant", Content: "Response 5"},
	}

	compacted, summary := c.CompactTranscript(messages, true)

	// Should keep system message + last N turns (3 turns = 6 messages)
	expectedLen := 1 + (3 * 2) // system + 3 turns
	if len(compacted) != expectedLen {
		t.Errorf("Expected %d messages after compaction, got %d", expectedLen, len(compacted))
	}

	// First message should still be system
	if compacted[0].Role != "system" {
		t.Errorf("First message should be system, got %s", compacted[0].Role)
	}

	// Should have compaction summary
	if summary == "" {
		t.Error("Should have compaction summary")
	}

	if !strings.Contains(summary, "compacted") {
		t.Error("Summary should mention compaction")
	}
}

func TestCompactTranscriptNoCompactionNeeded(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	bm := NewBudgetManager(BudgetConfig{
		NumCtx:              8192,
		HighWatermarkRatio:  0.8,
		LowWatermarkRatio:   0.6,
		ReserveOutputTokens: 2048,
		MaxTranscriptTurns:  10, // Large enough
	})
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	messages := []Message{
		{Role: "system", Content: "System"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}

	compacted, summary := c.CompactTranscript(messages, true)

	// No compaction needed
	if len(compacted) != len(messages) {
		t.Errorf("Expected %d messages (no compaction), got %d", len(messages), len(compacted))
	}

	if summary != "" {
		t.Error("Should not have compaction summary when no compaction needed")
	}
}

func TestBuildRollingMemory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	bm := NewBudgetManager(DefaultBudgetConfig())
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	update := RollingMemoryUpdate{
		GitBranch:        "feature/test",
		CurrentObjective: "Implement feature X",
		TaskBrief:        "Current: Task 1\nPending: 3 tasks",
		Timestamp:        time.Now(),
	}

	memory := c.BuildRollingMemory(update)

	if !strings.Contains(memory, "ROLLING MEMORY") {
		t.Error("Memory should contain header")
	}
	if !strings.Contains(memory, "feature/test") {
		t.Error("Memory should contain git branch")
	}
	if !strings.Contains(memory, "Implement feature X") {
		t.Error("Memory should contain objective")
	}
}

func TestCompactMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	bm := NewBudgetManager(DefaultBudgetConfig())
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	// Short message should not be truncated
	shortMsg := Message{Role: "user", Content: "Hello world"}
	result := c.CompactMessage(shortMsg, 100)
	if result.Content != shortMsg.Content {
		t.Error("Short message should not be truncated")
	}

	// Long message should be truncated
	longMsg := Message{Role: "user", Content: strings.Repeat("x", 200)}
	result = c.CompactMessage(longMsg, 100)
	if len(result.Content) > 100 {
		t.Errorf("Long message should be truncated to 100, got %d", len(result.Content))
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world", 2},
		{strings.Repeat("x", 100), 25},
		{strings.Repeat("x", 400), 100},
	}

	for _, tt := range tests {
		result := EstimateTokens(tt.input)
		if result != tt.expected {
			t.Errorf("EstimateTokens(%q) = %d, expected %d", tt.input[:min(len(tt.input), 20)], result, tt.expected)
		}
	}
}

func TestCleanOldLogs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	bm := NewBudgetManager(DefaultBudgetConfig())
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	// Create some log files
	logDir := c.GetLogDir()
	for i := 0; i < 5; i++ {
		filepath := filepath.Join(logDir, strings.Repeat("x", 10)+".log")
		os.WriteFile(filepath, []byte("test"), 0644)
	}

	// Clean with very long duration (should not remove anything)
	removed, err := c.CleanOldLogs(24 * time.Hour)
	if err != nil {
		t.Fatalf("Failed to clean logs: %v", err)
	}
	if removed != 0 {
		t.Errorf("Expected 0 removed (logs too new), got %d", removed)
	}
}

func TestToolOutputFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compactor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultCompactorConfig(tmpDir)
	cfg.EnableLogStorage = false // Disable for this test
	bm := NewBudgetManager(DefaultBudgetConfig())
	c, err := NewCompactor(cfg, bm)
	if err != nil {
		t.Fatalf("Failed to create compactor: %v", err)
	}

	output := &ToolOutput{
		Name:      "git_status",
		Command:   "git status",
		ExitCode:  0,
		Output:    "On branch main\nnothing to commit",
		Duration:  0.25,
		Timestamp: time.Now(),
	}

	result, err := c.CompactToolOutput(output)
	if err != nil {
		t.Fatalf("Failed to compact: %v", err)
	}

	// Check formatting
	if !strings.Contains(result, "### Tool: git_status") {
		t.Error("Should contain tool name header")
	}
	if !strings.Contains(result, "Command: `git status`") {
		t.Error("Should contain command")
	}
	if !strings.Contains(result, "Exit Code: 0") {
		t.Error("Should contain exit code")
	}
	if !strings.Contains(result, "```") {
		t.Error("Should contain code fence")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
