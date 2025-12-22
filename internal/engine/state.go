// Package engine implements the autonomous agent state machine.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ai/brewol/internal/logs"
	"github.com/ai/brewol/internal/ollama"
	"github.com/ai/brewol/internal/repo"
	"github.com/ai/brewol/internal/tools"
)

// State represents the current state of the autonomous agent
type State int

const (
	StateObserving State = iota
	StateDeciding
	StateExecuting
	StateVerifying
	StateCommitting
	StateRecovering
	StateTerminating
)

func (s State) String() string {
	switch s {
	case StateObserving:
		return "OBSERVING"
	case StateDeciding:
		return "DECIDING"
	case StateExecuting:
		return "EXECUTING"
	case StateVerifying:
		return "VERIFYING"
	case StateCommitting:
		return "COMMITTING"
	case StateRecovering:
		return "RECOVERING"
	case StateTerminating:
		return "TERMINATING"
	default:
		return "UNKNOWN"
	}
}

// BacklogItem represents a task in the backlog
type BacklogItem struct {
	ID          string
	Description string
	Priority    int // 1 = highest
	Source      string
	CreatedAt   time.Time
}

// Suggestion represents a suggestion from the model
type Suggestion struct {
	Item   string
	Status string // EXECUTING, QUEUED, SKIPPED
	Reason string // for SKIPPED
}

// CycleUpdate represents an update during a cycle
type CycleUpdate struct {
	State        State
	Message      string
	TokenContent string
	TokensPerSec float64
	ToolResult   *tools.ToolResult
	Suggestions  []Suggestion
	Objective    string
	Error        error
}

// Engine is the autonomous agent engine
type Engine struct {
	client       *ollama.Client
	tools        *tools.Registry
	project      *repo.Project
	verifier     *repo.Verifier
	session      *logs.Session
	messages     []ollama.Message
	backlog      []BacklogItem
	objective    string
	state        State
	summary      string
	cycleCount   int
	updates      chan CycleUpdate
	cancel       context.CancelFunc
	mu           sync.RWMutex
	goal         string // user-set goal
	speed        int    // throttle (0 = no throttle)
	paused       bool   // pause flag
	errorCount   int    // consecutive error count
	lastError    string // last error message
}

// Config holds engine configuration
type Config struct {
	WorkspaceRoot string
	Goal          string
}

// NewEngine creates a new autonomous engine
func NewEngine(cfg Config) (*Engine, error) {
	client := ollama.NewClient()

	toolRegistry := tools.NewRegistry(cfg.WorkspaceRoot)
	project := repo.DetectProject(cfg.WorkspaceRoot)
	verifier := repo.NewVerifier(project)

	session, err := logs.NewSession(cfg.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create logging session: %w", err)
	}

	e := &Engine{
		client:   client,
		tools:    toolRegistry,
		project:  project,
		verifier: verifier,
		session:  session,
		messages: make([]ollama.Message, 0),
		backlog:  make([]BacklogItem, 0),
		state:    StateObserving,
		updates:  make(chan CycleUpdate, 100),
		goal:     cfg.Goal,
	}

	return e, nil
}

// Updates returns the channel for receiving cycle updates
func (e *Engine) Updates() <-chan CycleUpdate {
	return e.updates
}

// Start begins the autonomous loop
func (e *Engine) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)

	go e.run(ctx)
}

// Stop stops the engine
func (e *Engine) Stop() {
	e.mu.Lock()
	e.state = StateTerminating
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()
}

// CancelCurrent cancels the current operation (single ESC)
func (e *Engine) CancelCurrent() {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()
}

// SetGoal sets the user goal
func (e *Engine) SetGoal(goal string) {
	e.mu.Lock()
	e.goal = goal
	e.mu.Unlock()

	// Add to backlog with high priority
	e.addToBacklog(BacklogItem{
		ID:          fmt.Sprintf("goal-%d", time.Now().UnixNano()),
		Description: goal,
		Priority:    1,
		Source:      "user",
		CreatedAt:   time.Now(),
	})
}

// SetSpeed sets the throttle speed (0 = no throttle)
func (e *Engine) SetSpeed(speed int) {
	e.mu.Lock()
	e.speed = speed
	e.mu.Unlock()
}

