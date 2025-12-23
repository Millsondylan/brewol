// Package context provides context window management and compaction for long-running agent sessions.
package context

import (
	"sync"
	"time"
)

// Default configuration values
const (
	DefaultNumCtx           = 8192 // Default context window size
	DefaultHighWatermark    = 0.80 // 80% of context triggers compaction
	DefaultLowWatermark     = 0.60 // Compact until 60% of context
	DefaultReserveOutput    = 2048 // Reserve tokens for output
	DefaultMaxTranscriptLen = 5    // Default number of turns to keep
)

// BudgetConfig holds configuration for context budget management
type BudgetConfig struct {
	NumCtx              int     // Context window size
	HighWatermarkRatio  float64 // Ratio to trigger compaction (0.0-1.0)
	LowWatermarkRatio   float64 // Target ratio after compaction (0.0-1.0)
	ReserveOutputTokens int     // Reserved tokens for output
	MaxTranscriptTurns  int     // Maximum number of transcript turns to keep
}

// DefaultBudgetConfig returns the default configuration
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		NumCtx:              DefaultNumCtx,
		HighWatermarkRatio:  DefaultHighWatermark,
		LowWatermarkRatio:   DefaultLowWatermark,
		ReserveOutputTokens: DefaultReserveOutput,
		MaxTranscriptTurns:  DefaultMaxTranscriptLen,
	}
}

// TokenUsage represents current token usage metrics
type TokenUsage struct {
	PromptTokens     int     // Tokens in the prompt
	CompletionTokens int     // Tokens generated
	TotalTokens      int     // Total tokens used
	ContextSize      int     // Configured context window size
	UsageRatio       float64 // Ratio of prompt tokens to context size
}

// CompactionEvent represents a compaction that occurred
type CompactionEvent struct {
	Timestamp      int64  // Unix timestamp
	Reason         string // Why compaction was triggered
	TokensBefore   int    // Tokens before compaction
	TokensAfter    int    // Tokens after compaction
	CompactedItems string // Description of what was compacted
}

// BudgetState represents the current state of context budget
type BudgetState struct {
	NumCtx           int     // Configured context window size
	HighWatermark    int     // Absolute token count for high watermark
	LowWatermark     int     // Absolute token count for low watermark
	AvailableTokens  int     // Tokens available for output
	LastPromptTokens int     // Last measured prompt tokens
	LastEvalTokens   int     // Last measured eval tokens
	UsageRatio       float64 // Current usage as ratio
	NeedsCompaction  bool    // Whether compaction is needed
}

// BudgetManager manages context window budget and triggers compaction
type BudgetManager struct {
	config           BudgetConfig
	lastPromptTokens int
	lastEvalTokens   int
	compactionEvents []CompactionEvent
	maxEvents        int
	mu               sync.RWMutex
}

// NewBudgetManager creates a new budget manager with the given configuration
func NewBudgetManager(cfg BudgetConfig) *BudgetManager {
	if cfg.NumCtx <= 0 {
		cfg.NumCtx = DefaultNumCtx
	}
	if cfg.HighWatermarkRatio <= 0 || cfg.HighWatermarkRatio > 1.0 {
		cfg.HighWatermarkRatio = DefaultHighWatermark
	}
	if cfg.LowWatermarkRatio <= 0 || cfg.LowWatermarkRatio >= cfg.HighWatermarkRatio {
		cfg.LowWatermarkRatio = DefaultLowWatermark
	}
	if cfg.ReserveOutputTokens <= 0 {
		cfg.ReserveOutputTokens = DefaultReserveOutput
	}
	if cfg.MaxTranscriptTurns <= 0 {
		cfg.MaxTranscriptTurns = DefaultMaxTranscriptLen
	}

	return &BudgetManager{
		config:           cfg,
		compactionEvents: make([]CompactionEvent, 0, 100),
		maxEvents:        100,
	}
}

