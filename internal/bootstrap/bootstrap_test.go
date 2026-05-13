package bootstrap

import (
	"testing"
)

// TestNewBootstrapper tests bootstrapper factory
func TestNewBootstrapper(t *testing.T) {
	logger := &testLogger{}

	tests := []struct {
		name    string
		mode    BootstrapMode
		wantErr bool
	}{
		{
			name:    "local mode",
			mode:    ModeLocal,
			wantErr: false,
		},
		{
			name:    "agent mode",
			mode:    ModeAgent,
			wantErr: false,
		},
		{
			name:    "invalid mode",
			mode:    "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewBootstrapper(tt.mode, logger, &BootstrapOptions{})
			if (err != nil) != tt.wantErr {
				t.Errorf("NewBootstrapper() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBootstrapManager tests the main bootstrap manager
func TestBootstrapManager(t *testing.T) {
	logger := &testLogger{}
	manager := NewBootstrapManager(logger)

	if manager == nil {
		t.Fatal("NewBootstrapManager returned nil")
	}
}

// testLogger implements Logger interface for testing
type testLogger struct {
	messages []string
}

func (tl *testLogger) Info(msg string, args ...interface{})  { tl.messages = append(tl.messages, msg) }
func (tl *testLogger) Error(msg string, args ...interface{}) { tl.messages = append(tl.messages, msg) }
func (tl *testLogger) Warn(msg string, args ...interface{})  { tl.messages = append(tl.messages, msg) }
func (tl *testLogger) Debug(msg string, args ...interface{}) { tl.messages = append(tl.messages, msg) }
