package context

import (
	"testing"
)

func TestDefaultBudgetConfig(t *testing.T) {
	cfg := DefaultBudgetConfig()

	if cfg.NumCtx != DefaultNumCtx {
		t.Errorf("Expected NumCtx %d, got %d", DefaultNumCtx, cfg.NumCtx)
	}
	if cfg.HighWatermarkRatio != DefaultHighWatermark {
		t.Errorf("Expected HighWatermarkRatio %f, got %f", DefaultHighWatermark, cfg.HighWatermarkRatio)
	}
	if cfg.LowWatermarkRatio != DefaultLowWatermark {
		t.Errorf("Expected LowWatermarkRatio %f, got %f", DefaultLowWatermark, cfg.LowWatermarkRatio)
	}
	if cfg.ReserveOutputTokens != DefaultReserveOutput {
		t.Errorf("Expected ReserveOutputTokens %d, got %d", DefaultReserveOutput, cfg.ReserveOutputTokens)
	}
}

func TestBudgetManagerSetGet(t *testing.T) {
	bm := NewBudgetManager(DefaultBudgetConfig())

	// Test default
	if bm.GetNumCtx() != DefaultNumCtx {
		t.Errorf("Expected NumCtx %d, got %d", DefaultNumCtx, bm.GetNumCtx())
	}

	// Test SetNumCtx
	bm.SetNumCtx(16384)
	if bm.GetNumCtx() != 16384 {
		t.Errorf("Expected NumCtx 16384, got %d", bm.GetNumCtx())
	}

	// Test invalid value (should keep current)
	bm.SetNumCtx(-100)
	if bm.GetNumCtx() != 16384 {
		t.Errorf("Expected NumCtx to remain 16384 after invalid set, got %d", bm.GetNumCtx())
	}
}

func TestBudgetManagerMetrics(t *testing.T) {
	bm := NewBudgetManager(BudgetConfig{
		NumCtx:              8192,
		HighWatermarkRatio:  0.8,
		LowWatermarkRatio:   0.6,
		ReserveOutputTokens: 2048,
		MaxTranscriptTurns:  5,
	})

	// Update metrics
	bm.UpdateMetrics(4096, 512)

	state := bm.GetState()

	if state.LastPromptTokens != 4096 {
		t.Errorf("Expected LastPromptTokens 4096, got %d", state.LastPromptTokens)
	}
	if state.LastEvalTokens != 512 {
		t.Errorf("Expected LastEvalTokens 512, got %d", state.LastEvalTokens)
	}

	// Check usage ratio (4096/8192 = 0.5)
	expectedRatio := 4096.0 / 8192.0
	if state.UsageRatio != expectedRatio {
		t.Errorf("Expected UsageRatio %f, got %f", expectedRatio, state.UsageRatio)
	}

	if state.NeedsCompaction {
		t.Errorf("Should not need compaction at 50%% usage")
	}
}

func TestBudgetManagerCompactionTrigger(t *testing.T) {
	bm := NewBudgetManager(BudgetConfig{
		NumCtx:              10000,
		HighWatermarkRatio:  0.8,
		LowWatermarkRatio:   0.6,
		ReserveOutputTokens: 2048,
		MaxTranscriptTurns:  5,
	})

	// Below high watermark (80% = 8000)
	bm.UpdateMetrics(7000, 100)
	if bm.NeedsCompaction() {
		t.Errorf("Should not need compaction at 7000/10000 (70%%)")
	}

	// At high watermark
	bm.UpdateMetrics(8000, 100)
	if !bm.NeedsCompaction() {
		t.Errorf("Should need compaction at 8000/10000 (80%%)")
	}

	// Above high watermark
	bm.UpdateMetrics(9000, 100)
	if !bm.NeedsCompaction() {
		t.Errorf("Should need compaction at 9000/10000 (90%%)")
	}

	// Check tokens to free (target is 60% = 6000)
	tokensToFree := bm.TokensToFree()
	expected := 9000 - 6000
	if tokensToFree != expected {
		t.Errorf("Expected TokensToFree %d, got %d", expected, tokensToFree)
	}
}

