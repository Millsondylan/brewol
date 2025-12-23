// Package memory provides rolling memory management for long-running sessions.
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config holds memory manager configuration
type Config struct {
	WorkspaceRoot   string
	UpdateInterval  int    // Update memory every N cycles (default 5)
	MaxContextTurns int    // Max turns to keep in active context (default 10)
	SummaryModel    string // Model to use for summarization (empty = use main model)
}

// DefaultConfig returns default configuration
func DefaultConfig(workspaceRoot string) Config {
	return Config{
		WorkspaceRoot:   workspaceRoot,
		UpdateInterval:  5,
		MaxContextTurns: 10,
	}
}

// WorkingMemory represents the compact memory blob fed to the model
type WorkingMemory struct {
	// Project info
	ProjectType    string   `json:"project_type"`
	BuildCommand   string   `json:"build_command"`
	TestCommand    string   `json:"test_command"`
	KeyDirectories []string `json:"key_directories"`
	KeyModules     []string `json:"key_modules"`

	// Conventions
	Conventions []string `json:"conventions"`
	Constraints []string `json:"constraints"`

	// Current state
	CurrentBranch   string `json:"current_branch"`
	LastGoodCommit  string `json:"last_good_commit"`
	LastGoodCommand string `json:"last_good_command"`

	// Backlog summary
	BacklogSummary []string `json:"backlog_summary"`

	// Session metadata
	CycleCount   int       `json:"cycle_count"`
	LastUpdated  time.Time `json:"last_updated"`
	UpdateReason string    `json:"update_reason"`
}

// Manager manages rolling memory for agent sessions
type Manager struct {
	config            Config
	memory            WorkingMemory
	memoryFile        string
	transcriptFile    *os.File
	fullLogFile       *os.File
	cyclesSinceUpdate int
	mu                sync.RWMutex
}

// NewManager creates a new memory manager
func NewManager(cfg Config) (*Manager, error) {
	// Create memory directory
	memDir := filepath.Join(cfg.WorkspaceRoot, ".brewol", "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory: %w", err)
	}

	memoryFile := filepath.Join(memDir, "working_memory.json")

	// Create transcript file with timestamp
	timestamp := time.Now().Format("20060102-150405")
	transcriptPath := filepath.Join(memDir, fmt.Sprintf("transcript_%s.jsonl", timestamp))
	transcriptFile, err := os.Create(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create transcript file: %w", err)
	}

	// Create full log file
	fullLogPath := filepath.Join(memDir, fmt.Sprintf("full_log_%s.jsonl", timestamp))
	fullLogFile, err := os.Create(fullLogPath)
	if err != nil {
		transcriptFile.Close()
		return nil, fmt.Errorf("failed to create full log file: %w", err)
	}

	m := &Manager{
		config:         cfg,
		memoryFile:     memoryFile,
		transcriptFile: transcriptFile,
		fullLogFile:    fullLogFile,
	}

	// Try to load existing memory
	m.loadMemory()

	return m, nil
}

// loadMemory loads working memory from disk
func (m *Manager) loadMemory() {
	data, err := os.ReadFile(m.memoryFile)
	if err != nil {
		// Initialize empty memory
		m.memory = WorkingMemory{
			LastUpdated: time.Now(),
		}
		return
	}

	if err := json.Unmarshal(data, &m.memory); err != nil {
		m.memory = WorkingMemory{
			LastUpdated: time.Now(),
		}
	}
}

// saveMemory saves working memory to disk
func (m *Manager) saveMemory() error {
	data, err := json.MarshalIndent(m.memory, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.memoryFile, data, 0644)
}

// GetWorkingMemory returns the current working memory
func (m *Manager) GetWorkingMemory() WorkingMemory {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memory
}

