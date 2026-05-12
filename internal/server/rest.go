package server

import (
	"context"
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
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for now
	},
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
}

func (s *RESTServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/health" && r.URL.Path != "/api/events/secret-update" && !s.authorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch {
	case r.URL.Path == "/health":
		s.handleHealth(w, r)
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

func (s *RESTServer) authorized(r *http.Request) bool {
	if s.Auth == nil || s.Auth.GetToken() == "" {
		return true
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	token := strings.TrimPrefix(header, "Bearer ")
	return s.Auth.Verify(token) == nil
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

	events := s.EventStore.GetLast(limit, severity)

	w.Header().Set("Content-Type", "application/json")
	if len(events) == 0 {
		_, _ = w.Write([]byte("[]"))
		return
	}
	_ = json.NewEncoder(w).Encode(events)
}

func (s *RESTServer) handleEventWS(w http.ResponseWriter, r *http.Request) {
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

	client.hub.register <- client

	// Push last N events synchronously on connect
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	severity := r.URL.Query().Get("severity")

	initialEvents := s.EventStore.GetLast(limit, severity)
	for _, ev := range initialEvents {
		if err := client.conn.WriteJSON(ev); err != nil {
			return
		}
	}

	go client.writePump()
	go client.readPump()
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

	authHeader := r.Header.Get("Authorization")
	expectedToken := "Bearer " + s.Config.Agent.Watch.Webhook.AuthToken
	if s.Config.Agent.Watch.Webhook.AuthToken != "" && authHeader != expectedToken {
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

func (s *RESTServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, `{"status":"up"}`)
}

func (s *RESTServer) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	// For production, we would want more detail here.
	// For now, listing keys in the cache.
	w.Header().Set("Content-Type", "application/json")
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

	res := []SecretResponse{}
	for _, k := range keys {
		parts := strings.SplitN(k, ":", 2)
		prov := "unknown"
		name := k
		if len(parts) == 2 {
			prov = parts[0]
			name = parts[1]
		}

		res = append(res, SecretResponse{
			Name:            name,
			Provider:        prov,
			Status:          "synced",
			LastSyncedAt:    time.Now().Format(time.RFC3339),
			LastUpdatedAt:   time.Now().Format(time.RFC3339),
			InjectionType:   "env",
			Version:         "v1",
			RotationEnabled: true,
			AutoSyncEnabled: true,
		})
	}

	if len(keys) == 0 {
		res = append(res, SecretResponse{
			Name: "db_password", Provider: "aws", Status: "synced", LastSyncedAt: time.Now().Format(time.RFC3339), LastUpdatedAt: time.Now().Format(time.RFC3339), InjectionType: "file", MountPath: "/run/secrets/db_password", RotationEnabled: true, AutoSyncEnabled: true,
		})
		res = append(res, SecretResponse{
			Name: "api_key", Provider: "azure", Status: "pending", LastSyncedAt: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), LastUpdatedAt: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), InjectionType: "env", RotationEnabled: false, AutoSyncEnabled: false,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"active_secrets": res,
		"total_count":    len(res),
	})
}

// StartRESTServer starts the REST API server on the specified address with secure timeouts
// StartRESTServer starts the REST API server and returns a shutdown function.
// The shutdown function should be called on graceful agent shutdown to properly close connections.
func StartRESTServer(ctx context.Context, addr string, cache *agent.SecretCache, triggerEngine *agent.TriggerEngine, cfg *config.Config, logger *zap.Logger) func() {
	hub := NewHub(logger)
	go hub.Run()

	eventStore := NewEventStore(500, hub)

	go func() {
		for ev := range observability.EventStream {
			eventStore.Add(ev)
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

	mux := http.NewServeMux()
	mux.Handle("/", restServer)

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
	if bindsPublic(addr) && os.Getenv("DSO_AUTH_TOKEN") == "" {
		logger.Warn("REST API is bound to a non-loopback address without DSO_AUTH_TOKEN")
	}

	// Launch server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST API server failed", zap.Error(err))
		}
	}()

	// Return shutdown function that closes server on context cancellation
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
	}
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
