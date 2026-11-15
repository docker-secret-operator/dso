package ledger

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// layout is an independent, hand-written rendering of the canonical input so
// that CanonicalHMACInput is checked against the specification rather than
// against itself.
func layout(provider, secret string, data map[string]string) []byte {
	var b bytes.Buffer
	b.WriteString("dso-ledger-hmac-v1")
	b.WriteByte(0x00)
	lp := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		b.Write(n[:])
		b.WriteString(s)
	}
	lp(provider)
	lp(secret)
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(data)))
	b.Write(n[:])
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lp(k)
		lp(data[k])
	}
	return b.Bytes()
}

func TestCanonicalHMACInput_MatchesTheSpecifiedLayout(t *testing.T) {
	cases := []struct {
		provider, secret string
		data             map[string]string
	}{
		{"aws", "prod/db_password", map[string]string{"user": "app", "password": "S3CR3T-FIXTURE-1"}},
		{"vault", "kv/batch", map[string]string{"token": "S3CR3T-FIXTURE-2"}},
		{"aws", "prod/db_password", nil},
		{"aws", "prod/db_password", map[string]string{}},
		{"aws", "prod/db_password", map[string]string{"k\x00": "v", "é": "ü"}},
	}
	for i, c := range cases {
		got := CanonicalHMACInput(c.provider, c.secret, c.data)
		if want := layout(c.provider, c.secret, c.data); !bytes.Equal(got, want) {
			t.Errorf("case %d: canonical input differs from the specified layout", i)
		}
	}
	// The first bytes are the domain string, a NUL, then the provider length.
	got := CanonicalHMACInput("aws", "s", nil)
	if !bytes.HasPrefix(got, append([]byte("dso-ledger-hmac-v1"), 0x00)) {
		t.Error("input must start with the domain string and a NUL separator")
	}
}

func TestCanonicalHMACInput_DoesNotModifyItsInput(t *testing.T) {
	data := map[string]string{"b": "2", "a": "1"}
	_ = CanonicalHMACInput("aws", "s", data)
	if len(data) != 2 || data["a"] != "1" || data["b"] != "2" {
		t.Fatalf("input map was modified: %v", data)
	}
}

// Vectors computed with an independent implementation (Python hmac/hashlib)
// over the layout of the design: HMAC-SHA256, base64url without padding. Key:
// 32 bytes of 0x42 (second key: 32 bytes of 0x07).
func TestNewHMACVersion_FixedVectors(t *testing.T) {
	key2 := bytes.Repeat([]byte{0x07}, HMACKeySize)
	cases := []struct {
		name             string
		key              []byte
		provider, secret string
		data             map[string]string
		want             string
	}{
		{"aws db", testKey, "aws", "prod/db_password", map[string]string{"password": "S3CR3T-FIXTURE-1", "user": "app"}, "pd1nPZIxhL0gJeJbRL2J7a_E_6gsB_QYowF5AAeh0eM"},
		{"vault batch", testKey, "vault", "kv/batch", map[string]string{"token": "S3CR3T-FIXTURE-2"}, "Ef6vGVmSj1G0IYXZK0k2aWP5Jxufyu5OY8AonzoXFOs"},
		{"empty data", testKey, "aws", "prod/db_password", map[string]string{}, "oIwEW5WFh1lCKal4P5kU9f-fPeXTtmcefdalUnS6LVg"},
		{"boundary a=bc,d=e", testKey, "aws", "prod/db_password", map[string]string{"a": "bc", "d": "e"}, "tefOz94OAX3XUQgJBBw2WCt5nEZLaNQ_PDdoj0oXUzA"},
		{"boundary a=b,cd=e", testKey, "aws", "prod/db_password", map[string]string{"a": "b", "cd": "e"}, "8qC5ZCbc1jNoMalLdZHzTXTW-t58pSzF35-mKeuq7rI"},
		{"non-ascii value", testKey, "aws", "prod/db_password", map[string]string{"password": "S3CR3T-FIXTURE-1", "user": "appé"}, "terTiY3kEWioD8yHyEXgs0oZ30cRGPm3BqVQuMyOmrk"},
		{"other key", key2, "aws", "prod/db_password", map[string]string{"password": "S3CR3T-FIXTURE-1", "user": "app"}, "4_rxGQ8GMQV4lSnsok7SWVMaY0qnSTZmS4p2dGz4Qjo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := NewHMACVersion(testKeyID, c.key, c.provider, c.secret, c.data)
			if err != nil {
				t.Fatal(err)
			}
			if v.ID != c.want {
				t.Fatalf("id = %s, want %s", v.ID, c.want)
			}
			if v.Kind != KindHMAC || v.KeyID != testKeyID || v.Source != "" {
				t.Fatalf("unexpected version metadata: %+v", v)
			}
			if err := v.validate("v", testKeyID); err != nil {
				t.Fatalf("a freshly built identity must validate: %v", err)
			}
		})
	}
}

