package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// The production defaults must not drift: NewTriggerEngine is the only
// production constructor and must keep using them.
func TestTriggerEngine_DefaultDirsPinned(t *testing.T) {
	if defaultTriggerStateDir != "/var/lib/dso/state" || defaultTriggerLockDir != "/var/lib/dso/locks" {
		t.Fatalf("production trigger directories changed: %q %q", defaultTriggerStateDir, defaultTriggerLockDir)
	}
}

// Injected directories are the ONLY ones touched, with the same creation
// semantics (state dir 0700) as the default path.
func TestNewTriggerEngineWithDirs_UsesInjectedDirsOnly(t *testing.T) {
	root := t.TempDir()
	state, locks := filepath.Join(root, "state"), filepath.Join(root, "locks")
	eng, err := NewTriggerEngineWithDirs(nil, nil, nil, zap.NewNop(), &config.Config{}, nil, state, locks)
	if err != nil {
		t.Fatalf("NewTriggerEngineWithDirs: %v", err)
	}
	defer eng.cancel()
	st, err := os.Stat(state)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("state dir not created 0700 at injected path: %v %v", st, err)
	}
	if _, err := os.Stat(locks); err != nil {
		t.Fatalf("lock dir not created at injected path: %v", err)
	}
}