// Pause pauses the engine
func (e *Engine) Pause() {
	e.mu.Lock()
	e.paused = true
	e.mu.Unlock()
}

// Resume resumes the engine
func (e *Engine) Resume() {
	e.mu.Lock()
	e.paused = false
	e.mu.Unlock()
}

// IsPaused returns whether the engine is paused
func (e *Engine) IsPaused() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.paused
}

// GetState returns the current state
func (e *Engine) GetState() State {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// GetObjective returns the current objective
func (e *Engine) GetObjective() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.objective
}

// GetBacklog returns the current backlog
func (e *Engine) GetBacklog() []BacklogItem {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]BacklogItem, len(e.backlog))
	copy(result, e.backlog)
	return result
}

// Checkpoint creates a manual checkpoint
func (e *Engine) Checkpoint(ctx context.Context) error {
	return e.createCheckpoint(ctx, "Manual checkpoint")
}

// Rollback rolls back to the last checkpoint
func (e *Engine) Rollback(ctx context.Context) error {
	e.sendUpdate(CycleUpdate{State: StateRecovering, Message: "Rolling back to last checkpoint..."})

	// Find the agent branch and reset to it
	result, err := e.tools.Execute(ctx, "git_status", json.RawMessage(`{}`))
	if err != nil {
		return err
	}

	// Parse the last checkpoint commit
	// For now, just do a git reset to HEAD~1
	result, err = e.tools.Execute(ctx, "git_reset_hard", json.RawMessage(`{"ref": "HEAD~1"}`))
	if err != nil {
		return err
	}

	e.sendUpdate(CycleUpdate{State: StateRecovering, Message: "Rollback complete: " + result.Output})
	return nil
}

// Client returns the Ollama client
func (e *Engine) Client() *ollama.Client {
	return e.client
}

// Project returns the detected project
func (e *Engine) Project() *repo.Project {
	return e.project
}

// Session returns the logging session
func (e *Engine) Session() *logs.Session {
	return e.session
}

