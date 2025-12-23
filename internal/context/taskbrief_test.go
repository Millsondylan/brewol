package context

import (
	"os"
	"strings"
	"testing"
)

func TestTaskBriefGenerate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskbrief_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	// Add some tasks
	ts.AddTask(&Task{Title: "Critical Task", Priority: TaskPriorityCritical, Category: TaskCategoryBuild, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "High Task", Priority: TaskPriorityHigh, Category: TaskCategoryGoal, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "Medium Task", Priority: TaskPriorityMedium, Category: TaskCategoryTodo, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "In Progress Task", Priority: TaskPriorityHigh, Category: TaskCategoryGoal, Status: TaskStatusInProgress})

	gen := NewTaskBriefGenerator(ts)

	cfg := TaskBriefConfig{
		Level:                  TaskBriefFull,
		MaxTasksShown:          3,
		CurrentObjective:       "Complete feature implementation",
		LastVerifyResult:       "PASSED",
		ShowCategoryCounts:     true,
		ShowVerificationResult: true,
	}

	brief := gen.Generate(cfg)

	if brief.Objective != "Complete feature implementation" {
		t.Errorf("Expected objective, got '%s'", brief.Objective)
	}

	if brief.CurrentTask == nil {
		t.Error("Expected current task")
	} else if brief.CurrentTask.Title != "In Progress Task" {
		t.Errorf("Expected 'In Progress Task' as current, got '%s'", brief.CurrentTask.Title)
	}

	if len(brief.NextTasks) != 3 {
		t.Errorf("Expected 3 next tasks, got %d", len(brief.NextTasks))
	}

	if brief.TotalPending != 4 {
		t.Errorf("Expected 4 total pending, got %d", brief.TotalPending)
	}

	if brief.VerifyResult != "PASSED" {
		t.Errorf("Expected verify result 'PASSED', got '%s'", brief.VerifyResult)
	}
}

func TestTaskBriefFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskbrief_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts.AddTask(&Task{Title: "Task 1", Priority: TaskPriorityHigh, Category: TaskCategoryGoal, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "Current", Priority: TaskPriorityHigh, Category: TaskCategoryGoal, Status: TaskStatusInProgress, NextAction: "Run tests"})

	gen := NewTaskBriefGenerator(ts)

	cfg := DefaultTaskBriefConfig(TaskBriefFull)
	cfg.CurrentObjective = "Test Objective"
	cfg.LastVerifyResult = "FAILED"

	brief := gen.Generate(cfg)
	formatted := brief.Format()

	// Check formatting contains expected sections
	if !strings.Contains(formatted, "TASK STATUS") {
		t.Error("Should contain TASK STATUS header")
	}
	if !strings.Contains(formatted, "Test Objective") {
		t.Error("Should contain objective")
	}
	if !strings.Contains(formatted, "Executing:") {
		t.Error("Should contain executing section")
	}
	if !strings.Contains(formatted, "Next:") {
		t.Error("Should contain next action")
	}
	if !strings.Contains(formatted, "Run tests") {
		t.Error("Should contain next action text")
	}
}

func TestTaskBriefFormatCompact(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskbrief_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts.AddTask(&Task{Title: "Task 1", Priority: TaskPriorityHigh, Status: TaskStatusPending})

	gen := NewTaskBriefGenerator(ts)

	cfg := DefaultTaskBriefConfig(TaskBriefCompact)
	cfg.CurrentObjective = "Compact Objective"

	brief := gen.Generate(cfg)
	compact := brief.FormatCompact()

	// Should be shorter than full format
	full := brief.Format()
	if len(compact) >= len(full) {
		t.Errorf("Compact format should be shorter than full format")
	}

	// Should still contain key info
	if !strings.Contains(compact, "TASK STATUS") {
		t.Error("Compact should contain header")
	}
	if !strings.Contains(compact, "Compact Objective") {
		t.Error("Compact should contain objective")
	}
}

func TestTaskBriefFormatOneLine(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskbrief_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts.AddTask(&Task{Title: "Current Task", Priority: TaskPriorityHigh, Status: TaskStatusInProgress})
	ts.AddTask(&Task{Title: "Pending Task", Priority: TaskPriorityHigh, Status: TaskStatusPending})

	gen := NewTaskBriefGenerator(ts)

	cfg := DefaultTaskBriefConfig(TaskBriefFull)
	cfg.CurrentObjective = "One Line Test"

	brief := gen.Generate(cfg)
	oneLine := brief.FormatOneLine()

	// Should not contain newlines
	if strings.Contains(oneLine, "\n") {
		t.Error("One line format should not contain newlines")
	}

	// Should contain key info separated by |
	if !strings.Contains(oneLine, "Goal:") {
		t.Error("Should contain goal")
	}
	if !strings.Contains(oneLine, "Doing:") {
		t.Error("Should contain current task")
	}
	if !strings.Contains(oneLine, "pending") {
		t.Error("Should contain pending count")
	}
}

