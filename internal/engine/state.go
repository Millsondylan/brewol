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

	ctxmgr "github.com/ai/brewol/internal/context"
	"github.com/ai/brewol/internal/logs"
	"github.com/ai/brewol/internal/memory"
	"github.com/ai/brewol/internal/ollama"
	"github.com/ai/brewol/internal/prompt"
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
	State           State
	Message         string
	TokenContent    string
	ThinkingContent string // Thinking trace tokens
	IsThinking      bool   // True if currently in thinking phase
	TokensPerSec    float64
	ToolResult      *tools.ToolResult
	Suggestions     []Suggestion
	Objective       string
	Error           error
}

// Engine is the autonomous agent engine
type Engine struct {
	client        *ollama.Client
	tools         *tools.Registry
	project       *repo.Project
	verifier      *repo.Verifier
	session       *logs.Session
	promptMgr     *prompt.Manager
	memoryMgr     *memory.Manager
	budgetMgr     *ctxmgr.BudgetManager
	taskStore     *ctxmgr.TaskStore
	taskBriefGen  *ctxmgr.TaskBriefGenerator
	compactor     *ctxmgr.Compactor
	messages      []ollama.Message
	backlog       []BacklogItem
	objective     string
	state         State
	summary       string
	cycleCount    int
	updates       chan CycleUpdate
	cancel        context.CancelFunc
	mu            sync.RWMutex
	goal          string // user-set goal
	speed         int    // throttle (0 = no throttle)
	paused        bool   // pause flag
	errorCount    int    // consecutive error count
	lastError     string // last error message
	lastVerifyOK  bool   // last verification result
	pendingCommit bool   // whether there are changes pending commit
	testMode      bool   // test mode flag
	maxCycles     int    // max cycles in test mode
}

