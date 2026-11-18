package harness

import "fmt"

// CheckCrashBoundary proves, from what was observed AT the crash (before any recovery),
// that the process died at the lifecycle boundary the armed point names -- not merely
// that a marker file was written. A fault point that fires one step early or late would
// otherwise let a case "pass" while exercising a different window.
//
// It covers the replacement-start pair of the restart strategy, whose two points differ
// only in whether ContainerStart has happened:
//
//	restart:after_intent_start        intent journalled, replacement created but NOT running
//	restart:after_start_before_ack    replacement RUNNING, journal still at the same intent
//
// Both leave the journal at intent_start_replacement: neither main nor fallback restart
// path journals a replacement_started record before the gate, health check and probe.
// Every other point returns no violations (their boundaries are not asserted here).
func CheckCrashBoundary(w World, p Point, crash Snapshot) []Violation {
	var wantRunning bool
	switch p.Name {
	case "restart:after_intent_start":
		wantRunning = false
	case "restart:after_start_before_ack":
		wantRunning = true
	default:
		return nil
	}
	var vs []Violation
	add := func(f string, a ...any) { vs = append(vs, Violation{"crash-at-boundary", fmt.Sprintf(f, a...)}) }

	rec, ok := crash.RecordFor(w.Service)
	switch {
	case !ok:
		add("%s: no journal record of the interrupted rotation at the crash", p.Name)
	case rec.Transition != "intent_start_replacement":
		add("%s: the journal ended at %q, want intent_start_replacement", p.Name, rec.Transition)
	}

	candidates, running := 0, 0
	for _, c := range crash.serviceContainers(w.Service, w.OwnerID) {
		if c.ID == w.OriginalID {
			continue
		}
		candidates++
		if c.Running {
			running++
		}
	}
	switch {
	case candidates == 0:
		add("%s: no replacement container existed at the crash", p.Name)
	case wantRunning && running == 0:
		add("%s: the replacement was not running at the crash: the point fired before ContainerStart", p.Name)
	case !wantRunning && running > 0:
		add("%s: the replacement was already running at the crash: the point fired after ContainerStart", p.Name)
	}
	return vs
}
