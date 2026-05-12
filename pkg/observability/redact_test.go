package observability

import (
	"testing"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Empty string", "", ""},
		{"Short string", "abc", "[REDACTED]"},
		{"Long string", "this is a very long secret that should be hidden", "[REDACTED]"},
		{"Special characters", "!@#$%^&*", "[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.input); got != tt.expected {
				t.Errorf("Redact() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShouldRedactKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"SECRET_KEY", true},
		{"DB_PASSWORD", true},
		{"api_token", true},
		{"AUTH_HEADER", true},
		{"CREDENTIALS", true},
		{"db_pwd", true},
		{"apikey", true},
		{"USER_NAME", false},
		{"APP_PORT", false},
		{"LOG_LEVEL", false},
		{"MY_KEY_VAL", true},    // contains "key"
		{"PASSWORD_HINT", true}, // contains "password"
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := ShouldRedactKey(tt.key); got != tt.expected {
				t.Errorf("ShouldRedactKey(%v) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}
