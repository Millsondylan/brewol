package context

import (
	"fmt"
	"strings"
)

// TaskBriefLevel represents the compaction level for task briefs
type TaskBriefLevel int

const (
	TaskBriefFull    TaskBriefLevel = iota // Full detail: objective + 3 tasks + counts + verification
	TaskBriefNormal                        // Normal: objective + 2 tasks + counts
	TaskBriefCompact                       // Compact: objective + 1 task + counts
	TaskBriefMinimal                       // Minimal: objective + counts only
)

// TaskBriefConfig holds configuration for task brief generation
type TaskBriefConfig struct {
	Level                  TaskBriefLevel
	MaxTasksShown          int    // Maximum tasks to show in brief
	MaxTitleLength         int    // Maximum length for task titles
	MaxDescriptionLength   int    // Maximum length for task descriptions
	ShowCategoryCounts     bool   // Whether to show category counts
	ShowPriorityCounts     bool   // Whether to show priority counts
	ShowVerificationResult bool   // Whether to show last verification result
	CurrentObjective       string // Current objective (required)
	LastVerifyResult       string // Last verification result summary
}

// DefaultTaskBriefConfig returns default configuration based on level
func DefaultTaskBriefConfig(level TaskBriefLevel) TaskBriefConfig {
	cfg := TaskBriefConfig{
		Level:                level,
		MaxTitleLength:       60,
		MaxDescriptionLength: 100,
		ShowCategoryCounts:   true,
		ShowPriorityCounts:   false,
	}

	switch level {
	case TaskBriefFull:
		cfg.MaxTasksShown = 3
		cfg.ShowVerificationResult = true
		cfg.ShowPriorityCounts = true
	case TaskBriefNormal:
		cfg.MaxTasksShown = 2
		cfg.ShowVerificationResult = true
	case TaskBriefCompact:
		cfg.MaxTasksShown = 1
		cfg.ShowVerificationResult = false
	case TaskBriefMinimal:
		cfg.MaxTasksShown = 0
		cfg.ShowCategoryCounts = true
		cfg.ShowVerificationResult = false
	}

	return cfg
}

// TaskBrief represents a compact task summary for the model context
type TaskBrief struct {
	Objective      string               // Current objective (1 line)
	CurrentTask    *Task                // Currently executing task
	NextTasks      []*Task              // Next N tasks
	CategoryCounts map[TaskCategory]int // Counts by category
	PriorityCounts map[TaskPriority]int // Counts by priority
	TotalPending   int                  // Total pending tasks
	VerifyResult   string               // Last verification result
	Level          TaskBriefLevel       // Compaction level used
}

// TaskBriefGenerator generates task briefs from a TaskStore
type TaskBriefGenerator struct {
	store *TaskStore
}

// NewTaskBriefGenerator creates a new task brief generator
func NewTaskBriefGenerator(store *TaskStore) *TaskBriefGenerator {
	return &TaskBriefGenerator{store: store}
}

// Generate creates a TaskBrief at the specified level
func (g *TaskBriefGenerator) Generate(cfg TaskBriefConfig) *TaskBrief {
	brief := &TaskBrief{
		Objective:      cfg.CurrentObjective,
		Level:          cfg.Level,
		CategoryCounts: g.store.GetCategoryCounts(),
		PriorityCounts: g.store.GetPriorityCounts(),
		TotalPending:   g.store.CountPending(),
	}

	// Get current task
	brief.CurrentTask = g.store.GetCurrentTask()

	// Get next tasks
	if cfg.MaxTasksShown > 0 {
		pending := g.store.GetPendingTasks()
		maxTasks := cfg.MaxTasksShown
		if len(pending) < maxTasks {
			maxTasks = len(pending)
		}
		brief.NextTasks = pending[:maxTasks]
	}

	// Set verification result
	if cfg.ShowVerificationResult {
		brief.VerifyResult = cfg.LastVerifyResult
	}

	return brief
}

