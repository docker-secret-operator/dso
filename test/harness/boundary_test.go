package harness

import "testing"

func restartWorld() World {
	w := world()
	w.Strategy, w.Proxied = Restart, false
	return w
}

func TestCheckCrashBoundary_ReplacementStartPair(t *testing.T) {
	journal := func(tr string) RotationRecord {
		return rec(tr, "in_progress", func(r *RotationRecord) { r.Strategy = "restart" })
	}
	orig := func(running bool) CView { return c("orig", "app_old_1", running) }
	started := func(running bool) CView { return c("cand", "app", running) }

	cases := []struct {
		name  string
		point string
		snap  Snapshot
		want  string // violation rules, "" = clean
	}{
		{"after_start_before_ack: replacement running, journal at the intent", "restart:after_start_before_ack",
			snap(journal("intent_start_replacement"), nil, orig(false), started(true)), ""},
		{"after_start_before_ack fired BEFORE the start (replacement stopped)", "restart:after_start_before_ack",
			snap(journal("intent_start_replacement"), nil, orig(false), started(false)), "crash-at-boundary"},
		{"after_start_before_ack with the journal already past the intent", "restart:after_start_before_ack",
			snap(journal("candidate_ready"), nil, orig(false), started(true)), "crash-at-boundary"},
		{"after_start_before_ack with no replacement at all", "restart:after_start_before_ack",
			snap(journal("intent_start_replacement"), nil, orig(false)), "crash-at-boundary"},
		{"after_start_before_ack with no journal record", "restart:after_start_before_ack",
			Snapshot{Containers: append(baseContainers(), orig(false), started(true))}, "crash-at-boundary"},
		{"after_intent_start: replacement created, not running", "restart:after_intent_start",
			snap(journal("intent_start_replacement"), nil, orig(false), started(false)), ""},
		{"after_intent_start fired AFTER the start (replacement running)", "restart:after_intent_start",
			snap(journal("intent_start_replacement"), nil, orig(false), started(true)), "crash-at-boundary"},
		{"a point with no asserted boundary is not checked", "restart:after_create_before_ack",
			snap(journal("candidate_created"), nil, orig(true)), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rules(CheckCrashBoundary(restartWorld(), pt(tc.point), tc.snap))
			// rules() joins one entry per violation; collapse repeats for the comparison
			if tc.want == "" && got != "" || tc.want != "" && got == "" {
				t.Fatalf("violations %q, want %q", got, tc.want)
			}
		})
	}
}
