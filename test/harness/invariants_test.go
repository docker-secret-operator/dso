package harness

import (
	"strings"
	"testing"
	"time"
)

const owner = "owner-A"

func c(id, name string, running bool) CView {
	return CView{ID: id, Name: name, Running: running, Owner: owner}
}

func world() World {
	return World{Service: "app", OwnerID: owner, Strategy: Rolling, Proxied: true, OriginalID: "orig",
		Bystanders: []string{"by"}, Foreign: []string{"foreign"}}
}

func baseContainers() []CView {
	return []CView{
		{ID: "by", Name: "unrelated", Running: true},
		{ID: "foreign", Name: "app_dso_backup_1", Running: true, Owner: "owner-B"},
	}
}

func snap(rec RotationRecord, routed []string, cs ...CView) Snapshot {
	return Snapshot{Containers: append(baseContainers(), cs...), Journal: []RotationRecord{rec}, RoutedTo: routed}
}

func rec(transition, status string, mod ...func(*RotationRecord)) RotationRecord {
	r := RotationRecord{RotationID: "rot", ServiceID: "app", Strategy: "rolling", Transition: transition, Status: status,
		OriginalContainerIDs: []string{"orig"}, LastUpdate: time.Now()}
	for _, f := range mod {
		f(&r)
	}
	return r
}

func rules(vs []Violation) string {
	var r []string
	for _, v := range vs {
		r = append(r, v.Rule)
	}
	return strings.Join(r, ",")
}

func pt(name string) Point {
	for _, p := range Points {
		if p.Name == name {
			return p
		}
	}
	panic(name)
}

func TestCheckRecovery_CleanPreCandidateRecoveryPasses(t *testing.T) {
	crash := snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true), c("cand", "app_dso_new_1", true))
	final := snap(rec("candidate_ready", "recovered"), []string{"orig"}, c("orig", "app", true))
	if vs := CheckRecovery(world(), pt("rolling:after_candidate_verified"), crash, final); len(vs) != 0 {
		t.Fatalf("unexpected violations:\n%s", FormatViolations(vs))
	}
}

func TestCheckRecovery_DetectsEachViolation(t *testing.T) {
	w := world()
	ready := pt("rolling:after_candidate_verified")
	handoff := pt("rolling:after_intent_swap")
	both := []CView{c("orig", "app_dso_backup_1", true), c("cand", "app", true)}

	tests := []struct {
		name  string
		p     Point
		crash Snapshot
		final Snapshot
		want  string
	}{
		{"only workload deleted", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			snap(rec("candidate_ready", "recovered"), nil), "workload-survives"},
		{"stopped and unreported", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			snap(rec("candidate_ready", "recovered"), nil, c("orig", "app", false)), "stopped-workload-reported"},
		{"bystander removed", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			Snapshot{Containers: []CView{c("orig", "app", true), baseContainers()[1]}, Journal: []RotationRecord{rec("candidate_ready", "recovered")}, RoutedTo: []string{"orig"}}, "bystander-untouched"},
		{"foreign stopped", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			Snapshot{Containers: []CView{baseContainers()[0], {ID: "foreign", Name: "app_dso_backup_1", Running: false, Owner: "owner-B"}, c("orig", "app", true)}, Journal: []RotationRecord{rec("candidate_ready", "recovered")}, RoutedTo: []string{"orig"}}, "bystander-untouched"},
		{"original retired before commit", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app_dso_backup_1", true), c("cand", "app", true)),
			snap(rec("candidate_ready", "recovered"), []string{"cand"}, c("cand", "app", true)), "original-not-retired"},
		{"removal after handoff", handoff,
			snap(rec("intent_swap", "in_progress", func(r *RotationRecord) { r.ExpectedProxyTarget = "cand" }), nil, both...),
			snap(rec("intent_swap", "critical_error", func(r *RotationRecord) { r.ExpectedProxyTarget = "cand" }), nil, c("cand", "app", true)), "no-removal-after-handoff"},
		{"non-artifact removed", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true), c("other", "app2", true)),
			snap(rec("candidate_ready", "recovered"), []string{"orig"}, c("orig", "app", true)), "only-artifacts-removed"},
		{"marked completed", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			snap(rec("candidate_ready", "completed"), []string{"orig"}, c("orig", "app", true)), "interrupted-not-successful"},
		{"recovered without holder", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			snap(rec("candidate_ready", "recovered"), nil, c("orig", "app_dso_backup_9", true)), "recovered-needs-holder"},
		{"routed to unrelated", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			snap(rec("candidate_ready", "recovered"), []string{"ghost"}, c("orig", "app", true)), "routing-consistent"},
		{"routed to unauthorised candidate", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true), c("cand", "app_dso_new_1", true)),
			snap(rec("candidate_ready", "in_progress"), []string{"cand"}, c("orig", "app", true), c("cand", "app_dso_new_1", true)), "routing-consistent"},
		{"unreachable", ready,
			snap(rec("candidate_ready", "in_progress"), nil, c("orig", "app", true)),
			snap(rec("candidate_ready", "recovered"), nil, c("orig", "app", true)), "service-reachable"},
		{"no holder claimed recovered", pt("rolling:between_renames"),
			snap(rec("intent_swap", "in_progress"), nil, both...),
			snap(rec("intent_swap", "recovered"), []string{"cand"}, c("orig", "app_dso_backup_1", true), c("cand", "app_dso_new_1", true)), "no-holder-must-be-manual"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rules(CheckRecovery(w, tc.p, tc.crash, tc.final))
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want a %q violation, got [%s]", tc.want, got)
			}
		})
	}
}