// GetWorkingMemoryText returns the working memory as formatted text for the model
func (m *Manager) GetWorkingMemoryText() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.memory.ProjectType == "" && m.memory.BuildCommand == "" {
		return "" // No memory yet
	}

	var b strings.Builder
	b.WriteString("## WORKING MEMORY\n\n")

	if m.memory.ProjectType != "" {
		b.WriteString(fmt.Sprintf("**Project Type:** %s\n", m.memory.ProjectType))
	}
	if m.memory.BuildCommand != "" {
		b.WriteString(fmt.Sprintf("**Build Command:** `%s`\n", m.memory.BuildCommand))
	}
	if m.memory.TestCommand != "" {
		b.WriteString(fmt.Sprintf("**Test Command:** `%s`\n", m.memory.TestCommand))
	}

	if len(m.memory.KeyDirectories) > 0 {
		b.WriteString(fmt.Sprintf("**Key Directories:** %s\n", strings.Join(m.memory.KeyDirectories, ", ")))
	}
	if len(m.memory.KeyModules) > 0 {
		b.WriteString(fmt.Sprintf("**Key Modules:** %s\n", strings.Join(m.memory.KeyModules, ", ")))
	}

	if len(m.memory.Conventions) > 0 {
		b.WriteString("\n**Conventions:**\n")
		for _, c := range m.memory.Conventions {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}

	if len(m.memory.Constraints) > 0 {
		b.WriteString("\n**Constraints:**\n")
		for _, c := range m.memory.Constraints {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}

	if m.memory.CurrentBranch != "" {
		b.WriteString(fmt.Sprintf("\n**Current Branch:** %s\n", m.memory.CurrentBranch))
	}
	if m.memory.LastGoodCommit != "" {
		b.WriteString(fmt.Sprintf("**Last Good Commit:** %s\n", m.memory.LastGoodCommit))
	}
	if m.memory.LastGoodCommand != "" {
		b.WriteString(fmt.Sprintf("**Last Good Command:** `%s`\n", m.memory.LastGoodCommand))
	}

	if len(m.memory.BacklogSummary) > 0 {
		b.WriteString("\n**Active Backlog:**\n")
		for i, item := range m.memory.BacklogSummary {
			if i >= 5 {
				b.WriteString(fmt.Sprintf("- ... and %d more\n", len(m.memory.BacklogSummary)-5))
				break
			}
			b.WriteString(fmt.Sprintf("- %s\n", item))
		}
	}

	b.WriteString(fmt.Sprintf("\n*Memory updated: %s (cycle %d)*\n",
		m.memory.LastUpdated.Format("15:04:05"), m.memory.CycleCount))

	return b.String()
}

// SetProjectInfo sets project information in memory
func (m *Manager) SetProjectInfo(projectType, buildCmd, testCmd string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.memory.ProjectType = projectType
	m.memory.BuildCommand = buildCmd
	m.memory.TestCommand = testCmd
}

// SetKeyDirectories sets key directories
func (m *Manager) SetKeyDirectories(dirs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memory.KeyDirectories = dirs
}

// SetKeyModules sets key modules
func (m *Manager) SetKeyModules(modules []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memory.KeyModules = modules
}

// AddConvention adds a convention to memory
func (m *Manager) AddConvention(convention string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicates
	for _, c := range m.memory.Conventions {
		if c == convention {
			return
		}
	}
	m.memory.Conventions = append(m.memory.Conventions, convention)
}

// AddConstraint adds a constraint to memory
func (m *Manager) AddConstraint(constraint string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicates
	for _, c := range m.memory.Constraints {
		if c == constraint {
			return
		}
	}
	m.memory.Constraints = append(m.memory.Constraints, constraint)
}

// SetGitState sets git state in memory
func (m *Manager) SetGitState(branch, lastGoodCommit string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.memory.CurrentBranch = branch
	if lastGoodCommit != "" {
		m.memory.LastGoodCommit = lastGoodCommit
	}
}

// SetLastGoodCommand sets the last successful command
func (m *Manager) SetLastGoodCommand(cmd string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memory.LastGoodCommand = cmd
}

// SetBacklogSummary sets the backlog summary
func (m *Manager) SetBacklogSummary(items []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memory.BacklogSummary = items
}

// LogEntry represents a log entry for the full transcript
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// LogMessage logs a message to the full transcript
func (m *Manager) LogMessage(role, content string) error {
	entry := LogEntry{
		Timestamp: time.Now(),
		Type:      role,
		Content:   content,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, err = m.fullLogFile.Write(append(data, '\n'))
	return err
}

// LogToolCall logs a tool call to the transcript
func (m *Manager) LogToolCall(name, args, output string, exitCode int, duration float64) error {
	entry := LogEntry{
		Timestamp: time.Now(),
		Type:      "tool:" + name,
		Content:   output,
		Metadata: map[string]interface{}{
			"args":      args,
			"exit_code": exitCode,
			"duration":  duration,
		},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, err = m.fullLogFile.Write(append(data, '\n'))
	return err
}

// OnCycleComplete is called after each agent cycle
func (m *Manager) OnCycleComplete(cycleNum int) bool {
	m.mu.Lock()
	m.cyclesSinceUpdate++
	m.memory.CycleCount = cycleNum
	shouldUpdate := m.cyclesSinceUpdate >= m.config.UpdateInterval
	m.mu.Unlock()

	if shouldUpdate {
		m.TriggerUpdate("periodic")
	}

	return shouldUpdate
}

// OnCheckpoint is called after a successful checkpoint commit
func (m *Manager) OnCheckpoint(commitSHA string) {
	m.mu.Lock()
	m.memory.LastGoodCommit = commitSHA
	m.mu.Unlock()

	m.TriggerUpdate("checkpoint")
}

// OnSignificantFailure is called after a significant failure
func (m *Manager) OnSignificantFailure(reason string) {
	m.TriggerUpdate("failure: " + reason)
}

// TriggerUpdate triggers a memory update
func (m *Manager) TriggerUpdate(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.memory.UpdateReason = reason
	m.memory.LastUpdated = time.Now()
	m.cyclesSinceUpdate = 0

	// Save to disk
	m.saveMemory()
}

// Reset clears the working memory (keeps logs on disk)
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.memory = WorkingMemory{
		LastUpdated:  time.Now(),
		UpdateReason: "reset",
	}
	m.cyclesSinceUpdate = 0
	m.saveMemory()
}

// Close closes the memory manager
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	if m.transcriptFile != nil {
		if err := m.transcriptFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.fullLogFile != nil {
		if err := m.fullLogFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// Final save
	if err := m.saveMemory(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetMemoryDir returns the memory directory path
func (m *Manager) GetMemoryDir() string {
	return filepath.Dir(m.memoryFile)
}

// SummarizerPrompt returns the prompt for the summarizer
func SummarizerPrompt(recentActivity string) string {
	return fmt.Sprintf(`You are a memory summarizer. Given the recent activity, extract ONLY durable facts that should be remembered.

RECENT ACTIVITY:
%s

OUTPUT FORMAT (JSON):
{
  "project_type": "type if discovered",
  "build_command": "command if discovered",
  "test_command": "command if discovered",
  "key_directories": ["dir1", "dir2"],
  "key_modules": ["mod1", "mod2"],
  "conventions": ["convention1"],
  "constraints": ["constraint1"],
  "backlog_summary": ["item1", "item2"]
}

RULES:
- Only include fields with confirmed information
- Keep entries concise (max 50 chars each)
- Max 5 items per array
- Focus on durable truths, not temporary state`, recentActivity)
}

// BuildDeterministicSummary builds a fallback summary from tool logs and git state
func BuildDeterministicSummary(projectType, buildCmd, testCmd, branch string, dirtyFiles []string) WorkingMemory {
	mem := WorkingMemory{
		ProjectType:   projectType,
		BuildCommand:  buildCmd,
		TestCommand:   testCmd,
		CurrentBranch: branch,
		LastUpdated:   time.Now(),
		UpdateReason:  "deterministic-fallback",
	}

	// Add dirty files as backlog if any
	if len(dirtyFiles) > 0 {
		for i, f := range dirtyFiles {
			if i >= 5 {
				break
			}
			mem.BacklogSummary = append(mem.BacklogSummary, "Uncommitted: "+f)
		}
	}

	return mem
}
