package ledger

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

// bodyOf is the canonical body of a sealed document: the document with the
// checksum emptied (design §16.2 rule 9).
func bodyOf(doc, sha string) string {
	return strings.Replace(doc, `"body_sha256":"`+sha+`"`, `"body_sha256":""`, 1)
}

func TestFixtureKeyID_IsTheDerivedId(t *testing.T) {
	if got := derivedKeyID(fixtureKey); got != vecFixtureKeyID {
		t.Fatalf("derived key id %s, design §49 F-1 says %s", got, vecFixtureKeyID)
	}
}

// T-U36: the canonical serialization of design §16.2 reproduces the fixed
// vectors of §31 byte for byte through both public paths, and through Unmarshal.
func TestVectors_CanonicalSerialization(t *testing.T) {
	cases := []struct {
		name  string
		build func() *Ledger
		doc   string
		sha   string
	}{
		{"V1 empty ledger", buildV1, vecV1Doc, vecV1SHA},
		{"V2 one idle entry", func() *Ledger { return buildV2(false) }, vecV2Doc, vecV2SHA},
		{"V3 nil versus empty slices", func() *Ledger { return buildV2(true) }, vecV2Doc, vecV2SHA},
		{"V4 unicode and special characters", buildV4, vecV4Doc, vecV4SHA},
		{"V5 timestamp normalization", buildV5, vecV5Doc, vecV5SHA},
		{"V6 entries in sorted order", func() *Ledger { return buildV6(false) }, vecV6Doc, vecV6SHA},
		{"V6 entries reversed", func() *Ledger { return buildV6(true) }, vecV6Doc, vecV6SHA},
		{"V9 replicas in sorted order", func() *Ledger { return buildV9(t, false) }, vecV9Doc, vecV9SHA},
		{"V9 replicas reversed", func() *Ledger { return buildV9(t, true) }, vecV9Doc, vecV9SHA},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.build()
			before := snapshot(t, in)
			got, err := Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.doc {
				t.Fatalf("Marshal differs from the fixed vector\n got: %s\nwant: %s", got, c.doc)
			}
			body, err := CanonicalBody(in)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != bodyOf(c.doc, c.sha) {
				t.Fatalf("CanonicalBody differs from the vector with its checksum emptied\n got: %s", body)
			}
			if sum, err := ComputeBodySHA256(in); err != nil || sum != c.sha {
				t.Fatalf("ComputeBodySHA256 = %q, %v; want %s", sum, err, c.sha)
			}
			if snapshot(t, in) != before {
				t.Fatal("the input was modified")
			}
			parsed, err := Unmarshal([]byte(c.doc))
			if err != nil {
				t.Fatalf("the fixed vector must be accepted by Unmarshal: %v", err)
			}
			if again, err := Marshal(parsed); err != nil || string(again) != c.doc {
				t.Fatalf("Marshal(Unmarshal(vector)) is not the vector: %v", err)
			}
		})
	}
}

// V7: a wrong checksum is ErrChecksum wrapping ErrInvalid, never accepted.
func TestVectors_WrongChecksumIsRejected(t *testing.T) {
	_, err := Unmarshal([]byte(vecV7Doc))
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, ErrChecksum) {
		t.Fatalf("V7: expected ErrChecksum wrapping ErrInvalid, got %v", err)
	}
}

// Rule 8: no Unicode normalization. NFC and NFD spellings are distinct names,
// distinct entries and distinct checksums.
func TestCanonical_NoUnicodeNormalization(t *testing.T) {
	mk := func(secret string) *Ledger {
		l := vecHeader(vecPlaceholderKeyID)
		l.Entries = []Entry{idleEntry("", "s", "p", secret)}
		return l
	}
	nfc, nfd := "café", "café"
	a, errA := ComputeBodySHA256(mk(nfc))
	b, errB := ComputeBodySHA256(mk(nfd))
	if errA != nil || errB != nil || a == b {
		t.Fatalf("NFC and NFD must hash differently: %q %q %v %v", a, b, errA, errB)
	}
	both := vecHeader(vecPlaceholderKeyID)
	both.Entries = []Entry{idleEntry("", "s", "p", nfc), idleEntry("", "s", "p", nfd)}
	if err := both.Validate(); err != nil {
		t.Fatalf("NFC and NFD names are distinct entry keys: %v", err)
	}
}

