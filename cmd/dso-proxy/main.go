// cmd/dso-proxy is the DSO TCP reverse-proxy binary.
//
// It is designed to run inside a Docker container as part of the generated
// docker-compose stack. Configuration is done entirely via environment
// variables so that the container image is stateless and reusable.
//
// Environment variables:
//
//	DSO_PROXY_BINDS     (required) Comma-separated bind specs:
//	                    "listenPort:service:targetPort"
//	                    e.g. "3000:api:3000,443:frontend:443"
//
//	DSO_PROXY_BACKENDS  (optional) Comma-separated initial backend specs:
//	                    "id:service:host:port"
//	                    e.g. "api-1:api:api:0"
//	                    port=0 means use the binding's TargetPort.
//
//	DSO_PROXY_API_PORT  (optional) HTTP control API port. Default: 9900.
//	                    The API is only reachable on the dso_mesh network.
//
// Startup sequence:
//  1. Parse DSO_PROXY_BINDS and open TCP listeners.
//  2. Parse DSO_PROXY_BACKENDS and pre-populate the registry.
//  3. Start the HTTP control API.
//  4. Block on SIGTERM / SIGINT.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker-secret-operator/dso/pkg/proxy"
	"go.uber.org/zap"
)

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dso-proxy: cannot init logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync() //nolint:errcheck

	os.Exit(run(log))
}

func run(log *zap.Logger) int {
	registry := proxy.NewRegistry()
	router := proxy.NewRouter(registry)
	server := proxy.NewServer(router, log)
	api := proxy.NewAPIServer(registry, server, log)

	// ── Parse and open port bindings ─────────────────────────────────────────
	bindsEnv := os.Getenv("DSO_PROXY_BINDS")
	if bindsEnv == "" {
		log.Warn("DSO_PROXY_BINDS is not set — proxy will accept no connections")
	} else {
		for _, spec := range strings.Split(bindsEnv, ",") {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			b, err := parseBindSpec(spec)
			if err != nil {
				log.Error("invalid DSO_PROXY_BINDS entry — skipping",
					zap.String("spec", spec),
					zap.Error(err))
				continue
			}
			if err := server.Bind(b); err != nil {
				log.Fatal("cannot bind port",
					zap.Int("port", b.ListenPort),
					zap.Error(err))
			}
		}
	}

	// ── Pre-register initial backends ─────────────────────────────────────────
	if backendsEnv := os.Getenv("DSO_PROXY_BACKENDS"); backendsEnv != "" {
		for _, spec := range strings.Split(backendsEnv, ",") {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			b, err := parseBackendSpec(spec)
			if err != nil {
				log.Error("invalid DSO_PROXY_BACKENDS entry — skipping",
					zap.String("spec", spec),
					zap.Error(err))
				continue
			}
			if err := registry.Add(b); err != nil {
				log.Warn("cannot register initial backend",
					zap.String("id", b.ID),
					zap.Error(err))
			} else {
				log.Info("backend registered",
					zap.String("id", b.ID),
					zap.String("service", b.Service),
					zap.String("host", b.Host))
			}
		}
	}

	// ── Start HTTP control API ────────────────────────────────────────────────
	apiPort := envInt("DSO_PROXY_API_PORT", 9900)
	apiAddr := fmt.Sprintf(":%d", apiPort)

	apiErrC := make(chan error, 1)
	go func() {
		if err := api.ListenAndServe(apiAddr); err != nil && err != http.ErrServerClosed {
			apiErrC <- err
		}
	}()

	log.Info("dso-proxy: started",
		zap.Int("api_port", apiPort),
		zap.Int("listeners", len(server.Bindings())))

	// ── Wait for shutdown signal ──────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-quit:
		log.Info("dso-proxy: received signal", zap.String("signal", sig.String()))
	case err := <-apiErrC:
		log.Error("dso-proxy: API server fatal error", zap.Error(err))
		return 1
	}

	log.Info("dso-proxy: shutting down")
	server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := api.Shutdown(ctx); err != nil {
		log.Warn("dso-proxy: API shutdown error", zap.Error(err))
	}

	log.Info("dso-proxy: clean exit")
	return 0
}

// ── Spec parsers ──────────────────────────────────────────────────────────────

// parseBindSpec parses "listenPort:service:targetPort".
// All three components are required; targetPort may be 0 to mean "use backend
// port".
func parseBindSpec(spec string) (proxy.PortBinding, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 {
		return proxy.PortBinding{},
			fmt.Errorf("expected 'listenPort:service:targetPort', got %q", spec)
	}
	listenPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return proxy.PortBinding{},
			fmt.Errorf("invalid listenPort %q: %w", parts[0], err)
	}
	targetPort, err := strconv.Atoi(parts[2])
	if err != nil {
		return proxy.PortBinding{},
			fmt.Errorf("invalid targetPort %q: %w", parts[2], err)
	}
	return proxy.PortBinding{
		ListenPort: listenPort,
		Service:    parts[1],
		TargetPort: targetPort,
	}, nil
}

// parseBackendSpec parses "id:service:host:port".
// port=0 signals "use the binding's TargetPort when dialling".
func parseBackendSpec(spec string) (*proxy.Backend, error) {
	parts := strings.SplitN(spec, ":", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("expected 'id:service:host:port', got %q", spec)
	}
	port, err := strconv.Atoi(parts[3])
	if err != nil {
		return nil, fmt.Errorf("invalid port %q: %w", parts[3], err)
	}
	return &proxy.Backend{
		ID:      parts[0],
		Service: parts[1],
		Host:    parts[2],
		Port:    port,
	}, nil
}

// envInt reads an integer from an environment variable, returning def if the
// variable is absent or cannot be parsed.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
