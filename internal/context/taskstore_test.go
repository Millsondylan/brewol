package context

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStoreBasicOperations(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	// Test AddTask
	task := &Task{
		Title:    "Test Task",
		Priority: TaskPriorityHigh,
		Category: TaskCategoryGoal,
		Status:   TaskStatusPending,
	}
	err = ts.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Check task was assigned an ID
	if task.ID == "" {
		t.Error("Task should have been assigned an ID")
	}

	// Test GetTask
	retrieved, ok := ts.GetTask(task.ID)
	if !ok {
		t.Fatal("Task not found")
	}
	if retrieved.Title != "Test Task" {
		t.Errorf("Expected title 'Test Task', got '%s'", retrieved.Title)
	}

	// Test Count
	if ts.Count() != 1 {
		t.Errorf("Expected count 1, got %d", ts.Count())
	}
}

func TestTaskStorePrioritySort(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	// Add tasks in non-priority order
	ts.AddTask(&Task{Title: "Low", Priority: TaskPriorityLow, Category: TaskCategoryOther})
	ts.AddTask(&Task{Title: "Critical", Priority: TaskPriorityCritical, Category: TaskCategoryTest})
	ts.AddTask(&Task{Title: "Medium", Priority: TaskPriorityMedium, Category: TaskCategoryTodo})

	tasks := ts.GetAllTasks()
	if len(tasks) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(tasks))
	}

	// Should be sorted by priority
	if tasks[0].Title != "Critical" {
		t.Errorf("Expected first task to be 'Critical', got '%s'", tasks[0].Title)
	}
	if tasks[1].Title != "Medium" {
		t.Errorf("Expected second task to be 'Medium', got '%s'", tasks[1].Title)
	}
	if tasks[2].Title != "Low" {
		t.Errorf("Expected third task to be 'Low', got '%s'", tasks[2].Title)
	}
}

func TestTaskStoreStatusFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts.AddTask(&Task{Title: "Pending1", Priority: TaskPriorityHigh, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "InProgress", Priority: TaskPriorityHigh, Status: TaskStatusInProgress})
	ts.AddTask(&Task{Title: "Completed", Priority: TaskPriorityHigh, Status: TaskStatusCompleted})
	ts.AddTask(&Task{Title: "Pending2", Priority: TaskPriorityMedium, Status: TaskStatusPending})

	// Test GetPendingTasks
	pending := ts.GetPendingTasks()
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending tasks, got %d", len(pending))
	}

	// Test GetCurrentTask
	current := ts.GetCurrentTask()
	if current == nil {
		t.Fatal("Expected current task, got nil")
	}
	if current.Title != "InProgress" {
		t.Errorf("Expected current task 'InProgress', got '%s'", current.Title)
	}

	// Test CountPending (includes in_progress)
	if ts.CountPending() != 3 {
		t.Errorf("Expected CountPending 3, got %d", ts.CountPending())
	}
}

func TestTaskStoreCategoryCounts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts.AddTask(&Task{Title: "Test1", Priority: TaskPriorityHigh, Category: TaskCategoryTest, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "Test2", Priority: TaskPriorityHigh, Category: TaskCategoryTest, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "Build1", Priority: TaskPriorityHigh, Category: TaskCategoryBuild, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "Completed", Priority: TaskPriorityHigh, Category: TaskCategoryTest, Status: TaskStatusCompleted})

	counts := ts.GetCategoryCounts()

	if counts[TaskCategoryTest] != 2 {
		t.Errorf("Expected 2 test tasks, got %d", counts[TaskCategoryTest])
	}
	if counts[TaskCategoryBuild] != 1 {
		t.Errorf("Expected 1 build task, got %d", counts[TaskCategoryBuild])
	}
}

func TestTaskStoreUpdate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	task := &Task{Title: "Original", Priority: TaskPriorityMedium, Status: TaskStatusPending}
	ts.AddTask(task)

	// Update task
	err = ts.UpdateTask(task.ID, func(t *Task) {
		t.Title = "Updated"
		t.Priority = TaskPriorityCritical
	})
	if err != nil {
		t.Fatalf("Failed to update task: %v", err)
	}

	retrieved, _ := ts.GetTask(task.ID)
	if retrieved.Title != "Updated" {
		t.Errorf("Expected title 'Updated', got '%s'", retrieved.Title)
	}
	if retrieved.Priority != TaskPriorityCritical {
		t.Errorf("Expected priority Critical, got %d", retrieved.Priority)
	}

	// Test SetTaskStatus
	err = ts.SetTaskStatus(task.ID, TaskStatusCompleted)
	if err != nil {
		t.Fatalf("Failed to set status: %v", err)
	}

	retrieved, _ = ts.GetTask(task.ID)
	if retrieved.Status != TaskStatusCompleted {
		t.Errorf("Expected status Completed, got %s", retrieved.Status)
	}
	if retrieved.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
}

