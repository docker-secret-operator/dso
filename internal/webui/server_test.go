package webui

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func TestNewServer(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name      string
		addr      string
		apiTarget string
		wantErr   bool
	}{
		{
			name:      "valid configuration",
			addr:      ":8472",
			apiTarget: "http://127.0.0.1:8471",
			wantErr:   false,
		},
		{
			name:      "empty address",
			addr:      "",
			apiTarget: "http://127.0.0.1:8471",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := NewServer(tt.addr, tt.apiTarget, logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewServer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && srv == nil {
				t.Error("NewServer() returned nil server with no error")
			}
		})
	}
}

func TestGetContentType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"index.html", "text/html; charset=utf-8"},
		{"app.js", "application/javascript; charset=utf-8"},
		{"style.css", "text/css; charset=utf-8"},
		{"data.json", "application/json; charset=utf-8"},
		{"image.svg", "image/svg+xml"},
		{"image.png", "image/png"},
		{"image.jpg", "image/jpeg"},
		{"font.woff2", "font/woff2"},
		{"unknown.xyz", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := getContentType(tt.filename)
			if got != tt.want {
				t.Errorf("getContentType(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestSetCacheHeaders(t *testing.T) {
	tests := []struct {
		filename   string
		wantPrefix string // prefix to match in Cache-Control
	}{
		{"_next/static/chunks/app.js", "public, immutable"},
		{"index.html", "public, must-revalidate"},
		{"app.js", "public, max-age=3600"},
		{"style.css", "public, max-age=3600"},
		{"image.png", "public, max-age=86400"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			w := httptest.NewRecorder()
			setCacheHeaders(w, tt.filename)

			cacheControl := w.Header().Get("Cache-Control")
			if !strings.HasPrefix(cacheControl, tt.wantPrefix) {
				t.Errorf("setCacheHeaders(%q) Cache-Control = %q, want prefix %q",
					tt.filename, cacheControl, tt.wantPrefix)
			}
		})
	}
}

func TestIsPortAvailable(t *testing.T) {
	// Find an available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	availablePort := addr.Port
	listener.Close()

	// The port should now be available
	if !IsPortAvailable(availablePort) {
		t.Errorf("IsPortAvailable(%d) = false, want true for available port", availablePort)
	}

	// Bind the port and check again
	listener, _ = net.Listen("tcp", fmt.Sprintf(":%d", availablePort))
	defer listener.Close()

	if IsPortAvailable(availablePort) {
		t.Errorf("IsPortAvailable(%d) = true, want false for bound port", availablePort)
	}
}

func TestGetURLForPort(t *testing.T) {
	tests := []struct {
		port int
		want string
	}{
		{8472, "http://127.0.0.1:8472/dashboard"},
		{8080, "http://127.0.0.1:8080/dashboard"},
		{3000, "http://127.0.0.1:3000/dashboard"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("port_%d", tt.port), func(t *testing.T) {
			got := GetURLForPort(tt.port)
			if got != tt.want {
				t.Errorf("GetURLForPort(%d) = %q, want %q", tt.port, got, tt.want)
			}
		})
	}
}

func TestReverseProxyConfig(t *testing.T) {
	// Create a mock API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"up"}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()

	// Create reverse proxy
	logger := zap.NewNop()
	proxy := NewReverseProxy(ProxyConfig{
		APITarget: apiServer.URL,
		Logger:    logger,
	})

	// Test proxying a request
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "up") {
		t.Errorf("Expected response to contain 'up', got %q", string(body))
	}
}

func TestProxyWebSocketBasic(t *testing.T) {
	// Create a mock WebSocket server
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/events/ws" {
			upgrader := websocket.Upgrader{}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Logf("WebSocket upgrade failed: %v", err)
				return
			}
			defer conn.Close()

			// Send a test message
			if err := conn.WriteMessage(websocket.TextMessage, []byte("test")); err != nil {
				t.Logf("Write failed: %v", err)
			}

			// Read a message
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Logf("Read failed: %v", err)
			} else if string(msg) != "hello" {
				t.Logf("Expected 'hello', got %q", string(msg))
			}
		}
	}))
	defer wsServer.Close()

	// Create dashboard server that proxies to the mock API
	logger := zap.NewNop()
	srv, err := NewServer(":0", wsServer.URL, logger)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test that the server can handle WebSocket proxying
	// (Full integration test would require actual server start)
	if srv == nil {
		t.Error("Server creation failed")
	}
}

func TestHandleStaticOrFallback(t *testing.T) {
	logger := zap.NewNop()
	srv, err := NewServer(":0", "http://127.0.0.1:8471", logger)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		wantStatusCode int
		wantContent    string
	}{
		{
			name:           "root redirects to dashboard",
			path:           "/",
			wantStatusCode: http.StatusOK,
			wantContent:    "",
		},
		{
			name:           "missing route falls back to index.html",
			path:           "/nonexistent",
			wantStatusCode: http.StatusOK,
			wantContent:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			srv.handleStaticOrFallback(w, req)

			if w.Code != tt.wantStatusCode {
				t.Errorf("Expected status %d, got %d", tt.wantStatusCode, w.Code)
			}
		})
	}
}

func TestShutdown(t *testing.T) {
	logger := zap.NewNop()
	srv, err := NewServer(":0", "http://127.0.0.1:8471", logger)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Listen()
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Wait for server goroutine to finish
	select {
	case err := <-errChan:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Server exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not shut down in time")
	}
}

func TestClientIPExtraction(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*http.Request)
		wantIP string
	}{
		{
			name: "X-Forwarded-For single IP",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "192.168.1.1")
			},
			wantIP: "192.168.1.1",
		},
		{
			name: "X-Forwarded-For multiple IPs",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1")
			},
			wantIP: "192.168.1.1",
		},
		{
			name: "X-Real-IP",
			setup: func(r *http.Request) {
				r.Header.Set("X-Real-IP", "203.0.113.1")
			},
			wantIP: "203.0.113.1",
		},
		{
			name: "RemoteAddr fallback",
			setup: func(r *http.Request) {
				r.RemoteAddr = "203.0.113.2:54321"
			},
			wantIP: "203.0.113.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			tt.setup(r)

			got := getClientIP(r)
			if got != tt.wantIP {
				t.Errorf("getClientIP() = %q, want %q", got, tt.wantIP)
			}
		})
	}
}
