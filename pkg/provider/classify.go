package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/rpc"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrClass is the retry classification of a GetSecret failure.
type ErrClass int

const (
	// ClassPermanent errors are never retried: retrying cannot change the
	// outcome (bad credentials, denied, not found, bad configuration, ...) and
	// would only hammer the provider. It is also the default for any error
	// shape that is not positively recognised as transient.
	ClassPermanent ErrClass = iota
	// ClassTransient errors may succeed on a later attempt (network failure,
	// timeout, 5xx).
	ClassTransient
	// ClassThrottled is transient, and additionally the provider asked us to
	// slow down; the retry delay has a floor and honours Retry-After.
	ClassThrottled
)

func (c ErrClass) String() string {
	switch c {
	case ClassTransient:
		return "transient"
	case ClassThrottled:
		return "throttled"
	default:
		return "permanent"
	}
}

// Classification is the result of Classify.
type Classification struct {
	Class ErrClass
	// RetryAfter is the provider-supplied Retry-After, if the error text
	// carried one; zero otherwise.
	RetryAfter time.Duration
}

// Retryable reports whether the failure may be retried.
func (c Classification) Retryable() bool { return c.Class != ClassPermanent }

var (
	// Status codes as printed by the SDKs. These survive the go-plugin RPC
	// boundary only as text (net/rpc flattens server errors to a string), so
	// classification has to work on text; typed checks below only help for
	// errors raised inside the daemon (timeouts, a dead plugin connection).
	reStatus = []*regexp.Regexp{
		regexp.MustCompile(`StatusCode: (\d{3})`),            // AWS smithy
		regexp.MustCompile(`(?m)^RESPONSE (\d{3}):`),         // Azure azcore
		regexp.MustCompile(`Code: (\d{3})\. (?:Errors|Raw)`), // Vault api
		regexp.MustCompile(`"status_code":\s*(\d{3})`),       // Huawei SDK
	}
	reRetryAfter = regexp.MustCompile(`(?i)retry-after:\s*(\d{1,6})\b`)
)

var permanentMarkers = []string{
	// AWS
	"resourcenotfoundexception", "accessdeniedexception", "unrecognizedclientexception",
	"invalidsignatureexception", "expiredtokenexception", "invalidclienttokenid",
	"invalidrequestexception", "invalidparameterexception", "decryptionfailure",
	"unauthorizedoperation", "no valid credential", "failed to retrieve credentials",
	// Azure
	"secretnotfound", "forbidden", "unauthorized", "invalidauthenticationtoken",
	"azurecli", "defaultazurecredential",
	// Vault
	"permission denied", "missing client token", "invalid token", "not found",
	// provider-side validation and lifecycle
	"not initialized", "unsupported", "invalid configuration", "invalid vault secret",
	"decoded to no key/value", "has no value", "has no string value",
}

var throttleMarkers = []string{
	"throttlingexception", "throttling", "toomanyrequests", "too many requests",
	"requestlimitexceeded", "rate exceeded", "slowdown", "ratelimit", "rate limit",
	"requestthrottled",
}

var transientMarkers = []string{
	"connection refused", "connection reset", "broken pipe", "i/o timeout",
	"tls handshake timeout", "timeout", "timed out", "deadline exceeded",
	"temporary failure in name resolution", "server misbehaving",
	"network is unreachable", "no route to host", "unexpected eof",
	"requesttimeout", "serviceunavailable", "internalfailure", "internalerror",
	"service unavailable", "bad gateway", "gateway timeout", "vault is sealed",
	"connectionerror", "requesttimeouterror", "http2: server sent goaway",
}

// Classify decides whether err (returned by a provider GetSecret for secret
// key) is worth retrying.
//
// Order matters and is deliberate:
//  1. Typed in-daemon errors (context, net.Error, io.EOF, rpc shutdown).
//  2. Text of the error, with the secret key name scrubbed out first so that a
//     secret called "timeout-token" cannot be misread as a timeout.
//  3. Permanent code names, then throttle names, then HTTP status, then
//     transient phrases. Provider code names beat the status code because AWS
//     reports throttling as HTTP 400 and some denials as 5xx-looking wrappers.
//  4. Anything unrecognised is PERMANENT: an unknown failure is not retried
//     (the agent poll loop re-attempts on its own schedule anyway).
func Classify(err error, key string) Classification {
	if err == nil {
		return Classification{Class: ClassPermanent}
	}

	// A plugin connection that has gone away cannot recover on the same
	// client: the store evicts and respawns the plugin, which is a
	// GetProvider concern, not something a retry on this instance can fix.
	if errors.Is(err, rpc.ErrShutdown) {
		return Classification{Class: ClassPermanent}
	}
	if errors.Is(err, context.Canceled) {
		return Classification{Class: ClassPermanent}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return Classification{Class: ClassTransient}
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return Classification{Class: ClassTransient}
	}

	msg := err.Error()
	msg = scrubKey(msg, key)
	low := strings.ToLower(msg)

	var ra time.Duration
	if m := reRetryAfter.FindStringSubmatch(msg); m != nil {
		if n, e := strconv.Atoi(m[1]); e == nil && n >= 0 {
			ra = time.Duration(n) * time.Second
		}
	}

	status := 0
	for _, re := range reStatus {
		if m := re.FindStringSubmatch(msg); m != nil {
			status, _ = strconv.Atoi(m[1])
			break
		}
	}

	if containsAny(low, throttleMarkers) || status == 429 {
		return Classification{Class: ClassThrottled, RetryAfter: ra}
	}
	// Transient phrases beat permanent text only when the status is not a
	// definitive client error; check status first for definitive answers.
	switch {
	case status == 408 || status == 425 || status >= 500 && status <= 599:
		return Classification{Class: ClassTransient, RetryAfter: ra}
	case status >= 400 && status <= 499:
		return Classification{Class: ClassPermanent}
	}
	if containsAny(low, permanentMarkers) {
		return Classification{Class: ClassPermanent}
	}
	if containsAny(low, transientMarkers) {
		return Classification{Class: ClassTransient, RetryAfter: ra}
	}
	return Classification{Class: ClassPermanent}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// scrubKey removes occurrences of key that stand alone (not embedded in a
// longer word), so a short key like "s" cannot mangle "StatusCode".
func scrubKey(msg, key string) string {
	if key == "" {
		return msg
	}
	isWord := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
	}
	var out strings.Builder
	i := 0
	for i < len(msg) {
		j := strings.Index(msg[i:], key)
		if j < 0 {
			out.WriteString(msg[i:])
			break
		}
		j += i
		end := j + len(key)
		before := j == 0 || !isWord(msg[j-1])
		after := end >= len(msg) || !isWord(msg[end])
		if before && after {
			out.WriteString(msg[i:j])
		} else {
			out.WriteString(msg[i:end])
		}
		i = end
	}
	return out.String()
}
