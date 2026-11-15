package ledger

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Deterministic fixtures. The key below is a test-only constant; the package
// has no key of its own and the production code never sees this value.
var (
	testKey   = bytes.Repeat([]byte{0x42}, HMACKeySize)
	testKeyID = "k-3f9a1c0e7b2d4a55"
	t0        = time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)
)

const (
	idLedger    = "5b1f0c9e7a2d4c3b8e6f1a0d9c8b7a65"
	idRotation  = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	idRotation2 = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"
	idRequest   = "11112222333344445555666677778888"
	idRecovery  = "aaaabbbbccccddddeeeeffff00001111"
	idRecovery2 = "99998888777766665555444433332222"
	created     = "2026-11-12T06:00:00Z"
)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func vp(v Version) *Version { return &v }

func hmacVer(t *testing.T, provider, secret, value string) Version {
	t.Helper()
	v, err := NewHMACVersion(testKeyID, testKey, provider, secret, map[string]string{"value": value})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func replica(num, id, name string, applied *Version, out ReplicaOutcome, sec int) Replica {
	return Replica{Number: num, ContainerID: id, ContainerName: name, Created: created, Applied: applied, Outcome: out, At: at(sec)}
}

// sampleLedger returns a valid document that exercises every record type:
// the three-secret worked example of the design (two pending secrets and one
// unchanged), a partially delivered entry, a non-Compose in-progress entry
// with a rotation intent and a manual request, and two recovery records.
func sampleLedger(t *testing.T) *Ledger {
	t.Helper()
	dbV4, dbV5 := hmacVer(t, "aws", "database_password", "v4"), hmacVer(t, "aws", "database_password", "v5")
	apiV8, apiV9 := hmacVer(t, "aws", "api_token", "v8"), hmacVer(t, "aws", "api_token", "v9")
	sigV2 := hmacVer(t, "aws", "signing_key", "v2")
	certOld, certNew := hmacVer(t, "aws", "tls_cert", "old"), hmacVer(t, "aws", "tls_cert", "new")
	batchOld, batchNew := hmacVer(t, "vault", "kv/batch", "old"), hmacVer(t, "vault", "kv/batch", "new")

	return &Ledger{
		SchemaVersion: SchemaVersion,
		LedgerID:      idLedger,
		Generation:    42,
		HMACKeyID:     testKeyID,
		CreatedAt:     t0,
		UpdatedAt:     at(200),
		Entries: []Entry{
			{
				Key:     Key{Project: "shop", Service: "api", Provider: "aws", Secret: "database_password"},
				Desired: vp(dbV5), DesiredSeq: 5, DesiredObservedAt: at(10),
				Applied:     vp(dbV4),
				State:       EntryPending,
				Replicas:    []Replica{replica("1", "c0ffee01", "shop-api-1", vp(dbV4), ReplicaSucceeded, 5)},
				LastOutcome: &LastOutcome{Kind: "succeeded", At: at(5), Strategy: "rolling"},
			},
			{
				Key:     Key{Project: "shop", Service: "api", Provider: "aws", Secret: "api_token"},
				Desired: vp(apiV9), DesiredSeq: 2, DesiredObservedAt: at(11),
				Applied:  vp(apiV8),
				State:    EntryPending,
				Replicas: []Replica{replica("1", "c0ffee01", "shop-api-1", vp(apiV8), ReplicaSucceeded, 5)},
			},
			{
				Key:     Key{Project: "shop", Service: "api", Provider: "aws", Secret: "signing_key"},
				Desired: vp(sigV2), DesiredSeq: 1, DesiredObservedAt: at(1),
				Applied:  vp(sigV2),
				State:    EntryIdle,
				Replicas: []Replica{replica("1", "c0ffee01", "shop-api-1", vp(sigV2), ReplicaSucceeded, 5)},
			},
			{
				Key:     Key{Project: "shop", Service: "web", Provider: "aws", Secret: "tls_cert"},
				Desired: vp(certNew), DesiredSeq: 3, DesiredObservedAt: at(20),
				Applied: vp(certOld),
				State:   EntryPartial,
				Replicas: []Replica{
					replica("1", "c0ffee11", "shop-web-1", vp(certNew), ReplicaSucceeded, 30),
					replica("2", "c0ffee12", "shop-web-2", vp(certNew), ReplicaSucceeded, 31),
					{Number: "3", ContainerID: "c0ffee13", ContainerName: "shop-web-3", Created: created,
						Applied: vp(certOld), Outcome: ReplicaRolledBack, Reason: "health verification failed", At: at(45)},
				},
				LastOutcome: &LastOutcome{Kind: "partial", Reason: "1 of 3 replicas rolled back", At: at(45), Strategy: "rolling"},
			},
			{
				Key:     Key{Project: "", Service: "legacy-batch", Provider: "vault", Secret: "kv/batch"},
				Desired: vp(batchNew), DesiredSeq: 3, DesiredObservedAt: at(90),
				Applied:  vp(batchOld),
				State:    EntryInProgress,
				Replicas: []Replica{replica("", "d00dfeed", "legacy-batch", vp(batchOld), ReplicaSucceeded, 2)},
				Rotation: &Intent{
					RotationID: idRotation, StartedAt: at(100), Desired: batchNew, Strategy: "restart",
					PriorReplicas: []Replica{replica("", "d00dfeed", "legacy-batch", vp(batchOld), ReplicaSucceeded, 2)},
				},
				Request: &Redelivery{
					RequestID: idRequest, RequestedAt: at(95), Actor: "alice", Reason: "manual",
					TargetSeq: 3, State: RedeliveryDispatched, RotationID: idRotation,
				},
			},
		},
		Recoveries: []RecoveryRecord{
			{
				RecoveryID: idRecovery,
				Scope:      RecoveryScope{Project: "shop", Service: "api"},
				Trigger:    TriggerLedgerIntent,
				Planned: []Action{
					{Op: ActionStop, ContainerID: "feed01", ContainerName: "shop-api_dso_new_1", Created: created},
					{Op: ActionRemove, ContainerID: "feed01", ContainerName: "shop-api_dso_new_1", Created: created},
				},
				Done:      []ActionResult{{Outcome: ActionOK}},
				State:     RecoveryInProgress,
				StartedAt: at(150),
			},
			{
				RecoveryID: idRecovery2,
				Scope:      RecoveryScope{ContainerName: "api_dso_backup_1731398400"},
				Trigger:    TriggerOrphanScan,
				Planned:    []Action{{Op: ActionRemove, ContainerID: "feed02", ContainerName: "api_dso_backup_1731398400", Created: created}},
				Done:       []ActionResult{{Outcome: ActionAlreadyDone, Reason: "container already gone"}},
				State:      RecoveryDone,
				StartedAt:  at(151), FinishedAt: at(152),
			},
		},
	}
}

func TestSampleLedger_IsValid(t *testing.T) {
	if err := sampleLedger(t).Validate(); err != nil {
		t.Fatalf("the reference document must be valid: %v", err)
	}
}

// The document model must match the schema of the design exactly: no field is
// invented, none is missing, and nothing can hold secret data.
func TestSchema_FieldsMatchTheDesign(t *testing.T) {
	want := map[reflect.Type][]string{
		reflect.TypeOf(Ledger{}):         {"schema_version", "ledger_id", "generation", "hmac_key_id", "created_at", "updated_at", "body_sha256", "entries", "recoveries"},
		reflect.TypeOf(Entry{}):          {"key", "desired", "desired_seq", "desired_observed_at", "applied", "state", "replicas", "rotation", "request", "last_outcome"},
		reflect.TypeOf(Key{}):            {"project", "service", "provider", "secret"},
		reflect.TypeOf(Version{}):        {"kind", "id", "key_id", "source"},
		reflect.TypeOf(Replica{}):        {"number", "container_id", "container_name", "created", "applied", "outcome", "reason", "at"},
		reflect.TypeOf(Intent{}):         {"rotation_id", "started_at", "desired", "strategy", "prior_replicas", "supersedes"},
		reflect.TypeOf(Redelivery{}):     {"request_id", "requested_at", "actor", "reason", "target_seq", "state", "rotation_id", "superseded_by"},
		reflect.TypeOf(LastOutcome{}):    {"kind", "reason", "at", "strategy"},
		reflect.TypeOf(RecoveryScope{}):  {"project", "service", "container_name"},
		reflect.TypeOf(Action{}):         {"op", "container_id", "container_name", "created"},
		reflect.TypeOf(ActionResult{}):   {"outcome", "reason"},
		reflect.TypeOf(RecoveryRecord{}): {"recovery_id", "scope", "trigger", "planned", "done", "state", "started_at", "finished_at"},
	}
	for typ, tags := range want {
		var got []string
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				t.Errorf("%s.%s has no json name", typ.Name(), f.Name)
			}
			got = append(got, name)
			switch f.Type.Kind() {
			case reflect.Map, reflect.Interface, reflect.Func, reflect.Chan:
				t.Errorf("%s.%s has kind %s: a ledger field must not be able to carry arbitrary secret data", typ.Name(), f.Name, f.Type.Kind())
			}
		}
		if !reflect.DeepEqual(got, tags) {
			t.Errorf("%s json fields = %v, want %v", typ.Name(), got, tags)
		}
	}
}

