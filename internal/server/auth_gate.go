package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/docker-secret-operator/dso/internal/webuiauth"
)

// authMethod records HOW a request was authenticated. The distinction matters
// beyond allow/deny: CSRF defenses apply to ambient (cookie) credentials and
// to unauthenticated open mode, but not to an explicit Authorization header.
type authMethod int

const (
	// authNone: no valid credential; the request must be rejected unless the
	// route is public.
	authNone authMethod = iota
	// authBearer: a valid DSO_AUTH_TOKEN bearer token. Browsers cannot attach
	// this header cross-site without a CORS preflight (DSO sends no CORS
	// headers), so it is not CSRF-able.
	authBearer
	// authSession: a valid WebUI session cookie (ambient credential).
	authSession
	// authOpen: legacy mode -- no DSO_AUTH_TOKEN and no WebUI configured, so
	// the API is intentionally unauthenticated. StartRESTServer only allows
	// this on loopback binds.
	authOpen
)

const webhookPath = "/api/events/secret-update"

// authenticate resolves how r is authenticated.
//
// Decision table (token = DSO_AUTH_TOKEN configured, webui = WebUI enabled):
//
//	valid bearer (token set)         -> authBearer
//	valid session cookie (webui)     -> authSession
//	webui enabled, nothing valid     -> authNone  (never "open": the WebUI login
//	                                    would otherwise be bypassable by simply
//	                                    not configuring a token)
//	token set, nothing valid         -> authNone
//	no token and no webui            -> authOpen  (legacy, loopback only)
func (s *RESTServer) authenticate(r *http.Request) (authMethod, *webuiauth.Session) {
	tokenConfigured := s.Auth.Enabled() // nil-safe; same predicate buildAccessWiring uses
	if tokenConfigured {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		token := strings.TrimPrefix(header, "Bearer ")
		if s.Auth.Verify(token) == nil {
			return authBearer, nil
		}
	}
	if s.WebUIAuth != nil {
		if sess, err := s.WebUIAuth.Validate(webuiauth.TokenFromRequest(r)); err == nil {
			return authSession, sess
		}
		return authNone, nil
	}
	if tokenConfigured {
		return authNone, nil
	}
	return authOpen, nil
}

func isUnsafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	return true
}

// originAllowed is the server-side CSRF defense for requests that are not
// bearer-authenticated. It deliberately does not rely on any client-generated
// token.
//
//   - Origin present: its host must equal the request Host (same-origin) or
//     match a configured webui.allowed_origins entry. Browsers send Origin on
//     every cross-origin request and on same-origin POST/PUT/PATCH/DELETE, so
//     a forged cross-site request always fails here. "null" never matches.
//   - Origin absent: fall back to Sec-Fetch-Site. A browser that declares
//     cross-site/same-site is rejected; same-origin/none is accepted.
//   - Neither header: a non-browser client (curl, scripts) or a very old
//     browser. Browsers that old also ignore SameSite=Strict, which is the one
//     residual gap; non-browser clients are not CSRF vectors.
func (s *RESTServer) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		switch strings.ToLower(r.Header.Get("Sec-Fetch-Site")) {
		case "", "same-origin", "none":
			return true
		}
		return false
	}
	if strings.EqualFold(origin, "null") {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range s.AllowedOrigins {
		if strings.EqualFold(strings.TrimRight(allowed, "/"), u.Scheme+"://"+u.Host) {
			return true
		}
	}
	return false
}

// hostGuard validates the Host header to defeat DNS rebinding against
// loopback-bound listeners. A rebinding page reaches 127.0.0.1 with
// "Host: attacker.example"; no legitimate client does.
//
// It is only installed (see buildAccessWiring) when it can be done without
// guessing: in legacy open mode (no token, WebUI disabled), where
// authentication cannot help, or when the operator sets webui.allowed_hosts.
// When the API requires a credential and allowed_hosts is unset, rebinding is
// already defeated by authentication (the attacker origin has neither the
// cookie nor the bearer token).
//
// webui.allowed_origins is a separate control: it only widens the CSRF Origin
// check (originAllowed) and never turns Host enforcement on by itself. Once a
// guard IS installed, the hosts of any allowed_origins entries are added to its
// allowlist so one reverse-proxy origin entry also covers legacy mode behind
// that proxy.
type hostGuard struct {
	allowed map[string]struct{}
}

func newHostGuard(hosts, origins []string) *hostGuard {
	g := &hostGuard{allowed: map[string]struct{}{}}
	for _, h := range hosts {
		if n := normalizeHost(h); n != "" {
			g.allowed[n] = struct{}{}
		}
	}
	for _, o := range origins {
		if u, err := url.Parse(strings.TrimSpace(o)); err == nil && u.Host != "" {
			if n := normalizeHost(u.Host); n != "" {
				g.allowed[n] = struct{}{}
			}
		}
	}
	return g
}

// normalizeHost lowercases, strips the port, IPv6 brackets and a trailing dot.
func normalizeHost(hostport string) string {
	h := strings.TrimSpace(hostport)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	return h
}

func isLoopbackName(h string) bool {
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (g *hostGuard) allows(hostport string) bool {
	h := normalizeHost(hostport)
	if h == "" {
		return false
	}
	if isLoopbackName(h) {
		return true
	}
	_, ok := g.allowed[h]
	return ok
}