func TestTaskBriefLevels(t *testing.T) {
	tests := []struct {
		level         TaskBriefLevel
		expectedTasks int
	}{
		{TaskBriefFull, 3},
		{TaskBriefNormal, 2},
		{TaskBriefCompact, 1},
		{TaskBriefMinimal, 0},
	}

	for _, tt := range tests {
		cfg := DefaultTaskBriefConfig(tt.level)
		if cfg.MaxTasksShown != tt.expectedTasks {
			t.Errorf("Level %d: expected MaxTasksShown %d, got %d", tt.level, tt.expectedTasks, cfg.MaxTasksShown)
		}
	}
}

func TestGetLevelForBudget(t *testing.T) {
	tests := []struct {
		tokens   int
		expected TaskBriefLevel
	}{
		{600, TaskBriefFull},
		{500, TaskBriefFull},
		{400, TaskBriefNormal},
		{300, TaskBriefNormal},
		{200, TaskBriefCompact},
		{150, TaskBriefCompact},
		{100, TaskBriefMinimal},
		{50, TaskBriefMinimal},
	}

	for _, tt := range tests {
		result := GetLevelForBudget(tt.tokens)
		if result != tt.expected {
			t.Errorf("GetLevelForBudget(%d) = %d, expected %d", tt.tokens, result, tt.expected)
		}
	}
}

func TestTaskBriefEstimateTokens(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskbrief_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts.AddTask(&Task{Title: "Task 1", Priority: TaskPriorityHigh, Status: TaskStatusPending})

	gen := NewTaskBriefGenerator(ts)

	cfg := DefaultTaskBriefConfig(TaskBriefFull)
	cfg.CurrentObjective = "Test"

	brief := gen.Generate(cfg)
	tokens := brief.EstimateTokens()

	// Should have some tokens
	if tokens == 0 {
		t.Error("Token estimate should be > 0")
	}

	// Should be roughly formatted length / 4
	formatted := brief.Format()
	expected := len(formatted) / 4
	if tokens != expected {
		t.Errorf("Expected token estimate %d, got %d", expected, tokens)
	}
}

func TestTaskBriefShrinkToLevel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskbrief_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	for i := 0; i < 10; i++ {
		ts.AddTask(&Task{Title: "Task", Priority: TaskPriorityMedium, Status: TaskStatusPending})
	}

	gen := NewTaskBriefGenerator(ts)

	// Generate full brief
	cfg := DefaultTaskBriefConfig(TaskBriefFull)
	cfg.CurrentObjective = "Full"
	fullBrief := gen.Generate(cfg)

	// Shrink to compact
	compactBrief := gen.ShrinkToLevel(fullBrief, TaskBriefCompact)

	if compactBrief.Level != TaskBriefCompact {
		t.Errorf("Expected level Compact, got %d", compactBrief.Level)
	}

	if len(compactBrief.NextTasks) > 1 {
		t.Errorf("Compact brief should have at most 1 task, got %d", len(compactBrief.NextTasks))
	}

	// Trying to shrink to same or higher level should return same
	sameBrief := gen.ShrinkToLevel(fullBrief, TaskBriefFull)
	if sameBrief != fullBrief {
		t.Error("Shrinking to same level should return same brief")
	}
}

func TestTaskBriefEmptyStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskbrief_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	gen := NewTaskBriefGenerator(ts)

	cfg := DefaultTaskBriefConfig(TaskBriefFull)
	cfg.CurrentObjective = "Empty Store Test"

	brief := gen.Generate(cfg)

	if brief.CurrentTask != nil {
		t.Error("Expected no current task")
	}
	if len(brief.NextTasks) != 0 {
		t.Error("Expected no next tasks")
	}
	if brief.TotalPending != 0 {
		t.Error("Expected 0 pending")
	}

	// Should still format without error
	formatted := brief.Format()
	if formatted == "" {
		t.Error("Format should still produce output")
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "h..."},
		{"hello world", 8, "hello..."},
		{"hi", 3, "hi"},
		{"ab", 2, "ab"},
		{"abc", 2, "ab"},
	}

	for _, tt := range tests {
		result := truncateStr(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateStr(%q, %d) = %q, expected %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}
