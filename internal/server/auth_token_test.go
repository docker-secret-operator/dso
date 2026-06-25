package server

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// TestStartRESTServer_TokenTooShort verifies that StartRESTServer rejects a
// DSO_AUTH_TOKEN shorter than 16 bytes (B4 fix).
func TestStartRESTServer_TokenTooShort(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "tooshort") // 8 bytes

	_, err := StartRESTServer(context.Background(), "127.0.0.1:0", nil, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for short token, got nil")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestStartRESTServer_TokenTooLong verifies that StartRESTServer rejects a
// DSO_AUTH_TOKEN exceeding 512 bytes (B4 fix).
func TestStartRESTServer_TokenTooLong(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", strings.Repeat("x", 513))

	_, err := StartRESTServer(context.Background(), "127.0.0.1:0", nil, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for oversized token, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestStartRESTServer_TokenAccepted verifies that a 16-byte token passes the
// validation gate.  The server may fail to bind (nil cache, etc.) but the
// failure must NOT be a token-validation error.
func TestStartRESTServer_TokenAccepted(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "exactly16bytesok") // exactly 16 bytes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := StartRESTServer(ctx, "127.0.0.1:0", nil, nil, nil, zap.NewNop())
	if err != nil && (strings.Contains(err.Error(), "too short") || strings.Contains(err.Error(), "exceeds maximum")) {
		t.Errorf("valid token was rejected: %v", err)
	}
}
