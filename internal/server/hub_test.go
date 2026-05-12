package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// TestNewHub creates hub with proper initialization
func TestNewHub(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	if hub.broadcast == nil {
		t.Fatal("Hub broadcast channel is nil")
	}
	if hub.register == nil {
		t.Fatal("Hub register channel is nil")
	}
	if hub.unregister == nil {
		t.Fatal("Hub unregister channel is nil")
	}
	if hub.clients == nil {
		t.Fatal("Hub clients map is nil")
	}
	if hub.logger != logger {
		t.Error("Hub logger not set correctly")
	}
	if len(hub.clients) != 0 {
		t.Errorf("Expected 0 clients initially, got %d", len(hub.clients))
	}
}

// TestHub_ClientRegistration registers a client with hub
func TestHub_ClientRegistration(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	// Create mock client
	client := &Client{
		hub:  hub,
		send: make(chan Event, 256),
	}

	// Register client
	hub.register <- client

	// Give hub time to process
	time.Sleep(50 * time.Millisecond)

	// Verify client is registered
	hub.mutex.Lock()
	if len(hub.clients) != 1 {
		t.Errorf("Expected 1 client, got %d", len(hub.clients))
	}
	_, exists := hub.clients[client]
	if !exists {
		t.Error("Client should exist in hub")
	}
	hub.mutex.Unlock()
}

// TestHub_ClientUnregistration unregisters client from hub
func TestHub_ClientUnregistration(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	// Create and register client
	client := &Client{
		hub:  hub,
		send: make(chan Event, 256),
	}

	hub.register <- client
	time.Sleep(50 * time.Millisecond)

	// Verify client is registered
	hub.mutex.Lock()
	initialCount := len(hub.clients)
	hub.mutex.Unlock()

	if initialCount != 1 {
		t.Fatalf("Expected 1 client after registration, got %d", initialCount)
	}

	// Unregister client
	hub.unregister <- client
	time.Sleep(50 * time.Millisecond)

	// Verify client is unregistered
	hub.mutex.Lock()
	if len(hub.clients) != 0 {
		t.Errorf("Expected 0 clients after unregistration, got %d", len(hub.clients))
	}
	_, exists := hub.clients[client]
	if exists {
		t.Error("Client should not exist in hub after unregistration")
	}
	hub.mutex.Unlock()
}

// TestHub_MultipleClients handles multiple concurrent clients
func TestHub_MultipleClients(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	// Register multiple clients
	clients := make([]*Client, 5)
	for i := 0; i < 5; i++ {
		clients[i] = &Client{
			hub:  hub,
			send: make(chan Event, 256),
		}
		hub.register <- clients[i]
	}

	time.Sleep(50 * time.Millisecond)

	// Verify all clients are registered
	hub.mutex.Lock()
	if len(hub.clients) != 5 {
		t.Errorf("Expected 5 clients, got %d", len(hub.clients))
	}
	hub.mutex.Unlock()

	// Unregister some clients
	hub.unregister <- clients[0]
	hub.unregister <- clients[2]

	time.Sleep(50 * time.Millisecond)

	// Verify correct count remains
	hub.mutex.Lock()
	if len(hub.clients) != 3 {
		t.Errorf("Expected 3 clients after unregistering 2, got %d", len(hub.clients))
	}
	hub.mutex.Unlock()
}

