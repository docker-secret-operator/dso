package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"strconv"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/api"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/execution"
	"github.com/docker-secret-operator/dso/internal/plugins"
	"github.com/docker-secret-operator/dso/internal/scheduler"
	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/docker/docker/client"
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
// Properly handles IPv6 addresses with ports like [::1]:8471
func isLoopbackHost(host string) bool {
	// Use net.SplitHostPort for proper host:port splitting (handles IPv6 correctly)
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		// No port, use full host
		h = host
	}

	// Try to parse as IP address
	ip := net.ParseIP(strings.TrimPrefix(strings.TrimSuffix(h, "]"), "["))
	if ip != nil {
		return ip.IsLoopback()
	}

	// Fall back to hostname checks
	return h == "localhost" || h == "" // Empty host is invalid
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
	ConfigAPI     *api.ConfigAPI
	DiscoveryAPI  *api.DiscoveryAPI
	// Phase 4 handlers
	DashboardHandler     *api.DashboardHandler
	ExecutionHandler     *api.ExecutionHandler
	GovernanceHandler    *api.GovernanceHandler
	OperationsDashboard  *api.OperationsDashboardHandler
	OperationsDLQ        *api.OperationsDLQHandler
	OperationsTrace      *api.OperationsTraceHandler
	ReviewHandler        *api.ReviewHandler
	OrchestrationHandler *api.OrchestrationHandler
	// Phase 4.6 visibility handlers
	ResourceMonitor       *api.ResourceMonitor
	MetricsHistoryHandler *api.MetricsHistoryHandler
	AuditExplorerHandler  *api.AuditExplorerHandler
	StaleWorkHandler      *api.StaleWorkHandler
	MetricsCollector      *api.MetricsCollector
	MetricsAPIHandler     *api.MetricsAPIHandler
	// Phase 5 authentication
	AuthenticationService   *auth.AuthenticationService
	AuthHandler             *api.AuthHandler
	UserManagementHandler   *api.UserManagementHandler
	SessionCleanupManager   *auth.SessionCleanupManager
	PermissionMatrix        *auth.PermissionMatrix
	AuthorizationMiddleware *auth.AuthorizationMiddleware
	// Phase 5.7 security operations
	SecurityHandler *api.SecurityHandler
	// Phase 5.8 alert rules and notification engine
	AlertHandler *api.AlertHandler
	// Phase 5.9 backup and restore
	BackupHandler *api.BackupHandler
	// Phase 5.10 plugins and extensions
	PluginHandler  *api.PluginHandler
	PluginRegistry *plugins.Registry
	PluginManager  *plugins.Manager
	EventBus       *plugins.EventBus
	// Phase 5.11 webhooks and integrations
	IntegrationHandler *api.IntegrationHandler
	IntegrationManager *plugins.IntegrationManager
	// Phase 5.12 internal scheduler
	SchedulerHandler *api.SchedulerHandler
	Scheduler        interface{} // *scheduler.Scheduler
	// Phase 6 intelligence and governance handlers
	RecommendationHandler *api.RecommendationHandler
	DriftHandler          *api.DriftHandler
	ForecastHandler       *api.ForecastHandler
	// startup time for uptime reporting
	startTime time.Time
}