// A container id is a replica attribute, never part of an entry's key.
func TestKey_HasNoContainerIdentity(t *testing.T) {
	typ := reflect.TypeOf(Key{})
	if typ.NumField() != 4 {
		t.Fatalf("Key has %d fields, want exactly project, service, provider, secret", typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		if strings.Contains(strings.ToLower(typ.Field(i).Name), "container") {
			t.Errorf("Key.%s: a container id must not be part of the key", typ.Field(i).Name)
		}
	}
}

func TestEntries_ProjectServiceProviderSecretAreSeparateDimensions(t *testing.T) {
	l := sampleLedger(t)
	base := l.Entries[0]
	ver := hmacVer(t, "aws", "database_password", "x")
	mk := func(k Key) Entry {
		e := base
		e.Key = k
		e.Desired, e.Applied, e.DesiredSeq = vp(ver), vp(ver), 1
		e.State = EntryIdle
		e.Replicas = nil
		e.LastOutcome = nil
		return e
	}
	// Each dimension on its own gives a distinct, acceptable entry.
	for _, k := range []Key{
		{Project: "shop", Service: "api", Provider: "aws", Secret: "other_secret"},
		{Project: "shop", Service: "api", Provider: "vault", Secret: "database_password"},
		{Project: "shop", Service: "worker", Provider: "aws", Secret: "database_password"},
		{Project: "blog", Service: "api", Provider: "aws", Secret: "database_password"},
		{Project: "", Service: "api", Provider: "aws", Secret: "database_password"},
	} {
		c := sampleLedger(t)
		c.Entries = []Entry{c.Entries[0], mk(k)}
		if err := c.Validate(); err != nil {
			t.Errorf("key differing in one dimension %+v must be accepted: %v", k, err)
		}
	}
	// The exact same tuple twice is rejected.
	dup := sampleLedger(t)
	dup.Entries = []Entry{dup.Entries[0], mk(dup.Entries[0].Key)}
	err := dup.Validate()
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("a duplicate (project, service, provider, secret) must be rejected, got %v", err)
	}
}

