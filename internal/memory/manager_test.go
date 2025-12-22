package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/workspace")

	if cfg.WorkspaceRoot != "/workspace" {
		t.Errorf("expected WorkspaceRoot /workspace, got %s", cfg.WorkspaceRoot)
	}
	if cfg.UpdateInterval != 5 {
		t.Errorf("expected UpdateInterval 5, got %d", cfg.UpdateInterval)
	}
	if cfg.MaxContextTurns != 10 {
		t.Errorf("expected MaxContextTurns 10, got %d", cfg.MaxContextTurns)
	}
}

func TestNewManager(t *testing.T) {
	tempDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot:   tempDir,
		UpdateInterval:  5,
		MaxContextTurns: 10,
	}

	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Check directory was created
	memDir := filepath.Join(tempDir, ".brewol", "memory")
	if _, err := os.Stat(memDir); os.IsNotExist(err) {
		t.Error("memory directory was not created")
	}

	// Check files were created
	if m.memoryFile == "" {
		t.Error("memoryFile should be set")
	}
	if m.transcriptFile == nil {
		t.Error("transcriptFile should be set")
	}
	if m.fullLogFile == nil {
		t.Error("fullLogFile should be set")
	}
}

func TestManager_SetProjectInfo(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.SetProjectInfo("go", "go build ./...", "go test ./...")

	mem := m.GetWorkingMemory()
	if mem.ProjectType != "go" {
		t.Errorf("expected ProjectType go, got %s", mem.ProjectType)
	}
	if mem.BuildCommand != "go build ./..." {
		t.Errorf("expected BuildCommand, got %s", mem.BuildCommand)
	}
	if mem.TestCommand != "go test ./..." {
		t.Errorf("expected TestCommand, got %s", mem.TestCommand)
	}
}

func TestManager_SetKeyDirectories(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	dirs := []string{"cmd/", "internal/", "pkg/"}
	m.SetKeyDirectories(dirs)

	mem := m.GetWorkingMemory()
	if len(mem.KeyDirectories) != 3 {
		t.Errorf("expected 3 key directories, got %d", len(mem.KeyDirectories))
	}
}

func TestManager_SetKeyModules(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	modules := []string{"engine", "tools", "tui"}
	m.SetKeyModules(modules)

	mem := m.GetWorkingMemory()
	if len(mem.KeyModules) != 3 {
		t.Errorf("expected 3 key modules, got %d", len(mem.KeyModules))
	}
}

func TestManager_AddConvention(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.AddConvention("Use gofmt for formatting")
	m.AddConvention("Tests must pass before commit")
	m.AddConvention("Use gofmt for formatting") // duplicate

	mem := m.GetWorkingMemory()
	if len(mem.Conventions) != 2 {
		t.Errorf("expected 2 conventions (no duplicates), got %d", len(mem.Conventions))
	}
}

func TestManager_AddConstraint(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.AddConstraint("No external dependencies")
	m.AddConstraint("Must support Go 1.20+")
	m.AddConstraint("No external dependencies") // duplicate

	mem := m.GetWorkingMemory()
	if len(mem.Constraints) != 2 {
		t.Errorf("expected 2 constraints (no duplicates), got %d", len(mem.Constraints))
	}
}

func TestManager_SetGitState(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.SetGitState("feature/test", "abc123")

	mem := m.GetWorkingMemory()
	if mem.CurrentBranch != "feature/test" {
		t.Errorf("expected branch feature/test, got %s", mem.CurrentBranch)
	}
	if mem.LastGoodCommit != "abc123" {
		t.Errorf("expected commit abc123, got %s", mem.LastGoodCommit)
	}

	// Update with empty commit should not overwrite
	m.SetGitState("main", "")
	mem = m.GetWorkingMemory()
	if mem.CurrentBranch != "main" {
		t.Errorf("expected branch main, got %s", mem.CurrentBranch)
	}
	if mem.LastGoodCommit != "abc123" {
		t.Errorf("expected commit to remain abc123, got %s", mem.LastGoodCommit)
	}
}

func TestManager_SetLastGoodCommand(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.SetLastGoodCommand("go test ./...")

	mem := m.GetWorkingMemory()
	if mem.LastGoodCommand != "go test ./..." {
		t.Errorf("expected last good command, got %s", mem.LastGoodCommand)
	}
}

func TestManager_SetBacklogSummary(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	items := []string{"Fix bug #1", "Add feature X", "Write tests"}
	m.SetBacklogSummary(items)

	mem := m.GetWorkingMemory()
	if len(mem.BacklogSummary) != 3 {
		t.Errorf("expected 3 backlog items, got %d", len(mem.BacklogSummary))
	}
}

