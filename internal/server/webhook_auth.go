package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"go.uber.org/zap"
)

// Webhook request signing (POST /api/events/secret-update)
//
// The sender computes, for the exact bytes of the request body:
//
//	X-DSO-Timestamp: <unix seconds, decimal>
//	X-DSO-Signature: v1=<lowercase hex HMAC-SHA256(secret, timestamp + "." + body)>
//
// This is the same construction used by Stripe/Slack-style webhooks: the
// timestamp is part of the signed string so it cannot be altered, and the
// version prefix lets the scheme evolve. The shared secret is
// agent.watch.webhook.hmac_secret; it is independent of DSO_AUTH_TOKEN and of
// the legacy auth_token.
//
// Replay protection has three layers:
//  1. the timestamp must be within +/- timestamp_tolerance of the server clock;
//  2. an accepted signature is remembered until its timestamp can no longer
//     pass layer 1, and a second request carrying the same signature is
//     rejected (409). Entries are only stored AFTER the signature verified, so
//     unauthenticated traffic cannot fill the table;
//  3. timestamps earlier than the process start time are rejected, which closes
//     the "replay a captured request after an agent restart" gap that an
//     in-memory table would otherwise leave open.
//
// Senders must make each delivery unique (e.g. include a delivery id or
// timestamp in the JSON body): two byte-identical bodies signed in the same
// second produce the same signature and the second is treated as a replay.
const (
	webhookTimestampHeader = "X-DSO-Timestamp"
	webhookSignatureHeader = "X-DSO-Signature"
	webhookSigPrefix       = "v1="

	defaultWebhookTolerance = 5 * time.Minute
	maxWebhookTolerance     = time.Hour
	minWebhookSecretBytes   = 16
	// recommendedWebhookSecretBytes is the length at which a warning stops:
	// HMAC-SHA256 keys should carry at least 256 bits (32 random bytes, e.g.
	// `openssl rand -hex 32`). Below it the config is accepted (>= the hard
	// minimum) but startup logs a warning; the minimum itself is unchanged so
	// existing configs keep working.
	recommendedWebhookSecretBytes = 32

	// maxWebhookBodyBytes bounds how much of the body is ever buffered.
	maxWebhookBodyBytes = 64 * 1024

	// maxReplayEntries bounds replay-table memory. If the table is full of
	// still-valid entries new signed requests are refused (fail closed) rather
	// than evicting an entry an attacker could then replay.
	maxReplayEntries = 10000
)

var (
	errWebhookReplay = errors.New("webhook replay")
	errReplayFull    = errors.New("webhook replay table full")
)

// replayGuard is a concurrency-safe, bounded, expiring set of accepted
// signatures. check-and-insert is a single critical section, so two concurrent
// deliveries of the same signed request cannot both succeed.
type replayGuard struct {
	mu    sync.Mutex
	seen  map[string]int64 // decoded MAC bytes -> unix expiry
	floor int64            // request timestamps below this (server start) are rejected
	now   func() time.Time
	max   int
}

// newReplayGuardAt creates an empty guard whose restart floor is start: any
// request timestamp earlier than start (in whole seconds) is rejected.
func newReplayGuardAt(start time.Time) *replayGuard {
	return &replayGuard{seen: make(map[string]int64), now: time.Now, max: maxReplayEntries, floor: start.Unix()}
}

func newReplayGuard() *replayGuard { return newReplayGuardAt(time.Now()) }

// remember records sig until expiry (unix seconds). It returns errWebhookReplay
// if sig was already accepted and still live.
func (g *replayGuard) remember(sig string, expiry int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now().Unix()
	if exp, ok := g.seen[sig]; ok && exp >= now {
		return errWebhookReplay
	}
	if len(g.seen) >= g.max {
		for k, exp := range g.seen {
			if exp < now {
				delete(g.seen, k)
			}
		}
		if len(g.seen) >= g.max {
			return errReplayFull
		}
	}
	g.seen[sig] = expiry
	return nil
}