// Format returns the task brief as a formatted string for the model context
func (b *TaskBrief) Format() string {
	var sb strings.Builder

	sb.WriteString("## TASK STATUS\n\n")

	// Current objective
	if b.Objective != "" {
		sb.WriteString(fmt.Sprintf("**Current Objective:** %s\n\n", truncateStr(b.Objective, 80)))
	}

	// Current task
	if b.CurrentTask != nil {
		sb.WriteString(fmt.Sprintf("**Executing:** [P%d] %s\n", b.CurrentTask.Priority, truncateStr(b.CurrentTask.Title, 60)))
		if b.CurrentTask.NextAction != "" {
			sb.WriteString(fmt.Sprintf("  → Next: %s\n", truncateStr(b.CurrentTask.NextAction, 50)))
		}
		sb.WriteString("\n")
	}

	// Next tasks
	if len(b.NextTasks) > 0 {
		sb.WriteString("**Next Tasks:**\n")
		for i, task := range b.NextTasks {
			sb.WriteString(fmt.Sprintf("%d. [P%d/%s] %s\n", i+1, task.Priority, task.Category, truncateStr(task.Title, 55)))
		}
		sb.WriteString("\n")
	}

	// Category counts
	if len(b.CategoryCounts) > 0 {
		sb.WriteString("**Remaining by Category:**\n")
		for cat, count := range b.CategoryCounts {
			if count > 0 {
				sb.WriteString(fmt.Sprintf("  %s: %d\n", cat, count))
			}
		}
		sb.WriteString("\n")
	}

	// Total pending
	if b.TotalPending > 0 {
		shown := len(b.NextTasks)
		if b.CurrentTask != nil {
			shown++
		}
		remaining := b.TotalPending - shown
		if remaining > 0 {
			sb.WriteString(fmt.Sprintf("**Total Remaining:** %d tasks\n\n", remaining))
		}
	}

	// Verification result
	if b.VerifyResult != "" {
		sb.WriteString(fmt.Sprintf("**Last Verification:** %s\n", b.VerifyResult))
	}

	return sb.String()
}

// FormatCompact returns a minimal version of the task brief
func (b *TaskBrief) FormatCompact() string {
	var sb strings.Builder

	sb.WriteString("## TASK STATUS\n")

	if b.Objective != "" {
		sb.WriteString(fmt.Sprintf("Objective: %s\n", truncateStr(b.Objective, 60)))
	}

	if b.CurrentTask != nil {
		sb.WriteString(fmt.Sprintf("Current: %s\n", truncateStr(b.CurrentTask.Title, 50)))
	} else if len(b.NextTasks) > 0 {
		sb.WriteString(fmt.Sprintf("Next: %s\n", truncateStr(b.NextTasks[0].Title, 50)))
	}

	if b.TotalPending > 0 {
		sb.WriteString(fmt.Sprintf("Pending: %d tasks\n", b.TotalPending))
	}

	return sb.String()
}

// FormatOneLine returns a single-line summary
func (b *TaskBrief) FormatOneLine() string {
	parts := make([]string, 0, 3)

	if b.Objective != "" {
		parts = append(parts, fmt.Sprintf("Goal: %s", truncateStr(b.Objective, 40)))
	}

	if b.CurrentTask != nil {
		parts = append(parts, fmt.Sprintf("Doing: %s", truncateStr(b.CurrentTask.Title, 30)))
	}

	if b.TotalPending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", b.TotalPending))
	}

	return strings.Join(parts, " | ")
}

// EstimateTokens provides a rough estimate of tokens in the formatted brief
func (b *TaskBrief) EstimateTokens() int {
	// Rough estimate: ~4 chars per token
	formatted := b.Format()
	return len(formatted) / 4
}

// ShrinkToLevel shrinks the brief to a specified level
func (g *TaskBriefGenerator) ShrinkToLevel(brief *TaskBrief, level TaskBriefLevel) *TaskBrief {
	if level <= brief.Level {
		return brief // Already at or below target level (higher number = more compact)
	}

	cfg := DefaultTaskBriefConfig(level)
	cfg.CurrentObjective = brief.Objective
	cfg.LastVerifyResult = brief.VerifyResult

	return g.Generate(cfg)
}

// GetLevelForBudget returns the appropriate level given available tokens
func GetLevelForBudget(availableTokens int) TaskBriefLevel {
	switch {
	case availableTokens >= 500:
		return TaskBriefFull
	case availableTokens >= 300:
		return TaskBriefNormal
	case availableTokens >= 150:
		return TaskBriefCompact
	default:
		return TaskBriefMinimal
	}
}

// Helper to truncate strings
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
