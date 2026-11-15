package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Fixtures of BL-02 design revision 6.3 (sections 31, 45 and 49). The expected
// documents and checksums live in vectors_test.go and are copied from the
// design; the builders below only construct the logical input.

// fixtureKey is the fixture key of design section 49: 32 bytes of 0x42.
var fixtureKey = bytes.Repeat([]byte{0x42}, HMACKeySize)

// derivedKeyID is the key-id derivation of design §4 item 2:
// "k-" + hex(first 8 bytes of SHA-256("dso-ledger-key-id-v1" || key)). The
// package itself does not derive key ids (that is S3); the fixtures do, so that
// a header id can be checked against its fixture key (§49 F-1).
func derivedKeyID(key []byte) string {
	sum := sha256.Sum256(append([]byte("dso-ledger-key-id-v1"), key...))
	return "k-" + hex.EncodeToString(sum[:8])
}

// fixtureData maps each fixture secret name to the data behind its new
// (index 0) and old (index 1) identity (design §49).
var fixtureData = map[string][2]map[string]string{
	"kv/batch":         {{"token": "NEW-FIXTURE-2"}, {"token": "OLD-FIXTURE-2"}},
	"db":               {{"p": "2"}, {"p": "1"}},
	"prod/db_password": {{"password": "NEW-FIXTURE-1"}, {"password": "OLD-FIXTURE-1"}},
}