// initWebhookGuard creates the replay guard exactly once, with start as the
// restart floor. StartRESTServer calls it before the listener accepts
// connections, so the floor is the server's start time and cannot be moved by
// any request. A second call is a no-op: the guard (and therefore the replay
// table) is never reset.
func (s *RESTServer) initWebhookGuard(start time.Time) {
	s.webhookOnce.Do(func() {
		if s.webhookGuard == nil {
			s.webhookGuard = newReplayGuardAt(start)
		}
	})
}

// replayGuard returns the server's guard. In production it was created by
// initWebhookGuard during startup. The fallback exists only for RESTServer
// values built without StartRESTServer (unit tests); there the floor is the
// first use.
func (s *RESTServer) replayGuard() *replayGuard {
	s.initWebhookGuard(time.Now())
	return s.webhookGuard
}

// WebhookSignature returns the X-DSO-Signature header value for a request.
// Exposed so senders (and tests) share the one canonical construction.
func WebhookSignature(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return webhookSigPrefix + hex.EncodeToString(mac.Sum(nil))
}

func webhookTolerance(cfg config.WebhookConfig) (time.Duration, error) {
	if cfg.TimestampTolerance == "" {
		return defaultWebhookTolerance, nil
	}
	d, err := time.ParseDuration(cfg.TimestampTolerance)
	if err != nil {
		return 0, fmt.Errorf("invalid webhook timestamp_tolerance %q: %w", cfg.TimestampTolerance, err)
	}
	if d <= 0 || d > maxWebhookTolerance {
		return 0, fmt.Errorf("webhook timestamp_tolerance %q must be > 0 and <= %s", cfg.TimestampTolerance, maxWebhookTolerance)
	}
	return d, nil
}

// validateWebhookConfig fails startup on unusable new settings and warns about
// deprecated or credential-less configurations. It never logs credentials.
func validateWebhookConfig(cfg *config.Config, logger *zap.Logger) error {
	if cfg == nil || !cfg.Agent.Watch.Webhook.Enabled {
		return nil
	}
	wh := cfg.Agent.Watch.Webhook
	if wh.HMACSecret != "" && len(wh.HMACSecret) < minWebhookSecretBytes {
		return fmt.Errorf("agent.watch.webhook.hmac_secret is too short (%d bytes); minimum is %d", len(wh.HMACSecret), minWebhookSecretBytes)
	}
	if _, err := webhookTolerance(wh); err != nil {
		return err
	}
	if n := len(wh.HMACSecret); n >= minWebhookSecretBytes && n < recommendedWebhookSecretBytes {
		// Length only: the value itself is never logged.
		logger.Warn(fmt.Sprintf("agent.watch.webhook.hmac_secret is only %d bytes; use at least %d random bytes (for example: openssl rand -hex 32)", n, recommendedWebhookSecretBytes))
	}
	legacy := wh.AuthToken != "" && !wh.DisableLegacyAuth
	if legacy {
		logger.Warn("DEPRECATED: webhook bearer authentication (agent.watch.webhook.auth_token) has no body integrity or replay protection and will be removed in a future release; configure agent.watch.webhook.hmac_secret and sign requests (X-DSO-Timestamp / X-DSO-Signature), then set disable_legacy_auth: true")
	}
	if wh.HMACSecret == "" && !legacy {
		logger.Warn("webhooks are enabled but no usable credential is configured; every webhook request will be rejected")
	}
	return nil
}

