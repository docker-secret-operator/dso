package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMarshal_IsDeterministicAndRoundTrips(t *testing.T) {
	l := sampleLedger(t)
	a := mustMarshal(t, l)
	b := mustMarshal(t, sampleLedger(t))
	if !bytes.Equal(a, b) {
		t.Fatal("the same content must always produce the same bytes")
	}
	got, err := Unmarshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if again := mustMarshal(t, got); !bytes.Equal(a, again) {
		t.Fatal("marshal(unmarshal(marshal(x))) must equal marshal(x)")
	}
	if got.BodySHA256 == "" || !isHex(got.BodySHA256, 64) {
		t.Errorf("body_sha256 not set to 64 hex characters: %q", got.BodySHA256)
	}
	// Compact, declaration-order output with the schema version first.
	if !bytes.HasPrefix(a, []byte(`{"schema_version":3,"ledger_id":"`+idLedger+`","generation":42,"hmac_key_id":"`+testKeyID+`",`)) {
		t.Errorf("unexpected canonical prefix: %.120s", a)
	}
	if bytes.ContainsAny(a, "\n") {
		t.Error("canonical output must be a single line without a trailing newline")
	}
	// Per-entry content survives the round trip. Unmarshal returns the canonical
	// order (design §39 h), which differs from the order of the sample, so the
	// entries are matched by their secret instead of by position.
	find := func(ledger *Ledger, secret string) *Entry {
		for i := range ledger.Entries {
			if ledger.Entries[i].Key.Secret == secret {
				return &ledger.Entries[i]
			}
		}
		t.Fatalf("entry %q not found", secret)
		return nil
	}
	if !reflect.DeepEqual(find(got, "tls_cert").Replicas, find(l, "tls_cert").Replicas) ||
		!reflect.DeepEqual(find(got, "kv/batch").Replicas, find(l, "kv/batch").Replicas) ||
		!reflect.DeepEqual(find(got, "kv/batch").Request, find(l, "kv/batch").Request) {
		t.Error("replicas (including rotation_id, delivery and adoption) or the request changed across a round trip")
	}
}

func TestMarshal_DoesNotEscapeHTMLAndIsStableForNilSlices(t *testing.T) {
	l := sampleLedger(t)
	l.Entries[3].Replicas[2].Reason = "a<b>&c"
	if out := mustMarshal(t, l); !bytes.Contains(out, []byte("a<b>&c")) {
		t.Error("HTML characters must not be escaped in the canonical form")
	}

	withNil := &Ledger{SchemaVersion: SchemaVersion, LedgerID: idLedger, HMACKeyID: testKeyID, CreatedAt: t0, UpdatedAt: t0}
	withEmpty := *withNil
	withEmpty.Entries = []Entry{}
	a, b := mustMarshal(t, withNil), mustMarshal(t, &withEmpty)
	if !bytes.Equal(a, b) {
		t.Fatal("nil and empty slices must serialize identically")
	}
	if !bytes.Contains(a, []byte(`"entries":[]`)) || bytes.Contains(a, []byte(`"recoveries"`)) || bytes.Contains(a, []byte(`"rotation"`)) {
		t.Errorf("empty slices must render as [] and the retired rotation and recoveries fields must be absent: %s", a)
	}
	if _, err := Unmarshal(a); err != nil {
		t.Fatalf("an empty ledger must round-trip: %v", err)
	}
}

func TestMarshal_DoesNotModifyItsInputAndIgnoresAStaleChecksum(t *testing.T) {
	l := sampleLedger(t)
	l.Entries[0].Replicas = nil // nil must stay nil in the caller's copy
	l.BodySHA256 = strings.Repeat("0", 64)
	out := mustMarshal(t, l)
	if l.BodySHA256 != strings.Repeat("0", 64) || l.Entries[0].Replicas != nil {
		t.Fatal("Marshal must not modify its input")
	}
	got, err := Unmarshal(out)
	if err != nil {
		t.Fatalf("a stale checksum in the input must be recomputed: %v", err)
	}
	if got.BodySHA256 == strings.Repeat("0", 64) {
		t.Fatal("the stale checksum was kept")
	}
}

func TestMarshal_RefusesInvalidDocuments(t *testing.T) {
	l := sampleLedger(t)
	l.Entries[0].Desired.Kind = "sha256"
	if _, err := Marshal(l); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Marshal must refuse an invalid document, got %v", err)
	}
	if _, err := Marshal(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Marshal(nil) must fail with ErrInvalid, got %v", err)
	}
	if _, err := CanonicalBody(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("CanonicalBody(nil) must fail with ErrInvalid, got %v", err)
	}
}