// run is the main autonomous loop
func (e *Engine) run(ctx context.Context) {
	defer close(e.updates)
	defer e.session.Close()

	// Initial setup
	e.initializeSession(ctx)

	for {
		select {
		case <-ctx.Done():
			e.sendUpdate(CycleUpdate{State: StateTerminating, Message: "Shutting down..."})
			return
		default:
		}

		// Check for paused state
		if e.IsPaused() {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Check for throttle
		e.mu.RLock()
		speed := e.speed
		e.mu.RUnlock()
		if speed > 0 {
			time.Sleep(time.Duration(speed) * time.Second)
		}

		// Run one cycle
		if err := e.runCycle(ctx); err != nil {
			if ctx.Err() != nil {
				// Context cancelled - restart with fresh context
				ctx, e.cancel = context.WithCancel(context.Background())
				e.sendUpdate(CycleUpdate{State: StateObserving, Message: "Operation cancelled, continuing..."})
				e.errorCount = 0
				continue
			}

			e.mu.Lock()
			e.errorCount++
			e.lastError = err.Error()
			errorCount := e.errorCount
			e.mu.Unlock()

			// Check for rate limit errors (403, 429, "limit", "quota")
			errStr := err.Error()
			isRateLimit := strings.Contains(errStr, "403") ||
				strings.Contains(errStr, "429") ||
				strings.Contains(errStr, "limit") ||
				strings.Contains(errStr, "quota") ||
				strings.Contains(errStr, "rate")

			if isRateLimit {
				e.sendUpdate(CycleUpdate{
					State:   StateRecovering,
					Error:   err,
					Message: "RATE LIMITED - Auto-pausing. Use /resume when ready.",
				})
				e.Pause()
				continue
			}

			// Exponential backoff for other errors
			if errorCount >= 3 {
				e.sendUpdate(CycleUpdate{
					State:   StateRecovering,
					Error:   err,
					Message: fmt.Sprintf("Too many errors (%d). Auto-pausing. Use /resume to retry.", errorCount),
				})
				e.Pause()
				continue
			}

			// Backoff before retry
			backoff := time.Duration(errorCount*errorCount) * time.Second
			e.sendUpdate(CycleUpdate{
				State:   StateRecovering,
				Error:   err,
				Message: fmt.Sprintf("Error %d/3. Retrying in %v...", errorCount, backoff),
			})
			time.Sleep(backoff)
			continue
		}

		// Success - reset error count
		e.mu.Lock()
		e.errorCount = 0
		e.mu.Unlock()

		e.cycleCount++
	}
}

func (e *Engine) initializeSession(ctx context.Context) {
	e.sendUpdate(CycleUpdate{State: StateObserving, Message: "Initializing session..."})

	// Create agent branch
	branchName := fmt.Sprintf("agent/%s", time.Now().Format("20060102-150405"))
	if tools.IsGitRepo(e.project.Root) {
		e.tools.Execute(ctx, "git_create_branch", json.RawMessage(fmt.Sprintf(`{"name": %q}`, branchName)))
	}

	// Build initial context
	e.buildSystemPrompt()
}

func (e *Engine) buildSystemPrompt() {
	systemPrompt := fmt.Sprintf(`You are an autonomous coding agent. Project: %s (%s). Be concise.`,
		e.project.Root,
		e.project.Type,
	)

	e.messages = []ollama.Message{
		{Role: "system", Content: systemPrompt},
	}
}

func (e *Engine) runCycle(ctx context.Context) error {
	// Wait for a goal before doing anything
	e.mu.RLock()
	goal := e.goal
	model := e.client.GetModel()
	e.mu.RUnlock()

	if goal == "" {
		e.setState(StateObserving)
		e.sendUpdate(CycleUpdate{State: StateObserving, Message: "Waiting for goal... Type your goal and press Enter"})
		time.Sleep(2 * time.Second)
		return nil
	}

	if model == "" {
		e.setState(StateObserving)
		e.sendUpdate(CycleUpdate{State: StateObserving, Message: "No model selected! Use /model to pick one"})
		time.Sleep(2 * time.Second)
		return nil
	}

	// Phase 1: Observe
	e.setState(StateObserving)
	e.sendUpdate(CycleUpdate{State: StateObserving, Message: fmt.Sprintf("Goal: %s | Model: %s", goal, model)})

	observation, err := e.observe(ctx)
	if err != nil {
		return fmt.Errorf("observe failed: %w", err)
	}

	// Add observation to conversation
	e.messages = append(e.messages, ollama.Message{
		Role:    "user",
		Content: observation,
	})

	// Phase 2: Decide
	e.setState(StateDeciding)
	e.sendUpdate(CycleUpdate{State: StateDeciding, Message: "Sending to model..."})

	response, err := e.decide(ctx)
	if err != nil {
		return fmt.Errorf("decide failed: %w", err)
	}

	e.sendUpdate(CycleUpdate{State: StateDeciding, Message: fmt.Sprintf("Got response (%d chars)", len(response.Message.Content))})

	// Parse suggestions from response
	suggestions := e.parseSuggestions(response.Message.Content)
	e.sendUpdate(CycleUpdate{State: StateDeciding, Suggestions: suggestions})

	// Add assistant response to conversation
	e.messages = append(e.messages, response.Message)

	// Phase 3: Execute tool calls
	if len(response.Message.ToolCalls) > 0 {
		e.setState(StateExecuting)
		for _, tc := range response.Message.ToolCalls {
			result, err := e.executeToolCall(ctx, tc)
			if err != nil {
				e.sendUpdate(CycleUpdate{State: StateExecuting, Error: err, ToolResult: result})
			} else {
				e.sendUpdate(CycleUpdate{State: StateExecuting, ToolResult: result})
			}

			// Add tool result to conversation
			e.messages = append(e.messages, ollama.Message{
				Role:    "tool",
				Content: result.Output,
			})
		}
	}

	// Phase 4: Verify
	e.setState(StateVerifying)
	verifyResult := e.verifier.QuickCheck(ctx)
	e.sendUpdate(CycleUpdate{
		State:   StateVerifying,
		Message: fmt.Sprintf("Verification %s (%.2fs)", boolToStatus(verifyResult.Success), verifyResult.Duration.Seconds()),
	})

	if !verifyResult.Success {
		// Add failure to conversation for the model to handle
		e.messages = append(e.messages, ollama.Message{
			Role:    "user",
			Content: fmt.Sprintf("VERIFICATION FAILED:\n%s\n\nPlease fix the issue.", verifyResult.Output),
		})
		return nil // Let the model handle it in the next cycle
	}

	// Phase 5: Checkpoint
	e.setState(StateCommitting)
	if err := e.createCheckpoint(ctx, e.objective); err != nil {
		e.sendUpdate(CycleUpdate{State: StateCommitting, Error: err})
	}

	// Update summary and trim context
	e.updateSummary()
	e.trimContext()

	return nil
}

func (e *Engine) observe(ctx context.Context) (string, error) {
	// Keep observations minimal to avoid entity too large errors
	e.mu.RLock()
	goal := e.goal
	e.mu.RUnlock()

	if goal != "" {
		return fmt.Sprintf("Goal: %s\nWhat should I do first?", goal), nil
	}

	return "No goal set. Waiting for instructions.", nil
}

func (e *Engine) decide(ctx context.Context) (*ollama.ChatResponse, error) {
	// Don't add extra prompts - just use what's in messages
	// Stream the response WITHOUT tools to avoid entity too large
	stream, err := e.client.ChatStream(ctx, e.messages, nil)
	if err != nil {
		return nil, err
	}

	var fullResponse ollama.ChatResponse
	var contentBuilder strings.Builder

	for chunk := range stream {
		if chunk.Error != nil {
			return nil, chunk.Error
		}

		contentBuilder.WriteString(chunk.Response.Message.Content)
		e.sendUpdate(CycleUpdate{
			State:        StateDeciding,
			TokenContent: chunk.Response.Message.Content,
			TokensPerSec: chunk.TokensPerSec,
		})

		if chunk.Response.Done {
			fullResponse = chunk.Response
		}
	}

	fullResponse.Message.Content = contentBuilder.String()

	// Log the response
	e.session.LogMessage("assistant", fullResponse.Message.Content, nil)

	return &fullResponse, nil
}

func (e *Engine) executeToolCall(ctx context.Context, tc ollama.ToolCall) (*tools.ToolResult, error) {
	e.sendUpdate(CycleUpdate{
		State:   StateExecuting,
		Message: fmt.Sprintf("Executing: %s", tc.Function.Name),
	})

	result, err := e.tools.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
	if result != nil {
		e.session.LogToolCall(tc.Function.Name, string(tc.Function.Arguments), result.Output, result.Duration, result.ExitCode, result.Error)
	}

	return result, err
}

func (e *Engine) createCheckpoint(ctx context.Context, message string) error {
	if !tools.IsGitRepo(e.project.Root) {
		return nil
	}

	if message == "" {
		message = fmt.Sprintf("Checkpoint at cycle %d", e.cycleCount)
	}

	commitMsg := fmt.Sprintf("[brewol] %s\n\nCycle: %d\nObjective: %s", message, e.cycleCount, e.objective)

	result, err := e.tools.Execute(ctx, "git_commit", json.RawMessage(fmt.Sprintf(`{"message": %q}`, commitMsg)))
	if err != nil {
		return err
	}

	e.sendUpdate(CycleUpdate{State: StateCommitting, Message: "Checkpoint: " + result.Output})
	e.session.LogCheckpoint("", message)

	return nil
}

func (e *Engine) refreshBacklog(ctx context.Context) {
	// Scan for TODOs
	issues, _ := repo.ScanForTODOs(e.project.Root)
	for _, issue := range issues {
		e.addToBacklog(BacklogItem{
			ID:          fmt.Sprintf("%s-%d", issue.Type, time.Now().UnixNano()),
			Description: fmt.Sprintf("%s in %s: %s", issue.Type, issue.File, issue.Message),
			Priority:    issue.Priority,
			Source:      "scan",
			CreatedAt:   time.Now(),
		})
	}

	// Check for failing tests
	testResult := e.verifier.RunTests(ctx)
	if !testResult.Success {
		failingTests := repo.GetFailingTests(testResult.Output, e.project.Type)
		for _, test := range failingTests {
			e.addToBacklog(BacklogItem{
				ID:          fmt.Sprintf("failing-test-%d", time.Now().UnixNano()),
				Description: fmt.Sprintf("Fix failing test: %s", test),
				Priority:    1, // Highest priority
				Source:      "test",
				CreatedAt:   time.Now(),
			})
		}
	}
}

func (e *Engine) addToBacklog(item BacklogItem) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for duplicates
	for _, existing := range e.backlog {
		if existing.Description == item.Description {
			return
		}
	}

	e.backlog = append(e.backlog, item)

	// Sort by priority
	sort.Slice(e.backlog, func(i, j int) bool {
		return e.backlog[i].Priority < e.backlog[j].Priority
	})
}

