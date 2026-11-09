package server

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/webui"
	"github.com/docker-secret-operator/dso/internal/webuiauth"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// accessWiring is the part of the REST server's security posture that is
// derived from configuration, the DSO_AUTH_TOKEN environment (via the
// Authenticator) and the bind address. It is built by buildAccessWiring and
// applied to a RESTServer by applyAccessWiring so the production wiring in
// StartRESTServer can be tested without opening a listener or touching
// /var/run/dso.
//
// It deliberately re-derives nothing about authentication itself: whether a
// bearer token is configured is asked of the same *auth.Authenticator that the
// RESTServer later uses to verify requests, so "legacy open mode" can never
// disagree with the gate in RESTServer.authenticate.
type accessWiring struct {
	// LegacyOpen is true when the API is intentionally unauthenticated: no
	// bearer token and no WebUI. StartRESTServer only permits this on a
	// loopback bind.
	LegacyOpen bool

	AllowedOrigins []string
	HostGuard      *hostGuard
	WebUIAuth      *webuiauth.Manager
	WebUI          http.Handler
}

// buildAccessWiring computes the access configuration. It has no side effects
// beyond logging, so errors can be returned before the server allocates
// anything else.
//
// Host and Origin settings are independent:
//
//   - webui.allowed_origins only widens the CSRF origin check (RESTServer.
//     originAllowed). It never enables Host-header enforcement by itself.
//   - Host-header enforcement (DNS-rebinding defense) is enabled when the API
//     is in legacy open mode, or when the operator sets webui.allowed_hosts.
//     When enabled, the allowlist is loopback names + allowed_hosts + the hosts
//     of allowed_origins (so a single reverse-proxy origin entry also covers
//     legacy mode behind that proxy).
func buildAccessWiring(cfg *config.Config, authn *auth.Authenticator, addr string, logger *zap.Logger) (*accessWiring, error) {
	w := &accessWiring{}
	webUIEnabled := cfg != nil && cfg.WebUI.Enabled
	w.LegacyOpen = !authn.Enabled() && !webUIEnabled

	var allowedHosts []string
	if cfg != nil {
		w.AllowedOrigins = cfg.WebUI.AllowedOrigins
		allowedHosts = cfg.WebUI.AllowedHosts
	}

	if w.LegacyOpen || len(allowedHosts) > 0 {
		w.HostGuard = newHostGuard(allowedHosts, w.AllowedOrigins)
		logger.Info("REST API Host header allowlist active (loopback names are always accepted)",
			zap.Strings("allowed_hosts", w.HostGuard.hosts()))
	}
	if w.LegacyOpen {
		logger.Warn("REST API has no credential configured (no DSO_AUTH_TOKEN and WebUI disabled); requests are served without authentication only on a loopback bind and only when " +
			envAllowUnauthenticatedLoopback + "=1, otherwise every protected route returns 401. Set DSO_AUTH_TOKEN or enable the WebUI.")
	}

	if !webUIEnabled {
		return w, nil
	}

	idleTimeout := webuiauth.DefaultIdleTimeout
	if cfg.WebUI.SessionIdleTimeout != "" {
		if d, err := time.ParseDuration(cfg.WebUI.SessionIdleTimeout); err == nil {
			idleTimeout = d
		} else {
			logger.Warn("Invalid webui.session_idle_timeout, using default",
				zap.String("value", cfg.WebUI.SessionIdleTimeout), zap.Error(err))
		}
	}
	mgr := webuiauth.NewManager(cfg.WebUI.Username, cfg.WebUI.PasswordHash, idleTimeout)
	if v := cfg.WebUI.SessionMaxLifetime; v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			mgr.SetMaxLifetime(d)
		} else {
			logger.Warn("Invalid webui.session_max_lifetime, using default",
				zap.String("value", v))
		}
	}
	if err := mgr.SetTrustedProxies(cfg.WebUI.TrustedProxies); err != nil {
		return nil, fmt.Errorf("invalid webui.trusted_proxies: %w", err)
	}
	for _, w := range mgr.TrustedProxyWarnings() {
		logger.Warn(w)
	}
	secure, warning := resolveCookieSecure(cfg.WebUI.CookieSecure, addr)
	mgr.Secure = secure
	if warning != "" {
		logger.Warn(warning)
	}
	w.WebUIAuth = mgr

	// Mount the embedded WebUI static assets onto this same listener.
	// WebUIConfig.ListenAddress is documented as "if empty, mounts onto the
	// existing REST API listener" -- that is the only mode implemented here. A
	// second, independently-addressed listener is explicitly out of scope (DSO
	// is one binary, one process, one mux), so a non-empty ListenAddress is
	// currently ignored; it does not cause a second http.Server to start.
	assets, err := webui.Assets()
	if err != nil {
		logger.Warn("Failed to load embedded WebUI assets; WebUI will not be served", zap.Error(err))
	} else {
		w.WebUI = webui.NewHandler(assets)
	}
	return w, nil
}

