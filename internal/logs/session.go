// Package logs provides session logging and transcript management.
package logs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session represents a logging session
type Session struct {
	ID          string
	StartTime   time.Time
	LogDir      string
	transcript  *os.File
	toolLog     *os.File
	thinkingLog *os.File
	mu          sync.Mutex
}

// Entry represents a log entry
type Entry struct {
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// NewSession creates a new logging session
func NewSession(baseDir string) (*Session, error) {
	now := time.Now()
	sessionID := now.Format("20060102-150405")
	logDir := filepath.Join(baseDir, ".brewol", "logs", sessionID)

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	transcript, err := os.Create(filepath.Join(logDir, "transcript.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to create transcript file: %w", err)
	}

	toolLog, err := os.Create(filepath.Join(logDir, "tools.jsonl"))
	if err != nil {
		transcript.Close()
		return nil, fmt.Errorf("failed to create tool log file: %w", err)
	}

	thinkingLog, err := os.Create(filepath.Join(logDir, "thinking.jsonl"))
	if err != nil {
		transcript.Close()
		toolLog.Close()
		return nil, fmt.Errorf("failed to create thinking log file: %w", err)
	}

	return &Session{
		ID:          sessionID,
		StartTime:   now,
		LogDir:      logDir,
		transcript:  transcript,
		toolLog:     toolLog,
		thinkingLog: thinkingLog,
	}, nil
}

// LogMessage logs a conversation message
func (s *Session) LogMessage(role, content string, metadata map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := Entry{
		Timestamp: time.Now(),
		Type:      role,
		Content:   content,
		Metadata:  metadata,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	_, err = s.transcript.Write(append(data, '\n'))
	return err
}

// LogThinking logs a thinking trace from the model
func (s *Session) LogThinking(cycleID int, thinking string, durationMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := Entry{
		Timestamp: time.Now(),
		Type:      "thinking",
		Content:   thinking,
		Metadata: map[string]interface{}{
			"cycle_id":    cycleID,
			"duration_ms": durationMs,
			"length":      len(thinking),
		},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	_, err = s.thinkingLog.Write(append(data, '\n'))
	return err
}

// LogToolCall logs a tool execution
func (s *Session) LogToolCall(name, args, output string, duration float64, exitCode int, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	metadata := map[string]interface{}{
		"args":      args,
		"duration":  duration,
		"exit_code": exitCode,
	}
	if err != nil {
		metadata["error"] = err.Error()
	}

	entry := Entry{
		Timestamp: time.Now(),
		Type:      "tool:" + name,
		Content:   output,
		Metadata:  metadata,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	_, err = s.toolLog.Write(append(data, '\n'))
	return err
}

// LogCheckpoint logs a checkpoint event
func (s *Session) LogCheckpoint(commitSHA, message string) error {
	return s.LogMessage("checkpoint", message, map[string]interface{}{
		"commit": commitSHA,
	})
}

// LogObjective logs an objective start/complete event
func (s *Session) LogObjective(objective, status string) error {
	return s.LogMessage("objective", objective, map[string]interface{}{
		"status": status,
	})
}

// SavePatch saves a patch file
func (s *Session) SavePatch(name, content string) error {
	patchDir := filepath.Join(s.LogDir, "patches")
	if err := os.MkdirAll(patchDir, 0755); err != nil {
		return err
	}

	patchFile := filepath.Join(patchDir, fmt.Sprintf("%s-%d.patch", name, time.Now().UnixNano()))
	return os.WriteFile(patchFile, []byte(content), 0644)
}

// Close closes the session
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	if s.transcript != nil {
		if err := s.transcript.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.toolLog != nil {
		if err := s.toolLog.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.thinkingLog != nil {
		if err := s.thinkingLog.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Path returns the log directory path
func (s *Session) Path() string {
	return s.LogDir
}

// GetDefaultLogDir returns the default log directory for a workspace
func GetDefaultLogDir(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".brewol", "logs")
}

// ListSessions returns a list of previous session IDs
func ListSessions(baseDir string) ([]string, error) {
	logDir := filepath.Join(baseDir, ".brewol", "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []string
	for _, entry := range entries {
		if entry.IsDir() {
			sessions = append(sessions, entry.Name())
		}
	}
	return sessions, nil
}

// ReadTranscript reads all entries from a session's transcript
func ReadTranscript(sessionDir string) ([]Entry, error) {
	transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")
	content, err := os.ReadFile(transcriptPath)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, line := range splitLines(string(content)) {
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