// Config holds engine configuration
type Config struct {
	WorkspaceRoot string
	Goal          string
	TestMode      bool // Enable test mode (exit after MaxCycles)
	MaxCycles     int  // Maximum cycles in test mode
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

	// Create prompt manager for instruction layering
	promptMgr := prompt.NewManager("brewol", cfg.WorkspaceRoot, string(project.Type))

	// Create memory manager for rolling memory
	memoryMgr, err := memory.NewManager(memory.Config{
		WorkspaceRoot:   cfg.WorkspaceRoot,
		UpdateInterval:  5,
		MaxContextTurns: 10,
	})
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to create memory manager: %w", err)
	}

	// Initialize memory with project info
	memoryMgr.SetProjectInfo(string(project.Type), project.BuildCommand, project.TestCommand)

	// Create context budget manager
	// Initialize with model's context size if available
	budgetCfg := ctxmgr.DefaultBudgetConfig()
	if model := client.GetModel(); model != "" {
		budgetCfg.NumCtx = client.GetModelContextSize()
	}
	budgetMgr := ctxmgr.NewBudgetManager(budgetCfg)

	// Create task store
	taskStore, err := ctxmgr.NewTaskStore(cfg.WorkspaceRoot)
	if err != nil {
		session.Close()
		memoryMgr.Close()
		return nil, fmt.Errorf("failed to create task store: %w", err)
	}

	// Create task brief generator
	taskBriefGen := ctxmgr.NewTaskBriefGenerator(taskStore)

	// Create compactor
	compactorCfg := ctxmgr.DefaultCompactorConfig(cfg.WorkspaceRoot)
	compactor, err := ctxmgr.NewCompactor(compactorCfg, budgetMgr)
	if err != nil {
		session.Close()
		memoryMgr.Close()
		return nil, fmt.Errorf("failed to create compactor: %w", err)
	}

	e := &Engine{
		client:       client,
		tools:        toolRegistry,
		project:      project,
		verifier:     verifier,
		session:      session,
		promptMgr:    promptMgr,
		memoryMgr:    memoryMgr,
		budgetMgr:    budgetMgr,
		taskStore:    taskStore,
		taskBriefGen: taskBriefGen,
		compactor:    compactor,
		messages:     make([]ollama.Message, 0),
		backlog:      make([]BacklogItem, 0),
		state:        StateObserving,
		updates:      make(chan CycleUpdate, 100),
		goal:         cfg.Goal,
		testMode:     cfg.TestMode,
		maxCycles:    cfg.MaxCycles,
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

// PromptManager returns the prompt manager
func (e *Engine) PromptManager() *prompt.Manager {
	return e.promptMgr
}

// MemoryManager returns the memory manager
func (e *Engine) MemoryManager() *memory.Manager {
	return e.memoryMgr
}

// GetEffectiveSystemPrompt returns the effective system prompt (redacted for display)
func (e *Engine) GetEffectiveSystemPrompt() string {
	return e.promptMgr.GetEffectivePromptRedacted()
}

// SetSessionInstructions sets session-level instructions and rebuilds the prompt
func (e *Engine) SetSessionInstructions(instructions string) {
	e.promptMgr.SetSessionInstructions(instructions)
	e.rebuildSystemPrompt()
}

// ClearSessionInstructions clears session instructions and rebuilds the prompt
func (e *Engine) ClearSessionInstructions() {
	e.promptMgr.ClearSessionInstructions()
	e.rebuildSystemPrompt()
}

// LoadInstructionsFromFile loads instructions from a file
func (e *Engine) LoadInstructionsFromFile(path string) error {
	if err := e.promptMgr.LoadInstructionsFromFile(path); err != nil {
		return err
	}
	e.rebuildSystemPrompt()
	return nil
}

// SaveSessionInstructions saves session instructions to user config
func (e *Engine) SaveSessionInstructions() error {
	return e.promptMgr.SaveSessionToUser()
}

// GetSummary returns a summary of the current operational state
func (e *Engine) GetSummary() Summary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Get backlog items (max 5)
	backlogItems := make([]string, 0, 5)
	for i, item := range e.backlog {
		if i >= 5 {
			break
		}
		backlogItems = append(backlogItems, item.Description)
	}

	branch := tools.GetCurrentBranch(e.project.Root)
	dirtyFiles := tools.GetDirtyFiles(e.project.Root)

	// Get context metrics
	budgetState := e.budgetMgr.GetState()
	lastCompaction := ""
	if event := e.budgetMgr.GetLastCompactionEvent(); event != nil {
		lastCompaction = event.CompactedItems
	}

	return Summary{
		CurrentObjective:   e.objective,
		CurrentState:       e.state.String(),
		CurrentGoal:        e.goal,
		CycleCount:         e.cycleCount,
		LastVerificationOK: e.lastVerifyOK,
		CurrentBranch:      branch,
		DirtyFiles:         dirtyFiles,
		BacklogItems:       backlogItems,
		IsPaused:           e.paused,
		ErrorCount:         e.errorCount,
		LastError:          e.lastError,
		NumCtx:             budgetState.NumCtx,
		PromptTokens:       budgetState.LastPromptTokens,
		EvalTokens:         budgetState.LastEvalTokens,
		ContextUsageRatio:  budgetState.UsageRatio,
		LastCompaction:     lastCompaction,
	}
}

// Summary represents an operational summary
type Summary struct {
	CurrentObjective   string
	CurrentState       string
	CurrentGoal        string
	CycleCount         int
	LastVerificationOK bool
	CurrentBranch      string
	DirtyFiles         []string
	BacklogItems       []string
	IsPaused           bool
	ErrorCount         int
	LastError          string
	// Context metrics
	NumCtx            int
	PromptTokens      int
	EvalTokens        int
	ContextUsageRatio float64
	LastCompaction    string
}

// ResetMemory resets the working memory
func (e *Engine) ResetMemory() {
	e.memoryMgr.Reset()
	e.rebuildSystemPrompt()
}

// GetWorkingMemory returns the current working memory text
func (e *Engine) GetWorkingMemory() string {
	return e.memoryMgr.GetWorkingMemoryText()
}

// BudgetManager returns the context budget manager
func (e *Engine) BudgetManager() *ctxmgr.BudgetManager {
	return e.budgetMgr
}

// TaskStore returns the task store
func (e *Engine) TaskStore() *ctxmgr.TaskStore {
	return e.taskStore
}