func TestCheckRecovery_HandoffPreservedBothIsAccepted(t *testing.T) {
	w := world()
	p := pt("rolling:after_intent_swap")
	both := []CView{c("orig", "app", true), c("cand", "app_dso_new_1", true)}
	r := func(status string) RotationRecord {
		return rec("intent_swap", status, func(r *RotationRecord) {
			r.ExpectedProxyTarget = "cand"
			r.CandidateContainerIDs = []string{"cand"}
		})
	}
	crash := snap(r("in_progress"), nil, both...)
	final := snap(r("critical_error"), []string{"orig", "cand"}, both...)
	if vs := CheckRecovery(w, p, crash, final); len(vs) != 0 {
		t.Fatalf("preserving both after a handoff must pass:\n%s", FormatViolations(vs))
	}
}

// With the agent's embedded-proxy RoutingVerifier (M0-04 3.1) recovery MAY retire the
// unverified candidate after a handoff, but only when the surviving real-name holder
// is observed receiving the traffic; without that evidence the removal is a violation.
func TestCheckRecovery_RemovalAfterHandoffRequiresObservedHolderRouting(t *testing.T) {
	w := world()
	p := pt("rolling:after_handoff_before_ack")
	both := []CView{c("orig", "app", true), c("cand", "app_dso_new_1", true)}
	r := func(status string) RotationRecord {
		return rec("handoff_done", status, func(r *RotationRecord) {
			r.ExpectedProxyTarget, r.CandidateContainerIDs = "cand", []string{"cand"}
		})
	}
	crash := snap(r("in_progress"), nil, both...)
	ok := snap(r("recovered"), []string{"orig"}, c("orig", "app", true))
	if vs := CheckRecovery(w, p, crash, ok); len(vs) != 0 {
		t.Fatalf("retiring the candidate with the holder observed routed must pass:\n%s", FormatViolations(vs))
	}
	noEvidence := snap(r("recovered"), nil, c("orig", "app", true))
	if got := rules(CheckRecovery(w, p, crash, noEvidence)); !strings.Contains(got, "no-removal-after-handoff") {
		t.Fatalf("removal without observed routing must be flagged, got [%s]", got)
	}
}

func TestCheckRecovery_StrategyCommitRules(t *testing.T) {
	w := world()
	w.Strategy, w.Proxied = Restart, false
	// restart: original (renamed _old_) is gone but cleanup was not even intended yet
	crash := snap(rec("original_stopped", "in_progress"), nil, c("orig", "app_old_1700", false), c("cand", "app", false))
	final := snap(rec("original_stopped", "critical_error"), nil, c("cand", "app", false))
	if got := rules(CheckRecovery(w, pt("restart:after_stop_before_ack"), crash, final)); !strings.Contains(got, "original-not-retired") {
		t.Fatalf("restart: deleting the stopped original before cleanup intent must be flagged, got [%s]", got)
	}
	// after the cleanup intent the original may be gone
	crash = snap(rec("intent_cleanup", "in_progress"), nil, c("orig", "app_old_1700", true), c("cand", "app", true))
	final = snap(rec("intent_cleanup", "recovered"), nil, c("cand", "app", true))
	if vs := CheckRecovery(w, pt("restart:after_intent_cleanup"), crash, final); len(vs) != 0 {
		t.Fatalf("retirement after the cleanup intent is allowed:\n%s", FormatViolations(vs))
	}
}

func TestCheckSecondPass(t *testing.T) {
	a := snap(rec("x", "recovered"), nil, c("orig", "app", true))
	b := snap(rec("x", "recovered"), nil, c("orig", "app", true))
	if vs := CheckSecondPass(a, b); len(vs) != 0 {
		t.Fatalf("%v", vs)
	}
	b2 := snap(rec("x", "recovered"), nil)
	if len(CheckSecondPass(a, b2)) == 0 {
		t.Fatal("a removed container went unnoticed")
	}
	b3 := snap(rec("x", "critical_error"), nil, c("orig", "app", true))
	if len(CheckSecondPass(a, b3)) == 0 {
		t.Fatal("a journal change went unnoticed")
	}
	b4 := snap(rec("x", "recovered"), nil, c("orig", "app", false))
	if len(CheckSecondPass(a, b4)) == 0 {
		t.Fatal("a stop went unnoticed")
	}
}

