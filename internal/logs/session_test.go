package logs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewSession(t *testing.T) {
	tempDir := t.TempDir()

	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Close()

	// Check session ID format (YYYYMMDD-HHMMSS)
	if len(s.ID) != 15 {
		t.Errorf("expected session ID length 15, got %d (%s)", len(s.ID), s.ID)
	}

	// Check log directory was created
	if _, err := os.Stat(s.LogDir); os.IsNotExist(err) {
		t.Error("log directory was not created")
	}

	// Check log files were created
	transcriptPath := filepath.Join(s.LogDir, "transcript.jsonl")
	if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
		t.Error("transcript.jsonl was not created")
	}

	toolsPath := filepath.Join(s.LogDir, "tools.jsonl")
	if _, err := os.Stat(toolsPath); os.IsNotExist(err) {
		t.Error("tools.jsonl was not created")
	}
}

func TestSession_LogMessage(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Close()

	// Log a message
	err = s.LogMessage("user", "Hello, world!", map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatalf("failed to log message: %v", err)
	}

	// Close and read the transcript
	s.Close()

	content, err := os.ReadFile(filepath.Join(s.LogDir, "transcript.jsonl"))
	if err != nil {
		t.Fatalf("failed to read transcript: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal(content[:len(content)-1], &entry); err != nil { // -1 to remove newline
		t.Fatalf("failed to unmarshal entry: %v", err)
	}

	if entry.Type != "user" {
		t.Errorf("expected type user, got %s", entry.Type)
	}
	if entry.Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %s", entry.Content)
	}
	if entry.Metadata["key"] != "value" {
		t.Errorf("expected metadata key=value, got %v", entry.Metadata)
	}
}

func TestSession_LogToolCall(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Close()

	// Log a tool call
	err = s.LogToolCall("exec", `{"command": "ls"}`, "file1\nfile2", 0.5, 0, nil)
	if err != nil {
		t.Fatalf("failed to log tool call: %v", err)
	}

	// Log a tool call with error
	err = s.LogToolCall("exec", `{"command": "fail"}`, "", 0.1, 1, errors.New("command failed"))
	if err != nil {
		t.Fatalf("failed to log tool call with error: %v", err)
	}

	// Close and read the tool log
	s.Close()

	content, err := os.ReadFile(filepath.Join(s.LogDir, "tools.jsonl"))
	if err != nil {
		t.Fatalf("failed to read tool log: %v", err)
	}

	lines := splitLines(string(content))
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 log lines, got %d", len(lines))
	}

	// Check first entry
	var entry1 Entry
	if err := json.Unmarshal([]byte(lines[0]), &entry1); err != nil {
		t.Fatalf("failed to unmarshal first entry: %v", err)
	}
	if entry1.Type != "tool:exec" {
		t.Errorf("expected type tool:exec, got %s", entry1.Type)
	}
	if entry1.Content != "file1\nfile2" {
		t.Errorf("expected output 'file1\\nfile2', got %s", entry1.Content)
	}

	// Check second entry has error
	var entry2 Entry
	if err := json.Unmarshal([]byte(lines[1]), &entry2); err != nil {
		t.Fatalf("failed to unmarshal second entry: %v", err)
	}
	if entry2.Metadata["error"] != "command failed" {
		t.Errorf("expected error 'command failed', got %v", entry2.Metadata["error"])
	}
}

func TestSession_LogCheckpoint(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Close()

	err = s.LogCheckpoint("abc123", "Test checkpoint")
	if err != nil {
		t.Fatalf("failed to log checkpoint: %v", err)
	}

	s.Close()

	content, err := os.ReadFile(filepath.Join(s.LogDir, "transcript.jsonl"))
	if err != nil {
		t.Fatalf("failed to read transcript: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal(content[:len(content)-1], &entry); err != nil {
		t.Fatalf("failed to unmarshal entry: %v", err)
	}

	if entry.Type != "checkpoint" {
		t.Errorf("expected type checkpoint, got %s", entry.Type)
	}
	if entry.Content != "Test checkpoint" {
		t.Errorf("expected content 'Test checkpoint', got %s", entry.Content)
	}
	if entry.Metadata["commit"] != "abc123" {
		t.Errorf("expected commit abc123, got %v", entry.Metadata["commit"])
	}
}

func TestSession_LogObjective(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Close()

	err = s.LogObjective("Fix all tests", "started")
	if err != nil {
		t.Fatalf("failed to log objective: %v", err)
	}

	s.Close()

	content, err := os.ReadFile(filepath.Join(s.LogDir, "transcript.jsonl"))
	if err != nil {
		t.Fatalf("failed to read transcript: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal(content[:len(content)-1], &entry); err != nil {
		t.Fatalf("failed to unmarshal entry: %v", err)
	}

	if entry.Type != "objective" {
		t.Errorf("expected type objective, got %s", entry.Type)
	}
	if entry.Content != "Fix all tests" {
		t.Errorf("expected content 'Fix all tests', got %s", entry.Content)
	}
	if entry.Metadata["status"] != "started" {
		t.Errorf("expected status started, got %v", entry.Metadata["status"])
	}
}

func TestSession_SavePatch(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Close()

	patchContent := `--- a/file.txt
+++ b/file.txt
@@ -1 +1 @@
-old
+new`

	err = s.SavePatch("test-patch", patchContent)
	if err != nil {
		t.Fatalf("failed to save patch: %v", err)
	}

	// Check patches directory was created
	patchDir := filepath.Join(s.LogDir, "patches")
	entries, err := os.ReadDir(patchDir)
	if err != nil {
		t.Fatalf("failed to read patches directory: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 patch file, got %d", len(entries))
	}

	// Read the patch content
	patchPath := filepath.Join(patchDir, entries[0].Name())
	content, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("failed to read patch file: %v", err)
	}

	if string(content) != patchContent {
		t.Errorf("patch content mismatch")
	}
}

func TestSession_Path(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Close()

	if s.Path() != s.LogDir {
		t.Errorf("Path() should return LogDir")
	}
}

func TestGetDefaultLogDir(t *testing.T) {
	result := GetDefaultLogDir("/workspace")
	expected := filepath.Join("/workspace", ".brewol", "logs")
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestListSessions(t *testing.T) {
	tempDir := t.TempDir()

	// No sessions yet
	sessions, err := ListSessions(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}

	// Create some sessions
	s1, _ := NewSession(tempDir)
	s1.Close()

	sessions, err = ListSessions(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}

func TestReadTranscript(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewSession(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Log some messages
	s.LogMessage("user", "Hello", nil)
	s.LogMessage("assistant", "World", nil)
	s.Close()

	// Read the transcript
	entries, err := ReadTranscript(s.LogDir)
	if err != nil {
		t.Fatalf("failed to read transcript: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Type != "user" || entries[0].Content != "Hello" {
		t.Errorf("first entry mismatch: %+v", entries[0])
	}
	if entries[1].Type != "assistant" || entries[1].Content != "World" {
		t.Errorf("second entry mismatch: %+v", entries[1])
	}
}

func TestReadTranscript_NotFound(t *testing.T) {
	_, err := ReadTranscript("/nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\nb\n", []string{"a", "b"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"\n\n", []string{"", ""}},
	}

	for _, tt := range tests {
		result := splitLines(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitLines(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}