func (s *RESTServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check RBAC authorization for all non-public routes
	// This happens after authentication middleware has injected the user into context
	if !s.checkAuthorization(w, r) {
		return
	}

	// FG4: block all routes for users who must change their password,
	// except the three endpoints they need to do so.
	if !s.checkPasswordChangeRequired(w, r) {
		return
	}

	switch {
	case r.URL.Path == "/health":
		s.handleHealth(w, r)
	// Authentication endpoints
	case strings.HasPrefix(r.URL.Path, "/api/auth"):
		if s.AuthHandler != nil {
			s.AuthHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Auth API not initialized", http.StatusInternalServerError)
		}
	// Configuration endpoints
	case r.URL.Path == "/api/config" && r.Method == "GET":
		if s.ConfigAPI != nil {
			s.ConfigAPI.HandleGetConfig(w, r)
		} else {
			http.Error(w, "Configuration API not initialized", http.StatusInternalServerError)
		}
	case r.URL.Path == "/api/config/raw" && r.Method == "GET":
		if s.ConfigAPI != nil {
			s.ConfigAPI.HandleGetRawConfig(w, r)
		} else {
			http.Error(w, "Configuration API not initialized", http.StatusInternalServerError)
		}
	case r.URL.Path == "/api/config/providers" && r.Method == "GET":
		if s.ConfigAPI != nil {
			s.ConfigAPI.HandleGetProviders(w, r)
		} else {
			http.Error(w, "Configuration API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/config/providers/") && strings.HasSuffix(r.URL.Path, "/test") && r.Method == "POST":
		if s.ConfigAPI != nil {
			// Extract provider name from path: /api/config/providers/{provider}/test
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 5 {
				providerName := parts[4]
				s.ConfigAPI.HandleTestProvider(w, r, providerName)
			} else {
				http.Error(w, "Invalid provider path", http.StatusBadRequest)
			}
		} else {
			http.Error(w, "Configuration API not initialized", http.StatusInternalServerError)
		}
	// Discovery endpoints
	case r.URL.Path == "/api/discovery/docker" && r.Method == "GET":
		if s.DiscoveryAPI != nil {
			s.DiscoveryAPI.HandleGetContainers(w, r)
		} else {
			http.Error(w, "Discovery API not initialized", http.StatusInternalServerError)
		}
	case r.URL.Path == "/api/discovery/docker/mappings" && r.Method == "GET":
		if s.DiscoveryAPI != nil {
			s.DiscoveryAPI.HandleGetMappings(w, r)
		} else {
			http.Error(w, "Discovery API not initialized", http.StatusInternalServerError)
		}
	case r.URL.Path == "/api/discovery/refresh" && r.Method == "POST":
		if s.DiscoveryAPI != nil {
			s.DiscoveryAPI.HandleRefresh(w, r)
		} else {
			http.Error(w, "Discovery API not initialized", http.StatusInternalServerError)
		}
	case r.URL.Path == "/api/discovery/metrics" && r.Method == "GET":
		if s.DiscoveryAPI != nil {
			s.DiscoveryAPI.HandleGetMetrics(w, r)
		} else {
			http.Error(w, "Discovery API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/secrets"):
		s.handleSecrets(w, r)
	case r.URL.Path == "/api/events/ws":
		s.handleEventWS(w, r)
	case r.URL.Path == "/api/events/secret-update" && r.Method == "POST":
		s.handleSecretUpdate(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/events"):
		s.handleEvents(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/logs"):
		s.handleLogs(w, r)
	// Phase 4 API routes
	case strings.HasPrefix(r.URL.Path, "/api/dashboard"):
		if s.DashboardHandler != nil {
			s.DashboardHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Dashboard API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/executions"):
		if s.ExecutionHandler != nil {
			s.ExecutionHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Execution API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/governance") || strings.HasPrefix(r.URL.Path, "/api/drafts") || strings.HasPrefix(r.URL.Path, "/api/approvals"):
		if s.GovernanceHandler != nil {
			s.GovernanceHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Governance API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/operations"):
		// Operations endpoints router
		if strings.HasPrefix(r.URL.Path, "/api/operations/dashboard") {
			if s.OperationsDashboard != nil {
				s.OperationsDashboard.ServeHTTP(w, r)
			} else {
				http.Error(w, "Operations Dashboard not initialized", http.StatusInternalServerError)
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/operations/dlq") {
			if s.OperationsDLQ != nil {
				s.OperationsDLQ.ServeHTTP(w, r)
			} else {
				http.Error(w, "Operations DLQ not initialized", http.StatusInternalServerError)
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/operations/trace") {
			if s.OperationsTrace != nil {
				s.OperationsTrace.ServeHTTP(w, r)
			} else {
				http.Error(w, "Operations Trace not initialized", http.StatusInternalServerError)
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/operations/resources") {
			if s.ResourceMonitor != nil {
				s.ResourceMonitor.ServeHTTP(w, r)
			} else {
				http.Error(w, "Resource Monitor not initialized", http.StatusInternalServerError)
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/operations/metrics-history") {
			if s.MetricsHistoryHandler != nil {
				s.MetricsHistoryHandler.ServeHTTP(w, r)
			} else {
				http.Error(w, "Metrics History not initialized", http.StatusInternalServerError)
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/operations/stale") {
			if s.StaleWorkHandler != nil {
				s.StaleWorkHandler.ServeHTTP(w, r)
			} else {
				http.Error(w, "Stale Work not initialized", http.StatusInternalServerError)
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/operations/alerts") {
			if s.OperationsDashboard != nil {
				s.OperationsDashboard.ServeHTTP(w, r)
			} else {
				http.Error(w, "Operations Alerts not initialized", http.StatusInternalServerError)
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/operations/recovery") {
			if s.OperationsDashboard != nil {
				s.OperationsDashboard.ServeHTTP(w, r)
			} else {
				http.Error(w, "Operations Recovery not initialized", http.StatusInternalServerError)
			}
		} else {
			http.NotFound(w, r)
		}
	case strings.HasPrefix(r.URL.Path, "/api/users"):
		if s.UserManagementHandler != nil {
			s.UserManagementHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "User management not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/sessions"):
		if s.UserManagementHandler != nil {
			s.UserManagementHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Session management not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/admin/sessions"):
		if s.UserManagementHandler != nil {
			s.UserManagementHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Admin session management not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/audit"):
		if s.AuditExplorerHandler != nil {
			s.AuditExplorerHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Audit Explorer not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/reviews"):
		if s.ReviewHandler != nil {
			s.ReviewHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Review API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/orchestration"):
		if s.OrchestrationHandler != nil {
			s.OrchestrationHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Orchestration API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/recommendations"):
		if s.RecommendationHandler != nil {
			s.RecommendationHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Recommendation API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/drift"):
		if s.DriftHandler != nil {
			s.DriftHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Drift API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/forecasts"):
		if s.ForecastHandler != nil {
			s.ForecastHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Forecast API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/metrics"):
		if s.MetricsAPIHandler != nil {
			s.MetricsAPIHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Metrics API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/security"):
		if s.SecurityHandler != nil {
			s.SecurityHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Security API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/alerts"):
		if s.AlertHandler != nil {
			s.AlertHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Alert API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/backups"):
		if s.BackupHandler != nil {
			s.BackupHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Backup API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/plugins"):
		if s.PluginHandler != nil {
			s.PluginHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Plugin API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/integrations"):
		if s.IntegrationHandler != nil {
			s.IntegrationHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Integration API not initialized", http.StatusInternalServerError)
		}
	case strings.HasPrefix(r.URL.Path, "/api/scheduler"):
		if s.SchedulerHandler != nil {
			s.SchedulerHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Scheduler API not initialized", http.StatusInternalServerError)
		}
	default:
		http.NotFound(w, r)
	}
}

// checkAuthorization verifies both authentication and RBAC authorization
func (s *RESTServer) checkAuthorization(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path

	// Check RBAC permissions
	// Check for exact path match
	requiredRoles, exists := s.PermissionMatrix.Rules[path]

	// Check for prefix matches for dynamic routes
	if !exists {
		bestMatch := ""
		// Check all rules for prefix matches to find the longest matching prefix
		for rulePath, roles := range s.PermissionMatrix.Rules {
			if strings.HasPrefix(path, rulePath) && rulePath != "/" {
				if len(rulePath) > len(bestMatch) {
					bestMatch = rulePath
					requiredRoles = roles
					exists = true
				}
			}
		}
	}

	// If no rules found, allow (shouldn't happen with proper matrix setup)
	if !exists {
		return true
	}

	// Public endpoints have empty required roles
	if len(requiredRoles) == 0 {
		return true
	}

	// Get user from context (set by authentication middleware)
	user := auth.CurrentUser(r.Context())
	if user == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return false
	}

	// Check if user has required role
	if !auth.CanAccessEndpoint(user.Role, requiredRoles...) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
		return false
	}

	return true
}

// checkPasswordChangeRequired returns false (and writes 403) when the authenticated user
// must change their password but is accessing a non-exempt endpoint.
func (s *RESTServer) checkPasswordChangeRequired(w http.ResponseWriter, r *http.Request) bool {
	user := auth.CurrentUser(r.Context())
	if user == nil || !user.MustChangePassword {
		return true
	}

	path := r.URL.Path
	exempt := path == "/api/auth/change-password" ||
		path == "/api/auth/logout" ||
		path == "/api/auth/me" ||
		path == "/health"
	if exempt {
		return true
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":                "password change required",
		"must_change_password": true,
	})
	return false
}

// ServerVersion is set at link time via -ldflags; defaults to "dev"
var ServerVersion = "dev"

func (s *RESTServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	uptimeDur := time.Since(s.startTime)
	uptime := uptimeDur.Round(time.Second).String()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "up",
		"version":        ServerVersion,
		"uptime":         uptime,
		"uptime_seconds": int64(uptimeDur.Seconds()),
		"goroutines":     runtime.NumGoroutine(),
		"memory_mb":      float64(memStats.Alloc) / (1024 * 1024),
		"memory_sys_mb":  float64(memStats.Sys) / (1024 * 1024),
		"num_gc":         memStats.NumGC,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	})
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
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 10000 {
			limit = l
		}
	}

	severity := r.URL.Query().Get("severity")
	validSeverities := map[string]bool{
		"info":     true,
		"warning":  true,
		"error":    true,
		"critical": true,
	}
	if severity != "" && !validSeverities[severity] {
		http.Error(w, "Invalid severity value", http.StatusBadRequest)
		return
	}

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
	// Fix C3: use constant-time comparison to prevent timing attacks
	if s.Config.Agent.Watch.Webhook.AuthToken != "" && subtle.ConstantTimeCompare([]byte(authHeader), []byte(expectedToken)) != 1 {
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
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_ = ctx

	q := r.URL.Query()

	// Parse limit (1–1000)
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	level := strings.ToLower(q.Get("level"))
	component := strings.ToLower(q.Get("component"))
	since := q.Get("since")

	var sinceTime time.Time
	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			sinceTime = t
		}
	}

	events := s.EventStore.GetLast(limit*2, "") // over-fetch then filter
	type logEntry struct {
		Timestamp string                 `json:"timestamp"`
		Level     string                 `json:"level"`
		Message   string                 `json:"message"`
		Component string                 `json:"component,omitempty"`
		Context   map[string]interface{} `json:"context,omitempty"`
	}

	entries := make([]logEntry, 0, limit)
	for _, ev := range events {
		if len(entries) >= limit {
			break
		}
		// Map event fields to log entry
		ts, _ := ev["timestamp"].(string)
		msg, _ := ev["message"].(string)
		if msg == "" {
			if a, ok := ev["action"].(string); ok {
				msg = a
			}
		}
		lvl := "info"
		if s, ok := ev["severity"].(string); ok && s != "" {
			lvl = s
		}
		comp := ""
		if a, ok := ev["action"].(string); ok {
			comp = a
		}

		// Apply filters
		if level != "" && lvl != level {
			continue
		}
		if component != "" && !strings.Contains(strings.ToLower(comp), component) {
			continue
		}
		if !sinceTime.IsZero() && ts != "" {
			if t, err := time.Parse(time.RFC3339, ts); err == nil && t.Before(sinceTime) {
				continue
			}
		}

		ctx := make(map[string]interface{})
		for k, v := range ev {
			if k != "timestamp" && k != "message" && k != "severity" && k != "action" {
				ctx[k] = v
			}
		}

		entries = append(entries, logEntry{
			Timestamp: ts,
			Level:     lvl,
			Message:   msg,
			Component: comp,
			Context:   ctx,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
		"count":   len(entries),
	}); err != nil {
		s.Logger.Error("failed to encode logs response", zap.Error(err))
	}
}

type secretResponse struct {
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	Status          string   `json:"status"`
	LastRotated     string   `json:"last_rotated,omitempty"`
	LastSyncedAt    string   `json:"last_synced_at,omitempty"`
	LastUpdatedAt   string   `json:"last_updated_at,omitempty"`
	LastError       string   `json:"last_error,omitempty"`
	InjectionType   string   `json:"injection_type"`
	MountPath       string   `json:"mount_path,omitempty"`
	Version         string   `json:"version,omitempty"`
	RotationEnabled bool     `json:"rotation_enabled"`
	AutoSyncEnabled bool     `json:"auto_sync_enabled"`
	Targets         []string `json:"targets"`
}

func (s *RESTServer) buildSecretResponse(key string) secretResponse {
	parts := strings.SplitN(key, ":", 2)
	prov := "unknown"
	name := key
	if len(parts) == 2 {
		prov = parts[0]
		name = parts[1]
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sr := secretResponse{
		Name:            name,
		Provider:        prov,
		Status:          "ok",
		LastRotated:     now,
		LastSyncedAt:    now,
		LastUpdatedAt:   now,
		InjectionType:   "env",
		Version:         "v1",
		RotationEnabled: true,
		AutoSyncEnabled: true,
		Targets:         []string{},
	}
	// Overlay config-derived metadata when available
	if s.Config != nil {
		for _, sec := range s.Config.Secrets {
			if sec.Name == name {
				if sec.Inject.Type != "" {
					sr.InjectionType = sec.Inject.Type
				}
				if sec.Inject.Path != "" {
					sr.MountPath = sec.Inject.Path
				}
				sr.RotationEnabled = sec.Rotation.Enabled
				break
			}
		}
	}
	return sr
}

// handleSecrets dispatches /api/secrets, /api/secrets/{name}, /api/secrets/{name}/rotate
func (s *RESTServer) handleSecrets(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/secrets")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" || path == "/":
		if r.Method == http.MethodGet {
			s.handleListSecrets(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasSuffix(path, "/rotate"):
		name := strings.TrimSuffix(path, "/rotate")
		if r.Method == http.MethodPost {
			s.handleRotateSecret(w, r, name)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		if r.Method == http.MethodGet {
			s.handleGetSecret(w, r, path)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func (s *RESTServer) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.Cache == nil {
		http.Error(w, "cache not initialized", http.StatusInternalServerError)
		return
	}
	keys := s.Cache.ListKeys()
	res := make([]secretResponse, 0, len(keys))
	for _, k := range keys {
		res = append(res, s.buildSecretResponse(k))
	}
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"active_secrets": res,
		"total_count":    len(res),
	}); err != nil {
		s.Logger.Error("failed to encode secrets list", zap.Error(err))
	}
}

func (s *RESTServer) handleGetSecret(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Content-Type", "application/json")
	if s.Cache == nil {
		http.Error(w, "cache not initialized", http.StatusInternalServerError)
		return
	}
	for _, k := range s.Cache.ListKeys() {
		parts := strings.SplitN(k, ":", 2)
		keyName := k
		if len(parts) == 2 {
			keyName = parts[1]
		}
		if keyName == name {
			sr := s.buildSecretResponse(k)
			if err := json.NewEncoder(w).Encode(sr); err != nil {
				s.Logger.Error("failed to encode secret detail", zap.Error(err))
			}
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "secret not found"})
}

func (s *RESTServer) handleRotateSecret(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Content-Type", "application/json")
	if s.Config == nil || s.TriggerEngine == nil {
		http.Error(w, "rotation not available", http.StatusServiceUnavailable)
		return
	}
	var targetSecret *config.SecretMapping
	for _, sec := range s.Config.Secrets {
		if sec.Name == name {
			s := sec
			targetSecret = &s
			break
		}
	}
	if targetSecret == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "secret not configured"})
		return
	}
	pName := targetSecret.Provider
	if pName == "" {
		for k := range s.Config.Providers {
			pName = k
			break
		}
	}
	pCfg, ok := s.Config.Providers[pName]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "provider not found"})
		return
	}
	if err := s.TriggerEngine.HandleWebhook(pName, pCfg, *targetSecret, time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.Logger.Error("rotation failed", zap.String("secret", name), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "rotation failed"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "rotated",
		"secret":     name,
		"provider":   pName,
		"rotated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// memSchedulerJobStore is a minimal in-memory implementation of scheduler.SchedulerStore.
// It satisfies the interface required by scheduler.NewScheduler so that SchedulerHandler
// can be wired up without needing a full database-backed adapter bridging the two type systems.
type memSchedulerJobStore struct {
	mu   sync.RWMutex
	jobs map[string]*scheduler.Job
}

func newMemSchedulerJobStore() *memSchedulerJobStore {
	return &memSchedulerJobStore{jobs: make(map[string]*scheduler.Job)}
}

func (m *memSchedulerJobStore) CreateJob(_ context.Context, job *scheduler.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	return nil
}

func (m *memSchedulerJobStore) UpdateJob(_ context.Context, job *scheduler.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	return nil
}

func (m *memSchedulerJobStore) GetJob(_ context.Context, id string) (*scheduler.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", id)
	}
	return j, nil
}

func (m *memSchedulerJobStore) ListJobs(_ context.Context) ([]*scheduler.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*scheduler.Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out, nil
}

func (m *memSchedulerJobStore) DeleteJob(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
	return nil
}

func (m *memSchedulerJobStore) ListJobsByStatus(_ context.Context, status scheduler.JobStatus) ([]*scheduler.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*scheduler.Job
	for _, j := range m.jobs {
		if j.Status == status {
			out = append(out, j)
		}
	}
	return out, nil
}

// memSchedulerExecStore is a minimal in-memory implementation of scheduler.ExecutionStore.
type memSchedulerExecStore struct {
	mu         sync.Mutex
	executions []*scheduler.JobExecution
}

func (m *memSchedulerExecStore) LogExecution(_ context.Context, exec *scheduler.JobExecution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executions = append(m.executions, exec)
	return nil
}

func (m *memSchedulerExecStore) GetExecutions(_ context.Context, jobID string, limit int) ([]*scheduler.JobExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*scheduler.JobExecution
	for i := len(m.executions) - 1; i >= 0 && len(out) < limit; i-- {
		if m.executions[i].JobID == jobID {
			out = append(out, m.executions[i])
		}
	}
	return out, nil
}

func (m *memSchedulerExecStore) CleanupOldExecutions(_ context.Context, _ int) error {
	return nil
}

// StartRESTServer starts the REST API server on the specified address with secure timeouts
// StartRESTServer starts the REST API server and returns a shutdown function.
// The shutdown function should be called on graceful agent shutdown to properly close connections.
func StartRESTServer(ctx context.Context, addr string, cache *agent.SecretCache, triggerEngine *agent.TriggerEngine, cfg *config.Config, logger *zap.Logger, storageProvider storage.StorageProvider) func() {
	hub := NewHub(logger)
	go hub.Run()

	eventStore := NewEventStore(500, hub)

	go func() {
		for ev := range observability.EventStream {
			eventStore.Add(ev)
		}
	}()

	// Create Docker client for discovery API
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Warn("Docker client not available for discovery API", zap.Error(err))
		// Continue without Docker - discovery API will be unavailable
	}

	// Initialize Phase 4 services and handlers
	draftService := services.NewDraftService(storageProvider.Drafts())
	reviewService := services.NewReviewService(
		storageProvider.Reviews(),
		storageProvider.Approvals(),
		storageProvider.ReviewActivities(),
		storageProvider.Audit(),
	)
	approvalService := services.NewApprovalService(
		storageProvider.Approvals(),
		storageProvider.Audit(),
	)
	auditService := services.NewAuditService(storageProvider.Audit())

	executionService := services.NewExecutionServiceWithPersistence(storageProvider, auditService)
	dashboardService := services.NewDashboardService(draftService, reviewService, approvalService, auditService)

	// Create EventPersister adapter
	persister := &eventPersisterAdapter{auditService: auditService}

	// Initialize execution layer components
	workerManager := execution.NewWorkerManager()
	executionQueue := execution.NewExecutionQueue()
	auditEvents := execution.NewExecutionAuditEvents(persister)
	resilience := execution.NewResilienceManager(auditEvents, workerManager, executionQueue)
	dispatcher := execution.NewDispatcher(workerManager, executionQueue, 5) // max 5 concurrent workers

	// Create handlers
	dashboardHandler := api.NewDashboardHandler(dashboardService, auditService)
	executionHandler := api.NewExecutionHandler(executionService, approvalService, draftService)
	governanceHandler := api.NewGovernanceHandler(draftService, reviewService, approvalService, auditService)
	operationsDashboard := api.NewOperationsDashboardHandler(dispatcher, workerManager, executionQueue, auditEvents, resilience)
	operationsDLQ := api.NewOperationsDLQHandler(resilience)
	operationsTrace := api.NewOperationsTraceHandler(auditEvents)
	reviewHandler := api.NewReviewHandler(reviewService, auditService, draftService)
	orchestrationHandler := api.NewOrchestrationHandler(dispatcher, workerManager, executionQueue, auditEvents, resilience)

	// Phase 4.6 visibility handlers
	var dbConn *sql.DB
	// Try to get database connection from SQLite provider
	type dbProvider interface {
		GetDB() *sql.DB
	}
	if provider, ok := storageProvider.(dbProvider); ok {
		dbConn = provider.GetDB()
	}

	metricsCollector := api.NewMetricsCollector(dispatcher, workerManager)
	if dbConn != nil {
		metricsCollector.SetDB(dbConn)
	}
	resourceMonitor := api.NewResourceMonitor(dbConn, dispatcher, workerManager, executionQueue)
	metricsHistoryHandler := api.NewMetricsHistoryHandler(metricsCollector)
	metricsAPIHandler := api.NewMetricsAPIHandler(dbConn, metricsCollector)
	auditExplorerHandler := api.NewAuditExplorerHandler(dbConn)
	if dbConn != nil {
		executionHandler.SetDB(dbConn)
	}
	staleWorkHandler := api.NewStaleWorkHandler(dbConn, resilience)

	// Phase 5 authentication initialization
	authenticationService := auth.NewAuthenticationService(
		storageProvider.Users(),
		storageProvider.Sessions(),
		24*time.Hour,
	)
	authenticationService.SetAuditLogger(auditService)
	authHandler := api.NewAuthHandler(authenticationService, auditService)
	userManagementHandler := api.NewUserManagementHandler(storageProvider.Users(), storageProvider.Sessions(), authenticationService, auditService)
	sessionCleanupManager := auth.NewSessionCleanupManager(authenticationService, 1*time.Hour)
	permissionMatrix := auth.NewPermissionMatrix()
	authorizationMiddleware := auth.NewAuthorizationMiddleware()

	// Bootstrap admin user on first startup
	adminUsername := os.Getenv("DSO_ADMIN_USERNAME")
	if adminUsername == "" {
		adminUsername = "admin"
	}
	adminPassword := os.Getenv("DSO_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "admin"
		logger.Warn("[BOOTSTRAP] Using default admin password. Set DSO_ADMIN_PASSWORD environment variable for production.")
	}

	if err := auth.BootstrapAuthSystem(ctx, storageProvider.Users(), auth.BootstrapOptions{
		AdminUsername: adminUsername,
		AdminPassword: adminPassword,
	}); err != nil {
		logger.Error("failed to bootstrap auth system", zap.Error(err))
	}

	// Phase 5.7 security operations initialization
	securityService := services.NewSecurityService(storageProvider)
	securityHandler := api.NewSecurityHandler(securityService)
	authHandler.SetSecurityService(securityService)

	// Phase 5.8 alert rules and notification engine initialization
	alertService := services.NewAlertService(storageProvider, logger)
	alertHandler := api.NewAlertHandler(alertService)

	// Phase 5.9 backup and restore initialization
	backupService := services.NewBackupService(storageProvider, logger, nil)
	backupHandler := api.NewBackupHandler(backupService)

	// Phase 5.10 plugin framework initialization
	pluginRegistry := plugins.NewRegistry()
	pluginEventBus := plugins.NewEventBus(logger)
	pluginManager := plugins.NewManager(pluginRegistry, storageProvider.Plugins(), logger)
	pluginManager.SetEventBus(pluginEventBus)

	// Register built-in plugins
	pluginRegistry.Register(plugins.NewMetricsPlugin())
	pluginRegistry.Register(plugins.NewAlertPlugin())
	pluginRegistry.Register(plugins.NewSecurityPlugin())
	pluginRegistry.Register(plugins.NewBackupPlugin())
	pluginRegistry.Register(plugins.NewExportPlugin())
	pluginRegistry.Register(plugins.NewNotificationPlugin())
	pluginRegistry.Register(plugins.NewAnalyticsPlugin())

	pluginHandler := api.NewPluginHandler(pluginManager, storageProvider.Plugins(), pluginEventBus, logger)

	// Phase 5.11 integration framework initialization
	integrationQueue := plugins.NewIntegrationQueue(logger)
	integrationManager := plugins.NewIntegrationManager(
		integrationQueue,
		storageProvider.IntegrationConfigs(),
		storageProvider.IntegrationDeliveries(),
		pluginEventBus,
		logger,
	)

	// Register built-in integration plugins
	integrationManager.Register("webhook-plugin", plugins.NewWebhookPlugin())
	integrationManager.Register("slack-plugin", plugins.NewSlackPlugin())
	integrationManager.Register("teams-plugin", plugins.NewTeamsPlugin())
	integrationManager.Register("email-plugin", plugins.NewEmailPlugin())
	integrationManager.Register("pagerduty-plugin", plugins.NewPagerDutyPlugin())
	integrationManager.Register("jira-plugin", plugins.NewJiraPlugin())
	integrationManager.Register("servicenow-plugin", plugins.NewServiceNowPlugin())

	integrationHandler := api.NewIntegrationHandler(
		integrationManager,
		storageProvider.IntegrationConfigs(),
		storageProvider.IntegrationDeliveries(),
		logger,
	)

	// Phase 5.12 scheduler initialization.
	// The scheduler package uses its own Job/JobExecution types which differ from
	// the storage layer types, so we use lightweight in-memory stores here.
	sch := scheduler.NewScheduler(
		newMemSchedulerJobStore(),
		&memSchedulerExecStore{},
		logger,
	)
	if err := sch.Initialize(ctx); err != nil {
		logger.Error("failed to initialize scheduler", zap.Error(err))
	}
	schedulerHandler := api.NewSchedulerHandler(sch)

	restServer := &RESTServer{
		startTime:               time.Now(),
		Cache:                   cache,
		TriggerEngine:           triggerEngine,
		Config:                  cfg,
		Logger:                  logger,
		Hub:                     hub,
		EventStore:              eventStore,
		ConfigAPI:               api.NewConfigAPI(logger),
		DiscoveryAPI:            api.NewDiscoveryAPI(logger, dockerCli, cfg),
		DashboardHandler:        dashboardHandler,
		ExecutionHandler:        executionHandler,
		GovernanceHandler:       governanceHandler,
		OperationsDashboard:     operationsDashboard,
		OperationsDLQ:           operationsDLQ,
		OperationsTrace:         operationsTrace,
		ReviewHandler:           reviewHandler,
		OrchestrationHandler:    orchestrationHandler,
		ResourceMonitor:         resourceMonitor,
		MetricsHistoryHandler:   metricsHistoryHandler,
		MetricsAPIHandler:       metricsAPIHandler,
		AuditExplorerHandler:    auditExplorerHandler,
		StaleWorkHandler:        staleWorkHandler,
		MetricsCollector:        metricsCollector,
		AuthenticationService:   authenticationService,
		AuthHandler:             authHandler,
		UserManagementHandler:   userManagementHandler,
		SessionCleanupManager:   sessionCleanupManager,
		PermissionMatrix:        permissionMatrix,
		AuthorizationMiddleware: authorizationMiddleware,
		SecurityHandler:         securityHandler,
		AlertHandler:            alertHandler,
		BackupHandler:           backupHandler,
		PluginHandler:           pluginHandler,
		PluginRegistry:          pluginRegistry,
		PluginManager:           pluginManager,
		EventBus:                pluginEventBus,
		IntegrationHandler:      integrationHandler,
		IntegrationManager:      integrationManager,
		SchedulerHandler:        schedulerHandler,
		Scheduler:               sch,
		RecommendationHandler:   api.NewStubRecommendationHandler(),
		DriftHandler:            api.NewStubDriftHandler(),
		ForecastHandler:         api.NewStubForecastHandler(),
	}

	mux := http.NewServeMux()

	// Apply authentication middleware to the rest server
	authenticationMiddleware := auth.NewMiddleware(authenticationService, map[string]bool{
		"/health":         true,
		"/api/auth/login": true,
		"/api/events":     true, // Events endpoint is public for backward compatibility
	})

	mux.Handle("/", authenticationMiddleware.Handler(restServer))

	// Get HTTP timeout values from environment or use defaults
	readTimeout := getHTTPTimeoutFromEnv("DSO_HTTP_READ_TIMEOUT", 15*time.Second)
	readHeaderTimeout := getHTTPTimeoutFromEnv("DSO_HTTP_READ_HEADER_TIMEOUT", 10*time.Second)
	writeTimeout := getHTTPTimeoutFromEnv("DSO_HTTP_WRITE_TIMEOUT", 30*time.Second)
	idleTimeout := getHTTPTimeoutFromEnv("DSO_HTTP_IDLE_TIMEOUT", 60*time.Second)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	logger.Info("Starting secure REST API server",
		zap.String("addr", addr),
		zap.Duration("read_timeout", server.ReadTimeout),
		zap.Duration("write_timeout", server.WriteTimeout))
	if bindsPublic(addr) {
		logger.Warn("REST API is bound to a non-loopback address — ensure DSO_ADMIN_PASSWORD is set")
	}

	// Start session cleanup manager
	if err := sessionCleanupManager.Start(); err != nil {
		logger.Error("failed to start session cleanup manager", zap.Error(err))
		return err
	}

	// Start alert service background worker
	if err := alertService.Start(ctx); err != nil {
		logger.Error("failed to start alert service", zap.Error(err))
	}

	// Start backup service background worker
	if err := backupService.Start(ctx); err != nil {
		logger.Error("failed to start backup service", zap.Error(err))
	}

	// Initialize plugin manager
	if err := pluginManager.Initialize(ctx); err != nil {
		logger.Error("failed to initialize plugin manager", zap.Error(err))
	}

	// Initialize integration manager
	if err := integrationManager.Initialize(ctx); err != nil {
		logger.Error("failed to initialize integration manager", zap.Error(err))
	}

	// Launch server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST API server failed", zap.Error(err))
		}
	}()

	// Fix H6: wrap all shutdown logic in sync.Once so it executes at most once,
	// regardless of whether the context goroutine or the returned closure fires first.
	var shutdownOnce sync.Once
	doShutdown := func() {
		shutdownOnce.Do(func() {
			metricsCollector.Stop()
			sessionCleanupManager.Stop()
			alertService.Stop()
			backupService.Stop()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := sch.Shutdown(shutdownCtx); err != nil {
				logger.Error("scheduler shutdown error", zap.Error(err))
			}
			if err := pluginManager.Shutdown(shutdownCtx); err != nil {
				logger.Error("plugin manager shutdown error", zap.Error(err))
			}
			if err := integrationManager.Shutdown(shutdownCtx); err != nil {
				logger.Error("integration manager shutdown error", zap.Error(err))
			}
			if err := server.Shutdown(shutdownCtx); err != nil {
				logger.Error("REST API server shutdown error", zap.Error(err))
			}
		})
	}

	go func() {
		<-ctx.Done()
		doShutdown()
	}()

	// Return explicit shutdown function for manual control
	return doShutdown
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

// eventPersisterAdapter bridges execution.EventPersister to services.AuditService
type eventPersisterAdapter struct {
	auditService *services.AuditService
}

func (a *eventPersisterAdapter) LogExecutionEvent(event execution.OrchestrationAuditEvent) error {
	auditEvent := &storage.AuditEvent{
		ID:            event.ID,
		Timestamp:     event.Timestamp,
		ActorID:       event.ActorID,
		ActorName:     event.ActorName,
		Action:        event.Action,
		Status:        event.Status,
		Resource:      "execution",
		ResourceID:    event.ExecutionID,
		ResourceType:  "execution",
		CorrelationID: event.CorrelationID,
		RequestID:     fmt.Sprintf("req-%d", time.Now().UnixNano()),
		Severity:      "info",
	}

	return a.auditService.LogEventWithDetails(context.Background(), auditEvent)
}

// getHTTPTimeoutFromEnv reads HTTP timeout configuration from environment variables
// Returns default value if env var is not set or invalid
func getHTTPTimeoutFromEnv(envVar string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(envVar); val != "" {
		if duration, err := time.ParseDuration(val); err == nil && duration > 0 {
			return duration
		}
	}
	return defaultValue
}