func TestCheckCorruptJournal(t *testing.T) {
	a := snap(rec("x", "in_progress"), nil, c("orig", "app", true), c("cand", "app_dso_new_1", true))
	if vs := CheckCorruptJournal(a, a, []string{"rotations.json.corrupt-1"}); len(vs) != 0 {
		t.Fatalf("%v", vs)
	}
	gone := snap(rec("x", "in_progress"), nil, c("orig", "app", true))
	if len(CheckCorruptJournal(a, gone, []string{"q"})) == 0 {
		t.Fatal("candidate removal with an unreadable journal must be flagged")
	}
	if len(CheckCorruptJournal(a, a, nil)) == 0 {
		t.Fatal("a missing quarantine file must be flagged")
	}
}

func TestCheckSignal(t *testing.T) {
	w := World{Service: "app", OwnerID: owner, Strategy: Signal, OriginalID: "orig"}
	good := Snapshot{Containers: []CView{c("orig", "app", true)}, Journal: []RotationRecord{rec("intent_signal_reload", "critical_error", func(r *RotationRecord) { r.Strategy = "signal" })}}
	files := map[string]string{"a": "new", "b": "old"}
	ev := SignalEvidence{ReloadsAtCrash: 1, ReloadsAfter: 1, FilesAtCrash: files, FilesAfter: map[string]string{"a": "new", "b": "old"}}
	if vs := CheckSignal(w, good, good, ev); len(vs) != 0 {
		t.Fatalf("%s", FormatViolations(vs))
	}
	bad := ev
	bad.ReloadsAfter = 2
	if !strings.Contains(rules(CheckSignal(w, good, good, bad)), "reload-not-repeated") {
		t.Fatal("repeated reload not detected")
	}
	bad = ev
	bad.FilesAfter = map[string]string{"a": "new", "b": "new"}
	if v := CheckSignal(w, good, good, bad); !strings.Contains(rules(v), "no-blind-redelivery") || strings.Contains(FormatViolations(v), "new") {
		t.Fatalf("partial delivery silently completed was not detected (or the report leaked content): %v", v)
	}
	ok := Snapshot{Containers: []CView{c("orig", "app", true)}, Journal: []RotationRecord{rec("signal_done", "completed", func(r *RotationRecord) { r.Strategy = "signal" })}}
	if !strings.Contains(rules(CheckSignal(w, good, ok, ev)), "signal-uncertain") {
		t.Fatal("a completed record for an interrupted signal rotation must be flagged")
	}
	gone := Snapshot{Journal: good.Journal}
	if !strings.Contains(rules(CheckSignal(w, good, gone, ev)), "signal-container-intact") {
		t.Fatal("a recreated/removed signalled container must be flagged")
	}
}

func TestArtifactBase(t *testing.T) {
	for name, want := range map[string]string{"app_dso_backup_17": "app", "app_dso_new_9": "app", "app_old_170000": "app"} {
		if b, ok := ArtifactBase(name); !ok || b != want {
			t.Errorf("%s -> %q %v", name, b, ok)
		}
	}
	for _, name := range []string{"app", "my_old_database", "legacy_old_service", "_dso_new_1", "app_old_x"} {
		if _, ok := ArtifactBase(name); ok {
			t.Errorf("%s misclassified as an artifact", name)
		}
	}
}

func TestRecordForPrefersTheNamedService(t *testing.T) {
	named := rec("candidate_ready", "in_progress")
	named.LastUpdate = time.Now().Add(-time.Hour)
	anon := rec("x", "recovered")
	anon.ServiceID = ""
	anon.LastUpdate = time.Now()
	other := rec("y", "completed")
	other.ServiceID = "other"
	other.LastUpdate = time.Now()
	s := Snapshot{Journal: []RotationRecord{anon, other, named}}
	if r, ok := s.RecordFor("app"); !ok || r.Transition != "candidate_ready" {
		t.Fatalf("%+v %v", r, ok)
	}
	if r, ok := (Snapshot{Journal: []RotationRecord{anon, other}}).RecordFor("app"); !ok || r.Transition != "x" {
		t.Fatalf("fallback: %+v %v", r, ok)
	}
	if _, ok := (Snapshot{Journal: []RotationRecord{other}}).RecordFor("app"); ok {
		t.Fatal("another service's record must never be used")
	}
}

func TestCheckSignalToleratesACrashBeforeTheIntentWasDurable(t *testing.T) {
	w := World{Service: "app", OwnerID: owner, Strategy: Signal, OriginalID: "orig"}
	noRecord := Snapshot{Containers: []CView{c("orig", "app", true)}}
	ev := SignalEvidence{ReloadsAtCrash: 0, ReloadsAfter: 0, FilesAtCrash: map[string]string{"a": "old"}, FilesAfter: map[string]string{"a": "old"}}
	if vs := CheckSignal(w, noRecord, noRecord, ev); len(vs) != 0 {
		t.Fatalf("nothing was durably in flight, nothing to be uncertain about:\n%s", FormatViolations(vs))
	}
	// ...but a record that WAS durable and then vanished is a violation.
	durable := Snapshot{Containers: noRecord.Containers, Journal: []RotationRecord{rec("intent_signal_publish", "in_progress", func(r *RotationRecord) { r.Strategy = "signal" })}}
	if !strings.Contains(rules(CheckSignal(w, durable, noRecord, ev)), "signal-uncertain") {
		t.Fatal("a vanished record went unnoticed")
	}
}
