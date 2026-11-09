package webuiauth

import (
	"context"
	"net/http"
)

type ctxKey int

const sessionCtxKey ctxKey = iota

// RequireSession is HTTP middleware that rejects requests without a valid
// session cookie with 401. On success it stores the Session on the request
// context for downstream handlers (e.g. to gate admin-only routes).
func (m *Manager) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := m.Validate(TokenFromRequest(r))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), sessionCtxKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CheckHandshake is used by websocket upgrade handlers to verify the
// session cookie BEFORE upgrading the connection. Returns the session on
// success; the caller should refuse the upgrade and respond 401 on error.
func (m *Manager) CheckHandshake(r *http.Request) (*Session, error) {
	return m.Validate(TokenFromRequest(r))
}

// SessionFromContext returns the Session stored by RequireSession, if any.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	sess, ok := ctx.Value(sessionCtxKey).(*Session)
	return sess, ok
}