func TestBudgetManagerState(t *testing.T) {
	bm := NewBudgetManager(BudgetConfig{
		NumCtx:              8192,
		HighWatermarkRatio:  0.8,
		LowWatermarkRatio:   0.6,
		ReserveOutputTokens: 2048,
		MaxTranscriptTurns:  5,
	})

	bm.UpdateMetrics(4096, 256)
	state := bm.GetState()

	// Check watermarks
	expectedHigh := 6553 // int(8192 * 0.8)
	expectedLow := 4915  // int(8192 * 0.6)

	if state.HighWatermark != expectedHigh {
		t.Errorf("Expected HighWatermark %d, got %d", expectedHigh, state.HighWatermark)
	}
	if state.LowWatermark != expectedLow {
		t.Errorf("Expected LowWatermark %d, got %d", expectedLow, state.LowWatermark)
	}

	// Check available tokens
	expectedAvailable := 8192 - 4096 - 2048
	if state.AvailableTokens != expectedAvailable {
		t.Errorf("Expected AvailableTokens %d, got %d", expectedAvailable, state.AvailableTokens)
	}
}

func TestBudgetManagerCompactionEvents(t *testing.T) {
	bm := NewBudgetManager(DefaultBudgetConfig())

	// Record some compaction events
	bm.RecordCompaction("high_watermark", 8000, 5000, "transcript+tools")
	bm.RecordCompaction("forced", 6000, 4000, "taskbrief")

	events := bm.GetCompactionEvents()
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}

	// Check last event
	lastEvent := bm.GetLastCompactionEvent()
	if lastEvent == nil {
		t.Fatal("Expected last event, got nil")
	}
	if lastEvent.Reason != "forced" {
		t.Errorf("Expected reason 'forced', got '%s'", lastEvent.Reason)
	}
	if lastEvent.TokensBefore != 6000 {
		t.Errorf("Expected TokensBefore 6000, got %d", lastEvent.TokensBefore)
	}
	if lastEvent.TokensAfter != 4000 {
		t.Errorf("Expected TokensAfter 4000, got %d", lastEvent.TokensAfter)
	}
}

func TestBudgetManagerTranscriptTurns(t *testing.T) {
	bm := NewBudgetManager(BudgetConfig{
		NumCtx:              8192,
		HighWatermarkRatio:  0.8,
		LowWatermarkRatio:   0.6,
		ReserveOutputTokens: 2048,
		MaxTranscriptTurns:  10,
	})

	if bm.GetMaxTranscriptTurns() != 10 {
		t.Errorf("Expected MaxTranscriptTurns 10, got %d", bm.GetMaxTranscriptTurns())
	}

	bm.SetMaxTranscriptTurns(5)
	if bm.GetMaxTranscriptTurns() != 5 {
		t.Errorf("Expected MaxTranscriptTurns 5, got %d", bm.GetMaxTranscriptTurns())
	}

	// Invalid value should be ignored
	bm.SetMaxTranscriptTurns(0)
	if bm.GetMaxTranscriptTurns() != 5 {
		t.Errorf("Expected MaxTranscriptTurns to remain 5, got %d", bm.GetMaxTranscriptTurns())
	}
}

func TestBudgetManagerConfigValidation(t *testing.T) {
	// Test that invalid config values get corrected
	bm := NewBudgetManager(BudgetConfig{
		NumCtx:              0,    // Invalid
		HighWatermarkRatio:  1.5,  // Invalid
		LowWatermarkRatio:   0.95, // Invalid (>= high)
		ReserveOutputTokens: -100, // Invalid
		MaxTranscriptTurns:  -1,   // Invalid
	})

	cfg := bm.GetConfig()

	if cfg.NumCtx != DefaultNumCtx {
		t.Errorf("Expected NumCtx to be corrected to %d, got %d", DefaultNumCtx, cfg.NumCtx)
	}
	if cfg.HighWatermarkRatio != DefaultHighWatermark {
		t.Errorf("Expected HighWatermarkRatio to be corrected to %f, got %f", DefaultHighWatermark, cfg.HighWatermarkRatio)
	}
	if cfg.LowWatermarkRatio != DefaultLowWatermark {
		t.Errorf("Expected LowWatermarkRatio to be corrected to %f, got %f", DefaultLowWatermark, cfg.LowWatermarkRatio)
	}
	if cfg.ReserveOutputTokens != DefaultReserveOutput {
		t.Errorf("Expected ReserveOutputTokens to be corrected to %d, got %d", DefaultReserveOutput, cfg.ReserveOutputTokens)
	}
	if cfg.MaxTranscriptTurns != DefaultMaxTranscriptLen {
		t.Errorf("Expected MaxTranscriptTurns to be corrected to %d, got %d", DefaultMaxTranscriptLen, cfg.MaxTranscriptTurns)
	}
}
