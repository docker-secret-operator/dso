package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait  = 10 * time.Second
	pingPeriod = 60 * time.Second // Kept simple

	// maxHubClients caps concurrent WebSocket connections. The rate limiter
	// deliberately exempts this endpoint from per-message throttling (a
	// single long-lived connection legitimately sends very few messages),
	// but without a cap here that exemption also meant no limit on how many
	// connections could be opened — each costing a buffered channel and two
	// goroutines. This bounds that instead.
	maxHubClients = 500

	// maxClientMessageBytes bounds a single inbound frame. Clients never need
	// to send anything meaningful; this only tolerates control/keepalive text.
	maxClientMessageBytes = 512
)

// wsRecheckInterval is how often an open socket re-verifies its credential
// (a var so tests can shorten it).
// Stored atomically because tests change it while earlier connections' pumps
// are still winding down.
var wsRecheckNanos atomic.Int64

func init() { wsRecheckNanos.Store(int64(15 * time.Second)) }

func wsRecheckInterval() time.Duration { return time.Duration(wsRecheckNanos.Load()) }

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan Event
	// recheck, when non-nil, is polled every wsRecheckInterval; returning false
	// ends the connection (credential revoked, expired or rotated).
	recheck func() bool
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Event
	register   chan *Client
	unregister chan *Client
	mutex      sync.Mutex
	logger     *zap.Logger
}

func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		broadcast:  make(chan Event, 64), // buffered to prevent EventStore.Add from blocking
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		logger:     logger,
	}
}

// ClientCount returns the number of currently connected WebSocket clients.
func (h *Hub) ClientCount() int {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return len(h.clients)
}

func (h *Hub) Run(ctx context.Context) {
	h.logger.Info("Starting WebSocket Hub")
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("Stopping WebSocket Hub")
			return
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true
			h.mutex.Unlock()
			h.logger.Info("Client connected to WS Hub. Total connections active", zap.Int("count", len(h.clients)))

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.logger.Info("Client disconnected from WS Hub. Remaining", zap.Int("count", len(h.clients)))
			}
			h.mutex.Unlock()

		case event := <-h.broadcast:
			h.mutex.Lock()
			for client := range h.clients {
				select {
				case client.send <- event:
				default: // Blocked/Dead connection logic
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mutex.Unlock()
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	recheckTicker := time.NewTicker(wsRecheckInterval())
	defer func() {
		ticker.Stop()
		recheckTicker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteJSON(event); err != nil {
				return
			}
		case <-recheckTicker.C:
			if c.recheck != nil && !c.recheck() {
				_ = c.conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session ended"),
					time.Now().Add(writeWait))
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump ensures the connection detects disconnects quickly since browsers often close TCP uncleanly.
// ctx is the request context; when it is cancelled the goroutine exits without blocking on unregister.
func (c *Client) readPump(ctx context.Context) {
	defer func() {
		select {
		case c.hub.unregister <- c:
		case <-ctx.Done():
			// Hub already stopped; skip unregister to avoid blocking forever.
		}
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxClientMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(pingPeriod + 10*time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pingPeriod + 10*time.Second))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
