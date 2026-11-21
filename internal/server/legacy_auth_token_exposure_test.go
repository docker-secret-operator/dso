package server

// The legacy inbound webhook bearer credential (agent.watch.webhook.auth_token)
// must not be readable through any API response or startup log, even when the
// webhook is enabled and the token is the only credential. `config show`
// redaction of this field is covered by pkg/config.TestRedactYAML_*.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/config"
)

const syntheticLegacyToken = "SYNTH-legacy-auth-token-7d41c9e2"

func TestLegacyAuthToken_NotExposedByAPIResponses(t *testing.T) {
	for name, mutate := range map[string]func(*config.WebhookConfig){
		"token only":         func(w *config.WebhookConfig) { w.HMACSecret = ""; w.AuthToken = syntheticLegacyToken },
		"token and hmac":     func(w *config.WebhookConfig) { w.AuthToken = syntheticLegacyToken },
		"token, legacy kept": func(w *config.WebhookConfig) { w.AuthToken = syntheticLegacyToken; w.AllowLegacyAuth = true },
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := newWebhookServer(t, mutate)
			for _, path := range []string{"/api/config/raw", "/api/integrations", "/api/secrets", "/api/events"} {
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != http.StatusOK {
					t.Fatalf("%s = %d", path, w.Code)
				}
				if strings.Contains(w.Body.String(), syntheticLegacyToken) {
					t.Errorf("%s exposes the legacy webhook token", path)
				}
			}
			// The integrations view only says that a credential exists.
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/integrations", nil))
			if !strings.Contains(w.Body.String(), `"auth_configured":true`) {
				t.Errorf("integrations should report auth_configured=true: %s", w.Body.String())
			}
		})
	}
}

func TestLegacyAuthToken_NotLoggedAtStartup(t *testing.T) {
	for name, mutate := range map[string]func(*config.WebhookConfig){
		"deprecated legacy-only warning": func(w *config.WebhookConfig) { w.HMACSecret = ""; w.AuthToken = syntheticLegacyToken },
		"compatibility-mode warning":     func(w *config.WebhookConfig) { w.AuthToken = syntheticLegacyToken; w.AllowLegacyAuth = true },
		"ignored-token notice":           func(w *config.WebhookConfig) { w.AuthToken = syntheticLegacyToken },
	} {
		t.Run(name, func(t *testing.T) {
			s, logs := newWebhookServer(t, mutate)
			if err := validateWebhookConfig(s.Config, s.Logger); err != nil {
				t.Fatal(err)
			}
			if logs.Len() == 0 {
				t.Fatal("expected the startup validation to log a notice for this configuration")
			}
			for _, e := range logs.All() {
				if strings.Contains(e.Message, syntheticLegacyToken) {
					t.Errorf("log message exposes the token: %s", e.Message)
				}
				for _, f := range e.Context {
					if strings.Contains(f.String, syntheticLegacyToken) {
						t.Errorf("log field %q exposes the token", f.Key)
					}
				}
			}
		})
	}
}
