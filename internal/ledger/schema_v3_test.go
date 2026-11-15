package ledger

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// S2.2 (docs/design/m1-ledger-reconciliation.md section 1.3, owner decision
// O-1): schema version 3 tests that are not covered by the amended S1/S2
// tests.

// rev63V2Doc is the version-2 V2 vector of BL-02 design revision 6.3, byte for
// byte: a valid sealed version-2 document as the previous library wrote it.
const rev63V2Doc = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"fe4fe2c0b9a87f9eac7045224f747ab32dce787a1bc841bfb6ba780f0b4f975d","entries":[{"key":{"project":"","service":"svc","provider":"p","secret":"s"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"rotation":null,"request":null,"last_outcome":null}],"recoveries":[]}`

// A version-2 document is not migrated: it is invalid (never a newer schema),
// so Open preserves it aside byte for byte under the existing corrupt-ledger
// rule and starts a fresh, empty (all UNKNOWN) version-3 ledger. The old file
// is never rewritten in place.
func TestS22_Version2File_PreservedAsCorrupt_FreshV3Ledger(t *testing.T) {
	if _, err := Unmarshal([]byte(rev63V2Doc)); err == nil || !strings.Contains(err.Error(), "unsupported schema version 2") {
		t.Fatalf("a version-2 document must be rejected as invalid: %v", err)
	}
	dir := stateDir(t)
	writeFile(t, filepath.Join(dir, "ledger.json"), []byte(rev63V2Doc), 0o600)
	fk := newFakeKeys(validKey(vecPlaceholderKeyID))
	s, res, log, _ := openT(t, dir, fk)
	if res.Mode != ModeDurable || res.Reason != ReasonCorrupt || res.Preserved == "" {
		t.Fatalf("a v2 file must take the corrupt-ledger path: %+v", res)
	}
	got, err := os.ReadFile(res.Preserved)
	if err != nil || !bytes.Equal(got, []byte(rev63V2Doc)) {
		t.Fatalf("the v2 file must be preserved byte for byte: %v", err)
	}
	l := s.Snapshot().Ledger
	if l.SchemaVersion != 3 || len(l.Entries) != 0 || l.HMACKeyID != vecPlaceholderKeyID {
		t.Fatalf("a fresh v3 ledger bound to the existing key was expected: %+v", l)
	}
	if fk.calls() != 0 || log.count(EventPreserved) != 1 {
		t.Fatalf("no key generation, one preservation event: %d %v", fk.calls(), log.kinds())
	}
	if onDisk, err := Unmarshal(readLedgerBytes(t, dir)); err != nil || onDisk.SchemaVersion != 3 {
		t.Fatalf("the new ledger is persisted as version 3: %v", err)
	}
}

// The retired surface is gone from the schema and the API: no in_progress
// state, no intent, no recovery journal, and the whole-document mutators are
// unexported.
func TestS22_RetiredSurfaceIsAbsent(t *testing.T) {
	if validEntryState("in_progress") {
		t.Error("in_progress must not be an entry state")
	}
	for _, f := range []string{"Rotation"} {
		if _, ok := reflect.TypeOf(Entry{}).FieldByName(f); ok {
			t.Errorf("Entry.%s must not exist in schema 3", f)
		}
	}
	if _, ok := reflect.TypeOf(Ledger{}).FieldByName("Recoveries"); ok {
		t.Error("Ledger.Recoveries must not exist in schema 3")
	}
	st := reflect.TypeOf(&Store{})
	for _, m := range []string{"Update", "UpdateMemoryOnly", "BeginRecovery", "CompleteRecovery", "RecoveryRecords"} {
		if _, ok := st.MethodByName(m); ok {
			t.Errorf("Store.%s must not be exported", m)
		}
	}
	// KEEP-UNWIRED surface stays available to the library.
	for _, m := range []string{"RequestRedelivery", "Gate", "Get", "Snapshot", "Health", "Close"} {
		if _, ok := st.MethodByName(m); !ok {
			t.Errorf("Store.%s must remain", m)
		}
	}
}

// Every schema-3 replica combination the validator accepts satisfies the
// relations of design section 1.3, checked over the full product of outcome,
// delivery, adoption, applied and rotation id.
func TestS22_ReplicaDeliveryRelations_Exhaustive(t *testing.T) {
	v := fxVer0("1")
	accepted := 0
	for _, out := range allReplicaOutcomes {
		for _, d := range []DeliveryState{DeliveryNone, DeliveryRequested, DeliveryVerified} {
			for _, a := range []AdoptionState{AdoptionUnknown, AdoptionUnverified, AdoptionVerified} {
				for _, applied := range []*Version{nil, v} {
					for _, rid := range []string{"", "rot-0000000000000001"} {
						r := Replica{ContainerID: "c", ContainerName: "n", Created: created0, At: t0,
							Outcome: out, Delivery: d, Adoption: a, Applied: applied, RotationID: rid}
						err := r.validate("r", derivedKeyID(fixtureKey))
						want := true
						switch {
						case a != AdoptionUnknown && applied == nil:
							want = false
						case d != DeliveryNone && rid == "":
							want = false
						case d == DeliveryRequested && out != ReplicaPending:
							want = false
						case d == DeliveryVerified && (applied == nil || a == AdoptionUnknown):
							want = false
						case (d == DeliveryVerified) != (out == ReplicaSucceeded):
							want = false
						case out == ReplicaAdopted && (applied != nil || d != DeliveryNone || a != AdoptionUnknown):
							want = false
						}
						if (err == nil) != want {
							t.Errorf("outcome %s delivery %s adoption %s applied %v rid %q: accepted=%v want %v (%v)",
								out, d, a, applied != nil, rid, err == nil, want, err)
						}
						if err == nil {
							accepted++
						}
					}
				}
			}
		}
	}
	if accepted == 0 {
		t.Fatal("vacuous")
	}
}