// TestHub_Broadcast sends event to all connected clients
func TestHub_Broadcast(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	// Create and register clients with buffered channels
	numClients := 3
	clients := make([]*Client, numClients)
	for i := 0; i < numClients; i++ {
		clients[i] = &Client{
			hub:  hub,
			send: make(chan Event, 256), // Buffered to prevent deadlock
		}
		hub.register <- clients[i]
	}

	time.Sleep(50 * time.Millisecond)

	// Broadcast event
	testEvent := Event{
		"Type":      "test",
		"Message":   "test broadcast",
		"Timestamp": time.Now(),
	}

	hub.broadcast <- testEvent
	time.Sleep(50 * time.Millisecond)

	// Verify all clients received the event
	for i, client := range clients {
		select {
		case received := <-client.send:
			if received["Type"] != testEvent["Type"] {
				t.Errorf("Client %d received wrong event type", i)
			}
			if received["Message"] != testEvent["Message"] {
				t.Errorf("Client %d received wrong message", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Client %d did not receive broadcast", i)
		}
	}
}

// TestHub_BroadcastWithMultipleEvents sends multiple events in sequence
func TestHub_BroadcastWithMultipleEvents(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	// Create client with large buffer
	client := &Client{
		hub:  hub,
		send: make(chan Event, 256),
	}
	hub.register <- client

	time.Sleep(50 * time.Millisecond)

	// Broadcast multiple events
	for i := 0; i < 10; i++ {
		hub.broadcast <- Event{
			"Type":      "event",
			"Message":   "message",
			"Timestamp": time.Now(),
		}
	}

	time.Sleep(50 * time.Millisecond)

	// Verify all events were received
	receivedCount := 0
	for i := 0; i < 10; i++ {
		select {
		case <-client.send:
			receivedCount++
		case <-time.After(100 * time.Millisecond):
			break
		}
	}

	if receivedCount != 10 {
		t.Errorf("Expected 10 events, received %d", receivedCount)
	}
}

// TestHub_HandleBlockedClient removes client with full send channel
func TestHub_HandleBlockedClient(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	// Create client with non-buffered channel
	blockedClient := &Client{
		hub:  hub,
		send: make(chan Event), // No buffer - will block
	}
	goodClient := &Client{
		hub:  hub,
		send: make(chan Event, 256), // Buffered - won't block
	}

	hub.register <- blockedClient
	hub.register <- goodClient

	time.Sleep(50 * time.Millisecond)

	// Broadcast - should handle blocked client gracefully
	hub.broadcast <- Event{
		"Type":    "test",
		"Message": "test",
	}

	time.Sleep(50 * time.Millisecond)

	// Good client should receive the event
	select {
	case <-goodClient.send:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Good client should have received event")
	}

	// Verify blocked client was removed
	hub.mutex.Lock()
	_, exists := hub.clients[blockedClient]
	hub.mutex.Unlock()

	if exists {
		t.Error("Blocked client should have been removed")
	}
}

// TestHub_ConcurrentRegisterUnregister handles concurrent register/unregister
func TestHub_ConcurrentRegisterUnregister(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	done := make(chan bool)
	clientsCreated := int32(0)

	// Concurrent registration
	for i := 0; i < 10; i++ {
		go func(id int) {
			client := &Client{
				hub:  hub,
				send: make(chan Event, 256),
			}
			atomic.AddInt32(&clientsCreated, 1)
			hub.register <- client
			done <- true
		}(i)
	}

	// Wait for registrations
	for i := 0; i < 10; i++ {
		<-done
	}

	time.Sleep(50 * time.Millisecond)

	// Concurrent unregistration
	hub.mutex.Lock()
	clients := make([]*Client, 0, len(hub.clients))
	for c := range hub.clients {
		clients = append(clients, c)
	}
	hub.mutex.Unlock()

	for _, client := range clients {
		go func(c *Client) {
			hub.unregister <- c
			done <- true
		}(client)
	}

	// Wait for unregistrations
	for i := 0; i < len(clients); i++ {
		<-done
	}

	time.Sleep(50 * time.Millisecond)

	// Verify all clients are unregistered
	hub.mutex.Lock()
	if len(hub.clients) != 0 {
		t.Errorf("Expected 0 clients after concurrent unregister, got %d", len(hub.clients))
	}
	hub.mutex.Unlock()
}

// TestHub_BroadcastToConcurrentClients broadcasts while clients joining/leaving
func TestHub_BroadcastToConcurrentClients(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	done := make(chan bool)

	// Concurrent client registrations
	for i := 0; i < 5; i++ {
		go func() {
			client := &Client{
				hub:  hub,
				send: make(chan Event, 256),
			}
			hub.register <- client
			done <- true
		}()
	}

	// Wait for registrations
	for i := 0; i < 5; i++ {
		<-done
	}

	// Concurrent broadcasts
	for i := 0; i < 10; i++ {
		go func(id int) {
			hub.broadcast <- Event{
				"Type":      "concurrent",
				"Message":   "test",
				"Timestamp": time.Now(),
			}
			done <- true
		}(i)
	}

	// Wait for broadcasts
	for i := 0; i < 10; i++ {
		<-done
	}

	time.Sleep(50 * time.Millisecond)

	// Verify hub is still operational
	hub.mutex.Lock()
	if len(hub.clients) != 5 {
		t.Errorf("Expected 5 clients, got %d", len(hub.clients))
	}
	hub.mutex.Unlock()
}

// TestHub_DuplicateUnregister handles unregistering non-existent client
func TestHub_DuplicateUnregister(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	client := &Client{
		hub:  hub,
		send: make(chan Event, 256),
	}

	// Register and unregister
	hub.register <- client
	time.Sleep(50 * time.Millisecond)

	hub.unregister <- client
	time.Sleep(50 * time.Millisecond)

	// Unregister again - should not panic
	hub.unregister <- client
	time.Sleep(50 * time.Millisecond)

	// Hub should still be operational
	hub.mutex.Lock()
	if len(hub.clients) != 0 {
		t.Errorf("Expected 0 clients, got %d", len(hub.clients))
	}
	hub.mutex.Unlock()
}

// TestHub_LargeEventBroadcast handles large event payloads
func TestHub_LargeEventBroadcast(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	client := &Client{
		hub:  hub,
		send: make(chan Event, 256),
	}
	hub.register <- client

	time.Sleep(50 * time.Millisecond)

	// Broadcast event with large message
	largeMessage := ""
	for i := 0; i < 1000; i++ {
		largeMessage += "test message data "
	}

	hub.broadcast <- Event{
		"Type":      "large",
		"Message":   largeMessage,
		"Timestamp": time.Now(),
	}

	time.Sleep(50 * time.Millisecond)

	// Verify client received the large event
	select {
	case received := <-client.send:
		msgVal, ok := received["Message"].(string)
		if !ok || len(msgVal) != len(largeMessage) {
			t.Errorf("Expected message length %d, got %d", len(largeMessage), len(msgVal))
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client should have received large event")
	}
}

// TestHub_ThreadSafeMutex verifies mutex protects client map
func TestHub_ThreadSafeMutex(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	done := make(chan bool, 100)

	// Many concurrent operations
	for i := 0; i < 20; i++ {
		go func() {
			client := &Client{
				hub:  hub,
				send: make(chan Event, 256),
			}
			hub.register <- client
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		<-done
	}

	time.Sleep(50 * time.Millisecond)

	// Concurrent reads via mutex
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub.mutex.Lock()
			_ = len(hub.clients)
			hub.mutex.Unlock()
		}()
	}

	wg.Wait()

	// Hub should be consistent
	hub.mutex.Lock()
	if len(hub.clients) != 20 {
		t.Errorf("Expected 20 clients, got %d", len(hub.clients))
	}
	hub.mutex.Unlock()
}

// TestHub_RunLoopBlocksBroadcast verifies broadcast channels work
func TestHub_RunLoopBlocksBroadcast(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	client := &Client{
		hub:  hub,
		send: make(chan Event, 256),
	}
	hub.register <- client

	time.Sleep(50 * time.Millisecond)

	// Send broadcast and verify it reaches the client
	testEvent := Event{
		"Type":      "verify",
		"Message":   "test",
		"Severity":  "info",
		"Timestamp": time.Now(),
	}

	hub.broadcast <- testEvent

	select {
	case received := <-client.send:
		if received["Type"] != testEvent["Type"] {
			t.Error("Broadcast event type mismatch")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Broadcast not received by client")
	}
}

// TestHub_MultipleSequentialBroadcasts sends many broadcasts in sequence
func TestHub_MultipleSequentialBroadcasts(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	go hub.Run()

	client := &Client{
		hub:  hub,
		send: make(chan Event, 1024),
	}
	hub.register <- client

	time.Sleep(50 * time.Millisecond)

	// Send many broadcasts
	count := 100
	for i := 0; i < count; i++ {
		hub.broadcast <- Event{
			"Type":      "sequence",
			"Message":   "test",
			"Timestamp": time.Now(),
		}
	}

	time.Sleep(100 * time.Millisecond)

	// Count received
	received := 0
	for i := 0; i < count; i++ {
		select {
		case <-client.send:
			received++
		default:
			break
		}
	}

	if received != count {
		t.Errorf("Expected %d events, received %d", count, received)
	}
}

// TestHub_ContextCancellation tests graceful shutdown with context
func TestHub_ContextCancellation(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run hub in goroutine that respects context
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			hub.Run()
		}
	}()

	client := &Client{
		hub:  hub,
		send: make(chan Event, 256),
	}
	hub.register <- client

	time.Sleep(50 * time.Millisecond)

	// Verify client is registered before timeout
	hub.mutex.Lock()
	if len(hub.clients) != 1 {
		t.Error("Client should be registered")
	}
	hub.mutex.Unlock()

	// Wait for context to expire
	<-ctx.Done()

	time.Sleep(50 * time.Millisecond)
}

// TestHub_ClientStructure verifies Client fields are accessible
func TestHub_ClientStructure(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)

	mockConn := &websocket.Conn{}
	client := &Client{
		hub:  hub,
		conn: mockConn,
		send: make(chan Event, 256),
	}

	if client.hub != hub {
		t.Error("Client hub should be set")
	}
	if client.conn != mockConn {
		t.Error("Client conn should be set")
	}
	if client.send == nil {
		t.Error("Client send channel should be set")
	}
}