// TaskBriefGenerator returns the task brief generator
func (e *Engine) TaskBriefGenerator() *ctxmgr.TaskBriefGenerator {
	return e.taskBriefGen
}

// Compactor returns the compactor
func (e *Engine) Compactor() *ctxmgr.Compactor {
	return e.compactor
}

// GetContextState returns the current context budget state
func (e *Engine) GetContextState() ctxmgr.BudgetState {
	return e.budgetMgr.GetState()
}

// SetNumCtx sets the context window size
func (e *Engine) SetNumCtx(numCtx int) {
	e.budgetMgr.SetNumCtx(numCtx)
	e.client.SetNumCtx(numCtx)
}

// GetNumCtx returns the current context window size
func (e *Engine) GetNumCtx() int {
	return e.budgetMgr.GetNumCtx()
}

// SyncContextSize updates the context size based on the current model
// Call this after changing the model
func (e *Engine) SyncContextSize() {
	modelCtxSize := e.client.GetModelContextSize()
	e.budgetMgr.SetNumCtx(modelCtxSize)
}

// GetTaskBrief returns a task brief at the specified level
func (e *Engine) GetTaskBrief(level ctxmgr.TaskBriefLevel) *ctxmgr.TaskBrief {
	e.mu.RLock()
	objective := e.objective
	verifyOK := e.lastVerifyOK
	e.mu.RUnlock()

	cfg := ctxmgr.DefaultTaskBriefConfig(level)
	cfg.CurrentObjective = objective
	if verifyOK {
		cfg.LastVerifyResult = "PASSED"
	} else {
		cfg.LastVerifyResult = "FAILED"
	}

	return e.taskBriefGen.Generate(cfg)
}

// ForceCompact forces a context compaction
func (e *Engine) ForceCompact() {
	e.compactContext("forced")
}

// compactContext performs context compaction
func (e *Engine) compactContext(reason string) {
	tokensBefore := e.budgetMgr.GetState().LastPromptTokens

	// 1. Compact transcript
	var msgs []ctxmgr.Message
	for _, m := range e.messages {
		msgs = append(msgs, ctxmgr.Message{Role: m.Role, Content: m.Content})
	}
	compactedMsgs, transcriptSummary := e.compactor.CompactTranscript(msgs, true)

	// Convert back to ollama.Message
	e.messages = make([]ollama.Message, 0, len(compactedMsgs))
	for _, m := range compactedMsgs {
		e.messages = append(e.messages, ollama.Message{Role: m.Role, Content: m.Content})
	}

	// 2. Shrink task brief
	brief := e.GetTaskBrief(ctxmgr.TaskBriefCompact)

	// 3. Update rolling memory
	branch := tools.GetCurrentBranch(e.project.Root)
	update := ctxmgr.RollingMemoryUpdate{
		GitBranch:         branch,
		CurrentObjective:  e.objective,
		TaskBrief:         brief.FormatCompact(),
		CompactionSummary: transcriptSummary,
		Timestamp:         time.Now(),
	}
	rollingMemory := e.compactor.BuildRollingMemory(update)

	// Update memory manager with the rolling memory
	e.memoryMgr.SetBacklogSummary([]string{rollingMemory})

	// Rebuild system prompt with new memory
	e.rebuildSystemPrompt()

	// Record compaction event
	tokensAfter := len(e.messages) * 100 // Rough estimate
	items := fmt.Sprintf("transcript(%d msgs)+taskbrief", len(compactedMsgs))
	e.budgetMgr.RecordCompaction(reason, tokensBefore, tokensAfter, items)

	e.sendUpdate(CycleUpdate{
		State:   e.state,
		Message: fmt.Sprintf("Context compacted: %s (was %d tokens)", items, tokensBefore),
	})
}

// run is the main autonomous loop
func (e *Engine) run(ctx context.Context) {
	defer close(e.updates)
	defer e.session.Close()
	defer e.memoryMgr.Close()

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

		// Check if test mode cycle limit reached
		if e.testMode && e.cycleCount >= e.maxCycles {
			e.sendUpdate(CycleUpdate{
				State:   StateTerminating,
				Message: fmt.Sprintf("Test mode: reached max cycles (%d). Exiting.", e.maxCycles),
			})
			return
		}
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
	// Get effective prompt from all instruction layers
	systemPrompt := e.promptMgr.GetEffectivePrompt()

	// Append working memory if available
	memoryText := e.memoryMgr.GetWorkingMemoryText()
	if memoryText != "" {
		systemPrompt += "\n\n" + memoryText
	}

	e.messages = []ollama.Message{
		{Role: "system", Content: systemPrompt},
	}
}

