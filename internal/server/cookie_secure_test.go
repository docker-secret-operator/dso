package server

import "testing"

func TestResolveCookieSecure(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		addr        string
		wantSecure  bool
		wantWarning bool
	}{
		{"always_loopback", "always", "127.0.0.1:8080", true, false},
		{"always_public", "always", "0.0.0.0:8080", true, false},
		{"never_loopback", "never", "127.0.0.1:8080", false, false},
		{"never_public", "never", "0.0.0.0:8080", false, false},
		{"auto_loopback", "auto", "127.0.0.1:8080", false, false},
		{"auto_public", "auto", "0.0.0.0:8080", true, true},
		{"empty_mode_loopback", "", "127.0.0.1:8080", false, false},
		{"empty_mode_public", "", "0.0.0.0:8080", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secure, warning := resolveCookieSecure(tt.mode, tt.addr)
			if secure != tt.wantSecure {
				t.Fatalf("secure = %v, want %v", secure, tt.wantSecure)
			}
			gotWarning := warning != ""
			if gotWarning != tt.wantWarning {
				t.Fatalf("warning present = %v (%q), want %v", gotWarning, warning, tt.wantWarning)
			}
		})
	}
}
