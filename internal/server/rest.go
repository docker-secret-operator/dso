package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"strconv"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/webui"
	"github.com/docker-secret-operator/dso/internal/webuiauth"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     checkWebSocketOrigin,
}

// checkWebSocketOrigin validates WebSocket origin headers to prevent CSWSH attacks
func checkWebSocketOrigin(r *http.Request) bool {
	// Get origin from request header (set by browser on cross-origin requests)
	origin := strings.TrimSpace(r.Header.Get("Origin"))

	// If no origin header, allow (same-origin requests don't send Origin)
	if origin == "" {
		return true
	}

	// Get the host from request
	host := r.Host
	if host == "" {
		return false
	}

	// Extract origin host (remove protocol)
	// origin format: "http://example.com:8080" or "https://example.com"
	originURL := origin
	if idx := strings.Index(originURL, "://"); idx != -1 {
		originURL = originURL[idx+3:]
	}

	// For loopback addresses, allow localhost variants
	if isLoopbackHost(host) {
		return isLoopbackHost(originURL)
	}

	// For non-loopback, origin must match host exactly
	return originURL == host
}

// isLoopbackHost checks if a host is a loopback address (localhost, 127.0.0.1, ::1)
func isLoopbackHost(host string) bool {
	// Remove port
	h := host
	if idx := strings.LastIndex(h, ":"); idx != -1 {
		h = h[:idx]
	}

	// Check for IPv6
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}

	// Check common loopback patterns
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "[::1]"
}

type WebhookPayload struct {
	Provider   string `json:"provider"`
	SecretName string `json:"secret_name"`
	EventType  string `json:"event_type"`
	Timestamp  string `json:"timestamp"`
}

// RESTServer handles administrative REST API requests
type RESTServer struct {
	Cache         *agent.SecretCache
	TriggerEngine *agent.TriggerEngine
	Config        *config.Config
	Logger        *zap.Logger
	Hub           *Hub
	EventStore    *EventStore
	Auth          *auth.Authenticator
	// WebUIAuth is optional. When non-nil, requests carrying a valid
	// dso_webui_session cookie are also accepted (in addition to a valid
	// DSO_AUTH_TOKEN bearer token). Neither mechanism weakens the other --
	// see internal/webuiauth's package doc.
	WebUIAuth *webuiauth.Manager
	// WebUI is optional. When non-nil, it serves the embedded WebUI static
	// assets (with SPA fallback) for any request whose path is neither
	// under /api nor exactly /health. It rides on this same listener --
	// there is no second HTTP server. Static assets are served without the
	// auth gate below (equivalent to any SPA's public shell); the app itself
	// enforces auth client-side, and every data-bearing endpoint stays
	// behind the /api prefix and the auth check.
	WebUI http.Handler
}

// secureHeaders wraps a handler and adds security response headers to every reply.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// maxRequestBodyBytes caps incoming request bodies to prevent memory exhaustion.
// WebSocket upgrade requests carry no body so they are exempt.
const maxRequestBodyBytes = 64 * 1024 // 64 KB

