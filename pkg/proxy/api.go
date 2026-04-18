package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// APIServer exposes a lightweight HTTP control plane that allows external
// components (the DSO agent, deployment scripts, health checkers) to manage
// backends and query proxy state without restarting the process.
//
// Endpoints:
//
//	GET    /health          → liveness probe
//	GET    /backends        → list all registered backends
//	POST   /backends        → register a new backend
//	DELETE /backends/{id}   → deregister a backend by ID
//	GET    /bindings        → list active port bindings
type APIServer struct {
	registry *Registry
	server   *Server
	log      *zap.Logger
	http     *http.Server
}

// NewAPIServer wires the registry and proxy server into an HTTP mux and
// returns a ready-to-start APIServer. Call ListenAndServe to begin accepting
// requests.
func NewAPIServer(registry *Registry, server *Server, log *zap.Logger) *APIServer {
	api := &APIServer{
		registry: registry,
		server:   server,
		log:      log,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.handleHealth)
	mux.HandleFunc("/backends", api.handleBackends)
	mux.HandleFunc("/backends/", api.handleBackendByID)
	mux.HandleFunc("/bindings", api.handleBindings)

	api.http = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return api
}

// ListenAndServe starts the HTTP server on addr (e.g. ":9900").
// Blocks until the server is stopped. Returns http.ErrServerClosed on
// graceful shutdown.
func (a *APIServer) ListenAndServe(addr string) error {
	a.http.Addr = addr
	a.log.Info("proxy: API server listening", zap.String("addr", addr))
	return a.http.ListenAndServe()
}

// Shutdown performs a graceful drain with the given context.
func (a *APIServer) Shutdown(ctx context.Context) error {
	return a.http.Shutdown(ctx)
}

// ── Handlers ─────────────────────────────────────────────────────────────────

// handleHealth responds to GET /health with a 200 and backend count.
func (a *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	backends := a.registry.All()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"backends": len(backends),
	})
}

// handleBackends routes GET and POST on /backends.
func (a *APIServer) handleBackends(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listBackends(w, r)
	case http.MethodPost:
		a.addBackend(w, r)
	default:
		methodNotAllowed(w)
	}
}

// handleBackendByID routes DELETE (and future GET) on /backends/{id}.
func (a *APIServer) handleBackendByID(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/backends/"):]
	if id == "" {
		http.Error(w, "backend ID required in path", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		a.removeBackend(w, r, id)
	case http.MethodGet:
		a.getBackend(w, r, id)
	default:
		methodNotAllowed(w)
	}
}

// handleBindings responds to GET /bindings with the list of active PortBindings.
func (a *APIServer) handleBindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bindings": a.server.Bindings(),
	})
}

// ── CRUD operations ───────────────────────────────────────────────────────────

func (a *APIServer) listBackends(w http.ResponseWriter, _ *http.Request) {
	backends := a.registry.All()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"backends": backends,
		"count":    len(backends),
	})
}

func (a *APIServer) addBackend(w http.ResponseWriter, r *http.Request) {
	var b Backend
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}

	// Auto-generate an ID from the backend's attributes if the caller omitted it.
	if b.ID == "" {
		b.ID = fmt.Sprintf("%s-%s-%d", b.Service, b.Host, b.Port)
	}

	if err := a.registry.Add(&b); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	a.log.Info("proxy: backend registered",
		zap.String("id", b.ID),
		zap.String("service", b.Service),
		zap.String("host", b.Host),
		zap.Int("port", b.Port))

	writeJSON(w, http.StatusCreated, &b)
}

func (a *APIServer) removeBackend(w http.ResponseWriter, _ *http.Request, id string) {
	if err := a.registry.Remove(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	a.log.Info("proxy: backend removed", zap.String("id", id))
	w.WriteHeader(http.StatusNoContent)
}

func (a *APIServer) getBackend(w http.ResponseWriter, _ *http.Request, id string) {
	// Linear scan — backends list is expected to be small (< 100).
	for _, b := range a.registry.All() {
		if b.ID == id {
			writeJSON(w, http.StatusOK, b)
			return
		}
	}
	http.Error(w, fmt.Sprintf("backend %q not found", id), http.StatusNotFound)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