func TestBodyChecksum_IsTheSHA256OfTheCanonicalBody(t *testing.T) {
	l := sampleLedger(t)
	sealed := mustMarshal(t, l)
	got, err := Unmarshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	body, err := CanonicalBody(got)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if got.BodySHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("body_sha256 is not the SHA-256 of the canonical body")
	}
	// The canonical body is the sealed document with the checksum emptied.
	want := strings.Replace(string(sealed), `"body_sha256":"`+got.BodySHA256+`"`, `"body_sha256":""`, 1)
	if string(body) != want {
		t.Fatal("the canonical body must be the sealed document with body_sha256 set to an empty string")
	}
	if cs, err := ComputeBodySHA256(got); err != nil || cs != got.BodySHA256 {
		t.Fatalf("ComputeBodySHA256 = %q, %v", cs, err)
	}
}

func TestUnmarshal_IsStrict(t *testing.T) {
	sealed := string(mustMarshal(t, sampleLedger(t)))
	cases := map[string]string{
		"unknown top-level field": strings.Replace(sealed, `{"schema_version":3,`, `{"schema_version":3,"surprise":true,`, 1),
		"unknown nested field":    strings.Replace(sealed, `"project":"shop"`, `"project":"shop","bogus":1`, 1),
		"unknown in replica":      strings.Replace(sealed, `"delivery":"verified"`, `"delivery":"verified","bogus":1`, 1),
		"retired rotation field":  strings.Replace(sealed, `"request":null`, `"rotation":null,"request":null`, 1),
		"retired recoveries":      strings.Replace(sealed, `"entries":[`, `"recoveries":[],"entries":[`, 1),
		"trailing object":         sealed + `{}`,
		"trailing garbage":        sealed + `x`,
		"wrong field type":        strings.Replace(sealed, `"generation":42`, `"generation":"42"`, 1),
		"negative sequence":       strings.Replace(sealed, `"desired_seq":5`, `"desired_seq":-5`, 1),
		"not an object":           `[]`,
		"empty input":             ``,
		"null":                    `null`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Unmarshal([]byte(doc))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("must wrap ErrInvalid: %v", err)
			}
			var sv *SchemaVersionError
			if errors.As(err, &sv) {
				t.Fatal("a malformed document must not be reported as a newer schema")
			}
		})
	}
}

func TestUnmarshal_SchemaVersions(t *testing.T) {
	sealed := string(mustMarshal(t, sampleLedger(t)))
	// A newer schema is reported as such, ahead of every other check, even when
	// the document contains fields this version does not know.
	for _, doc := range []string{
		strings.Replace(sealed, `"schema_version":3`, `"schema_version":4`, 1),
		`{"schema_version":7,"shiny_new_field":{"x":1}}`,
		`{"schema_version":2147483647}`,
	} {
		_, err := Unmarshal([]byte(doc))
		var sv *SchemaVersionError
		if !errors.As(err, &sv) || sv.Supported != SchemaVersion || sv.Found <= SchemaVersion {
			t.Fatalf("expected a SchemaVersionError for %.60s, got %v", doc, err)
		}
		if errors.Is(err, ErrInvalid) {
			t.Fatal("a newer schema is not a corrupt document and must not match ErrInvalid")
		}
		if !strings.Contains(err.Error(), "newer") {
			t.Errorf("unclear message: %v", err)
		}
	}
	// Anything else is invalid.
	for name, doc := range map[string]string{
		"zero":      strings.Replace(sealed, `"schema_version":3`, `"schema_version":0`, 1),
		"negative":  strings.Replace(sealed, `"schema_version":3`, `"schema_version":-1`, 1),
		"version 1": strings.Replace(sealed, `"schema_version":3`, `"schema_version":1`, 1),
		"version 2": strings.Replace(sealed, `"schema_version":3`, `"schema_version":2`, 1),
		"missing":   strings.Replace(sealed, `"schema_version":3,`, ``, 1),
		"as string": strings.Replace(sealed, `"schema_version":3`, `"schema_version":"2"`, 1),
		"as float":  strings.Replace(sealed, `"schema_version":3`, `"schema_version":3.5`, 1),
	} {
		if _, err := Unmarshal([]byte(doc)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: expected ErrInvalid, got %v", name, err)
		}
	}
}

func TestUnmarshal_DoesNotEchoDocumentContent(t *testing.T) {
	marker := "S3CR3T-MARKER-IN-INPUT"
	for _, doc := range []string{
		`{"schema_version":3,"ledger_id":"` + marker,
		`{"schema_version":3,"ledger_id":"` + marker + `"}`,
		`{"schema_version":3,"generation":"` + marker + `"}`,
		`{"schema_version":3,"` + marker + `":1}`,
		`{"schema_version":3,"` + marker + `":1,"` + marker + `":2}`,
	} {
		_, err := Unmarshal([]byte(doc))
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("error echoes document content: %v", err)
		}
	}
}