func TestNewHMACVersion_IsDeterministicAndOrderIndependent(t *testing.T) {
	data := map[string]string{}
	for i := 0; i < 50; i++ {
		data[fmt.Sprintf("key-%02d", i)] = fmt.Sprintf("value-%d", i)
	}
	first, err := NewHMACVersion(testKeyID, testKey, "aws", "s", data)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ { // Go randomizes map iteration order per range
		again, err := NewHMACVersion(testKeyID, testKey, "aws", "s", data)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("identity changed between calls: %v vs %v", again, first)
		}
	}
}

func TestNewHMACVersion_BindsEveryInput(t *testing.T) {
	base := func() (string, string, map[string]string) {
		return "aws", "prod/db", map[string]string{"a": "1", "b": "2"}
	}
	ref := func(provider, secret string, data map[string]string, key []byte) string {
		v, err := NewHMACVersion(testKeyID, key, provider, secret, data)
		if err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	p, s, d := base()
	want := ref(p, s, d, testKey)
	differs := map[string]string{
		"provider":   ref("vault", s, d, testKey),
		"secret":     ref(p, "prod/db2", d, testKey),
		"value":      ref(p, s, map[string]string{"a": "1", "b": "3"}, testKey),
		"key name":   ref(p, s, map[string]string{"a": "1", "bb": "2"}, testKey),
		"extra pair": ref(p, s, map[string]string{"a": "1", "b": "2", "c": "3"}, testKey),
		"key bytes":  ref(p, s, d, bytes.Repeat([]byte{0x43}, HMACKeySize)),
		"swap":       ref("prod/db", "aws", d, testKey), // provider and secret are not interchangeable
	}
	for what, got := range differs {
		if got == want {
			t.Errorf("changing the %s must change the identity", what)
		}
	}
}

func TestNewHMACVersion_RejectsBadInputsWithoutLeakingData(t *testing.T) {
	marker := "S3CR3T-MARKER-NEVER-IN-ERRORS"
	data := map[string]string{"password": marker}
	cases := []struct {
		name             string
		keyID            string
		key              []byte
		provider, secret string
	}{
		{"short key", testKeyID, make([]byte, 16), "aws", "s"},
		{"empty key", testKeyID, nil, "aws", "s"},
		{"long key", testKeyID, make([]byte, 64), "aws", "s"},
		{"bad key id", "key-1", testKey, "aws", "s"},
		{"empty key id", "", testKey, "aws", "s"},
		{"empty provider", testKeyID, testKey, "", "s"},
		{"empty secret", testKeyID, testKey, "aws", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := NewHMACVersion(c.keyID, c.key, c.provider, c.secret, data)
			if err == nil {
				t.Fatal("expected an error")
			}
			if (v != Version{}) {
				t.Errorf("a failed call must return the zero Version, got %+v", v)
			}
			if strings.Contains(err.Error(), marker) {
				t.Error("an error must never contain secret data")
			}
		})
	}
}

func TestNewHMACVersion_ResultCarriesNoSecretData(t *testing.T) {
	marker := "S3CR3T-MARKER-IN-VERSION"
	v, err := NewHMACVersion(testKeyID, testKey, "aws", "s", map[string]string{"p": marker})
	if err != nil {
		t.Fatal(err)
	}
	if s := fmt.Sprintf("%+v %#v", v, v); strings.Contains(s, marker) {
		t.Errorf("version rendering leaks the value: %s", s)
	}
}

