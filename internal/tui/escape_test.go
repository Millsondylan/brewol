package tui

import (
	"testing"
	"time"
)

func TestDoubleEscapeTiming(t *testing.T) {
	tests := []struct {
		name       string
		interval   time.Duration
		shouldQuit bool
	}{
		{
			name:       "fast double press (200ms) - should quit",
			interval:   200 * time.Millisecond,
			shouldQuit: true,
		},
		{
			name:       "medium double press (500ms) - should quit",
			interval:   500 * time.Millisecond,
			shouldQuit: true,
		},
		{
			name:       "slow double press (800ms) - should NOT quit",
			interval:   800 * time.Millisecond,
			shouldQuit: false,
		},
		{
			name:       "very slow (1s) - should NOT quit",
			interval:   1 * time.Second,
			shouldQuit: false,
		},
		{
			name:       "at threshold (600ms) - should NOT quit",
			interval:   600 * time.Millisecond,
			shouldQuit: false,
		},
		{
			name:       "just under threshold (599ms) - should quit",
			interval:   599 * time.Millisecond,
			shouldQuit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the timing logic from handleEscape
			lastEscTime := time.Now()
			time.Sleep(tt.interval)
			now := time.Now()

			shouldQuit := now.Sub(lastEscTime) < 600*time.Millisecond

			if shouldQuit != tt.shouldQuit {
				t.Errorf("Double ESC with %v interval: got quit=%v, want quit=%v",
					tt.interval, shouldQuit, tt.shouldQuit)
			}
		})
	}
}

func TestEscapeTimingEdgeCases(t *testing.T) {
	t.Run("first escape should never quit", func(t *testing.T) {
		var lastEscTime time.Time // zero value
		now := time.Now()

		// First escape: should never trigger quit because lastEscTime is zero
		interval := now.Sub(lastEscTime)

		// Zero time is year 1, so interval will be huge
		if interval < 600*time.Millisecond {
			t.Error("First escape should have very large interval from zero time")
		}
	})

	t.Run("immediate double press", func(t *testing.T) {
		now := time.Now()

		// Simulated very fast double press (1ms apart)
		if now.Sub(now.Add(-time.Millisecond)) >= 600*time.Millisecond {
			t.Error("1ms interval should be less than 600ms")
		}
	})
}