// The worked example of the amended M2-4: three secrets of one service keep
// independent desired and applied state.
func TestMultipleSecrets_KeepIndependentDesiredAndApplied(t *testing.T) {
	l := sampleLedger(t)
	raw, err := Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*Entry{}
	for i := range got.Entries {
		e := &got.Entries[i]
		if e.Key.Project == "shop" && e.Key.Service == "api" {
			byName[e.Key.Secret] = e
		}
	}
	if len(byName) != 3 {
		t.Fatalf("expected the three secrets of shop/api, got %d", len(byName))
	}
	want := map[string]Comparison{"database_password": Different, "api_token": Different, "signing_key": Same}
	for name, cmp := range want {
		e := byName[name]
		if e.Desired == nil || e.Applied == nil {
			t.Fatalf("%s lost its desired or applied identity", name)
		}
		if c := Compare(*e.Applied, *e.Desired); c != cmp {
			t.Errorf("%s: applied vs desired = %s, want %s", name, c, cmp)
		}
	}
	// No identity is shared between secrets: each secret's versions differ from
	// the other secrets' versions (the secret name is inside the HMAC input).
	seen := map[string]string{}
	for name, e := range byName {
		for _, v := range []*Version{e.Desired, e.Applied} {
			if other, dup := seen[v.ID]; dup && other != name {
				t.Errorf("%s and %s share a version identity", name, other)
			}
			seen[v.ID] = name
		}
	}
	// Changing one secret's desired state leaves the others untouched.
	e := sampleLedger(t)
	before := append([]Entry(nil), e.Entries...)
	e.Entries[1].Desired = vp(hmacVer(t, "aws", "api_token", "v10"))
	e.Entries[1].DesiredSeq++
	for i := range e.Entries {
		if i == 1 {
			continue
		}
		if !reflect.DeepEqual(e.Entries[i], before[i]) {
			t.Errorf("entry %d changed when only api_token's desired state was replaced", i)
		}
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPerReplicaState_PartialSuccessIsRepresented(t *testing.T) {
	got, err := Unmarshal(mustMarshal(t, sampleLedger(t)))
	if err != nil {
		t.Fatal(err)
	}
	var cert *Entry
	for i := range got.Entries {
		if got.Entries[i].Key.Secret == "tls_cert" {
			cert = &got.Entries[i]
		}
	}
	if cert == nil || cert.State != EntryPartial || len(cert.Replicas) != 3 {
		t.Fatalf("partial entry not preserved: %+v", cert)
	}
	// Two replicas hold the desired version, the third kept its previous one,
	// and the service-level applied version did not advance.
	for i, wantCmp := range []Comparison{Same, Same, Different} {
		if c := Compare(*cert.Replicas[i].Applied, *cert.Desired); c != wantCmp {
			t.Errorf("replica %d applied vs desired = %s, want %s", i+1, c, wantCmp)
		}
	}
	if Compare(*cert.Applied, *cert.Desired) != Different {
		t.Error("entry-level applied must still differ from desired after partial success")
	}
	if Compare(*cert.Applied, *cert.Replicas[2].Applied) != Same {
		t.Error("the failed replica must retain the entry's previous applied version")
	}
	if cert.Replicas[2].Outcome != ReplicaRolledBack || cert.Replicas[0].Outcome != ReplicaSucceeded {
		t.Error("per-replica outcomes were not preserved")
	}
}

func mustMarshal(t *testing.T, l *Ledger) []byte {
	t.Helper()
	b, err := Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidate_RejectsInvalidDocuments(t *testing.T) {
	bad := time.Date(2026, 11, 12, 9, 0, 0, 0, time.FixedZone("x", 3600))
	cases := []struct {
		name   string
		mutate func(l *Ledger)
		want   string // substring of the error (a field path or key phrase)
	}{
		{"unknown version kind", func(l *Ledger) { l.Entries[0].Desired.Kind = "sha256" }, "kind"},
		{"empty version kind", func(l *Ledger) { l.Entries[0].Applied.Kind = "" }, "kind"},
		{"hmac without key id", func(l *Ledger) { l.Entries[0].Desired.KeyID = "" }, "key_id"},
		{"hmac with another key id", func(l *Ledger) { l.Entries[0].Desired.KeyID = "k-0000000000000000" }, "key_id"},
		{"hmac id wrong length", func(l *Ledger) { l.Entries[0].Desired.ID = "AAAA" }, "id"},
		{"hmac id padded", func(l *Ledger) { l.Entries[0].Desired.ID += "=" }, "id"},
		{"hmac with source", func(l *Ledger) { l.Entries[0].Desired.Source = "s1:Zm9v" }, "source"},
		{"provider with key id", func(l *Ledger) {
			l.Entries[0].Desired = &Version{Kind: KindProvider, ID: "17", KeyID: testKeyID}
		}, "key_id"},
		{"provider empty id", func(l *Ledger) { l.Entries[0].Desired = &Version{Kind: KindProvider} }, "id"},
		{"provider bad source", func(l *Ledger) {
			l.Entries[0].Desired = &Version{Kind: KindProvider, ID: "17", Source: "plain"}
		}, "source"},
		{"ledger key id empty with hmac versions", func(l *Ledger) { l.HMACKeyID = "" }, "key_id"},
		{"ledger key id malformed", func(l *Ledger) { l.HMACKeyID = "key-1" }, "hmac_key_id"},
		{"bad ledger id", func(l *Ledger) { l.LedgerID = "XYZ" }, "ledger_id"},
		{"unsupported schema", func(l *Ledger) { l.SchemaVersion = 1 }, "schema_version"},
		{"zero created_at", func(l *Ledger) { l.CreatedAt = time.Time{} }, "created_at"},
		{"non-UTC updated_at", func(l *Ledger) { l.UpdatedAt = bad }, "updated_at"},
		{"non-UTC replica time", func(l *Ledger) { l.Entries[0].Replicas[0].At = bad }, "UTC"},
		{"bad body checksum shape", func(l *Ledger) { l.BodySHA256 = "abc" }, "body_sha256"},
		{"desired_seq without desired", func(l *Ledger) {
			l.Entries[2].Desired = nil
			l.Entries[2].DesiredObservedAt = time.Time{}
		}, "desired_seq"},
		{"desired without seq", func(l *Ledger) { l.Entries[0].DesiredSeq = 0 }, "desired_seq"},
		{"unknown entry state", func(l *Ledger) { l.Entries[0].State = "stuck" }, "state"},
		{"in_progress without intent", func(l *Ledger) { l.Entries[4].Rotation = nil }, "state"},
		{"intent without in_progress", func(l *Ledger) { l.Entries[0].Rotation = l.Entries[4].Rotation }, "state"},
		{"partial with every replica succeeded", func(l *Ledger) {
			l.Entries[3].Replicas[2].Outcome = ReplicaSucceeded
		}, "partial"},
		{"partial with no success", func(l *Ledger) {
			l.Entries[3].Replicas[0].Outcome = ReplicaFailed
			l.Entries[3].Replicas[1].Outcome = ReplicaFailed
		}, "partial"},
		{"adopted entry with non-adopted replica", func(l *Ledger) {
			l.Entries[2].State = EntryAdopted
		}, "outcome"},
		{"succeeded replica without applied", func(l *Ledger) { l.Entries[0].Replicas[0].Applied = nil }, "applied"},
		{"unknown replica outcome", func(l *Ledger) { l.Entries[0].Replicas[0].Outcome = "done" }, "outcome"},
		{"bad replica created", func(l *Ledger) { l.Entries[0].Replicas[0].Created = "yesterday" }, "created"},
		{"bad replica number", func(l *Ledger) { l.Entries[0].Replicas[0].Number = "one" }, "number"},
		{"duplicate container ids", func(l *Ledger) {
			l.Entries[3].Replicas[1].ContainerID = l.Entries[3].Replicas[0].ContainerID
		}, "duplicate container id"},
		{"empty secret", func(l *Ledger) { l.Entries[0].Key.Secret = "" }, "secret"},
		{"empty provider", func(l *Ledger) { l.Entries[0].Key.Provider = "" }, "provider"},
		{"control character in service", func(l *Ledger) { l.Entries[0].Key.Service = "a\nb" }, "service"},
		{"control character in reason", func(l *Ledger) { l.Entries[3].Replicas[2].Reason = "bad\x00reason" }, "reason"},
		{"intent id malformed", func(l *Ledger) { l.Entries[4].Rotation.RotationID = "nope" }, "rotation_id"},
		{"intent supersedes itself", func(l *Ledger) { l.Entries[4].Rotation.Supersedes = idRotation }, "supersedes"},
		{"intent without strategy", func(l *Ledger) { l.Entries[4].Rotation.Strategy = "" }, "strategy"},
		{"request target beyond desired_seq", func(l *Ledger) { l.Entries[4].Request.TargetSeq = 4 }, "target_seq"},
		{"unknown request state", func(l *Ledger) { l.Entries[4].Request.State = "queued" }, "state"},
		{"dispatched request without rotation", func(l *Ledger) { l.Entries[4].Request.RotationID = "" }, "rotation_id"},
		{"superseded request without successor", func(l *Ledger) {
			l.Entries[4].Request.State = RedeliverySuperseded
		}, "superseded_by"},
		{"request without desired", func(l *Ledger) {
			l.Entries[2].Request = &Redelivery{RequestID: idRequest, RequestedAt: at(1), Actor: "a", Reason: "manual", TargetSeq: 1, State: RedeliveryRequested}
			l.Entries[2].Desired = nil
			l.Entries[2].DesiredSeq = 0
			l.Entries[2].DesiredObservedAt = time.Time{}
		}, "request"},
		{"last outcome without kind", func(l *Ledger) { l.Entries[0].LastOutcome.Kind = "" }, "kind"},
		{"unknown recovery trigger", func(l *Ledger) { l.Recoveries[0].Trigger = "cron" }, "trigger"},
		{"unknown recovery state", func(l *Ledger) { l.Recoveries[0].State = "paused" }, "state"},
		{"unknown action", func(l *Ledger) { l.Recoveries[0].Planned[0].Op = "kill" }, "op"},
		{"more results than actions", func(l *Ledger) {
			l.Recoveries[0].Done = []ActionResult{{Outcome: ActionOK}, {Outcome: ActionOK}, {Outcome: ActionOK}}
		}, "done"},
		{"done record with a failed action", func(l *Ledger) { l.Recoveries[1].Done[0].Outcome = ActionFailedOp }, "done"},
		{"done record missing a result", func(l *Ledger) { l.Recoveries[1].Done = nil }, "done"},
		{"planned record with results", func(l *Ledger) { l.Recoveries[0].State = RecoveryPlanned }, "done"},
		{"finished record without finish time", func(l *Ledger) { l.Recoveries[1].FinishedAt = time.Time{} }, "finished_at"},
		{"open record with finish time", func(l *Ledger) { l.Recoveries[0].FinishedAt = at(160) }, "finished_at"},
		{"scope with both forms", func(l *Ledger) { l.Recoveries[0].Scope.ContainerName = "x" }, "scope"},
		{"scope with neither form", func(l *Ledger) { l.Recoveries[0].Scope = RecoveryScope{} }, "scope"},
		{"duplicate recovery id", func(l *Ledger) { l.Recoveries[1].RecoveryID = idRecovery }, "recovery_id"},
		{"two open records for one scope", func(l *Ledger) {
			l.Recoveries[1].Scope = l.Recoveries[0].Scope
			l.Recoveries[1].State = RecoveryPlanned
			l.Recoveries[1].Done = nil
			l.Recoveries[1].FinishedAt = time.Time{}
		}, "non-terminal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := sampleLedger(t)
			tc.mutate(l)
			err := l.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error must wrap ErrInvalid: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	if err := (*Ledger)(nil).Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a nil document must be rejected, got %v", err)
	}
}
