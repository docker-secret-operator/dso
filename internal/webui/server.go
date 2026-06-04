package webui

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Server provides HTTP server for the DSO web dashboard
type Server struct {
	// Addr is the address to listen on (e.g., ":8472")
	Addr string
	// APITarget is the DSO REST API address (e.g., "http://127.0.0.1:8471")
	APITarget string
	// Logger for debugging
	Logger *zap.Logger
	// httpServer is the underlying http.Server
	httpServer *http.Server
	// assets is the embedded filesystem
	assets fs.FS
	// apiProxy is the reverse proxy for API requests
	apiProxy *http.Client
}

// NewServer creates a new dashboard server
func NewServer(addr, apiTarget string, logger *zap.Logger) (*Server, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	assets, err := GetAssets()
	if err != nil {
		return nil, fmt.Errorf("failed to get embedded assets: %w", err)
	}

	srv := &Server{
		Addr:      addr,
		APITarget: apiTarget,
		Logger:    logger,
		assets:    assets,
	}

	// Create the router
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", srv.handleAPIProxy)
	mux.HandleFunc("/api/events/ws", srv.handleWebSocketProxy)
	mux.HandleFunc("/", srv.handleStaticOrFallback)

	srv.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	return srv, nil
}

// Start begins listening for HTTP requests
func (s *Server) Start(ctx context.Context) error {
	s.Logger.Info("Starting dashboard server",
		zap.String("addr", s.Addr),
		zap.String("api_target", s.APITarget))

	// Start server in goroutine and monitor for shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.Logger.Error("Dashboard server shutdown error", zap.Error(err))
		}
	}()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("dashboard server failed: %w", err)
	}

	return nil
}

// Listen starts the server synchronously and blocks until shutdown
// This is useful for the `dso ui` command
func (s *Server) Listen() error {
	s.Logger.Info("Starting dashboard server",
		zap.String("addr", s.Addr),
		zap.String("api_target", s.APITarget))

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("dashboard server failed: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown dashboard server: %w", err)
	}
	return nil
}

// handleAPIProxy reverses proxy API requests to the DSO REST API
func (s *Server) handleAPIProxy(w http.ResponseWriter, r *http.Request) {
	// Skip WebSocket requests (handled separately)
	if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		s.handleWebSocketProxy(w, r)
		return
	}

	// Create reverse proxy and proxy the request
	proxy := NewReverseProxy(ProxyConfig{
		APITarget: s.APITarget,
		Logger:    s.Logger,
	})

	proxy.ServeHTTP(w, r)
}

// handleWebSocketProxy upgrades and proxies WebSocket connections
func (s *Server) handleWebSocketProxy(w http.ResponseWriter, r *http.Request) {
	ProxyWebSocket(w, r, WebSocketProxyConfig{
		APITarget: s.APITarget,
		Logger:    s.Logger,
	})
}

// handleStaticOrFallback serves static files from the embedded filesystem
// or falls back to index.html for dynamic routing (SPA behavior)
func (s *Server) handleStaticOrFallback(w http.ResponseWriter, r *http.Request) {
	// Normalize path
	reqPath := r.URL.Path
	if reqPath == "/" {
		reqPath = "/dashboard"
	}

	// Try to serve the requested file
	if s.tryServeFile(w, r, reqPath) {
		return
	}

	// For routes without file extensions, try with .html
	if !strings.Contains(path.Base(reqPath), ".") {
		if s.tryServeFile(w, r, reqPath+".html") {
			return
		}
	}

	// Fallback to index.html for SPA routing
	s.Logger.Debug("Falling back to index.html", zap.String("path", reqPath))
	if s.tryServeFile(w, r, "/index.html") {
		return
	}

	// Last resort: 404
	http.NotFound(w, r)
}

// tryServeFile attempts to serve a file from the embedded filesystem
func (s *Server) tryServeFile(w http.ResponseWriter, r *http.Request, filePath string) bool {
	// Clean up the path
	filePath = strings.TrimPrefix(filePath, "/")
	if filePath == "" {
		filePath = "index.html"
	}

	// Try to open the file
	file, err := s.assets.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	// Get file info
	info, err := file.Stat()
	if err != nil {
		return false
	}

	// Don't serve directories
	if info.IsDir() {
		return false
	}

	// Determine content type
	contentType := getContentType(filePath)
	w.Header().Set("Content-Type", contentType)

	// Set cache headers
	setCacheHeaders(w, filePath)

	// Serve the file
	// Read the file into memory (embedded assets are small)
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return true
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		s.Logger.Debug("Failed to write response", zap.Error(err))
	}
	return true
}

// getContentType returns the MIME type for a given filename
func getContentType(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(filename, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(filename, ".mjs"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(filename, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(filename, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(filename, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(filename, ".png"):
		return "image/png"
	case strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(filename, ".gif"):
		return "image/gif"
	case strings.HasSuffix(filename, ".webp"):
		return "image/webp"
	case strings.HasSuffix(filename, ".woff"):
		return "font/woff"
	case strings.HasSuffix(filename, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(filename, ".ttf"):
		return "font/ttf"
	default:
		return "application/octet-stream"
	}
}

// setCacheHeaders sets appropriate cache headers for static assets
func setCacheHeaders(w http.ResponseWriter, filename string) {
	// Static assets can be cached long-term (Next.js uses content hashing)
	if strings.HasPrefix(filename, "_next/static/") {
		// _next/static files are content-hashed, safe to cache indefinitely
		w.Header().Set("Cache-Control", "public, immutable, max-age=31536000") // 1 year
	} else if strings.HasSuffix(filename, ".html") {
		// HTML files should not be cached to allow updates
		w.Header().Set("Cache-Control", "public, must-revalidate, max-age=0")
	} else if strings.HasSuffix(filename, ".js") || strings.HasSuffix(filename, ".css") {
		// JavaScript and CSS can be cached with revalidation
		w.Header().Set("Cache-Control", "public, max-age=3600") // 1 hour
	} else {
		// Other assets: moderate caching
		w.Header().Set("Cache-Control", "public, max-age=86400") // 1 day
	}
}

// GetURLForPort returns the dashboard URL for the given port
func GetURLForPort(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/dashboard", port)
}

// IsPortAvailable checks if a port is available
func IsPortAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
