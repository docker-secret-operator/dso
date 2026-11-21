package harness

import "testing"

// Stage D (Option E, owner-approved): the two rolling swap crash points are documented
// manual-recovery states. Recovery must stay fail-closed; the matrix asserts that, and no
// longer requires the service to answer traffic there. These tests prove the relaxed
// expectation is exactly that narrow.

func swapCrash(point string) (World, Snapshot) {
	w := world() // rolling, proxied, original "orig"
	j := rec("intent_swap", "in_progress", func(r *RotationRecord) { r.CandidateContainerIDs = []string{"cand"} })
	switch point {
	case "rolling:after_swap_before_commit_ack":
		return w, snap(j, nil, c("orig", "app_dso_backup_1", true), c("cand", "app", true))
	default: // rolling:between_renames
		return w, snap(j, nil, c("orig", "app_dso_backup_1", true), c("cand", "app_dso_new_1", true))
	}
}

// manualFinal is the observed hosted outcome: everything preserved, critical_error, the
// journal still at intent_swap, nobody routed.
func manualFinal(crash Snapshot) Snapshot {
	f := Snapshot{Containers: append([]CView(nil), crash.Containers...)}
	f.Journal = []RotationRecord{rec("intent_swap", "critical_error")}
	return f
}

func TestDocumentedManualPointsAreExactlyTheTwoSwapPoints(t *testing.T) {
	var got []string
	for _, p := range Points {
		if p.DocumentedManual {
			got = append(got, p.Name)
			if p.Note == "" {
				t.Errorf("%s: a documented manual-recovery point must explain itself in its Note", p.Name)
			}
		}
	}
	want := "rolling:between_renames,rolling:after_swap_before_commit_ack"
	if joined := join(got); joined != want {
		t.Fatalf("DocumentedManual points = %q, want exactly %q (the owner-approved scope)", joined, want)
	}
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

func TestCheckCrashBoundary_SwapLayoutsAreDistinguished(t *testing.T) {
	for _, point := range []string{"rolling:after_swap_before_commit_ack", "rolling:between_renames"} {
		w, crash := swapCrash(point)
		if vs := CheckCrashBoundary(w, pt(point), crash); len(vs) != 0 {
			t.Errorf("%s: the genuine layout was rejected:\n%s", point, FormatViolations(vs))
		}
	}
	// Each layout must be rejected by the OTHER point's expectation.
	wA, crashA := swapCrash("rolling:after_swap_before_commit_ack")
	if vs := CheckCrashBoundary(wA, pt("rolling:between_renames"), crashA); !hasRule(vs, "crash-at-boundary") {
		t.Error("the fully swapped layout was accepted as between_renames")
	}
	wB, crashB := swapCrash("rolling:between_renames")
	if vs := CheckCrashBoundary(wB, pt("rolling:after_swap_before_commit_ack"), crashB); !hasRule(vs, "crash-at-boundary") {
		t.Error("the half-swapped layout was accepted as after_swap_before_commit_ack")
	}
	// Journal not at intent_swap, original not renamed, no running holder.
	for name, mod := range map[string]func(*Snapshot){
		"journal not at intent_swap": func(s *Snapshot) { s.Journal = []RotationRecord{rec("candidate_ready", "in_progress")} },
		"original not renamed":       func(s *Snapshot) { s.Containers[len(s.Containers)-2].Name = "app" },
		"holder stopped":             func(s *Snapshot) { s.Containers[len(s.Containers)-1].Running = false },
	} {
		w, crash := swapCrash("rolling:after_swap_before_commit_ack")
		mod(&crash)
		if vs := CheckCrashBoundary(w, pt("rolling:after_swap_before_commit_ack"), crash); !hasRule(vs, "crash-at-boundary") {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCheckManualRecovery_FailClosedOutcomePasses(t *testing.T) {
	for _, point := range []string{"rolling:after_swap_before_commit_ack", "rolling:between_renames"} {
		w, crash := swapCrash(point)
		final := manualFinal(crash)
		if vs := CheckManualRecovery(w, pt(point), crash, final); len(vs) != 0 {
			t.Errorf("%s: the documented fail-closed outcome was reported:\n%s", point, FormatViolations(vs))
		}
		// service-reachable is not required there, but nothing else is relaxed.
		if vs := CheckRecovery(w, pt(point), crash, final); hasRule(vs, "service-reachable") {
			t.Errorf("%s: service-reachable is still required at a documented manual-recovery point", point)
		}
	}
}

func TestCheckManualRecovery_NegativesStillReported(t *testing.T) {
	point := "rolling:after_swap_before_commit_ack"
	cases := []struct {
		name string
		mod  func(w *World, crash Snapshot, final *Snapshot)
	}{
		{"status recovered instead of critical_error", func(w *World, _ Snapshot, f *Snapshot) { f.Journal = []RotationRecord{rec("intent_swap", "recovered")} }},
		{"status completed", func(w *World, _ Snapshot, f *Snapshot) { f.Journal = []RotationRecord{rec("intent_swap", "completed")} }},
		{"recovery committed the swap on the operator's behalf", func(w *World, _ Snapshot, f *Snapshot) {
			f.Journal = []RotationRecord{rec("swap_committed", "critical_error")}
		}},
		{"recovery rolled the operation back", func(w *World, _ Snapshot, f *Snapshot) {
			f.Journal = []RotationRecord{rec("rolled_back", "critical_error")}
		}},
		{"no journal record", func(w *World, _ Snapshot, f *Snapshot) { f.Journal = nil }},
		{"the backup (original) was deleted", func(w *World, _ Snapshot, f *Snapshot) { f.Containers = dropID(f.Containers, "orig") }},
		{"the candidate was deleted", func(w *World, _ Snapshot, f *Snapshot) { f.Containers = dropID(f.Containers, "cand") }},
		{"the backup was renamed (moved aside)", func(w *World, _ Snapshot, f *Snapshot) {
			for i := range f.Containers {
				if f.Containers[i].ID == "orig" {
					f.Containers[i].Name = "app_dso_new_9"
				}
			}
		}},
		{"the candidate was renamed", func(w *World, _ Snapshot, f *Snapshot) {
			for i := range f.Containers {
				if f.Containers[i].ID == "cand" {
					f.Containers[i].Name = "app_dso_backup_9"
				}
			}
		}},
		{"a bystander was deleted", func(w *World, _ Snapshot, f *Snapshot) { f.Containers = dropID(f.Containers, "by") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, crash := swapCrash(point)
			final := manualFinal(crash)
			tc.mod(&w, crash, &final)
			vs := append(CheckManualRecovery(w, pt(point), crash, final), CheckRecovery(w, pt(point), crash, final)...)
			if len(vs) == 0 {
				t.Fatal("a destructive or non-fail-closed outcome at a manual-recovery point was accepted")
			}
		})
	}
}

// The relaxation must not leak: every other proxied point still requires reachability.
func TestServiceReachableStillRequiredElsewhere(t *testing.T) {
	for _, point := range []string{"rolling:after_intent_swap", "rolling:after_swap_committed", "rolling:after_intent_cleanup"} {
		w := world()
		crash := snap(rec("intent_swap", "in_progress"), nil, c("orig", "app", true))
		final := snap(rec("completed", "recovered"), nil, c("orig", "app", true)) // running, nobody routed
		if vs := CheckRecovery(w, pt(point), crash, final); !hasRule(vs, "service-reachable") {
			t.Errorf("%s: service-reachable is no longer required", point)
		}
	}
}

func dropID(cs []CView, id string) []CView {
	var out []CView
	for _, c := range cs {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}