// T-U3: the checksum (and the parser) detect truncation and single-byte flips.
func TestChecksum_DetectsTruncationAndSingleByteFlips(t *testing.T) {
	sealed := mustMarshal(t, sampleLedger(t))
	if _, err := Unmarshal(sealed); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(sealed); n++ {
		if _, err := Unmarshal(sealed[:n]); err == nil {
			t.Fatalf("truncation to %d of %d bytes was not detected", n, len(sealed))
		}
	}
	for i := range sealed {
		flipped := append([]byte(nil), sealed...)
		flipped[i] ^= 0x01
		if _, err := Unmarshal(flipped); err == nil {
			t.Fatalf("flipping the low bit of byte %d (%q) was not detected", i, sealed[i])
		}
	}
}

func TestChecksum_MismatchAndMissingAreReported(t *testing.T) {
	sealed := string(mustMarshal(t, sampleLedger(t)))
	got, err := Unmarshal([]byte(sealed))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"content changed, checksum kept": strings.Replace(sealed, `"generation":42`, `"generation":43`, 1),
		"checksum emptied":               strings.Replace(sealed, got.BodySHA256, "", 1),
		"checksum of other content":      strings.Replace(sealed, got.BodySHA256, strings.Repeat("a", 64), 1),
	}
	for name, doc := range cases {
		_, err := Unmarshal([]byte(doc))
		if !errors.Is(err, ErrChecksum) || !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: expected ErrChecksum wrapping ErrInvalid, got %v", name, err)
		}
	}
}

// Decoding re-validates, so a syntactically fine but semantically invalid
// document is rejected even when its checksum is consistent.
func TestUnmarshal_ValidatesAfterDecoding(t *testing.T) {
	l := sampleLedger(t)
	l.Entries[0].Replicas[0].Outcome = "bogus"
	// Bypass Marshal's own validation to build a consistent-looking document.
	c := normalized(l)
	sum, err := ComputeBodySHA256(c)
	if err != nil {
		t.Fatal(err)
	}
	c.BodySHA256 = sum
	raw, err := encode(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unmarshal(raw); !errors.Is(err, ErrInvalid) || errors.Is(err, ErrChecksum) {
		t.Fatalf("expected a validation error with a good checksum, got %v", err)
	}
}

// T-U12 (the S1 part): nothing that identifies a secret value in plaintext or
// through an unkeyed hash can appear in the serialized document.
func TestSerializedDocument_ContainsNoPlaintextAndNoUnkeyedHash(t *testing.T) {
	values := []string{"S3CR3T-FIXTURE-1", "S3CR3T-FIXTURE-2", "S3CR3T-FIXTURE-USER-3"}
	data := map[string]string{"password": values[0], "token": values[1], "user": values[2]}
	v, err := NewHMACVersion(testKeyID, testKey, "aws", "prod/db_password", data)
	if err != nil {
		t.Fatal(err)
	}
	l := sampleLedger(t)
	l.Entries[0].Desired = vp(v)
	out := mustMarshal(t, l)

	forbidden := map[string][]byte{}
	for _, val := range values {
		forbidden["plaintext "+val] = []byte(val)
		sum := sha256.Sum256([]byte(val))
		forbidden["sha256 hex of "+val] = []byte(hex.EncodeToString(sum[:]))
		forbidden["sha256 base64url of "+val] = []byte(base64.RawURLEncoding.EncodeToString(sum[:]))
		forbidden["sha256 base64 of "+val] = []byte(base64.StdEncoding.EncodeToString(sum[:]))
	}
	for _, in := range [][]byte{CanonicalHMACInput("aws", "prod/db_password", data)} {
		sum := sha256.Sum256(in)
		forbidden["sha256 hex of the canonical input"] = []byte(hex.EncodeToString(sum[:]))
		forbidden["sha256 base64url of the canonical input"] = []byte(base64.RawURLEncoding.EncodeToString(sum[:]))
	}
	for what, needle := range forbidden {
		if bytes.Contains(out, needle) {
			t.Errorf("serialized ledger contains %s", what)
		}
	}
	// Every version identity in the document is an HMAC (keyed) or a provider id.
	for _, e := range sampleLedger(t).Entries {
		for _, ver := range []*Version{e.Desired, e.Applied} {
			if ver != nil && ver.Kind != KindHMAC && ver.Kind != KindProvider {
				t.Errorf("unexpected identity kind %q", ver.Kind)
			}
		}
	}
}
