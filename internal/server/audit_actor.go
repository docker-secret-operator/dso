package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode"
)

// auditActor is the server-verified principal behind an operator action, as
// recorded in the audit "user" field. It is derived ONLY from the outcome of
// the server's own authentication (authenticate / authenticateWebhook), never
// from request headers, query or body (X-Actor, X-User, X-Forwarded-User, ...
// are ignored).
//
// Actor formats (strings change only for NEW events; the audit record schema
// is untouched):
//
//	webui:user:<username>          valid WebUI session cookie
//	api-bearer:<12-hex fingerprint> valid DSO_AUTH_TOKEN bearer token (the log redactor masks "token:" text, hence "bearer")
//	webhook                        HMAC-authenticated webhook
//	anonymous-legacy-open          endpoint open by explicit configuration
//	unauthenticated                nothing verified (Authenticated == false)
//
// The static bearer token carries no identity: the fingerprint identifies
// WHICH credential was used, never a person. Rotating the token changes it.
type auditActor struct {
	// Name is the string written to the audit "user" field.
	Name string
	// Authenticated is true only when a credential (or an explicit open-mode
	// configuration) was verified. "unauthenticated" is never attributed to a
	// principal.
	Authenticated bool
}

func (a auditActor) String() string { return a.Name }

const (
	actorUnauthenticated = "unauthenticated"
	actorLegacyOpen      = "anonymous-legacy-open"
	actorWebhook         = "webhook"

	maxActorUsernameLen = 64

	// Audit event names for the session/auth lifecycle (M3). Status values:
	// success | failure | rate_limited.
	auditAuthLoginEvent      = "auth_login"
	auditAuthLogoutEvent     = "auth_logout"
	auditSessionsRevokeEvent = "sessions_revoke_others"
	auditSessionRevokeEvent  = "session_revoke"
)

// webuiActor is the actor for a verified WebUI operator session.
func webuiActor(username string) auditActor {
	return auditActor{Name: "webui:user:" + sanitizeActorPart(username), Authenticated: true}
}

// webhookActor is the actor for a request that passed authenticateWebhook. The
// HMAC secret is shared, so it identifies the authentication method only.
func webhookActor() auditActor { return auditActor{Name: actorWebhook, Authenticated: true} }

// actorFromContext resolves the actor for r by re-running the server's own
// authentication (idempotent, no side effects). Handlers are reached only
// after the gate in ServeHTTP, so for protected routes the result matches the
// gate's decision; a request that would be denied yields an unauthenticated
// actor.
func (s *RESTServer) actorFromContext(r *http.Request) auditActor {
	method, sess := s.authenticate(r)
	switch method {
	case authSession:
		if sess != nil && sess.Username != "" {
			return webuiActor(sess.Username)
		}
	case authBearer:
		if s.Auth != nil {
			return auditActor{Name: "api-bearer:" + tokenFingerprint(s.Auth.GetToken()), Authenticated: true}
		}
	case authOpen:
		return auditActor{Name: actorLegacyOpen, Authenticated: true}
	}
	return auditActor{Name: actorUnauthenticated}
}

// tokenFingerprint returns a short stable credential id: the first 6 bytes of
// HMAC-SHA256 keyed by the token over a fixed label. It cannot be used to
// recover a high-entropy token; it is an identifier, not a secret, and is not
// protection against offline guessing of a weak token.
func tokenFingerprint(token string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("dso-audit-credential-id-v1"))
	return hex.EncodeToString(m.Sum(nil)[:6])
}

// sanitizeActorPart strips control/non-printable characters and bounds length
// so a configured username cannot forge extra audit structure.
func sanitizeActorPart(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)
	if len(s) > maxActorUsernameLen {
		s = s[:maxActorUsernameLen]
	}
	return s
}