// rebuildSystemPrompt rebuilds the system prompt and updates messages[0]
func (e *Engine) rebuildSystemPrompt() {
	systemPrompt := e.promptMgr.GetEffectivePrompt()

	memoryText := e.memoryMgr.GetWorkingMemoryText()
	if memoryText != "" {
		systemPrompt += "\n\n" + memoryText
	}

	if len(e.messages) > 0 {
		e.messages[0] = ollama.Message{Role: "system", Content: systemPrompt}
	} else {
		e.messages = []ollama.Message{{Role: "system", Content: systemPrompt}}
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

	// Execute tool calls from the model's response
	e.setState(StateExecuting)
	if len(response.Message.ToolCalls) > 0 {
		var toolResults []string
		for _, toolCall := range response.Message.ToolCalls {
			toolName := toolCall.Function.Name
			e.sendUpdate(CycleUpdate{State: StateExecuting, Message: fmt.Sprintf("Executing tool: %s", toolName)})

			result, err := e.tools.Execute(ctx, toolName, toolCall.Function.Arguments)
			if err != nil {
				errMsg := fmt.Sprintf("Tool %s failed: %v", toolName, err)
				e.sendUpdate(CycleUpdate{State: StateExecuting, Message: errMsg})
				toolResults = append(toolResults, errMsg)
			} else if result != nil {
				e.sendUpdate(CycleUpdate{State: StateExecuting, ToolResult: result})
				toolResults = append(toolResults, fmt.Sprintf("[%s] %s", toolName, result.Output))
			}
		}

		// Add all tool results back to conversation
		if len(toolResults) > 0 {
			e.messages = append(e.messages, ollama.Message{
				Role:    "user",
				Content: fmt.Sprintf("Tool Results:\n%s", strings.Join(toolResults, "\n\n")),
			})
		}
	} else if response.Message.Content != "" {
		// Model provided text response without tools - just continue
		e.sendUpdate(CycleUpdate{State: StateExecuting, Message: "Model responded with text only, no tools executed"})
	}

	// Trim context to avoid growing too large
	e.trimContext()

	// Update git state in memory
	branch := tools.GetCurrentBranch(e.project.Root)
	e.memoryMgr.SetGitState(branch, "")

	// Notify memory manager of cycle completion (may trigger periodic update)
	e.memoryMgr.OnCycleComplete(e.cycleCount + 1)

	// Short pause before next cycle
	time.Sleep(2 * time.Second)

	return nil
}

// extractCommands finds executable commands in the model response
func (e *Engine) extractCommands(content string) []string {
	var commands []string
	lines := strings.Split(content, "\n")
	inCodeBlock := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for code blocks
		if strings.HasPrefix(line, "```bash") || strings.HasPrefix(line, "```sh") || strings.HasPrefix(line, "```shell") {
			inCodeBlock = true
			continue
		}
		if strings.HasPrefix(line, "```") && inCodeBlock {
			inCodeBlock = false
			continue
		}

		// Capture commands in code blocks
		if inCodeBlock && line != "" && !strings.HasPrefix(line, "#") {
			commands = append(commands, line)
		}

		// Also look for "$ command" or "RUN: command" patterns
		if strings.HasPrefix(line, "$ ") {
			commands = append(commands, strings.TrimPrefix(line, "$ "))
		}
		if strings.HasPrefix(line, "RUN: ") {
			commands = append(commands, strings.TrimPrefix(line, "RUN: "))
		}
	}

	return commands
}

