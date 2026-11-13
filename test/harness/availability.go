package harness

import (
	"fmt"
	"sort"
	"time"
)

// Window summarises the samples that STARTED inside [From, To).
type Window struct {
	From, To     time.Time
	Total, OK    int
	FailKinds    map[string]int
	FirstFailure time.Time // zero when none
	LastFailure  time.Time
	// LongestOutage is the longest run of consecutive failed samples (ordered by
	// start), measured from the first failed sample's start to the start of the next
	// successful one (or the window end).
	LongestOutage time.Duration
	Backends      map[string]int
}

// Failed is the number of failed requests in the window.
func (w Window) Failed() int { return w.Total - w.OK }

// Summarize builds a Window over [from, to).
func Summarize(samples []Sample, from, to time.Time) Window {
	w := Window{From: from, To: to, FailKinds: map[string]int{}, Backends: map[string]int{}}
	var in []Sample
	for _, s := range samples {
		if !s.Start.Before(from) && s.Start.Before(to) {
			in = append(in, s)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Start.Before(in[j].Start) })
	var runStart time.Time
	closeRun := func(end time.Time) {
		if !runStart.IsZero() {
			if d := end.Sub(runStart); d > w.LongestOutage {
				w.LongestOutage = d
			}
			runStart = time.Time{}
		}
	}
	for _, s := range in {
		w.Total++
		if s.Backend != "" && s.OK {
			w.Backends[s.Backend]++
		}
		if s.OK {
			w.OK++
			closeRun(s.Start)
			continue
		}
		w.FailKinds[s.FailKind]++
		if w.FirstFailure.IsZero() {
			w.FirstFailure = s.Start
		}
		w.LastFailure = s.Start
		if runStart.IsZero() {
			runStart = s.Start
		}
	}
	closeRun(to)
	return w
}

// Marks are the instants the harness observed, all on the harness' own clock.
type Marks struct {
	TrafficStart  time.Time
	RotationStart time.Time // trigger armed
	AgentDeath    time.Time // the agent process was observed dead
	RestartBegin  time.Time // the replacement agent was started
	AgentReady    time.Time // the replacement agent logged "now running" (recovery finished)
	TrafficEnd    time.Time
}

// Availability reports three separate things and never merges them:
//
//	Rotation:   the ordinary-rotation window (only meaningful for an uninterrupted run)
//	Death:      from agent death until the first successful request after the restart
//	Recovery:   restart begin -> first success, restart begin -> stable service
//
// The DSO proxy listener runs INSIDE the agent process, so killing the agent kills
// the listener: Death is expected to contain connection failures. That is reported,
// not hidden, and is never presented as zero downtime.
type Availability struct {
	Rotation Window
	Death    Window // [AgentDeath, firstSuccessAfterRestart)
	// DeathToServing is the unavailability caused by the unexpected death: the time
	// from the death to the first successful request that follows the restart.
	DeathToServing time.Duration
	// RestartToFirstSuccess / RestartToStable measure recovery after the restart.
	RestartToFirstSuccess time.Duration
	RestartToStable       time.Duration // first success from which StableRun consecutive successes follow
	ServedAfterRestart    bool
}

// StableRun is how many consecutive successful requests count as "stable".
const StableRun = 25

// Analyze computes Availability from samples and marks. Zero-valued marks skip the
// corresponding measurement.
func Analyze(samples []Sample, m Marks, rotationEnd time.Time) Availability {
	var a Availability
	sorted := append([]Sample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })

	if !m.RotationStart.IsZero() && !rotationEnd.IsZero() {
		a.Rotation = Summarize(sorted, m.RotationStart, rotationEnd)
	}
	if m.AgentDeath.IsZero() || m.RestartBegin.IsZero() {
		return a
	}
	firstOK := time.Time{}
	for _, s := range sorted {
		if s.OK && !s.Start.Before(m.RestartBegin) {
			firstOK = s.Start
			break
		}
	}
	if firstOK.IsZero() {
		a.Death = Summarize(sorted, m.AgentDeath, endOr(m.TrafficEnd, sorted))
		return a
	}
	a.ServedAfterRestart = true
	a.Death = Summarize(sorted, m.AgentDeath, firstOK)
	a.DeathToServing = firstOK.Sub(m.AgentDeath)
	a.RestartToFirstSuccess = firstOK.Sub(m.RestartBegin)
	run := 0
	for i, s := range sorted {
		if s.Start.Before(m.RestartBegin) {
			continue
		}
		if s.OK {
			run++
			if run >= StableRun {
				// the stable period began at the first success of this run
				a.RestartToStable = sorted[i-run+1].Start.Sub(m.RestartBegin)
				break
			}
		} else {
			run = 0
		}
	}
	return a
}

func endOr(t time.Time, s []Sample) time.Time {
	if !t.IsZero() {
		return t
	}
	if len(s) > 0 {
		return s[len(s)-1].End
	}
	return time.Now()
}

// ListenerDiedWithAgent reports whether the death window shows the failures the
// architecture predicts (listener closed => connection refused). If it returns
// false while the agent was verified dead, the topology documented in
// docs/testing/REAL-PROCESS-CRASH-TESTS.md no longer holds and must be revisited.
func (a Availability) ListenerDiedWithAgent() bool {
	return a.Death.FailKinds["connection_refused"] > 0
}

// String renders the three measurements.
func (a Availability) String() string {
	return fmt.Sprintf(
		"(a) ordinary rotation: %d requests, %d failed, longest outage %v\n"+
			"(b) unexpected agent death: %d requests in the death window, %d failed %v, first failure %s, death->serving %v (served after restart: %v)\n"+
			"(c) recovery after restart: first success after %v, stable after %v",
		a.Rotation.Total, a.Rotation.Failed(), a.Rotation.LongestOutage,
		a.Death.Total, a.Death.Failed(), a.Death.FailKinds, tsOrDash(a.Death.FirstFailure), a.DeathToServing, a.ServedAfterRestart,
		a.RestartToFirstSuccess, a.RestartToStable)
}

func tsOrDash(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("15:04:05.000")
}