// fxVer builds the identity of fixture data with the production encoder,
// bound to the given provider and secret (design §5.2, §49 F-2).
func fxVer(t *testing.T, provider, secret string, which int) *Version {
	t.Helper()
	v, err := NewHMACVersion(derivedKeyID(fixtureKey), fixtureKey, provider, secret, fixtureData[secret][which])
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

// fixtureIntegrity checks a fixture against rules F-1 and F-2 of design §49 and
// returns every violation: the header hmac_key_id must be the id derived from
// the fixture key, and every HMAC identity in the document must equal the MAC of
// fixture data computed with the provider and secret of its own entry key. The
// expected values are recomputed with NewHMACVersion, never copied.
func fixtureIntegrity(l *Ledger) []string {
	var problems []string
	derived := derivedKeyID(fixtureKey)
	if l.HMACKeyID != derived {
		problems = append(problems, "header hmac_key_id is not the id derived from the fixture key")
	}
	for i := range l.Entries {
		e := &l.Entries[i]
		data, known := fixtureData[e.Key.Secret]
		if !known {
			problems = append(problems, "entry "+e.Key.Secret+": no fixture data for its secret")
			continue
		}
		matches := func(v *Version) bool {
			for _, d := range data {
				want, err := NewHMACVersion(derived, fixtureKey, e.Key.Provider, e.Key.Secret, d)
				if err == nil && *v == want {
					return true
				}
			}
			return false
		}
		check := func(where string, v *Version) {
			if v != nil && !matches(v) {
				problems = append(problems, "entry "+e.Key.Secret+" "+where+": identity is not the MAC of fixture data under the provider and secret of its entry key")
			}
		}
		check("desired", e.Desired)
		check("applied", e.Applied)
		for j := range e.Replicas {
			check("replica applied", e.Replicas[j].Applied)
		}
		if e.Rotation != nil {
			check("intent desired", &e.Rotation.Desired)
			for j := range e.Rotation.PriorReplicas {
				check("prior replica applied", e.Rotation.PriorReplicas[j].Applied)
			}
		}
	}
	return problems
}

const created0 = "2026-11-12T06:00:00Z"

// vecHeader is the common header of the vectors (design §31): ledger id 32 x
// 'a', generation 1, both times 2026-11-12T08:00:00Z. keyID is the header
// hmac_key_id.
func vecHeader(keyID string) *Ledger {
	return &Ledger{SchemaVersion: SchemaVersion, LedgerID: strings.Repeat("a", 32), Generation: 1, HMACKeyID: keyID, CreatedAt: t0, UpdatedAt: t0}
}

// vecPlaceholderKeyID is the header id of the vectors that contain no HMAC
// identity and state no key (design §49 F-3).
const vecPlaceholderKeyID = "k-0123456789abcdef"

func idleEntry(project, service, provider, secret string) Entry {
	return Entry{Key: Key{Project: project, Service: service, Provider: provider, Secret: secret}, State: EntryIdle}
}

func reversedReplicas(s []Replica) []Replica {
	c := append([]Replica(nil), s...)
	for i, j := 0, len(c)-1; i < j; i, j = i+1, j-1 {
		c[i], c[j] = c[j], c[i]
	}
	return c
}

func buildV1() *Ledger { return vecHeader(vecPlaceholderKeyID) }

func buildV2(emptySlices bool) *Ledger {
	l := vecHeader(vecPlaceholderKeyID)
	e := idleEntry("", "svc", "p", "s")
	if emptySlices {
		e.Replicas = []Replica{}
		l.Recoveries = []RecoveryRecord{}
	}
	l.Entries = []Entry{e}
	return l
}

func buildV4() *Ledger {
	l := vecHeader(vecPlaceholderKeyID)
	l.Entries = []Entry{idleEntry("p<1>", "svc&", "p", "dbé \U0001F600")}
	return l
}

func buildV5() *Ledger {
	l := vecHeader(vecPlaceholderKeyID)
	l.CreatedAt = time.Date(2026, 11, 12, 8, 0, 0, 120000000, time.UTC)
	l.UpdatedAt = time.Date(2026, 11, 12, 8, 0, 0, 123456789, time.FixedZone("X", 0))
	return l
}

func buildV6(reversed bool) *Ledger {
	l := vecHeader(vecPlaceholderKeyID)
	a, b := idleEntry("", "a", "p", "s"), idleEntry("", "b", "p", "s")
	l.Entries = []Entry{a, b}
	if reversed {
		l.Entries = []Entry{b, a}
	}
	return l
}

func buildV8(reversed bool) *Ledger {
	l := vecHeader(vecPlaceholderKeyID)
	mk := func(idc, svc string, off time.Duration) RecoveryRecord {
		return RecoveryRecord{RecoveryID: strings.Repeat(idc, 32), Scope: RecoveryScope{Project: "p", Service: svc}, Trigger: TriggerOrphanScan,
			State: RecoveryPlanned, StartedAt: t0.Add(off)}
	}
	r1, r2 := mk("1", "s1", time.Second), mk("2", "s2", 2*time.Second)
	l.Recoveries = []RecoveryRecord{r1, r2}
	if reversed {
		l.Recoveries = []RecoveryRecord{r2, r1}
	}
	return l
}

func replicaAt(num, cid, name string, applied *Version, out ReplicaOutcome) Replica {
	return Replica{Number: num, ContainerID: cid, ContainerName: name, Created: created0, Applied: applied, Outcome: out, At: t0}
}

// buildV9 is the replica-order vector (design §31, corrected in revision 6.3):
// derived header key id, HMAC identities bound to provider aws and secret db.
func buildV9(t *testing.T, reversed bool) *Ledger {
	va := fxVer(t, "aws", "db", 1)
	r1 := replicaAt("1", "c1", "shop-api-1", va, ReplicaSucceeded)
	r2 := replicaAt("2", "c2", "shop-api-2", va, ReplicaSucceeded)
	l := vecHeader(derivedKeyID(fixtureKey))
	e := Entry{Key: Key{Project: "shop", Service: "api", Provider: "aws", Secret: "db"}, Desired: va, DesiredSeq: 1, Applied: va, State: EntryIdle,
		Replicas: []Replica{r1, r2}}
	if reversed {
		e.Replicas = []Replica{r2, r1}
	}
	l.Entries = []Entry{e}
	return l
}

func vecAction(op ActionOp, cid, name string) Action {
	return Action{Op: op, ContainerID: cid, ContainerName: name, Created: created0}
}

// buildV10 is the frozen nested-slice vector of design §45.1. With reversed
// true every orderable slice is supplied in reverse order, while the planned
// actions and done results of recovery R1 keep their (positional) order.
func buildV10(t *testing.T, reversed bool) *Ledger {
	v1 := fxVer(t, "aws", "kv/batch", 1)
	v2 := fxVer(t, "aws", "kv/batch", 0)
	w1 := fxVer(t, "aws", "db", 1)
	w2 := fxVer(t, "aws", "db", 0)
	leg := []Replica{replicaAt("", "c-leg-1", "legacy-1", v1, ReplicaSucceeded), replicaAt("", "c-leg-2", "legacy-2", v1, ReplicaSucceeded)}
	prior := []Replica{replicaAt("", "c-leg-1", "legacy-1", v1, ReplicaSucceeded), replicaAt("", "c-leg-2", "legacy-2", v1, ReplicaSucceeded)}
	api := []Replica{replicaAt("", "c-api-1", "shop-api-1", w2, ReplicaSucceeded), replicaAt("", "c-api-2", "shop-api-2", w2, ReplicaSucceeded),
		replicaAt("", "c-api-3", "shop-api-3", w1, ReplicaRolledBack)}
	e1 := Entry{Key: Key{Service: "legacy", Provider: "aws", Secret: "kv/batch"}, Desired: v2, DesiredSeq: 3, Applied: v1, State: EntryInProgress, Replicas: leg,
		Rotation: &Intent{RotationID: strings.Repeat("a", 32), StartedAt: t0, Desired: *v2, Strategy: "restart", PriorReplicas: prior}}
	e2 := Entry{Key: Key{Project: "shop", Service: "api", Provider: "aws", Secret: "db"}, Desired: w2, DesiredSeq: 7, Applied: w1, State: EntryPartial, Replicas: api,
		LastOutcome: &LastOutcome{Kind: "partial", Reason: "1 of 3 replicas rolled back", At: t0, Strategy: "rolling"}}
	r1 := RecoveryRecord{RecoveryID: strings.Repeat("1", 32), Scope: RecoveryScope{Project: "p", Service: "s1"}, Trigger: TriggerOrphanScan,
		Planned: []Action{vecAction(ActionStop, "c-x1", "svc_dso_new_"), vecAction(ActionRemove, "c-x1", "svc_dso_new_"), vecAction(ActionStart, "c-x2", "svc")},
		Done:    []ActionResult{{Outcome: ActionOK}, {Outcome: ActionAlreadyDone}}, State: RecoveryInProgress, StartedAt: t0.Add(time.Second)}
	r2 := RecoveryRecord{RecoveryID: strings.Repeat("2", 32), Scope: RecoveryScope{Project: "p", Service: "s2"}, Trigger: TriggerLedgerIntent,
		Planned: []Action{vecAction(ActionRemove, "c-y1", "svc2_dso_backup_")}, State: RecoveryPlanned, StartedAt: t0.Add(2 * time.Second)}
	l := vecHeader(derivedKeyID(fixtureKey))
	l.Entries = []Entry{e1, e2}
	l.Recoveries = []RecoveryRecord{r1, r2}
	if reversed {
		e1.Replicas = reversedReplicas(e1.Replicas)
		rot := *e1.Rotation
		rot.PriorReplicas = reversedReplicas(rot.PriorReplicas)
		e1.Rotation = &rot
		e2.Replicas = reversedReplicas(e2.Replicas)
		l.Entries = []Entry{e2, e1}
		l.Recoveries = []RecoveryRecord{r2, r1}
	}
	return l
}

// snapshot renders every field of a document, in slice order, so that any
// change of content or order is visible.
func snapshot(t *testing.T, l *Ledger) string {
	t.Helper()
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