func TestManager_GetWorkingMemoryText_Empty(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	text := m.GetWorkingMemoryText()
	if text != "" {
		t.Error("expected empty text for empty memory")
	}
}

func TestManager_GetWorkingMemoryText_WithData(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.SetProjectInfo("go", "go build ./...", "go test ./...")
	m.SetKeyDirectories([]string{"cmd/", "internal/"})
	m.SetKeyModules([]string{"engine", "tools"})
	m.AddConvention("Use gofmt")
	m.AddConstraint("Go 1.20+")
	m.SetGitState("main", "abc123")
	m.SetLastGoodCommand("go test ./...")
	m.SetBacklogSummary([]string{"Item 1", "Item 2", "Item 3", "Item 4", "Item 5", "Item 6"})

	text := m.GetWorkingMemoryText()

	// Check key content
	if !strings.Contains(text, "WORKING MEMORY") {
		t.Error("expected WORKING MEMORY header")
	}
	if !strings.Contains(text, "go") {
		t.Error("expected project type")
	}
	if !strings.Contains(text, "go build ./...") {
		t.Error("expected build command")
	}
	if !strings.Contains(text, "go test ./...") {
		t.Error("expected test command")
	}
	if !strings.Contains(text, "cmd/") {
		t.Error("expected key directories")
	}
	if !strings.Contains(text, "engine") {
		t.Error("expected key modules")
	}
	if !strings.Contains(text, "Use gofmt") {
		t.Error("expected convention")
	}
	if !strings.Contains(text, "Go 1.20+") {
		t.Error("expected constraint")
	}
	if !strings.Contains(text, "main") {
		t.Error("expected branch")
	}
	if !strings.Contains(text, "abc123") {
		t.Error("expected commit")
	}
	if !strings.Contains(text, "... and 1 more") {
		t.Error("expected truncation indicator for backlog")
	}
}

func TestManager_LogMessage(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = m.LogMessage("user", "Hello, world!")
	if err != nil {
		t.Fatalf("failed to log message: %v", err)
	}

	err = m.LogMessage("assistant", "Hi there!")
	if err != nil {
		t.Fatalf("failed to log message: %v", err)
	}

	m.Close()

	// Verify log file contents
	memDir := m.GetMemoryDir()
	entries, _ := os.ReadDir(memDir)
	var fullLogPath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "full_log_") {
			fullLogPath = filepath.Join(memDir, e.Name())
			break
		}
	}

	if fullLogPath == "" {
		t.Fatal("full log file not found")
	}

	content, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 log lines, got %d", len(lines))
	}

	// Parse first entry
	var entry LogEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry.Type != "user" || entry.Content != "Hello, world!" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

func TestManager_LogToolCall(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = m.LogToolCall("exec", `{"command": "ls"}`, "file1\nfile2", 0, 0.5)
	if err != nil {
		t.Fatalf("failed to log tool call: %v", err)
	}

	m.Close()

	// Verify metadata
	memDir := m.GetMemoryDir()
	entries, _ := os.ReadDir(memDir)
	var fullLogPath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "full_log_") {
			fullLogPath = filepath.Join(memDir, e.Name())
			break
		}
	}

	content, _ := os.ReadFile(fullLogPath)
	var entry LogEntry
	json.Unmarshal([]byte(strings.TrimSpace(string(content))), &entry)

	if entry.Type != "tool:exec" {
		t.Errorf("expected type tool:exec, got %s", entry.Type)
	}
	if entry.Metadata["exit_code"].(float64) != 0 {
		t.Errorf("expected exit_code 0")
	}
}

