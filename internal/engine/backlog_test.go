package engine

import (
	"testing"
	"time"
)

func TestBacklogPrioritySort(t *testing.T) {
	e := &Engine{
		backlog: []BacklogItem{},
	}

	// Add items in non-priority order
	e.addToBacklog(BacklogItem{
		ID:          "low",
		Description: "Low priority task",
		Priority:    4,
		Source:      "test",
		CreatedAt:   time.Now(),
	})

	e.addToBacklog(BacklogItem{
		ID:          "high",
		Description: "High priority task",
		Priority:    1,
		Source:      "test",
		CreatedAt:   time.Now(),
	})

	e.addToBacklog(BacklogItem{
		ID:          "medium",
		Description: "Medium priority task",
		Priority:    2,
		Source:      "test",
		CreatedAt:   time.Now(),
	})

	// Verify order
	backlog := e.GetBacklog()
	if len(backlog) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(backlog))
	}

	if backlog[0].Priority != 1 {
		t.Errorf("Expected first item priority 1, got %d", backlog[0].Priority)
	}

	if backlog[1].Priority != 2 {
		t.Errorf("Expected second item priority 2, got %d", backlog[1].Priority)
	}

	if backlog[2].Priority != 4 {
		t.Errorf("Expected third item priority 4, got %d", backlog[2].Priority)
	}
}

func TestBacklogDeduplication(t *testing.T) {
	e := &Engine{
		backlog: []BacklogItem{},
	}

	// Add same item twice
	e.addToBacklog(BacklogItem{
		ID:          "task1",
		Description: "Same task",
		Priority:    2,
		Source:      "test",
		CreatedAt:   time.Now(),
	})

	e.addToBacklog(BacklogItem{
		ID:          "task2",
		Description: "Same task", // Same description
		Priority:    1,           // Different priority
		Source:      "test",
		CreatedAt:   time.Now(),
	})

	// Should only have one item
	backlog := e.GetBacklog()
	if len(backlog) != 1 {
		t.Fatalf("Expected 1 item (deduplicated), got %d", len(backlog))
	}

	// Should keep the first one's priority
	if backlog[0].Priority != 2 {
		t.Errorf("Expected priority 2 (first item), got %d", backlog[0].Priority)
	}
}

func TestParseSuggestions(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name    string
		content string
		want    []Suggestion
	}{
		{
			name: "basic suggestions",
			content: `Some text before

SUGGESTIONS:
Fix failing tests — EXECUTING
Add documentation — QUEUED
Refactor utils — SKIPPED (no measurable driver)

More text after`,
			want: []Suggestion{
				{Item: "Fix failing tests", Status: "EXECUTING"},
				{Item: "Add documentation", Status: "QUEUED"},
				{Item: "Refactor utils", Status: "SKIPPED", Reason: "no measurable driver"},
			},
		},
		{
			name:    "no suggestions section",
			content: "Just regular text without suggestions",
			want:    nil,
		},
		{
			name: "empty suggestions",
			content: `SUGGESTIONS:

Next section`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.parseSuggestions(tt.content)

			if len(got) != len(tt.want) {
				t.Fatalf("parseSuggestions() returned %d items, want %d", len(got), len(tt.want))
			}

			for i, s := range got {
				if s.Item != tt.want[i].Item {
					t.Errorf("Item[%d] = %q, want %q", i, s.Item, tt.want[i].Item)
				}
				if s.Status != tt.want[i].Status {
					t.Errorf("Status[%d] = %q, want %q", i, s.Status, tt.want[i].Status)
				}
				if s.Reason != tt.want[i].Reason {
					t.Errorf("Reason[%d] = %q, want %q", i, s.Reason, tt.want[i].Reason)
				}
			}
		})
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateObserving, "OBSERVING"},
		{StateDeciding, "DECIDING"},
		{StateExecuting, "EXECUTING"},
		{StateVerifying, "VERIFYING"},
		{StateCommitting, "COMMITTING"},
		{StateRecovering, "RECOVERING"},
		{StateTerminating, "TERMINATING"},
		{State(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