// T-U42 (design §21.2, §45.4 g, §49): the corrected §31 example round-trips, has
// the published checksum and length, and its HMAC identities are bound to the
// provider and secret of each entry key and to the derived header key id.
func TestExample_RoundTripsAndFixtureIntegrity(t *testing.T) {
	l, err := Unmarshal([]byte(vecExampleIndented))
	if err != nil {
		t.Fatalf("the indented example must be accepted: %v", err)
	}
	if l.Entries[0].Key.Service != "legacy-batch" || l.Entries[1].Key.Service != "api" {
		t.Fatalf("the example is in sorted order, got %q then %q", l.Entries[0].Key.Service, l.Entries[1].Key.Service)
	}
	out, err := Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != vecExampleLen {
		t.Errorf("canonical length %d, design says %d", len(out), vecExampleLen)
	}
	if sum, err := ComputeBodySHA256(l); err != nil || sum != vecExampleSHA {
		t.Errorf("checksum %q (%v), design says %s", sum, err, vecExampleSHA)
	}
	if problems := fixtureIntegrity(l); len(problems) != 0 {
		t.Errorf("fixture integrity: %v", problems)
	}
	if again, err := Unmarshal(out); err != nil || !bytes.Equal(mustMarshal(t, again), out) {
		t.Errorf("the canonical form must round-trip: %v", err)
	}
}

// §49 F-1 and F-2 are enforced for every active HMAC-bearing fixture: V9, V10
// (both input orders) and the §31 example. The check is itself tested against
// the two defects found in revisions 6.2 and earlier.
func TestFixtureIntegrity_AllActiveHMACFixtures(t *testing.T) {
	example, err := Unmarshal([]byte(vecExampleIndented))
	if err != nil {
		t.Fatal(err)
	}
	for name, l := range map[string]*Ledger{
		"V9":                 buildV9(t, false),
		"V9 reversed":        buildV9(t, true),
		"V10":                buildV10(t, false),
		"V10 reversed":       buildV10(t, true),
		"section 31 example": example,
	} {
		if problems := fixtureIntegrity(l); len(problems) != 0 {
			t.Errorf("%s: %v", name, problems)
		}
		if len(l.Entries) == 0 {
			t.Errorf("%s: no entries to check", name)
		}
	}
	// The vectors parsed from the design text pass the same check.
	for name, doc := range map[string]string{"V9 document": vecV9Doc, "V10 document": vecV10Doc, "V10 reversed file": vecV10ReversedFile} {
		l, err := Unmarshal([]byte(doc))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if problems := fixtureIntegrity(l); len(problems) != 0 {
			t.Errorf("%s: %v", name, problems)
		}
	}

	// The check detects R62-01 (a placeholder header key id) and R62-02 (an
	// entry whose provider differs from the one bound into its identities).
	placeholder := buildV10(t, false)
	placeholder.HMACKeyID = vecPlaceholderKeyID
	if len(fixtureIntegrity(placeholder)) == 0 {
		t.Error("a placeholder header key id must be reported")
	}
	wrongProvider := buildV10(t, false)
	for i := range wrongProvider.Entries {
		if wrongProvider.Entries[i].Key.Secret == "kv/batch" {
			wrongProvider.Entries[i].Key.Provider = "vault"
		}
	}
	if len(fixtureIntegrity(wrongProvider)) == 0 {
		t.Error("an entry provider different from the provider in its identities must be reported")
	}
	wrongSecret := buildV9(t, false)
	wrongSecret.Entries[0].Key.Secret = "kv/batch"
	if len(fixtureIntegrity(wrongSecret)) == 0 {
		t.Error("an entry secret different from the secret in its identities must be reported")
	}
}