func (s *RESTServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// WebUI static assets: anything that isn't under /api and isn't /health
	// is delegated to the embedded static/SPA-fallback handler, before the
	// API auth gate below. The prefix check is deliberately "/api" (not
	// "/api/") so this can never be fooled by an /api-prefixed but
	// non-slash-separated path; every real API route below is registered
	// under "/api/..." so the two spaces are disjoint by construction. This
	// means an unknown path like /api/does-not-exist always falls through
	// to the switch below (and its default 404), never to the SPA handler.
	if s.WebUI != nil && r.URL.Path != "/health" && !strings.HasPrefix(r.URL.Path, "/api") {
		s.WebUI.ServeHTTP(w, r)
		return
	}

	// Cap request body size on all non-WebSocket endpoints to prevent OOM via
	// large payloads. A 64 KB limit is generous for any legitimate DSO payload.
	if r.URL.Path != "/api/events/ws" {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	}

	// Public endpoints that don't require authorization.
	// SEC-C3: /api/events/secret-update is intentionally NOT public. The webhook
	// endpoint must pass through the global DSO auth middleware first (defense in
	// depth), and then perform its own webhook-token check as a second factor.
	publicPaths := map[string]bool{
		"/health":          true,
		"/api/auth/login":  true,
		"/api/auth/logout": true,
	}

	isPublic := publicPaths[r.URL.Path]
	if !isPublic && !s.authorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch {
	case r.URL.Path == "/health":
		s.handleHealth(w, r)
	case r.URL.Path == "/api/auth/login" && r.Method == "POST":
		s.handleAuthLogin(w, r)
	case r.URL.Path == "/api/auth/logout" && r.Method == "POST":
		s.handleAuthLogout(w, r)
	case r.URL.Path == "/api/discovery":
		s.handleDiscovery(w, r)
	case r.URL.Path == "/api/config/raw":
		s.handleConfigRaw(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/secrets"):
		s.handleListSecrets(w, r)
	case r.URL.Path == "/api/events/ws":
		s.handleEventWS(w, r)
	case r.URL.Path == "/api/events/secret-update" && r.Method == "POST":
		s.handleSecretUpdate(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/events"):
		s.handleEvents(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/logs"):
		s.handleLogs(w, r)
	default:
		http.NotFound(w, r)
	}
}

// authorized implements the composed dual-auth gate: a request is allowed in
// if EITHER a valid DSO_AUTH_TOKEN bearer token OR (when the WebUI is
// enabled) a valid webui session cookie is present. This composes the two
// independent mechanisms rather than replacing either -- see
// internal/webuiauth's package doc for why they must coexist.
func (s *RESTServer) authorized(r *http.Request) bool {
	tokenOK := s.Auth == nil || s.Auth.GetToken() == ""
	if !tokenOK {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		token := strings.TrimPrefix(header, "Bearer ")
		tokenOK = s.Auth.Verify(token) == nil
	}
	if tokenOK {
		return true
	}

	if s.WebUIAuth != nil {
		if _, err := s.WebUIAuth.Validate(webuiauth.TokenFromRequest(r)); err == nil {
			return true
		}
	}

	return false
}

// handleAuthLogin authenticates the single configured webui operator
// identity and, on success, sets an HttpOnly session cookie. The token is
// never returned in the JSON body -- only via the Set-Cookie header -- so
// browser JS can never read or store it (no localStorage token code).
func (s *RESTServer) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.WebUIAuth == nil {
		http.Error(w, "WebUI is disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	sess, err := s.WebUIAuth.Login(creds.Username, creds.Password)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	http.SetCookie(w, s.WebUIAuth.SessionCookie(sess))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleAuthLogout invalidates the session tied to the request's cookie, if
// any, and clears the cookie in the browser.
func (s *RESTServer) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if s.WebUIAuth == nil {
		http.Error(w, "WebUI is disabled", http.StatusNotFound)
		return
	}
	s.WebUIAuth.Logout(webuiauth.TokenFromRequest(r))
	http.SetCookie(w, s.WebUIAuth.ExpiredCookie())
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleDiscovery reports which optional feature areas are enabled so the
// WebUI can decide what to render, without exposing secret values or raw
// config.
func (s *RESTServer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	webhookEnabled := false
	secretCount := 0
	if s.Config != nil {
		webhookEnabled = s.Config.Agent.Watch.Webhook.Enabled
		secretCount = len(s.Config.Secrets)
	}

	resp := map[string]interface{}{
		"webui_enabled":   s.WebUIAuth != nil,
		"webhook_enabled": webhookEnabled,
		"secret_count":    secretCount,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Error("Failed to encode discovery response", zap.Error(err))
	}
}

// handleConfigRaw returns a read-only, secret-redacted view of the loaded
// configuration. Provider credentials and any secret values are never
// present in *config.Config (they are fetched at rotation time, not stored),
// but we still explicitly avoid dumping s.Config.Providers verbatim in case
// a provider config later grows a credential field -- only the metadata the
// WebUI's read-only configuration view needs is surfaced.
func (s *RESTServer) handleConfigRaw(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.Config == nil {
		_, _ = w.Write([]byte("{}"))
		return
	}

	type secretView struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
	}
	secrets := make([]secretView, 0, len(s.Config.Secrets))
	for _, sec := range s.Config.Secrets {
		secrets = append(secrets, secretView{Name: sec.Name, Provider: sec.Provider})
	}

	providers := make([]string, 0, len(s.Config.Providers))
	for name := range s.Config.Providers {
		providers = append(providers, name)
	}

	resp := map[string]interface{}{
		"secrets":   secrets,
		"providers": providers,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Error("Failed to encode config/raw response", zap.Error(err))
	}
}

func (s *RESTServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, `{"status":"up"}`)
}

func (s *RESTServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	severity := r.URL.Query().Get("severity")

	w.Header().Set("Content-Type", "application/json")
	events := s.EventStore.GetLast(limit, severity)

	if len(events) == 0 {
		_, _ = w.Write([]byte("[]"))
		return
	}
	if err := json.NewEncoder(w).Encode(events); err != nil {
		s.Logger.Error("Failed to encode events response", zap.Error(err))
	}
}

func (s *RESTServer) handleEventWS(w http.ResponseWriter, r *http.Request) {
	if s.Hub.ClientCount() >= maxHubClients {
		http.Error(w, "too many active connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.Logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	client := &Client{
		hub:  s.Hub,
		conn: conn,
		send: make(chan Event, 256),
	}

	// Push last N events synchronously on connect
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	severity := r.URL.Query().Get("severity")

	// Register the client with the hub before starting pumps so writePump is the
	// sole goroutine that calls conn.WriteJSON — websocket.Conn is not goroutine-safe.
	s.Hub.register <- client
	go client.writePump()
	go client.readPump(r.Context())

	// Push historical events through the send channel (never directly on conn).
	initialEvents := s.EventStore.GetLast(limit, severity)
	for _, ev := range initialEvents {
		select {
		case client.send <- ev:
		default:
			// Client buffer full; skip remaining history rather than block.
		}
	}
}

func (s *RESTServer) handleSecretUpdate(w http.ResponseWriter, r *http.Request) {
	if s.Config == nil || !s.Config.Agent.Watch.Webhook.Enabled {
		http.Error(w, "Webhooks are disabled", http.StatusForbidden)
		return
	}
	if s.Config.Agent.Watch.Webhook.AuthToken == "" {
		http.Error(w, "Webhook auth token is required when webhooks are enabled", http.StatusForbidden)
		return
	}

	// SEC-C4: use a constant-time comparison to avoid leaking the webhook token
	// via response timing. The endpoint is reachable from external systems, so a
	// byte-by-byte short-circuiting comparison is a directly exploitable timing
	// oracle. crypto/subtle.ConstantTimeCompare is already the project-wide pattern
	// (see internal/auth/auth.go, pkg/provider/load.go).
	authHeader := r.Header.Get("Authorization")
	expectedToken := "Bearer " + s.Config.Agent.Watch.Webhook.AuthToken
	if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expectedToken)) != 1 {
		observability.BackendFailuresTotal.WithLabelValues("webhook", "unauthorized").Inc()
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload WebhookPayload
	if ierr := json.NewDecoder(r.Body).Decode(&payload); ierr != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	s.Logger.Info("Webhook received", zap.String("secret", payload.SecretName), zap.String("provider", payload.Provider))

	var targetSecret *config.SecretMapping
	for _, sec := range s.Config.Secrets {
		if sec.Name == payload.SecretName {
			targetSecret = &sec
			break
		}
	}

	if targetSecret == nil {
		http.Error(w, "Secret not configured", http.StatusNotFound)
		return
	}

	pName := payload.Provider
	if pName == "" {
		pName = targetSecret.Provider
	}
	if pName == "" {
		// Default to first provider
		for k := range s.Config.Providers {
			pName = k
			break
		}
	}
	pCfg, ok := s.Config.Providers[pName]
	if !ok {
		http.Error(w, "Provider not found", http.StatusBadRequest)
		return
	}

	err := s.TriggerEngine.HandleWebhook(pName, pCfg, *targetSecret, payload.Timestamp)
	if err != nil {
		s.Logger.Error("Webhook execution failed", zap.Error(err), zap.String("secret", targetSecret.Name))
		http.Error(w, "Internal rotation failure", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, `{"status":"accepted"}`)
}

// handleLogs returns recent runtime events from the in-memory EventStore.
// OPS-C3: previously this returned a hardcoded {"status":"up"} stub that
// fabricated nothing useful. It now serves the same real event data as
// /api/events so operators get actual runtime logs instead of a placeholder.
func (s *RESTServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	severity := r.URL.Query().Get("severity")

	w.Header().Set("Content-Type", "application/json")

	if s.EventStore == nil {
		_, _ = w.Write([]byte("[]"))
		return
	}

	events := s.EventStore.GetLast(limit, severity)
	if len(events) == 0 {
		_, _ = w.Write([]byte("[]"))
		return
	}
	if err := json.NewEncoder(w).Encode(events); err != nil {
		s.Logger.Error("Failed to encode logs response", zap.Error(err))
	}
}

func (s *RESTServer) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	// For production, we would want more detail here.
	// For now, listing keys in the cache.
	w.Header().Set("Content-Type", "application/json")

	if s.Cache == nil {
		http.Error(w, "Cache not initialized", http.StatusInternalServerError)
		return
	}

	keys := s.Cache.ListKeys()

	type SecretResponse struct {
		Name            string `json:"name"`
		Provider        string `json:"provider"`
		Status          string `json:"status"`
		LastSyncedAt    string `json:"last_synced_at"`
		LastUpdatedAt   string `json:"last_updated_at"`
		LastError       string `json:"last_error,omitempty"`
		InjectionType   string `json:"injection_type"`
		MountPath       string `json:"mount_path,omitempty"`
		Version         string `json:"version,omitempty"`
		RotationEnabled bool   `json:"rotation_enabled"`
		AutoSyncEnabled bool   `json:"auto_sync_enabled"`
	}

	// Build a lookup map from config for rotation and injection metadata.
	type secretMeta struct {
		rotationEnabled bool
		injectionType   string
	}
	configMeta := make(map[string]secretMeta)
	if s.Config != nil {
		for _, sec := range s.Config.Secrets {
			injType := "env"
			if sec.Inject.Type != "" {
				injType = sec.Inject.Type
			}
			configMeta[sec.Name] = secretMeta{
				rotationEnabled: sec.Rotation.Enabled,
				injectionType:   injType,
			}
		}
	}

	res := []SecretResponse{}
	for _, k := range keys {
		parts := strings.SplitN(k, ":", 2)
		prov := "unknown"
		name := k
		if len(parts) == 2 {
			prov = parts[0]
			name = parts[1]
		}

		meta := configMeta[name]
		res = append(res, SecretResponse{
			Name:            name,
			Provider:        prov,
			Status:          "synced", // key is present in cache → it was successfully synced
			InjectionType:   meta.injectionType,
			RotationEnabled: meta.rotationEnabled,
			// LastSyncedAt, LastUpdatedAt, Version omitted — not yet tracked; omitempty prevents fabrication
		})
	}

	// OPS-C3: never fabricate secret state. When the live cache is empty (the
	// normal state on first startup or in Local Mode) return an empty list rather
	// than injecting fictional secrets. Reviewers and operators must see real data.

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"active_secrets": res,
		"total_count":    len(res),
	}); err != nil {
		s.Logger.Error("Failed to encode secrets list response", zap.Error(err))
	}
}

// StartRESTServer starts the REST API server on the specified address with secure
// timeouts and returns a shutdown function.
//
// SEC-C1: if the server is asked to bind to a non-loopback address while
// DSO_AUTH_TOKEN is unset, startup fails with an error instead of silently
// serving an unauthenticated secrets API to the network. Loopback and
// unix-socket deployments may still run without a token for local development.
// The shutdown function should be called on graceful agent shutdown to properly
// close connections.
const (
	minAuthTokenBytes = 16  // brute-force floor
	maxAuthTokenBytes = 512 // prevents constant-time compare from touching megabytes
)

func StartRESTServer(ctx context.Context, addr string, cache *agent.SecretCache, triggerEngine *agent.TriggerEngine, cfg *config.Config, logger *zap.Logger) (func(), error) {
	if bindsPublic(addr) && os.Getenv("DSO_AUTH_TOKEN") == "" {
		return nil, fmt.Errorf(
			"refusing to start REST API on non-loopback address %q without authentication: "+
				"set DSO_AUTH_TOKEN, or bind the API to a loopback address (e.g. 127.0.0.1)", addr)
	}

	if token := os.Getenv("DSO_AUTH_TOKEN"); token != "" {
		if len(token) < minAuthTokenBytes {
			return nil, fmt.Errorf(
				"DSO_AUTH_TOKEN is too short (%d bytes); minimum is %d bytes — use a random token of at least 16 characters",
				len(token), minAuthTokenBytes)
		}
		if len(token) > maxAuthTokenBytes {
			return nil, fmt.Errorf(
				"DSO_AUTH_TOKEN exceeds maximum length (%d bytes); maximum is %d bytes",
				len(token), maxAuthTokenBytes)
		}
	}

	hub := NewHub(logger)
	go hub.Run(ctx)

	eventStore := NewEventStore(500, hub)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-observability.EventStream:
				if !ok {
					return
				}
				eventStore.Add(ev)
			}
		}
	}()

	restServer := &RESTServer{
		Cache:         cache,
		TriggerEngine: triggerEngine,
		Config:        cfg,
		Logger:        logger,
		Hub:           hub,
		EventStore:    eventStore,
		Auth:          auth.NewAuthenticator(),
	}

	// Only construct a webuiauth.Manager (and thereby expose /api/auth/*)
	// when the operator has explicitly enabled the WebUI in config. This
	// keeps existing deployments' behavior byte-for-byte identical unless
	// they opt in.
	if cfg != nil && cfg.WebUI.Enabled {
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
		mgr.Secure = bindsPublic(addr)
		restServer.WebUIAuth = mgr

		// Mount the embedded WebUI static assets onto this same listener.
		// WebUIConfig.ListenAddress is documented as "if empty, mounts onto
		// the existing REST API listener" -- that is the only mode
		// implemented here. A second, independently-addressed listener is
		// explicitly out of scope for this integration (DSO is one binary,
		// one process, one mux), so a non-empty ListenAddress is currently
		// ignored other than being available for a future iteration to act
		// on; it does not cause a second http.Server to start.
		assets, err := webui.Assets()
		if err != nil {
			logger.Warn("Failed to load embedded WebUI assets; WebUI will not be served", zap.Error(err))
		} else {
			restServer.WebUI = webui.NewHandler(assets)
		}
	}

	// Rate-limit: 10 requests/second sustained, burst of 30.
	// This is generous for any legitimate DSO client while stopping floods.
	rl := newRateLimitMiddleware(10, 30)

	mux := http.NewServeMux()
	mux.Handle("/", rl.wrap(secureHeaders(restServer)))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	logger.Info("Starting secure REST API server",
		zap.String("addr", addr),
		zap.Duration("read_timeout", server.ReadTimeout),
		zap.Duration("write_timeout", server.WriteTimeout))

	// Launch server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST API server failed", zap.Error(err))
		}
	}()

	// Return shutdown function that closes server on context cancellation
	// #nosec G118 -- this goroutine only runs after ctx.Done(), so deriving the
	// shutdown timeout from ctx would give Shutdown() zero grace period. A
	// fresh Background() context is required.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("REST API server shutdown error", zap.Error(err))
		}
	}()

	// Return explicit shutdown function for manual control
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("REST API server shutdown error", zap.Error(err))
		}
	}, nil
}

func bindsPublic(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}
