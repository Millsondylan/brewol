package context

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusSkipped    TaskStatus = "skipped"
)

// TaskPriority represents task priority levels
type TaskPriority int

const (
	TaskPriorityCritical TaskPriority = 1 // P1: Failing tests, build errors
	TaskPriorityHigh     TaskPriority = 2 // P2: User goals, FIXME/HACK
	TaskPriorityMedium   TaskPriority = 3 // P3: TODO comments
	TaskPriorityLow      TaskPriority = 4 // P4: Style, docs
)

// TaskCategory represents the type of task
type TaskCategory string

const (
	TaskCategoryBuild    TaskCategory = "build"    // Build/compile issues
	TaskCategoryTest     TaskCategory = "test"     // Test failures
	TaskCategoryGoal     TaskCategory = "goal"     // User-defined goals
	TaskCategoryTodo     TaskCategory = "todo"     // Code TODOs
	TaskCategoryFixme    TaskCategory = "fixme"    // Code FIXMEs
	TaskCategoryRefactor TaskCategory = "refactor" // Refactoring tasks
	TaskCategoryDocs     TaskCategory = "docs"     // Documentation
	TaskCategoryOther    TaskCategory = "other"    // Other tasks
)

// Task represents a task in the task store
type Task struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description,omitempty"`
	Priority     TaskPriority `json:"priority"`
	Status       TaskStatus   `json:"status"`
	Category     TaskCategory `json:"category"`
	Files        []string     `json:"files,omitempty"`         // Related files
	EvidenceLogs []string     `json:"evidence_logs,omitempty"` // Log file paths
	NextAction   string       `json:"next_action,omitempty"`   // Suggested next action
	Attempts     int          `json:"attempts"`                // Number of attempts
	Source       string       `json:"source,omitempty"`        // Where the task came from
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	CompletedAt  *time.Time   `json:"completed_at,omitempty"`
}

// TaskStore manages persistent task storage
type TaskStore struct {
	tasks    map[string]*Task
	filePath string
	mu       sync.RWMutex
}

// NewTaskStore creates a new task store with the given workspace root
func NewTaskStore(workspaceRoot string) (*TaskStore, error) {
	// Create store directory
	storeDir := filepath.Join(workspaceRoot, ".brewol", "tasks")
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create task store directory: %w", err)
	}

	filePath := filepath.Join(storeDir, "tasks.json")

	ts := &TaskStore{
		tasks:    make(map[string]*Task),
		filePath: filePath,
	}

	// Load existing tasks
	if err := ts.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load tasks: %w", err)
	}

	return ts, nil
}

// load reads tasks from disk
func (ts *TaskStore) load() error {
	data, err := os.ReadFile(ts.filePath)
	if err != nil {
		return err
	}

	var tasks []*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return err
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.tasks = make(map[string]*Task)
	for _, task := range tasks {
		ts.tasks[task.ID] = task
	}

	return nil
}

// save writes tasks to disk
func (ts *TaskStore) save() error {
	ts.mu.RLock()
	tasks := make([]*Task, 0, len(ts.tasks))
	for _, task := range ts.tasks {
		tasks = append(tasks, task)
	}
	ts.mu.RUnlock()

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ts.filePath, data, 0644)
}