// applyAccessWiring copies the computed access configuration onto s.
func (s *RESTServer) applyAccessWiring(w *accessWiring) {
	s.AllowedOrigins = w.AllowedOrigins
	s.HostGuard = w.HostGuard
	s.WebUIAuth = w.WebUIAuth
	s.WebUI = w.WebUI
}

// envAllowUnauthenticatedLoopback is the explicit opt-in that restores the
// pre-M1 legacy open mode (F-E1): with no DSO_AUTH_TOKEN and the WebUI
// disabled, the REST API on a loopback bind serves protected routes to any
// local caller. Accepted values are those of strconv.ParseBool.
const envAllowUnauthenticatedLoopback = "DSO_ALLOW_UNAUTHENTICATED_LOOPBACK"

// unauthenticatedLoopbackAllowed reports whether the operator explicitly
// opted in to unauthenticated loopback REST access.
func unauthenticatedLoopbackAllowed() bool {
	v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(envAllowUnauthenticatedLoopback)))
	return err == nil && v
}

// enforceCredentialPolicy applies F-E1 (fail closed): when the wiring is in
// legacy open mode (no bearer token, no WebUI), anonymous requests to
// protected routes are rejected unless the operator set
// DSO_ALLOW_UNAUTHENTICATED_LOOPBACK. StartRESTServerSupervised calls this
// after applyAccessWiring; it is a separate step so the wiring helpers keep
// describing the credential configuration while this decides the deployment
// policy. Public routes (/health, login/logout, the HMAC-signed webhook) are
// unaffected.
//
// The decision is assigned, never inherited: allowAnonymous is reset to false
// first, so whatever state the RESTServer was in beforehand, the only outcome
// that leaves anonymous access open is legacy-open wiring plus the explicit
// opt-in.
func (s *RESTServer) enforceCredentialPolicy(w *accessWiring, logger *zap.Logger) {
	s.allowAnonymous = false
	if !w.LegacyOpen {
		return
	}
	if unauthenticatedLoopbackAllowed() {
		s.allowAnonymous = true
		logger.Warn("SECURITY: " + envAllowUnauthenticatedLoopback + " is set: the REST API serves every protected route WITHOUT authentication to any local process on this host. Set DSO_AUTH_TOKEN instead.")
		return
	}
	logger.Error("REST API has no credential configured; protected routes will return 401. Set DSO_AUTH_TOKEN (or enable the WebUI). To restore the old unauthenticated loopback behaviour, set " + envAllowUnauthenticatedLoopback + "=1 (not recommended).")
}

// hosts returns the explicitly allowed (non-loopback) hostnames, sorted, for
// startup logging.
func (g *hostGuard) hosts() []string {
	out := make([]string, 0, len(g.allowed))
	for h := range g.allowed {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
