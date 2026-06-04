package webui

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ProxyConfig holds configuration for reverse proxying
type ProxyConfig struct {
	// APITarget is the DSO REST API address (e.g., http://127.0.0.1:8471)
	APITarget string
	// Logger for debugging proxy operations
	Logger *zap.Logger
}

// NewReverseProxy creates a configured reverse proxy to the API server
func NewReverseProxy(cfg ProxyConfig) *httputil.ReverseProxy {
	target, err := url.Parse(cfg.APITarget)
	if err != nil {
		panic(fmt.Sprintf("invalid api target: %v", err))
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// Custom director to preserve request paths and headers
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Preserve the original request path
		// httputil.NewSingleHostReverseProxy sets req.URL.Path,
		// but we want to preserve the original path for proper routing
		req.Header.Set("X-Forwarded-For", getClientIP(req))
		req.Header.Set("X-Forwarded-Proto", req.Header.Get("X-Forwarded-Proto"))
		if req.Header.Get("X-Forwarded-Proto") == "" {
			if req.TLS != nil {
				req.Header.Set("X-Forwarded-Proto", "https")
			} else {
				req.Header.Set("X-Forwarded-Proto", "http")
			}
		}

		if cfg.Logger != nil {
			cfg.Logger.Debug("Proxying request to API",
				zap.String("path", req.URL.Path),
				zap.String("target", target.String()),
				zap.String("method", req.Method))
		}
	}

	// Error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if cfg.Logger != nil {
			cfg.Logger.Error("API proxy error",
				zap.Error(err),
				zap.String("path", r.URL.Path),
				zap.String("method", r.Method))
		}
		http.Error(w, "API Gateway Error", http.StatusBadGateway)
	}

	// Custom response modifier to add CORS headers
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Add CORS headers to API responses
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		resp.Header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		resp.Header.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		return nil
	}

	return proxy
}

// WebSocketProxyConfig holds configuration for WebSocket proxying
type WebSocketProxyConfig struct {
	// APITarget is the DSO REST API address for WebSocket connection
	APITarget string
	// Logger for debugging WebSocket operations
	Logger *zap.Logger
}

// ProxyWebSocket upgrades a WebSocket connection and proxies it to the API server
func ProxyWebSocket(w http.ResponseWriter, r *http.Request, cfg WebSocketProxyConfig) {
	// Parse the target URL
	targetURL, err := url.Parse(cfg.APITarget)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Error("Invalid API target", zap.Error(err))
		}
		http.Error(w, "Invalid API target", http.StatusInternalServerError)
		return
	}

	// Build the WebSocket URL
	wsScheme := "ws"
	if targetURL.Scheme == "https" {
		wsScheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s%s", wsScheme, targetURL.Host, r.URL.Path)

	// Add query parameters if present
	if r.URL.RawQuery != "" {
		wsURL = fmt.Sprintf("%s?%s", wsURL, r.URL.RawQuery)
	}

	if cfg.Logger != nil {
		cfg.Logger.Debug("Proxying WebSocket",
			zap.String("path", r.URL.Path),
			zap.String("target", wsURL))
	}

	// Upgrade client connection
	clientUpgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// Allow WebSocket connections from any origin
			// The dashboard and API may be on different ports
			return true
		},
	}

	clientConn, err := clientUpgrader.Upgrade(w, r, nil)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Error("WebSocket upgrade failed", zap.Error(err))
		}
		return
	}
	defer clientConn.Close()

	// Create dialer for backend connection
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		NetDial: (&net.Dialer{
			Timeout: 15 * time.Second,
		}).Dial,
	}

	// Connect to backend WebSocket
	backendConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Error("Failed to connect to backend WebSocket",
				zap.Error(err),
				zap.String("url", wsURL))
		}
		clientConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "backend unavailable"))
		return
	}
	defer backendConn.Close()

	// Proxy messages bidirectionally
	// Client -> Backend
	go func() {
		for {
			mt, data, err := clientConn.ReadMessage()
			if err != nil {
				if cfg.Logger != nil && !websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
					cfg.Logger.Debug("Client WebSocket read error", zap.Error(err))
				}
				return
			}
			if err := backendConn.WriteMessage(mt, data); err != nil {
				if cfg.Logger != nil {
					cfg.Logger.Debug("Backend WebSocket write error", zap.Error(err))
				}
				return
			}
		}
	}()

	// Backend -> Client
	for {
		mt, data, err := backendConn.ReadMessage()
		if err != nil {
			if cfg.Logger != nil && !websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				cfg.Logger.Debug("Backend WebSocket read error", zap.Error(err))
			}
			return
		}
		if err := clientConn.WriteMessage(mt, data); err != nil {
			if cfg.Logger != nil {
				cfg.Logger.Debug("Client WebSocket write error", zap.Error(err))
			}
			return
		}
	}
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (from upstream proxies)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take the first IP if there are multiple
		if idx := strings.Index(forwarded, ","); idx != -1 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}

	// Check X-Real-IP header
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return real
	}

	// Fall back to remote address
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
