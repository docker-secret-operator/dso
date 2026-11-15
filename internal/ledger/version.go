package ledger

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// HMACKeySize is the size in bytes of the HMAC key the identity encoder
// accepts (HMAC-SHA256 with a 256-bit key). Key creation, storage and rotation
// are not part of this package.
const HMACKeySize = 32

// hmacDomain separates ledger identities from every other use of the key.
const hmacDomain = "dso-ledger-hmac-v1"

// Comparison is the result of comparing two Versions.
type Comparison int

const (
	// Incomparable means the two identities must not be compared: different
	// kinds, HMACs made under different key ids, or an unknown kind. It is
	// neither "unchanged" nor "changed"; callers must treat it as such.
	Incomparable Comparison = iota
	// Same means the two identities denote the same secret value.
	Same
	// Different means the two comparable identities denote different values.
	Different
)

func (c Comparison) String() string {
	switch c {
	case Same:
		return "same"
	case Different:
		return "different"
	default:
		return "incomparable"
	}
}

// Compare compares two identities without ever treating an uncomparable pair
// as equal. Plain == on Version is not a meaningful comparison and must not be
// used for that purpose.
//
//   - different kinds are Incomparable (a provider id and an HMAC never match);
//   - two HMACs are comparable only when their KeyIDs are equal, otherwise
//     Incomparable even if the id strings happen to be identical;
//   - two provider ids are Same only when id and source are both equal (a
//     source change counts as a change);
//   - an unknown kind is Incomparable.
func Compare(a, b Version) Comparison {
	if a.Kind != b.Kind {
		return Incomparable
	}
	switch a.Kind {
	case KindHMAC:
		if a.KeyID != b.KeyID {
			return Incomparable
		}
		if subtle.ConstantTimeCompare([]byte(a.ID), []byte(b.ID)) == 1 {
			return Same
		}
		return Different
	case KindProvider:
		if a.ID == b.ID && a.Source == b.Source {
			return Same
		}
		return Different
	default:
		return Incomparable
	}
}

// ValidKeyID reports whether s has the shape of a ledger key id: "k-" followed
// by 16 lowercase hex characters. It checks the format only; deriving an id
// from key bytes is key lifecycle and is not part of this package.
func ValidKeyID(s string) bool {
	return strings.HasPrefix(s, "k-") && isHex(s[2:], 16)
}

// CanonicalHMACInput returns the exact byte string that is MACed to identify a
// secret value:
//
//	"dso-ledger-hmac-v1" 0x00
//	|| u64be(len(provider))   || provider
//	|| u64be(len(secretName)) || secretName
//	|| u64be(number of keys)
//	|| for each key k in bytewise-ascending order:
//	       u64be(len(k)) || k || u64be(len(v)) || v
//
// The encoding is length-prefixed and namespaced (provider and secret name are
// inside the message), so no boundary shift between fields can collide. data is
// the map as fetched and delivered; it is read, never retained.
//
// The returned buffer contains secret values. Callers that obtain it must
// overwrite it with clear() as soon as they are done; NewHMACVersion does.
func CanonicalHMACInput(provider, secretName string, data map[string]string) []byte {
	keys := make([]string, 0, len(data))
	size := len(hmacDomain) + 1 + 8 + len(provider) + 8 + len(secretName) + 8
	for k, v := range data {
		keys = append(keys, k)
		size += 8 + len(k) + 8 + len(v)
	}
	sort.Strings(keys) // Go compares strings bytewise

	msg := make([]byte, 0, size)
	msg = append(msg, hmacDomain...)
	msg = append(msg, 0x00)
	msg = appendLP(msg, provider)
	msg = appendLP(msg, secretName)
	msg = binary.BigEndian.AppendUint64(msg, uint64(len(keys)))
	for _, k := range keys {
		msg = appendLP(msg, k)
		msg = appendLP(msg, data[k])
	}
	return msg
}

func appendLP(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint64(b, uint64(len(s)))
	return append(b, s...)
}

// NewHMACVersion returns the HMAC identity of data under key, tagged with
// keyID. The Version carries only the MAC and the key id, never the data.
//
// key must be exactly HMACKeySize bytes; there is deliberately no default key
// and no fallback to an unkeyed hash. keyID must have the ValidKeyID shape and
// the caller guarantees that it identifies key (the package does not derive or
// verify it). Errors never contain secret data.
func NewHMACVersion(keyID string, key []byte, provider, secretName string, data map[string]string) (Version, error) {
	if !ValidKeyID(keyID) {
		return Version{}, fmt.Errorf("%w: key id has an invalid format", ErrInvalid)
	}
	if len(key) != HMACKeySize {
		return Version{}, fmt.Errorf("%w: hmac key must be exactly %d bytes", ErrInvalid, HMACKeySize)
	}
	if provider == "" || secretName == "" {
		return Version{}, fmt.Errorf("%w: provider and secret name are required", ErrInvalid)
	}

	msg := CanonicalHMACInput(provider, secretName, data)
	defer clear(msg)
	mac := hmac.New(sha256.New, key)
	mac.Write(msg) // hash.Hash.Write never returns an error
	return Version{
		Kind:  KindHMAC,
		ID:    base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),
		KeyID: keyID,
	}, nil
}

// validate checks the Version on its own: its kind, and the fields that kind
// requires or forbids. keyID, when non-empty, is the ledger's hmac_key_id and
// an HMAC identity must carry exactly that key id.
func (v Version) validate(path, ledgerKeyID string) error {
	switch v.Kind {
	case KindProvider:
		if v.ID == "" || hasControl(v.ID) {
			return invalid(path+".id", "provider id must be a non-empty string without control characters")
		}
		if v.KeyID != "" {
			return invalid(path+".key_id", "a provider version must not carry a key id")
		}
		if v.Source != "" {
			if err := validSource(v.Source); err != nil {
				return invalid(path+".source", "%v", err)
			}
		}
	case KindHMAC:
		raw, err := base64.RawURLEncoding.Strict().DecodeString(v.ID)
		if err != nil || len(raw) != sha256.Size {
			return invalid(path+".id", "an hmac id must be base64url (no padding) of a 32-byte MAC")
		}
		if !ValidKeyID(v.KeyID) {
			return invalid(path+".key_id", "an hmac version requires a key id of the form k-<16 hex>")
		}
		if v.KeyID != ledgerKeyID {
			return invalid(path+".key_id", "hmac key id differs from the ledger's hmac_key_id")
		}
		if v.Source != "" {
			return invalid(path+".source", "an hmac version must have an empty source")
		}
	default:
		return invalid(path+".kind", "unknown version kind %s", quote(string(v.Kind)))
	}
	return nil
}

// validSource accepts "" (handled by the caller) or "s1:" followed by a
// non-empty base64url string, the shape of a keyed endpoint digest.
func validSource(s string) error {
	rest, ok := strings.CutPrefix(s, "s1:")
	if !ok || rest == "" {
		return errors.New("source must be empty or s1:<base64url>")
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(rest); err != nil {
		return errors.New("source must be empty or s1:<base64url>")
	}
	return nil
}
