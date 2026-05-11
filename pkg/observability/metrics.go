package observability

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	// SecretRequestsTotal tracks total fetch attempts per provider and status
	SecretRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_secret_requests_total",
			Help: "Total number of secret requests received by the agent",
		},
		[]string{"provider", "status"},
	)

	// SecretFetchLatency tracks the time taken to fetch secrets from backends
	SecretFetchLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_secret_fetch_latency_seconds",
			Help:    "Latency of secret fetches from providers",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provider"},
	)

	// SecretCacheHitsTotal tracks cache efficiency
	SecretCacheHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_secret_cache_hits_total",
			Help: "Total number of secret cache hits",
		},
		[]string{"secret"},
	)

	// SecretCacheMissesTotal tracks cache misses
	SecretCacheMissesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dso_secret_cache_misses_total",
			Help: "Total number of secret cache misses",
		},
	)

	// BackendFailuresTotal tracks errors per provider
	BackendFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_backend_failures_total",
			Help: "Total number of provider-specific errors",
		},
		[]string{"provider", "error_type"},
	)

	// DaemonReconnectsTotal tracks Docker daemon reconnect attempts
	DaemonReconnectsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_daemon_reconnects_total",
			Help: "Total number of Docker daemon reconnect attempts",
		},
		[]string{"status"},
	)

	// RequestTimeoutsTotal tracks request timeouts
	RequestTimeoutsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_request_timeouts_total",
			Help: "Total number of request timeouts",
		},
		[]string{"provider"},
	)

	// RotationDurationSeconds tracks secret rotation duration
	RotationDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_rotation_duration_seconds",
			Help:    "Duration of secret rotation operations",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
		[]string{"strategy"},
	)

	// CacheItemsTotal tracks number of items in cache
	CacheItemsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_cache_items_total",
			Help: "Current number of items in secret cache",
		},
	)

	// EventsProcessedTotal tracks processed events
	EventsProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_events_processed_total",
			Help: "Total number of Docker events processed",
		},
		[]string{"action"},
	)

	// ReconciliationDurationSeconds tracks reconciliation duration
	ReconciliationDurationSeconds = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "dso_reconciliation_duration_seconds",
			Help:    "Duration of runtime state reconciliation",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30},
		},
	)
)

// StartMetricsServer starts the Prometheus exporter with security measures
func StartMetricsServer(addr string, logger *zap.Logger) {
	mux := http.NewServeMux()

	// Wrap metrics handler with security checks
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET requests
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Restrict metrics access to localhost only (unless explicitly configured)
		// This prevents metrics from being scraped from external networks by default
		remoteHost := r.RemoteAddr
		if !isLocalhostRequest(remoteHost) {
			// Log potential unauthorized access attempt
			logger.Warn("Metrics access from non-localhost address denied",
				zap.String("remote_addr", remoteHost))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Serve metrics
		promhttp.Handler().ServeHTTP(w, r)
	})

	// Health check endpoint (always accessible for container orchestration)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy"}`)
	})

	logger.Info("Starting Prometheus metrics server", zap.String("addr", addr), zap.String("note", "metrics restricted to localhost"))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,  // prevent slowloris attacks
		WriteTimeout:      10 * time.Second, // prevent clients from holding connections
		IdleTimeout:       30 * time.Second, // close idle connections
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("Metrics server failed", zap.Error(err))
	}
}

// isLocalhostRequest checks if request is from localhost (127.0.0.1 or ::1)
func isLocalhostRequest(remoteAddr string) bool {
	// Extract IP from RemoteAddr (format: "IP:PORT")
	if remoteAddr == "" {
		return false
	}

	// Handle IPv6 format [IP]:PORT
	if remoteAddr[0] == '[' {
		idx := strings.LastIndex(remoteAddr, "]:")
		if idx != -1 {
			remoteAddr = remoteAddr[1:idx]
		}
	} else {
		// Handle IPv4 format IP:PORT
		idx := strings.LastIndex(remoteAddr, ":")
		if idx != -1 {
			remoteAddr = remoteAddr[:idx]
		}
	}

	// Check against localhost addresses
	return remoteAddr == "127.0.0.1" || remoteAddr == "localhost" ||
		remoteAddr == "::1" || remoteAddr == "[::1]"
}