// AddTask adds a new task to the store
func (ts *TaskStore) AddTask(task *Task) error {
	if task.ID == "" {
		task.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	task.UpdatedAt = time.Now()
	if task.Status == "" {
		task.Status = TaskStatusPending
	}

	ts.mu.Lock()
	ts.tasks[task.ID] = task
	ts.mu.Unlock()

	return ts.save()
}

// UpdateTask updates an existing task
func (ts *TaskStore) UpdateTask(id string, update func(*Task)) error {
	ts.mu.Lock()
	task, exists := ts.tasks[id]
	if !exists {
		ts.mu.Unlock()
		return fmt.Errorf("task not found: %s", id)
	}
	update(task)
	task.UpdatedAt = time.Now()
	ts.mu.Unlock()

	return ts.save()
}

// GetTask returns a task by ID
func (ts *TaskStore) GetTask(id string) (*Task, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	task, exists := ts.tasks[id]
	if !exists {
		return nil, false
	}

	// Return a copy
	taskCopy := *task
	return &taskCopy, true
}

// RemoveTask removes a task by ID
func (ts *TaskStore) RemoveTask(id string) error {
	ts.mu.Lock()
	delete(ts.tasks, id)
	ts.mu.Unlock()

	return ts.save()
}

// GetAllTasks returns all tasks sorted by priority then created time
func (ts *TaskStore) GetAllTasks() []*Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	tasks := make([]*Task, 0, len(ts.tasks))
	for _, task := range ts.tasks {
		taskCopy := *task
		tasks = append(tasks, &taskCopy)
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	return tasks
}

// GetTasksByStatus returns tasks filtered by status
func (ts *TaskStore) GetTasksByStatus(status TaskStatus) []*Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var tasks []*Task
	for _, task := range ts.tasks {
		if task.Status == status {
			taskCopy := *task
			tasks = append(tasks, &taskCopy)
		}
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	return tasks
}

// GetTasksByCategory returns tasks filtered by category
func (ts *TaskStore) GetTasksByCategory(category TaskCategory) []*Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var tasks []*Task
	for _, task := range ts.tasks {
		if task.Category == category {
			taskCopy := *task
			tasks = append(tasks, &taskCopy)
		}
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	return tasks
}

// GetPendingTasks returns all pending tasks sorted by priority
func (ts *TaskStore) GetPendingTasks() []*Task {
	return ts.GetTasksByStatus(TaskStatusPending)
}

// GetCurrentTask returns the current in-progress task, if any
func (ts *TaskStore) GetCurrentTask() *Task {
	tasks := ts.GetTasksByStatus(TaskStatusInProgress)
	if len(tasks) > 0 {
		return tasks[0]
	}
	return nil
}

// GetNextTask returns the next task to work on (highest priority pending)
func (ts *TaskStore) GetNextTask() *Task {
	tasks := ts.GetPendingTasks()
	if len(tasks) > 0 {
		return tasks[0]
	}
	return nil
}

// SetTaskStatus updates a task's status
func (ts *TaskStore) SetTaskStatus(id string, status TaskStatus) error {
	return ts.UpdateTask(id, func(t *Task) {
		t.Status = status
		if status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusSkipped {
			now := time.Now()
			t.CompletedAt = &now
		}
	})
}

// IncrementAttempts increments the attempt count for a task
func (ts *TaskStore) IncrementAttempts(id string) error {
	return ts.UpdateTask(id, func(t *Task) {
		t.Attempts++
	})
}

// SetNextAction sets the suggested next action for a task
func (ts *TaskStore) SetNextAction(id string, action string) error {
	return ts.UpdateTask(id, func(t *Task) {
		t.NextAction = action
	})
}

// AddEvidence adds an evidence log path to a task
func (ts *TaskStore) AddEvidence(id string, logPath string) error {
	return ts.UpdateTask(id, func(t *Task) {
		t.EvidenceLogs = append(t.EvidenceLogs, logPath)
	})
}

// GetCategoryCounts returns counts of pending tasks by category
func (ts *TaskStore) GetCategoryCounts() map[TaskCategory]int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	counts := make(map[TaskCategory]int)
	for _, task := range ts.tasks {
		if task.Status == TaskStatusPending || task.Status == TaskStatusInProgress {
			counts[task.Category]++
		}
	}
	return counts
}

// GetPriorityCounts returns counts of pending tasks by priority
func (ts *TaskStore) GetPriorityCounts() map[TaskPriority]int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	counts := make(map[TaskPriority]int)
	for _, task := range ts.tasks {
		if task.Status == TaskStatusPending || task.Status == TaskStatusInProgress {
			counts[task.Priority]++
		}
	}
	return counts
}

// Count returns the total number of tasks
func (ts *TaskStore) Count() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.tasks)
}

// CountPending returns the number of pending tasks
func (ts *TaskStore) CountPending() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	count := 0
	for _, task := range ts.tasks {
		if task.Status == TaskStatusPending || task.Status == TaskStatusInProgress {
			count++
		}
	}
	return count
}

// Clear removes all tasks
func (ts *TaskStore) Clear() error {
	ts.mu.Lock()
	ts.tasks = make(map[string]*Task)
	ts.mu.Unlock()

	return ts.save()
}

// ClearCompleted removes all completed tasks
func (ts *TaskStore) ClearCompleted() error {
	ts.mu.Lock()
	for id, task := range ts.tasks {
		if task.Status == TaskStatusCompleted || task.Status == TaskStatusSkipped {
			delete(ts.tasks, id)
		}
	}
	ts.mu.Unlock()

	return ts.save()
}

// FilePath returns the path to the task store file
func (ts *TaskStore) FilePath() string {
	return ts.filePath
}
