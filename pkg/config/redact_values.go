package config

import (
	"math"
	"regexp"
	"strings"
)

// Value-based detection. Key names alone are not a safe basis for redaction:
// credentials routinely sit under innocuous keys ("endpoint", "connection",
// "notes"). These detectors look at the VALUE, and the free-form policy below
// is fail-closed (unknown key => sensitive) rather than name-matched.

var (
	// scheme://user[:pass]@host -- userinfo embedded in any URL.
	userinfoURLPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://[^/\s@]*@`)
	// Any absolute URL (used only inside free-form maps, where it is reduced to
	// scheme://host).
	anyURLPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://\S+$`)
	// JWT / JWS: base64url header starting {"...
	jwtPattern = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]*$`)
	// Well-known token formats.
	knownTokenPattern = regexp.MustCompile(`^(?:` +
		`(?:AKIA|ASIA|AGPA|AIDA|AROA)[A-Z0-9]{16}` + // AWS access key ids
		`|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}` + // GitHub
		`|xox[abprs]-[A-Za-z0-9-]{10,}` + // Slack
		`|sk-[A-Za-z0-9_-]{16,}` + // OpenAI-style
		`|AIza[0-9A-Za-z_-]{30,}` + // Google API key
		`|glpat-[A-Za-z0-9_-]{16,}` + // GitLab
		`|hv[sbr]\.[A-Za-z0-9_-]{16,}|s\.[A-Za-z0-9]{20,}` + // HashiCorp Vault tokens
		`|dop_v1_[a-f0-9]{16,}|npm_[A-Za-z0-9]{20,}` +
		`)$`)
	pemPattern = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+-----`)
	// UUID-shaped identifiers (role_id, tenant ids, ...). Only treated as
	// sensitive inside free-form maps.
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// freeFormBenignKeys are the only keys of a free-form map whose values are
// shown (unless the VALUE itself looks like a credential). The list is
// deliberately short and limited to settings that are neither secrets nor
// identifiers of secrets. Anything not listed is redacted.
var freeFormBenignKeys = map[string]bool{
	"region": true, "regions": true, "location": true, "zone": true,
	"namespace": true, "mount": true, "mount_path": true,
	"version": true, "kv_version": true, "engine": true, "type": true,
	"auth_method": true, "method": true, "timeout": true, "retries": true,
	"project": true, "project_id": true, "name": true, "profile": true,
}

// sensitiveShape reports whether v is, by its own shape, a credential. The
// returned kind says how to render it (userinfo URLs keep scheme://host).
func sensitiveShape(v string) (redactKind, bool) {
	t := strings.TrimSpace(v)
	switch {
	case t == "":
		return 0, false
	case userinfoURLPattern.MatchString(t):
		return kindURL, true
	case jwtPattern.MatchString(t), knownTokenPattern.MatchString(t), pemPattern.MatchString(t):
		return kindSecret, true
	}
	return 0, false
}

// shannonEntropy is the entropy of s in bits per character.
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	freq := map[rune]float64{}
	n := 0.0
	for _, r := range s {
		freq[r]++
		n++
	}
	h := 0.0
	for _, c := range freq {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}

// opaqueTokenChars limits the heuristic to token-like strings: no dots, colons,
// slashes or spaces, so hostnames, URLs, paths and prose are never matched.
var opaqueTokenChars = regexp.MustCompile(`^[A-Za-z0-9_+=-]+$`)

// looksRandom is a heuristic for opaque secrets with no recognisable format:
// 20+ characters, no whitespace, at least two character classes and high
// entropy. It is applied only where the schema gives no better guidance
// (free-form maps and keys the schema does not define), never to known fields
// such as container names or paths.
func looksRandom(v string) bool {
	t := strings.TrimSpace(v)
	if len(t) < 20 || !opaqueTokenChars.MatchString(t) {
		return false
	}
	var lower, upper, digit bool
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	classes := 0
	for _, b := range []bool{lower, upper, digit} {
		if b {
			classes++
		}
	}
	return classes >= 2 && shannonEntropy(t) >= 3.7
}

// valueKind decides how a scalar in a context without schema guidance is
// rendered: shape first, then (optionally) the opaque-random heuristic.
func valueKind(v string, heuristic bool) (redactKind, bool) {
	if k, ok := sensitiveShape(v); ok {
		return k, true
	}
	if heuristic && looksRandom(v) {
		return kindSecret, true
	}
	return 0, false
}

// freeFormKind decides how a value in a free-form map is rendered, given its
// key. Fail-closed: only allowlisted keys with non-credential-looking values
// stay visible.
func freeFormKind(key, value string) (redactKind, bool) {
	if k, ok := valueKind(value, true); ok {
		return k, true
	}
	if credentialKeyPattern.MatchString(key) {
		return kindSecret, true
	}
	if anyURLPattern.MatchString(strings.TrimSpace(value)) {
		return kindURL, true // keep scheme://host, drop path/query/userinfo
	}
	if uuidPattern.MatchString(strings.TrimSpace(value)) {
		return kindSecret, true // role_id, secret_id, tenant ids
	}
	if freeFormBenignKeys[strings.ToLower(key)] {
		return 0, false
	}
	return kindSecret, true
}