func TestManager_OnCycleComplete(t *testing.T) {
	tempDir := t.TempDir()
	cfg := Config{
		WorkspaceRoot:   tempDir,
		UpdateInterval:  3, // Update every 3 cycles
		MaxContextTurns: 10,
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// First 2 cycles should not trigger update
	if m.OnCycleComplete(1) {
		t.Error("should not update on cycle 1")
	}
	if m.OnCycleComplete(2) {
		t.Error("should not update on cycle 2")
	}
	// Third cycle should trigger update
	if !m.OnCycleComplete(3) {
		t.Error("should update on cycle 3")
	}

	mem := m.GetWorkingMemory()
	if mem.CycleCount != 3 {
		t.Errorf("expected cycle count 3, got %d", mem.CycleCount)
	}
	if mem.UpdateReason != "periodic" {
		t.Errorf("expected update reason 'periodic', got %s", mem.UpdateReason)
	}
}

func TestManager_OnCheckpoint(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.OnCheckpoint("abc123def")

	mem := m.GetWorkingMemory()
	if mem.LastGoodCommit != "abc123def" {
		t.Errorf("expected commit abc123def, got %s", mem.LastGoodCommit)
	}
	if mem.UpdateReason != "checkpoint" {
		t.Errorf("expected update reason 'checkpoint', got %s", mem.UpdateReason)
	}
}

func TestManager_OnSignificantFailure(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.OnSignificantFailure("test failed")

	mem := m.GetWorkingMemory()
	if mem.UpdateReason != "failure: test failed" {
		t.Errorf("expected failure reason, got %s", mem.UpdateReason)
	}
}

func TestManager_TriggerUpdate(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	beforeUpdate := time.Now()
	time.Sleep(10 * time.Millisecond)

	m.TriggerUpdate("manual")

	mem := m.GetWorkingMemory()
	if mem.UpdateReason != "manual" {
		t.Errorf("expected reason 'manual', got %s", mem.UpdateReason)
	}
	if mem.LastUpdated.Before(beforeUpdate) {
		t.Error("LastUpdated should be after trigger")
	}
}

func TestManager_Reset(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Set some data
	m.SetProjectInfo("go", "go build", "go test")
	m.AddConvention("test")

	// Reset
	m.Reset()

	mem := m.GetWorkingMemory()
	if mem.ProjectType != "" {
		t.Error("expected empty project type after reset")
	}
	if len(mem.Conventions) != 0 {
		t.Error("expected empty conventions after reset")
	}
	if mem.UpdateReason != "reset" {
		t.Errorf("expected reason 'reset', got %s", mem.UpdateReason)
	}
}

func TestManager_PersistMemory(t *testing.T) {
	tempDir := t.TempDir()

	// First manager
	m1, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m1.SetProjectInfo("go", "go build ./...", "go test ./...")
	m1.AddConvention("Use gofmt")
	m1.TriggerUpdate("save")
	m1.Close()

	// Second manager should load persisted memory
	m2, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m2.Close()

	mem := m2.GetWorkingMemory()
	if mem.ProjectType != "go" {
		t.Errorf("expected persisted project type 'go', got %s", mem.ProjectType)
	}
	if mem.BuildCommand != "go build ./..." {
		t.Errorf("expected persisted build command")
	}
	if len(mem.Conventions) != 1 || mem.Conventions[0] != "Use gofmt" {
		t.Errorf("expected persisted convention")
	}
}

func TestManager_GetMemoryDir(t *testing.T) {
	tempDir := t.TempDir()
	m, err := NewManager(DefaultConfig(tempDir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	dir := m.GetMemoryDir()
	expected := filepath.Join(tempDir, ".brewol", "memory")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

func TestSummarizerPrompt(t *testing.T) {
	prompt := SummarizerPrompt("Recent activity here")

	if !strings.Contains(prompt, "memory summarizer") {
		t.Error("expected summarizer role in prompt")
	}
	if !strings.Contains(prompt, "Recent activity here") {
		t.Error("expected activity in prompt")
	}
	if !strings.Contains(prompt, "project_type") {
		t.Error("expected JSON schema in prompt")
	}
}

func TestBuildDeterministicSummary(t *testing.T) {
	mem := BuildDeterministicSummary("go", "go build", "go test", "main", []string{"file1.go", "file2.go"})

	if mem.ProjectType != "go" {
		t.Errorf("expected project type go, got %s", mem.ProjectType)
	}
	if mem.BuildCommand != "go build" {
		t.Errorf("expected build command")
	}
	if mem.TestCommand != "go test" {
		t.Errorf("expected test command")
	}
	if mem.CurrentBranch != "main" {
		t.Errorf("expected branch main")
	}
	if len(mem.BacklogSummary) != 2 {
		t.Errorf("expected 2 backlog items from dirty files, got %d", len(mem.BacklogSummary))
	}
	if !strings.HasPrefix(mem.BacklogSummary[0], "Uncommitted:") {
		t.Error("expected uncommitted prefix in backlog")
	}
	if mem.UpdateReason != "deterministic-fallback" {
		t.Errorf("expected reason 'deterministic-fallback', got %s", mem.UpdateReason)
	}
}

func TestBuildDeterministicSummary_ManyDirtyFiles(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"}
	mem := BuildDeterministicSummary("go", "", "", "", files)

	// Should only keep first 5
	if len(mem.BacklogSummary) != 5 {
		t.Errorf("expected max 5 backlog items, got %d", len(mem.BacklogSummary))
	}
}
