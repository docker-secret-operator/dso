package harness

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Strategy is a DSO rotation strategy.
type Strategy string

const (
	Rolling Strategy = "rolling"
	Signal  Strategy = "signal"
	Restart Strategy = "restart"
)

// Scenario selects how the workload is arranged so the point is reachable.
type Scenario string

const (
	// ScenarioHappy: a rotation that would succeed if not interrupted.
	ScenarioHappy Scenario = "happy"
	// ScenarioCandidateUnhealthy: the replacement never becomes healthy, so the
	// rotation takes its rollback path (the only way "rollback" points execute).
	ScenarioCandidateUnhealthy Scenario = "candidate_unhealthy"
	// ScenarioCrashDuringRecovery: stage 1 crashes at Base; stage 2 (the restarted
	// agent) is told to die at Name, i.e. DURING recovery; stage 3 restarts again and
	// must still converge safely.
	ScenarioCrashDuringRecovery Scenario = "crash_during_recovery"
)

// Point is one instrumented transition. Name is the exact DSO_FAULT_POINT value
// the dsofault-tagged binary dies at.
type Point struct {
	Name       string
	Strategies []Strategy
	Class      string // coarse transition class, for the report
	Scenario   Scenario
	// Nth makes the binary die on the Nth hit of Name (0 or 1 = first).
	Nth int
	// Base is, for ScenarioCrashDuringRecovery, the stage-1 crash point that leaves
	// something for recovery to act on.
	Base string

	// HandoffStarted: the journal already shows the proxy handoff intent, so (the
	// production agent wires no RoutingVerifier) recovery must preserve everything.
	HandoffStarted bool
	// NoNameHolder: after this point no container holds the real service name
	// (original renamed away, candidate not yet promoted): recovery cannot establish
	// a healthy holder and MUST flag the rotation for manual recovery.
	NoNameHolder bool
	// Note is shown in the report.
	Note string
}

// Case is one cell of the matrix: a strategy and a point.
type Case struct {
	Strategy Strategy
	Point    Point
}

// ID is a stable subtest name.
func (c Case) ID() string {
	return string(c.Strategy) + "/" + strings.NewReplacer(":", "-", "_", "-").Replace(c.Point.Name) + nthSuffix(c.Point)
}

func nthSuffix(p Point) string {
	if p.Nth > 1 {
		return fmt.Sprintf("#%d", p.Nth)
	}
	return ""
}

var (
	rollingOnly = []Strategy{Rolling}
	signalOnly  = []Strategy{Signal}
	restartOnly = []Strategy{Restart}
	allStrats   = []Strategy{Rolling, Signal, Restart}
)

func rp(name, class string, o ...func(*Point)) Point {
	p := Point{Name: "rolling:" + name, Strategies: rollingOnly, Class: class, Scenario: ScenarioHappy}
	for _, f := range o {
		f(&p)
	}
	return p
}

func handoff(p *Point) { p.HandoffStarted = true }

// Points is the registry of every instrumented point. TestRegistryMatchesSource
// keeps it in sync with the production call sites.
var Points = []Point{
	// ---- rolling (proxy-fronted, the production topology) ----
	rp("after_intent_create_candidate", "candidate_creation"),
	rp("after_candidate_create_before_ack", "candidate_creation"),
	rp("after_secret_delivery", "secret_delivery"),
	rp("after_start_before_verify", "readiness"),
	rp("after_candidate_verified", "readiness"),
	rp("after_intent_handoff", "proxy_handoff", handoff),
	rp("after_handoff_before_ack", "proxy_handoff", handoff),
	rp("after_intent_swap", "commit", handoff),
	rp("between_renames", "commit", handoff, func(p *Point) { p.NoNameHolder = true }),
	rp("after_swap_before_commit_ack", "commit", handoff),
	rp("after_swap_committed", "commit", handoff),
	rp("after_intent_cleanup", "original_retirement", handoff),
	rp("after_cleanup_before_ack", "original_retirement", handoff),
	rp("after_intent_rollback", "rollback", func(p *Point) { p.Scenario = ScenarioCandidateUnhealthy }),

	// ---- signal (file mode, two secrets so one publication can be partial) ----
	sp("after_intent_publish", "secret_publication"),
	sp("after_spec_0_published", "secret_publication", func(p *Point) {
		p.Note = "first of two secret files replaced, second still old: genuinely PARTIAL publication"
	}),
	sp("after_spec_1_published", "secret_publication", func(p *Point) { p.Note = "both files replaced, unverified" }),
	sp("after_publish_before_ack", "secret_publication"),
	sp("after_intent_reload", "signal_delivery"),
	sp("after_signal_before_ack", "signal_delivery"),
	sp("during_readiness", "readiness"),
	sp("before_final_ack", "readiness"),

	// ---- restart (env mode, full recreation) ----
	xp("after_intent_rename", "original_rename"),
	xp("after_rename", "original_rename", func(p *Point) { p.NoNameHolder = true }),
	xp("after_intent_create", "candidate_creation", func(p *Point) { p.NoNameHolder = true }),
	xp("after_create_before_ack", "candidate_creation", func(p *Point) { p.NoNameHolder = true }),
	xp("after_intent_stop", "original_stop", func(p *Point) { p.NoNameHolder = true }),
	xp("after_stop_before_ack", "original_stop", func(p *Point) { p.NoNameHolder = true }),
	xp("after_intent_start", "replacement_start", func(p *Point) { p.NoNameHolder = true }),
	xp("after_start_before_ack", "replacement_start", func(p *Point) { p.NoNameHolder = true }),
	xp("after_intent_cleanup", "original_retirement"),
	xp("after_cleanup_before_ack", "original_retirement"),

	// ---- durable-state persistence, every strategy ----
	{Name: "state:before_persist_rename", Strategies: allStrats, Class: "state_persist", Scenario: ScenarioHappy, Nth: 4,
		Note: "dies after the temp state file was written and fsynced but BEFORE it replaced the journal: the previous version must be intact"},
	{Name: "state:after_persist_rename", Strategies: allStrats, Class: "state_persist", Scenario: ScenarioHappy, Nth: 4,
		Note: "dies right after a journal record became durable, before the action it announces"},

	// ---- crash during recovery itself ----
	{Name: "recovery:start", Strategies: rollingOnly, Class: "recovery", Scenario: ScenarioCrashDuringRecovery, Base: "rolling:after_candidate_verified"},
	{Name: "recovery:before_retire_artifact", Strategies: rollingOnly, Class: "recovery", Scenario: ScenarioCrashDuringRecovery, Base: "rolling:after_candidate_verified"},
	{Name: "recovery:after_retire_artifact", Strategies: rollingOnly, Class: "recovery", Scenario: ScenarioCrashDuringRecovery, Base: "rolling:after_candidate_verified"},
}