func (e *Engine) observe(ctx context.Context) (string, error) {
	// Gather actual context about the workspace
	e.mu.RLock()
	goal := e.goal
	e.mu.RUnlock()

	if goal == "" {
		return "No goal set. Waiting for instructions.", nil
	}

	// Build observation by gathering workspace state
	var parts []string
	parts = append(parts, fmt.Sprintf("Goal: %s", goal))

	// Get git status if in a git repo
	if tools.IsGitRepo(e.project.Root) {
		result, err := e.tools.Execute(ctx, "git_status", json.RawMessage(`{}`))
		if err == nil && result != nil && result.Output != "" {
			parts = append(parts, fmt.Sprintf("Git Status:\n%s", result.Output))
		}
	}

	// Check for dirty files
	dirtyFiles := tools.GetDirtyFiles(e.project.Root)
	if len(dirtyFiles) > 0 {
		parts = append(parts, fmt.Sprintf("Modified files: %v", dirtyFiles))
	}

	// First cycle - guide the model
	if e.cycleCount == 0 {
		parts = append(parts, "\nThis is your first cycle. Start by understanding the codebase and working towards the goal.")
	}

	return strings.Join(parts, "\n\n"), nil
}

func (e *Engine) decide(ctx context.Context) (*ollama.ChatResponse, error) {
	// Get tools from registry
	tools := e.tools.ToOllamaTools()

	// Stream the response WITH tools for autonomous execution
	stream, err := e.client.ChatStream(ctx, e.messages, tools)
	if err != nil {
		return nil, err
	}

	var fullResponse ollama.ChatResponse
	var contentBuilder strings.Builder
	var thinkingBuilder strings.Builder
	thinkingStartTime := time.Now()
	wasThinking := false

	for chunk := range stream {
		if chunk.Error != nil {
			return nil, chunk.Error
		}

		// Handle thinking content (separate from answer content)
		if chunk.ThinkingContent != "" || chunk.IsThinking {
			thinkingBuilder.WriteString(chunk.ThinkingContent)
			wasThinking = true
			e.sendUpdate(CycleUpdate{
				State:           StateDeciding,
				ThinkingContent: chunk.ThinkingContent,
				IsThinking:      true,
				TokensPerSec:    chunk.TokensPerSec,
			})
		}

		// Handle answer content
		if chunk.Response.Message.Content != "" {
			contentBuilder.WriteString(chunk.Response.Message.Content)
			e.sendUpdate(CycleUpdate{
				State:        StateDeciding,
				TokenContent: chunk.Response.Message.Content,
				IsThinking:   false,
				TokensPerSec: chunk.TokensPerSec,
			})
		}

		if chunk.Response.Done {
			fullResponse = chunk.Response

			// Capture token metrics on final chunk
			if chunk.Metrics != nil {
				e.budgetMgr.UpdateMetrics(chunk.Metrics.PromptEvalCount, chunk.Metrics.EvalCount)
			}
		}
	}

	fullResponse.Message.Content = contentBuilder.String()

	// Log thinking trace to disk (NOT added to conversation)
	if wasThinking {
		thinkingDuration := time.Since(thinkingStartTime).Milliseconds()
		e.session.LogThinking(e.cycleCount, thinkingBuilder.String(), thinkingDuration)
	}

	// Log the response (content only, NOT thinking)
	e.session.LogMessage("assistant", fullResponse.Message.Content, nil)

	// Check if compaction is needed
	if e.budgetMgr.NeedsCompaction() {
		e.compactContext("high_watermark")
	}

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
	// Use the compactor's transcript compaction with budget manager's max turns
	maxTurns := e.budgetMgr.GetMaxTranscriptTurns()
	maxMessages := maxTurns*2 + 1 // +1 for system message

	if len(e.messages) <= maxMessages {
		return
	}

	// Convert to compactor Message format
	var msgs []ctxmgr.Message
	for _, m := range e.messages {
		msgs = append(msgs, ctxmgr.Message{Role: m.Role, Content: m.Content})
	}

	// Compact transcript
	compactedMsgs, _ := e.compactor.CompactTranscript(msgs, true)

	// Convert back to ollama.Message
	e.messages = make([]ollama.Message, 0, len(compactedMsgs))
	for _, m := range compactedMsgs {
		e.messages = append(e.messages, ollama.Message{Role: m.Role, Content: m.Content})
	}
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
