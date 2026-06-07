package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"strconv"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/api"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/execution"
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
	ConfigAPI     *api.ConfigAPI
	DiscoveryAPI  *api.DiscoveryAPI
	// Phase 4 handlers
	DashboardHandler        *api.DashboardHandler
	ExecutionHandler        *api.ExecutionHandler
	GovernanceHandler       *api.GovernanceHandler
	OperationsDashboard     *api.OperationsDashboardHandler
	OperationsDLQ           *api.OperationsDLQHandler
	OperationsTrace         *api.OperationsTraceHandler
	ReviewHandler           *api.ReviewHandler
	OrchestrationHandler    *api.OrchestrationHandler
	// Phase 4.6 visibility handlers
	ResourceMonitor         *api.ResourceMonitor
	MetricsHistoryHandler   *api.MetricsHistoryHandler
	AuditExplorerHandler    *api.AuditExplorerHandler
	StaleWorkHandler        *api.StaleWorkHandler
	MetricsCollector        *api.MetricsCollector
	// Phase 5 authentication
	AuthenticationService   *auth.AuthenticationService
	AuthHandler             *api.AuthHandler
	SessionCleanupManager   *auth.SessionCleanupManager
	PermissionMatrix        *auth.PermissionMatrix
	AuthorizationMiddleware *auth.AuthorizationMiddleware
}

func (s *RESTServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check RBAC authorization for all non-public routes
	// This happens after authentication middleware has injected the user into context
	if !s.checkAuthorization(w, r) {
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
		s.handleListSecrets(w, r)
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

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"active_secrets": res,
		"total_count":    len(res),
	}); err != nil {
		s.Logger.Error("Failed to encode secrets list response", zap.Error(err))
	}
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
	resourceMonitor := api.NewResourceMonitor(dbConn, dispatcher, workerManager, executionQueue)
	metricsHistoryHandler := api.NewMetricsHistoryHandler(metricsCollector)
	auditExplorerHandler := api.NewAuditExplorerHandler(dbConn)
	staleWorkHandler := api.NewStaleWorkHandler(dbConn, resilience)

	// Phase 5 authentication initialization
	authenticationService := auth.NewAuthenticationService(
		storageProvider.Users(),
		storageProvider.Sessions(),
		24*time.Hour,
	)
	authHandler := api.NewAuthHandler(authenticationService, auditService)
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

	restServer := &RESTServer{
		Cache:                  cache,
		TriggerEngine:          triggerEngine,
		Config:                 cfg,
		Logger:                 logger,
		Hub:                    hub,
		EventStore:             eventStore,
		Auth:                   auth.NewAuthenticator(),
		ConfigAPI:              api.NewConfigAPI(logger),
		DiscoveryAPI:           api.NewDiscoveryAPI(logger, dockerCli, cfg),
		DashboardHandler:       dashboardHandler,
		ExecutionHandler:       executionHandler,
		GovernanceHandler:      governanceHandler,
		OperationsDashboard:    operationsDashboard,
		OperationsDLQ:          operationsDLQ,
		OperationsTrace:        operationsTrace,
		ReviewHandler:          reviewHandler,
		OrchestrationHandler:   orchestrationHandler,
		ResourceMonitor:        resourceMonitor,
		MetricsHistoryHandler:  metricsHistoryHandler,
		AuditExplorerHandler:   auditExplorerHandler,
		StaleWorkHandler:       staleWorkHandler,
		MetricsCollector:       metricsCollector,
		AuthenticationService:  authenticationService,
		AuthHandler:            authHandler,
		SessionCleanupManager:  sessionCleanupManager,
		PermissionMatrix:       permissionMatrix,
		AuthorizationMiddleware: authorizationMiddleware,
	}

	mux := http.NewServeMux()

	// Apply authentication middleware to the rest server
	authenticationMiddleware := auth.NewMiddleware(authenticationService, map[string]bool{
		"/health":         true,
		"/api/auth/login": true,
		"/api/events":     true, // Events endpoint is public for backward compatibility
	})

	mux.Handle("/", authenticationMiddleware.Handler(restServer))

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

	// Start session cleanup manager
	sessionCleanupManager.Start()

	// Launch server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST API server failed", zap.Error(err))
		}
	}()

	// Return shutdown function that closes server on context cancellation
	go func() {
		<-ctx.Done()
		metricsCollector.Stop()
		sessionCleanupManager.Stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("REST API server shutdown error", zap.Error(err))
		}
	}()

	// Return explicit shutdown function for manual control
	return func() {
		metricsCollector.Stop()
		sessionCleanupManager.Stop()
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