func (s *RESTServer) webhookDeny(r *http.Request, reason string) {
	observability.BackendFailuresTotal.WithLabelValues("webhook", "unauthorized").Inc()
	if s.Logger == nil {
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Reason code and peer only: never the signature, timestamp value, token
	// or body.
	s.Logger.Warn("Webhook request rejected", zap.String("reason", reason), zap.String("remote", host))
}

// authenticateWebhook enforces the webhook's OWN authentication and returns the
// exact request body for the handler to parse. It is the only authentication
// for this route (the route is exempt from the global bearer gate in
// ServeHTTP), so every code path either returns (body, true) after a
// credential check succeeded or writes an error response and returns false.
//
// Order of checks: configuration -> credential selection -> cheap header
// checks (no body buffered) -> bounded body read -> constant-time MAC compare
// -> replay registration.
func (s *RESTServer) authenticateWebhook(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if s.Config == nil || !s.Config.Agent.Watch.Webhook.Enabled {
		http.Error(w, "Webhooks are disabled", http.StatusForbidden)
		return nil, false
	}
	wh := s.Config.Agent.Watch.Webhook
	hmacOn := wh.HMACSecret != ""
	legacyOn := wh.AuthToken != "" && !wh.DisableLegacyAuth
	if !hmacOn && !legacyOn {
		http.Error(w, "Webhook authentication is required when webhooks are enabled", http.StatusForbidden)
		return nil, false
	}

	if r.ContentLength > maxWebhookBodyBytes {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return nil, false
	}

	sigHeader := strings.TrimSpace(r.Header.Get(webhookSignatureHeader))
	switch {
	case sigHeader != "":
		if !hmacOn {
			s.webhookDeny(r, "signature_not_configured")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return nil, false
		}
		return s.verifySignedWebhook(w, r, wh, sigHeader)
	case legacyOn:
		return s.verifyLegacyWebhook(w, r, wh)
	default:
		s.webhookDeny(r, "missing_signature")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}
}

func (s *RESTServer) verifySignedWebhook(w http.ResponseWriter, r *http.Request, wh config.WebhookConfig, sigHeader string) ([]byte, bool) {
	deny := func(reason string, code int, msg string) ([]byte, bool) {
		s.webhookDeny(r, reason)
		http.Error(w, msg, code)
		return nil, false
	}

	tol, err := webhookTolerance(wh)
	if err != nil { // validated at startup; defensive for hand-built configs
		tol = defaultWebhookTolerance
	}

	// Cheap checks first: nothing below buffers the body.
	tsStr := strings.TrimSpace(r.Header.Get(webhookTimestampHeader))
	if tsStr == "" || len(tsStr) > 12 {
		return deny("bad_timestamp", http.StatusUnauthorized, "Unauthorized")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || ts <= 0 {
		return deny("bad_timestamp", http.StatusUnauthorized, "Unauthorized")
	}
	guard := s.replayGuard()
	now := guard.now()
	skew := now.Sub(time.Unix(ts, 0))
	if skew > tol || skew < -tol {
		return deny("stale_timestamp", http.StatusUnauthorized, "Unauthorized")
	}
	if ts < guard.floor {
		return deny("predates_process_start", http.StatusUnauthorized, "Unauthorized")
	}
	if !strings.HasPrefix(sigHeader, webhookSigPrefix) || len(sigHeader) != len(webhookSigPrefix)+sha256.Size*2 {
		return deny("malformed_signature", http.StatusUnauthorized, "Unauthorized")
	}
	sig, err := hex.DecodeString(sigHeader[len(webhookSigPrefix):])
	if err != nil {
		return deny("malformed_signature", http.StatusUnauthorized, "Unauthorized")
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return deny("body_too_large", http.StatusRequestEntityTooLarge, "Request body too large")
		}
		return deny("body_read_error", http.StatusBadRequest, "Invalid Payload")
	}

	mac := hmac.New(sha256.New, []byte(wh.HMACSecret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) { // constant-time
		return deny("bad_signature", http.StatusUnauthorized, "Unauthorized")
	}

	// Authenticated: only now is it safe to spend memory remembering it.
	// The table is keyed on the DECODED MAC bytes, never on the header text:
	// hex decoding is case-insensitive, so "ABCD" and "abcd" are the same
	// signature and must be the same replay-table entry.
	switch err := guard.remember(string(sig), ts+int64(tol/time.Second)+1); {
	case errors.Is(err, errWebhookReplay):
		return deny("replay", http.StatusConflict, "Duplicate delivery")
	case errors.Is(err, errReplayFull):
		return deny("replay_table_full", http.StatusServiceUnavailable, "Try again later")
	}
	return body, true
}

func (s *RESTServer) verifyLegacyWebhook(w http.ResponseWriter, r *http.Request, wh config.WebhookConfig) ([]byte, bool) {
	// Constant-time comparison of the whole header value, as before.
	expected := "Bearer " + wh.AuthToken
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
		s.webhookDeny(r, "bad_legacy_token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	s.legacyWarned.Do(func() {
		if s.Logger != nil {
			s.Logger.Warn("DEPRECATED: a webhook request authenticated with the legacy bearer token; migrate the sender to HMAC signing")
		}
	})
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return nil, false
		}
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}