func sp(name, class string, o ...func(*Point)) Point {
	p := Point{Name: "signal:" + name, Strategies: signalOnly, Class: class, Scenario: ScenarioHappy}
	for _, f := range o {
		f(&p)
	}
	return p
}

func xp(name, class string, o ...func(*Point)) Point {
	p := Point{Name: "restart:" + name, Strategies: restartOnly, Class: class, Scenario: ScenarioHappy}
	for _, f := range o {
		f(&p)
	}
	return p
}

// Matrix expands Points into every (strategy, point) cell.
func Matrix() []Case {
	var out []Case
	for _, p := range Points {
		for _, s := range p.Strategies {
			out = append(out, Case{Strategy: s, Point: p})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// FilterCases keeps the cases whose ID matches pattern (an RE2 expression); an
// empty pattern keeps all. It exists so a long matrix can be run in slices.
func FilterCases(cases []Case, pattern string) ([]Case, error) {
	if pattern == "" {
		return cases, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid case filter %q: %w", pattern, err)
	}
	var out []Case
	for _, c := range cases {
		if re.MatchString(c.ID()) {
			out = append(out, c)
		}
	}
	return out, nil
}

var pointNameRe = regexp.MustCompile(`^[a-z]+:[a-z0-9_]+$`)

// ValidatePointName rejects names that could not be an exact fault point.
func ValidatePointName(name string) error {
	if !pointNameRe.MatchString(name) {
		return fmt.Errorf("invalid fault point name %q (want <area>:<snake_case>)", name)
	}
	return nil
}

// FaultEnv returns the environment entries that arm the tagged binary to die at p.
// markerPath is where the binary records (point, pid, nth) just before dying.
func FaultEnv(point string, nth int, markerPath string) ([]string, error) {
	if err := ValidatePointName(point); err != nil {
		return nil, err
	}
	env := []string{"DSO_FAULT_POINT=" + point, "DSO_FAULT_MARKER=" + markerPath}
	if nth > 1 {
		env = append(env, fmt.Sprintf("DSO_FAULT_NTH=%d", nth))
	}
	return env, nil
}

// Marker is what the tagged binary wrote immediately before killing itself.
type Marker struct {
	Point string
	PID   int
	Nth   int
}

// ParseMarker parses the marker file contents.
func ParseMarker(b []byte) (Marker, error) {
	var m Marker
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return m, fmt.Errorf("malformed marker line %q", line)
		}
		switch k {
		case "point":
			m.Point = v
		case "pid":
			if _, err := fmt.Sscanf(v, "%d", &m.PID); err != nil {
				return m, err
			}
		case "nth":
			if _, err := fmt.Sscanf(v, "%d", &m.Nth); err != nil {
				return m, err
			}
		}
	}
	if m.Point == "" || m.PID == 0 {
		return m, fmt.Errorf("incomplete marker %q", b)
	}
	return m, nil
}

// VerifyFault checks that the process the harness started died, by SIGKILL, at the
// armed point, and that the marker's PID is the PID the harness started. A marker
// written by any other process, or at another point, is a failure.
func VerifyFault(wantPoint string, startedPID int, exit ExitInfo, marker []byte) error {
	m, err := ParseMarker(marker)
	if err != nil {
		return fmt.Errorf("fault marker: %w", err)
	}
	if m.Point != wantPoint {
		return fmt.Errorf("process died at %q, armed point was %q", m.Point, wantPoint)
	}
	if m.PID != startedPID {
		return fmt.Errorf("fault marker pid %d is not the pid the harness started (%d)", m.PID, startedPID)
	}
	if !exit.Signaled || exit.Signal.String() != "killed" {
		return fmt.Errorf("agent did not die by SIGKILL: %s", exit)
	}
	return nil
}