// SetNumCtx updates the context window size
func (b *BudgetManager) SetNumCtx(numCtx int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if numCtx > 0 {
		b.config.NumCtx = numCtx
	}
}

// GetNumCtx returns the current context window size
func (b *BudgetManager) GetNumCtx() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config.NumCtx
}

// GetConfig returns a copy of the current configuration
func (b *BudgetManager) GetConfig() BudgetConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

// UpdateMetrics updates the budget manager with new token metrics
func (b *BudgetManager) UpdateMetrics(promptTokens, evalTokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastPromptTokens = promptTokens
	b.lastEvalTokens = evalTokens
}

// GetState returns the current budget state
func (b *BudgetManager) GetState() BudgetState {
	b.mu.RLock()
	defer b.mu.RUnlock()

	highWatermark := int(float64(b.config.NumCtx) * b.config.HighWatermarkRatio)
	lowWatermark := int(float64(b.config.NumCtx) * b.config.LowWatermarkRatio)
	availableTokens := b.config.NumCtx - b.lastPromptTokens - b.config.ReserveOutputTokens

	usageRatio := 0.0
	if b.config.NumCtx > 0 {
		usageRatio = float64(b.lastPromptTokens) / float64(b.config.NumCtx)
	}

	return BudgetState{
		NumCtx:           b.config.NumCtx,
		HighWatermark:    highWatermark,
		LowWatermark:     lowWatermark,
		AvailableTokens:  availableTokens,
		LastPromptTokens: b.lastPromptTokens,
		LastEvalTokens:   b.lastEvalTokens,
		UsageRatio:       usageRatio,
		NeedsCompaction:  b.lastPromptTokens >= highWatermark,
	}
}

// NeedsCompaction returns true if the context needs to be compacted
func (b *BudgetManager) NeedsCompaction() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	highWatermark := int(float64(b.config.NumCtx) * b.config.HighWatermarkRatio)
	return b.lastPromptTokens >= highWatermark
}

// TargetTokens returns the target number of prompt tokens after compaction
func (b *BudgetManager) TargetTokens() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return int(float64(b.config.NumCtx) * b.config.LowWatermarkRatio)
}

// TokensToFree returns the number of tokens that need to be freed
func (b *BudgetManager) TokensToFree() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	target := int(float64(b.config.NumCtx) * b.config.LowWatermarkRatio)
	if b.lastPromptTokens <= target {
		return 0
	}
	return b.lastPromptTokens - target
}

// RecordCompaction records a compaction event
func (b *BudgetManager) RecordCompaction(reason string, tokensBefore, tokensAfter int, compactedItems string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	event := CompactionEvent{
		Timestamp:      timeNow(),
		Reason:         reason,
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
		CompactedItems: compactedItems,
	}

	b.compactionEvents = append(b.compactionEvents, event)

	// Keep only the last N events
	if len(b.compactionEvents) > b.maxEvents {
		b.compactionEvents = b.compactionEvents[len(b.compactionEvents)-b.maxEvents:]
	}
}

// GetCompactionEvents returns recent compaction events
func (b *BudgetManager) GetCompactionEvents() []CompactionEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	events := make([]CompactionEvent, len(b.compactionEvents))
	copy(events, b.compactionEvents)
	return events
}

// GetLastCompactionEvent returns the most recent compaction event, or nil if none
func (b *BudgetManager) GetLastCompactionEvent() *CompactionEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.compactionEvents) == 0 {
		return nil
	}

	event := b.compactionEvents[len(b.compactionEvents)-1]
	return &event
}

// GetMaxTranscriptTurns returns the configured max transcript turns
func (b *BudgetManager) GetMaxTranscriptTurns() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config.MaxTranscriptTurns
}

// SetMaxTranscriptTurns sets the max transcript turns to keep
func (b *BudgetManager) SetMaxTranscriptTurns(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > 0 {
		b.config.MaxTranscriptTurns = n
	}
}

// Helper to get current time as Unix timestamp
func timeNow() int64 {
	return time.Now().Unix()
}
