package webuiauth

import (
	"net"
	"net/http"
)

// Peek reports whether token is still a live session WITHOUT refreshing its
// idle timer (unlike Validate). It is for long-lived connections (WebSocket)
// that must notice logout, revocation, idle expiry and absolute-lifetime
// expiry; using Validate there would let an open socket keep an otherwise idle
// session alive forever.
func (m *Manager) Peek(token string) error {
	if token == "" {
		return ErrNoSession
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[token]
	if !ok {
		return ErrNoSession
	}
	now := m.now()
	if now.Sub(sess.LastSeenAt) > m.idleTimeout ||
		(m.maxLifetime > 0 && now.Sub(sess.CreatedAt) > m.maxLifetime) {
		delete(m.sessions, token)
		return ErrSessionExpired
	}
	return nil
}

// PeerIsTrustedProxy reports whether the socket peer of r is one of the
// configured webui.trusted_proxies. It is the same trust decision clientKey
// uses for X-Forwarded-For; no new proxy trust is introduced.
func (m *Manager) PeerIsTrustedProxy(r *http.Request) bool {
	ip := net.ParseIP(clientKeyFromRequest(r))
	if ip == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.trustedProxies) > 0 && m.isTrustedProxy(ip)
}