func (e *Engine) parseSuggestions(content string) []Suggestion {
	var suggestions []Suggestion

	lines := strings.Split(content, "\n")
	inSuggestions := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SUGGESTIONS:") {
			inSuggestions = true
			continue
		}
		if inSuggestions && line != "" {
			// Parse "Item — STATUS" or "Item — STATUS (reason)"
			parts := strings.Split(line, "—")
			if len(parts) >= 2 {
				item := strings.TrimSpace(parts[0])
				statusPart := strings.TrimSpace(parts[1])

				status := "QUEUED"
				reason := ""

				if strings.Contains(statusPart, "EXECUTING") {
					status = "EXECUTING"
				} else if strings.Contains(statusPart, "SKIPPED") {
					status = "SKIPPED"
					if idx := strings.Index(statusPart, "("); idx != -1 {
						reason = strings.Trim(statusPart[idx:], "()")
					}
				}

				suggestions = append(suggestions, Suggestion{
					Item:   item,
					Status: status,
					Reason: reason,
				})
			}
		}
		// Stop at the next section
		if inSuggestions && (strings.HasPrefix(line, "#") || strings.HasPrefix(line, "##")) {
			break
		}
	}

	return suggestions
}

func (e *Engine) updateSummary() {
	// Build a rolling summary from recent messages
	var summaryParts []string

	// Keep last 5 objectives
	objectives := 0
	for i := len(e.messages) - 1; i >= 0 && objectives < 5; i-- {
		msg := e.messages[i]
		if msg.Role == "assistant" && strings.Contains(msg.Content, "Objective:") {
			summaryParts = append(summaryParts, fmt.Sprintf("- Cycle %d: %s", e.cycleCount-objectives, truncateString(msg.Content, 200)))
			objectives++
		}
	}

	if len(summaryParts) > 0 {
		e.summary = "Recent actions:\n" + strings.Join(summaryParts, "\n")
	}
}

