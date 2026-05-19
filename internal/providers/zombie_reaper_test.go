package providers

import (
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestZombieReaper_Constructor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	zr := NewZombieReaper(logger)
	if zr == nil {
		t.Fatal("expected non-nil ZombieReaper")
	}
}

func TestZombieReaper_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	zr := NewZombieReaper(logger)
	// Stop without Start — should not panic
	zr.Stop()
}
