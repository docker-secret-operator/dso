package ledger

import (
	"testing"
)

// Owner requirement for S1.1: T-U42 and T-U44 each, independently, verify the
// F-1 and F-2 fixture integrity of design §49 for all three active
// HMAC-bearing fixtures: V9, V10 and the §31 example. Each of the six
// test/fixture combinations below recomputes every HMAC identity with
// NewHMACVersion, verifies the derived key id, verifies that each identity is
// bound to the provider and secret of its own entry key, and proves that the
// check detects placeholder and incorrect identity data.

// hmacFixture is one active fixture: how many HMAC identities it must contain,
// every in-memory form the tests use for it, and a function returning a fresh
// copy parsed from its frozen document (used for the negative controls).
type hmacFixture struct {
	name       string
	identities int
	forms      func(t *testing.T) map[string]*Ledger
	fresh      func(t *testing.T) *Ledger
}

func parseFrozen(t *testing.T, doc string) *Ledger {
	t.Helper()
	l, err := Unmarshal([]byte(doc))
	if err != nil {
		t.Fatalf("a frozen vector must parse: %v", err)
	}
	return l
}

func activeHMACFixtures() []hmacFixture {
	return []hmacFixture{
		{
			name: "V9", identities: 4,
			forms: func(t *testing.T) map[string]*Ledger {
				return map[string]*Ledger{
					"built, sorted replicas":   buildV9(t, false),
					"built, reversed replicas": buildV9(t, true),
					"frozen document":          parseFrozen(t, vecV9Doc),
				}
			},
			fresh: func(t *testing.T) *Ledger { return parseFrozen(t, vecV9Doc) },
		},
		{
			name: "V10", identities: 9,
			forms: func(t *testing.T) map[string]*Ledger {
				return map[string]*Ledger{
					"built, forward input":  buildV10(t, false),
					"built, reversed input": buildV10(t, true),
					"frozen document":       parseFrozen(t, vecV10Doc),
					"frozen reversed file":  parseFrozen(t, vecV10ReversedFile),
				}
			},
			fresh: func(t *testing.T) *Ledger { return parseFrozen(t, vecV10Doc) },
		},
		{
			name: "section 31 example", identities: 8,
			forms: func(t *testing.T) map[string]*Ledger {
				return map[string]*Ledger{"frozen indented document": parseFrozen(t, vecExampleIndented)}
			},
			fresh: func(t *testing.T) *Ledger { return parseFrozen(t, vecExampleIndented) },
		},
	}
}

// identityVersions returns every HMAC identity of a document together with the
// entry key it belongs to.
func identityVersions(l *Ledger) (out []struct {
	v *Version
	k Key
}) {
	add := func(v *Version, k Key) {
		if v != nil && v.Kind == KindHMAC {
			out = append(out, struct {
				v *Version
				k Key
			}{v, k})
		}
	}
	for i := range l.Entries {
		e := &l.Entries[i]
		add(e.Desired, e.Key)
		add(e.Applied, e.Key)
		for j := range e.Replicas {
			add(e.Replicas[j].Applied, e.Key)
		}
	}
	return out
}

// requireHMACFixtureIntegrity runs the whole contract for one test label.
func requireHMACFixtureIntegrity(t *testing.T, label string) {
	derived := derivedKeyID(fixtureKey)
	// F-1: the derivation itself equals the value stated in the design.
	if derived != vecFixtureKeyID {
		t.Fatalf("%s: derived key id %s, design §49 F-1 says %s", label, derived, vecFixtureKeyID)
	}
	for _, fx := range activeHMACFixtures() {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			for form, l := range fx.forms(t) {
				ids := identityVersions(l)
				if len(ids) != fx.identities {
					t.Errorf("%s, %s: %d HMAC identities found, want %d (the check must not be vacuous)", fx.name, form, len(ids), fx.identities)
				}
				// F-1: the header carries the derived key id, and so does every identity.
				if l.HMACKeyID != derived {
					t.Errorf("%s, %s: header hmac_key_id %q is not the derived id %q", fx.name, form, l.HMACKeyID, derived)
				}
				for _, iv := range ids {
					// F-2: recompute the identity with NewHMACVersion from the provider
					// and secret of the entry key; it must equal one of the fixture
					// identities of that secret, including its key id.
					data, known := fixtureData[iv.k.Secret]
					if !known {
						t.Errorf("%s, %s: no fixture data for secret %q", fx.name, form, iv.k.Secret)
						continue
					}
					matched := false
					for _, d := range data {
						want, err := NewHMACVersion(derived, fixtureKey, iv.k.Provider, iv.k.Secret, d)
						if err != nil {
							t.Fatal(err)
						}
						if *iv.v == want {
							matched = true
						}
					}
					if !matched {
						t.Errorf("%s, %s: an identity of %s/%s is not the MAC of fixture data under that provider and secret", fx.name, form, iv.k.Provider, iv.k.Secret)
					}
				}
				if problems := fixtureIntegrity(l); len(problems) != 0 {
					t.Errorf("%s, %s: %v", fx.name, form, problems)
				}
			}

			// The check must detect incorrect or placeholder data. Each control
			// starts from a fresh parse of the frozen document.
			controls := map[string]func(l *Ledger){
				"placeholder header key id":          func(l *Ledger) { l.HMACKeyID = vecPlaceholderKeyID },
				"another valid key id in the header": func(l *Ledger) { l.HMACKeyID = "k-ffffffffffffffff" },
				"entry provider changed": func(l *Ledger) {
					for i := range l.Entries {
						l.Entries[i].Key.Provider = "vault"
					}
				},
				"entry secret changed to another fixture secret": func(l *Ledger) {
					for i := range l.Entries {
						if l.Entries[i].Key.Secret == "db" {
							l.Entries[i].Key.Secret = "kv/batch"
						} else {
							l.Entries[i].Key.Secret = "db"
						}
					}
				},
				"identity value tampered": func(l *Ledger) {
					v := *l.Entries[0].Desired
					b := []byte(v.ID)
					if b[0] == 'A' {
						b[0] = 'B'
					} else {
						b[0] = 'A'
					}
					v.ID = string(b)
					l.Entries[0].Desired = &v
				},
				"identity key id replaced by a placeholder": func(l *Ledger) {
					v := *l.Entries[0].Desired
					v.KeyID = vecPlaceholderKeyID
					l.Entries[0].Desired = &v
				},
				"replica identity from another secret": func(l *Ledger) {
					other := "kv/batch"
					if l.Entries[0].Key.Secret == "kv/batch" {
						other = "db"
					}
					v := fxVer(t, l.Entries[0].Key.Provider, other, 0)
					l.Entries[0].Replicas[0].Applied = v
				},
			}
			for name, mutate := range controls {
				l := fx.fresh(t)
				if len(fixtureIntegrity(l)) != 0 {
					t.Fatalf("%s: the unmodified fixture must pass before a control is applied", fx.name)
				}
				mutate(l)
				if len(fixtureIntegrity(l)) == 0 {
					t.Errorf("%s: the control %q was not detected", fx.name, name)
				}
			}
		})
	}
}

// T-U42: fixture integrity of V9, V10 and the §31 example.
func TestT_U42_FixtureIntegrity_V9_V10_Example(t *testing.T) {
	requireHMACFixtureIntegrity(t, "T-U42")
}

// T-U44: fixture integrity of V9, V10 and the §31 example.
func TestT_U44_FixtureIntegrity_V9_V10_Example(t *testing.T) {
	requireHMACFixtureIntegrity(t, "T-U44")
}