func TestCompare(t *testing.T) {
	h1 := hmacVer(t, "aws", "s", "one")
	h1again := hmacVer(t, "aws", "s", "one")
	h2 := hmacVer(t, "aws", "s", "two")
	otherKey := h1
	otherKey.KeyID = "k-ffffffffffffffff" // same id string, different key id
	otherKey2 := h2
	otherKey2.KeyID = "k-ffffffffffffffff"
	p17 := Version{Kind: KindProvider, ID: "17", Source: "s1:Zm9v"}
	p17same := Version{Kind: KindProvider, ID: "17", Source: "s1:Zm9v"}
	p18 := Version{Kind: KindProvider, ID: "18", Source: "s1:Zm9v"}
	p17elsewhere := Version{Kind: KindProvider, ID: "17", Source: "s1:YmFy"}

	cases := []struct {
		name string
		a, b Version
		want Comparison
	}{
		{"hmac same value", h1, h1again, Same},
		{"hmac different value", h1, h2, Different},
		{"hmac, different key ids, identical id string", h1, otherKey, Incomparable},
		{"hmac, different key ids, different ids", h1, otherKey2, Incomparable},
		{"provider same", p17, p17same, Same},
		{"provider different id", p17, p18, Different},
		{"provider different source counts as a change", p17, p17elsewhere, Different},
		{"provider versus hmac", p17, h1, Incomparable},
		{"hmac versus provider", h1, p17, Incomparable},
		{"unknown kind on both sides", Version{Kind: "sha256", ID: "x"}, Version{Kind: "sha256", ID: "x"}, Incomparable},
		{"unknown kind versus hmac", Version{Kind: "sha256", ID: h1.ID, KeyID: h1.KeyID}, h1, Incomparable},
		{"zero versions", Version{}, Version{}, Incomparable},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("%s: Compare = %s, want %s", c.name, got, c.want)
		}
		if got := Compare(c.b, c.a); got != c.want {
			t.Errorf("%s (reversed): Compare = %s, want %s", c.name, got, c.want)
		}
	}
	// Incomparable is never reported as equal, whatever the caller does with it.
	if Incomparable == Same || Incomparable == Different {
		t.Fatal("Incomparable must be a distinct result")
	}
	if Compare(Version{}, Version{}).String() != "incomparable" || Same.String() != "same" || Different.String() != "different" {
		t.Error("unexpected Comparison strings")
	}
}

func TestValidKeyID(t *testing.T) {
	for id, want := range map[string]bool{
		"k-3f9a1c0e7b2d4a55":   true,
		"k-0000000000000000":   true,
		"k-3F9A1C0E7B2D4A55":   false, // lowercase only
		"k-3f9a1c0e7b2d4a5":    false,
		"k-3f9a1c0e7b2d4a555":  false,
		"3f9a1c0e7b2d4a55":     false,
		"K-3f9a1c0e7b2d4a55":   false,
		"k-3f9a1c0e7b2d4a5g":   false,
		"":                     false,
		"k-":                   false,
		"k-3f9a1c0e7b2d4a55\n": false,
	} {
		if got := ValidKeyID(id); got != want {
			t.Errorf("ValidKeyID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestVersionValidate_AcceptsBothKindsAndRejectsBadForms(t *testing.T) {
	ok := []Version{
		hmacVer(t, "aws", "s", "x"),
		{Kind: KindProvider, ID: "17"},
		{Kind: KindProvider, ID: "arn:aws:secretsmanager:version/abc", Source: "s1:Zm9vYmFy"},
	}
	for _, v := range ok {
		if err := v.validate("v", testKeyID); err != nil {
			t.Errorf("%+v must be valid: %v", v, err)
		}
	}
	bad := []Version{
		{},
		{Kind: "SHA256", ID: "x"},
		{Kind: KindProvider, ID: "17", Source: "s1:"},
		{Kind: KindProvider, ID: "17", Source: "s1:!!!"},
		{Kind: KindProvider, ID: "17", Source: "s2:Zm9v"},
		{Kind: KindProvider, ID: "bad\nid"},
		{Kind: KindHMAC, ID: "", KeyID: testKeyID},
		{Kind: KindHMAC, ID: strings.Repeat("A", 43), KeyID: ""},
	}
	for _, v := range bad {
		if err := v.validate("v", testKeyID); err == nil {
			t.Errorf("%+v must be rejected", v)
		}
	}
}
