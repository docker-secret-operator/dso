package harness

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CaseReport is the outcome of one cell of the matrix.
type CaseReport struct {
	ID       string
	Strategy Strategy
	Point    string
	Scenario Scenario

	FaultFired   bool   // the marker was written and verified
	FaultPID     int    // pid the harness started
	AgentExit    string // how the agent died
	PIDVerified  bool   // marker pid == started pid, and the signal was SIGKILL
	CrashJournal string // last transition durable at the crash
	// Container layouts (name, short id, running) of the service's own containers at the
	// crash, after recovery, and after the second recovery pass, and the second pass's
	// verdict ("unchanged" or the violations): the evidence behind the pass/fail line.
	CrashLayout    []string
	FinalLayout    []string
	SecondLayout   []string
	SecondPass     string
	FinalStatus    string // journal status after recovery
	FinalOutcome   string // short human summary of the post-recovery layout
	Violations     []Violation
	SecretFindings int
	Availability   *Availability
	Notes          []string
	Duration       time.Duration
	// NotExecuted is set when the case could not run (for example the point was
	// unreachable); it is reported, never silently counted as a pass.
	NotExecuted string
}

// Passed reports whether the case executed and found no violations.
func (c CaseReport) Passed() bool {
	return c.NotExecuted == "" && c.FaultFired && c.PIDVerified && len(c.Violations) == 0 && c.SecretFindings == 0
}

// Report aggregates the matrix.
type Report struct {
	Started  time.Time
	Topology string
	Cases    []CaseReport
}

// Counts returns passed, failed and not-executed totals.
func (r Report) Counts() (passed, failed, notExecuted int) {
	for _, c := range r.Cases {
		switch {
		case c.NotExecuted != "":
			notExecuted++
		case c.Passed():
			passed++
		default:
			failed++
		}
	}
	return
}

// JSON renders the machine-readable report.
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Markdown renders the human report: a matrix table, then every failure.
func (r Report) Markdown() string {
	var b strings.Builder
	p, f, n := r.Counts()
	fmt.Fprintf(&b, "# Real-process crash-recovery report\n\nstarted %s; %d passed, %d failed, %d not executed, %d total\n\n", r.Started.Format(time.RFC3339), p, f, n, len(r.Cases))
	if r.Topology != "" {
		fmt.Fprintf(&b, "Topology: %s\n\n", r.Topology)
	}
	b.WriteString("| strategy | point | scenario | fault verified (pid, SIGKILL) | journal at crash | after recovery | violations | secret findings | result |\n|---|---|---|---|---|---|---|---|---|\n")
	cases := append([]CaseReport(nil), r.Cases...)
	sort.SliceStable(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	for _, c := range cases {
		res := "PASS"
		switch {
		case c.NotExecuted != "":
			res = "NOT EXECUTED: " + c.NotExecuted
		case !c.Passed():
			res = "FAIL"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %v | %s | %s | %d | %d | %s |\n", c.Strategy, c.Point, c.Scenario, c.PIDVerified && c.FaultFired, dash(c.CrashJournal), dash(c.FinalStatus+" "+c.FinalOutcome), len(c.Violations), c.SecretFindings, res)
	}
	for _, c := range cases {
		if c.Passed() && c.Availability == nil && len(c.Notes) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", c.ID)
		if len(c.Violations) > 0 {
			b.WriteString("\nViolations:\n" + FormatViolations(c.Violations))
		}
		for _, n := range c.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		if c.Availability != nil {
			b.WriteString("\n```\n" + c.Availability.String() + "\n```\n")
		}
	}
	return b.String()
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