// T-U44 with V10 (design §45.4 a to e, §39).
func TestV10_NestedSliceVector(t *testing.T) {
	fwd, rev := buildV10(t, false), buildV10(t, true)
	fwdBefore, revBefore := snapshot(t, fwd), snapshot(t, rev)
	if fwdBefore == revBefore {
		t.Fatal("the reversed input must differ from the forward input")
	}
	for name, in := range map[string]*Ledger{"forward": fwd, "reversed": rev} {
		got, err := Marshal(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != vecV10Doc { // a, b
			t.Errorf("%s: Marshal differs from the frozen canonical document", name)
		}
		body, err := CanonicalBody(in)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != bodyOf(vecV10Doc, vecV10SHA) {
			t.Errorf("%s: CanonicalBody differs from the frozen body", name)
		}
		if sum, err := ComputeBodySHA256(in); err != nil || sum != vecV10SHA {
			t.Errorf("%s: checksum %q, %v; want %s", name, sum, err, vecV10SHA)
		}
	}
	if snapshot(t, fwd) != fwdBefore || snapshot(t, rev) != revBefore { // d
		t.Fatal("Marshal, CanonicalBody or ComputeBodySHA256 modified the caller's input")
	}

	// c: the reversed-order file with the canonical checksum is accepted and
	// returned in canonical order.
	got, err := Unmarshal([]byte(vecV10ReversedFile))
	if err != nil {
		t.Fatalf("the reversed-order file must be accepted: %v", err)
	}
	if again, err := Marshal(got); err != nil || string(again) != vecV10Doc {
		t.Fatalf("the parsed reversed file does not re-marshal to the canonical document: %v", err)
	}
	if got.Entries[0].Key.Service != "legacy" || got.Entries[1].Key.Service != "api" ||
		got.Entries[0].Replicas[0].ContainerName != "legacy-1" ||
		got.Entries[1].Replicas[2].ContainerName != "shop-api-3" {
		t.Error("Unmarshal must return entries and replicas in canonical order")
	}
	// Mutating the returned document must not touch the parsed input or the
	// vector constants (strings are immutable; the check is on a second parse).
	got.Entries[0].Replicas[0].Reason = "MUTATED"
	again, err := Unmarshal([]byte(vecV10ReversedFile))
	if err != nil || again.Entries[0].Replicas[0].Reason != "delivery_requested" {
		t.Error("a returned document must not alias shared state")
	}
	// The removed recovery and intent structures are not part of schema 3, and
	// the positional-slice negative controls of revision 6.3 went with them.
	for _, retired := range []string{`"recoveries"`, `"rotation"`, `"planned"`, `"prior_replicas"`} {
		if strings.Contains(vecV10Doc, retired) {
			t.Errorf("schema 3 vector must not contain %s", retired)
		}
	}
}

// T-U44 (design §39 b, c): the normalization behind both APIs deep-copies the
// slices it sorts and never aliases or modifies caller-owned data.
func TestNormalization_DeepCopiesAndNeverMutatesTheInput(t *testing.T) {
	in := buildV10(t, true)
	before := snapshot(t, in)
	n := normalized(in)

	n.Entries[0].Replicas[0].Reason = "MUTATED-ENTRY-REPLICA"
	n.Entries[1].Replicas[0].Reason = "MUTATED-OTHER-REPLICA"
	n.Entries[0], n.Entries[1] = n.Entries[1], n.Entries[0]
	if snapshot(t, in) != before {
		t.Fatal("normalized() aliases or modifies the caller's slices")
	}
	// The reversed input stays reversed after every public call.
	for _, call := range []func() error{
		func() error { _, err := Marshal(in); return err },
		func() error { _, err := CanonicalBody(in); return err },
		func() error { _, err := ComputeBodySHA256(in); return err },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
		if snapshot(t, in) != before {
			t.Fatal("a public call reordered or modified the caller's input")
		}
	}
	if in.Entries[0].Key.Service != "api" || in.Entries[0].Replicas[0].ContainerName != "shop-api-3" {
		t.Fatal("the caller's reversed order was not preserved")
	}
}

// Concurrent calls on one shared input: a sort of the caller's slices in place
// would be a data race (this test is meaningful under -race).
func TestCanonicalAPIs_ConcurrentCallsOnSharedInput(t *testing.T) {
	in := buildV10(t, true)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				if out, err := Marshal(in); err != nil || string(out) != vecV10Doc {
					t.Error("Marshal result changed under concurrency")
					return
				}
				if body, err := CanonicalBody(in); err != nil || string(body) != bodyOf(vecV10Doc, vecV10SHA) {
					t.Error("CanonicalBody result changed under concurrency")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// T-U44 (design §39 g, h, i): tie-breaks, the fixpoint, and duplicate keys.
func TestCanonical_TieBreaksFixpointAndDuplicateKeys(t *testing.T) {
	// Replicas with the same name and different ids are ordered by id; entries
	// differing only in project by project.
	v := fxVer(t, "aws", "db", 0)
	l := vecHeader(derivedKeyID(fixtureKey))
	l.Entries = []Entry{
		{Key: Key{Project: "b", Service: "s", Provider: "aws", Secret: "db"}, Desired: v, DesiredSeq: 1, Applied: v, State: EntryIdle,
			Replicas: []Replica{replicaAt("1", "id-2", "same", v, ReplicaSucceeded), replicaAt("2", "id-1", "same", v, ReplicaSucceeded)}},
		{Key: Key{Project: "a", Service: "s", Provider: "aws", Secret: "db"}, Desired: v, DesiredSeq: 1, Applied: v, State: EntryIdle},
	}
	out := mustMarshal(t, l)
	got, err := Unmarshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries[0].Key.Project != "a" || got.Entries[1].Replicas[0].ContainerID != "id-1" {
		t.Errorf("tie-breaks not applied: %+v", got)
	}
	if again := mustMarshal(t, got); !bytes.Equal(again, out) {
		t.Error("Marshal(Unmarshal(Marshal(x))) must equal Marshal(x)")
	}

	// A duplicate logical key: Marshal validates the unsorted input and refuses;
	// CanonicalBody does not validate and still hashes it.
	dup := vecHeader(vecPlaceholderKeyID)
	dup.Entries = []Entry{idleEntry("", "a", "p", "s"), idleEntry("", "a", "p", "s")}
	if _, err := Marshal(dup); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("Marshal must reject a duplicate logical key before sorting, got %v", err)
	}
	if _, err := CanonicalBody(dup); err != nil {
		t.Errorf("CanonicalBody does not validate: %v", err)
	}
}