func (e *Engine) trimContext() {
	// Keep only system message + last 4 messages to avoid entity too large
	const maxMessages = 5

	if len(e.messages) <= maxMessages {
		return
	}

	// Keep system message (first) and last few messages
	newMessages := make([]ollama.Message, 0, maxMessages)
	newMessages = append(newMessages, e.messages[0]) // system message

	// Add last messages
	startIdx := len(e.messages) - (maxMessages - 1)
	if startIdx < 1 {
		startIdx = 1
	}
	newMessages = append(newMessages, e.messages[startIdx:]...)

	e.messages = newMessages
}

func (e *Engine) recover(ctx context.Context) {
	e.setState(StateRecovering)
	e.sendUpdate(CycleUpdate{State: StateRecovering, Message: "Attempting recovery..."})

	// Only do git reset if there are actual uncommitted changes from a failed operation
	// Don't reset on API errors
	e.mu.RLock()
	lastErr := e.lastError
	e.mu.RUnlock()

	isAPIError := strings.Contains(lastErr, "API error") ||
		strings.Contains(lastErr, "403") ||
		strings.Contains(lastErr, "429") ||
		strings.Contains(lastErr, "limit")

	if !isAPIError && tools.IsGitRepo(e.project.Root) {
		// Check if there are uncommitted changes before resetting
		dirtyFiles := tools.GetDirtyFiles(e.project.Root)
		if len(dirtyFiles) > 0 {
			e.sendUpdate(CycleUpdate{State: StateRecovering, Message: "Rolling back uncommitted changes..."})
			e.tools.Execute(ctx, "git_reset_hard", json.RawMessage(`{"ref": "HEAD"}`))
		}
	}
}

func (e *Engine) setState(state State) {
	e.mu.Lock()
	e.state = state
	e.mu.Unlock()
}

func (e *Engine) sendUpdate(update CycleUpdate) {
	select {
	case e.updates <- update:
	default:
		// Channel full, skip update
	}
}

func boolToStatus(b bool) string {
	if b {
		return "PASSED"
	}
	return "FAILED"
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