func TestTaskStoreRemove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	task := &Task{Title: "ToRemove", Priority: TaskPriorityMedium}
	ts.AddTask(task)

	err = ts.RemoveTask(task.ID)
	if err != nil {
		t.Fatalf("Failed to remove task: %v", err)
	}

	_, ok := ts.GetTask(task.ID)
	if ok {
		t.Error("Task should have been removed")
	}

	if ts.Count() != 0 {
		t.Errorf("Expected count 0, got %d", ts.Count())
	}
}

func TestTaskStorePersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create and add tasks
	ts1, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts1.AddTask(&Task{Title: "Persistent Task", Priority: TaskPriorityHigh, Category: TaskCategoryGoal})

	// Create new store instance (should load from disk)
	ts2, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create second task store: %v", err)
	}

	if ts2.Count() != 1 {
		t.Fatalf("Expected 1 task after reload, got %d", ts2.Count())
	}

	tasks := ts2.GetAllTasks()
	if tasks[0].Title != "Persistent Task" {
		t.Errorf("Expected title 'Persistent Task', got '%s'", tasks[0].Title)
	}
}

func TestTaskStoreClear(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	ts.AddTask(&Task{Title: "Task1", Priority: TaskPriorityHigh, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "Task2", Priority: TaskPriorityHigh, Status: TaskStatusCompleted})
	ts.AddTask(&Task{Title: "Task3", Priority: TaskPriorityHigh, Status: TaskStatusSkipped})

	// Test ClearCompleted
	err = ts.ClearCompleted()
	if err != nil {
		t.Fatalf("Failed to clear completed: %v", err)
	}

	if ts.Count() != 1 {
		t.Errorf("Expected 1 task after clear completed, got %d", ts.Count())
	}

	// Test Clear all
	ts.AddTask(&Task{Title: "Task4", Priority: TaskPriorityHigh, Status: TaskStatusPending})
	err = ts.Clear()
	if err != nil {
		t.Fatalf("Failed to clear all: %v", err)
	}

	if ts.Count() != 0 {
		t.Errorf("Expected 0 tasks after clear, got %d", ts.Count())
	}
}

func TestTaskStoreNextTask(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	// Empty store should return nil
	if ts.GetNextTask() != nil {
		t.Error("Expected nil for empty store")
	}

	ts.AddTask(&Task{Title: "Low", Priority: TaskPriorityLow, Status: TaskStatusPending})
	ts.AddTask(&Task{Title: "Critical", Priority: TaskPriorityCritical, Status: TaskStatusPending})

	next := ts.GetNextTask()
	if next == nil {
		t.Fatal("Expected next task, got nil")
	}
	if next.Title != "Critical" {
		t.Errorf("Expected 'Critical' as next task, got '%s'", next.Title)
	}
}

func TestTaskStoreEvidence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	task := &Task{Title: "With Evidence", Priority: TaskPriorityHigh}
	ts.AddTask(task)

	ts.AddEvidence(task.ID, "/path/to/log1.txt")
	ts.AddEvidence(task.ID, "/path/to/log2.txt")

	retrieved, _ := ts.GetTask(task.ID)
	if len(retrieved.EvidenceLogs) != 2 {
		t.Errorf("Expected 2 evidence logs, got %d", len(retrieved.EvidenceLogs))
	}
}

func TestTaskStoreIncrementAttempts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	task := &Task{Title: "Retryable", Priority: TaskPriorityHigh}
	ts.AddTask(task)

	ts.IncrementAttempts(task.ID)
	ts.IncrementAttempts(task.ID)
	ts.IncrementAttempts(task.ID)

	retrieved, _ := ts.GetTask(task.ID)
	if retrieved.Attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", retrieved.Attempts)
	}
}

func TestTaskStoreFilePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	expected := filepath.Join(tmpDir, ".brewol", "tasks", "tasks.json")
	if ts.FilePath() != expected {
		t.Errorf("Expected file path '%s', got '%s'", expected, ts.FilePath())
	}
}

func TestTaskStoreTimestamps(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskstore_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ts, err := NewTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create task store: %v", err)
	}

	before := time.Now()
	task := &Task{Title: "Timestamped", Priority: TaskPriorityHigh}
	ts.AddTask(task)
	after := time.Now()

	retrieved, _ := ts.GetTask(task.ID)

	// CreatedAt should be set
	if retrieved.CreatedAt.Before(before) || retrieved.CreatedAt.After(after) {
		t.Error("CreatedAt not set correctly")
	}

	// UpdatedAt should be set
	if retrieved.UpdatedAt.Before(before) || retrieved.UpdatedAt.After(after) {
		t.Error("UpdatedAt not set correctly")
	}

	// Update and check UpdatedAt changes
	time.Sleep(10 * time.Millisecond)
	ts.UpdateTask(task.ID, func(t *Task) {
		t.Title = "Updated"
	})

	retrieved2, _ := ts.GetTask(task.ID)
	if !retrieved2.UpdatedAt.After(retrieved.UpdatedAt) {
		t.Error("UpdatedAt should be updated after modification")
	}
}
