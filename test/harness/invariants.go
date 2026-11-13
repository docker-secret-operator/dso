package harness

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CView is the harness' read-only view of one container.
type CView struct {
	ID      string
	Name    string // without the leading "/"
	Running bool
	Health  string // "", "starting", "healthy", "unhealthy"
	Owner   string // dso.owner label
}

// Snapshot is everything observed about the world at one moment.
type Snapshot struct {
	Containers []CView
	Journal    []RotationRecord
	// RoutedTo are the container IDs that answered real traffic through the proxy
	// since the previous snapshot (proxied scenarios only).
	RoutedTo []string
}

// World is what the test knows statically.
type World struct {
	Service    string
	OwnerID    string
	Strategy   Strategy
	Proxied    bool
	OriginalID string
	Bystanders []string // unrelated containers (no DSO labels)
	Foreign    []string // containers owned by a DIFFERENT agent
}

// Violation is one failed invariant.
type Violation struct{ Rule, Detail string }

func (v Violation) String() string { return v.Rule + ": " + v.Detail }

// FormatViolations renders violations one per line.
func FormatViolations(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		b.WriteString("  - " + v.String() + "\n")
	}
	return b.String()
}

var oldSuffix = regexp.MustCompile(`_old_[0-9]+$`)

// ArtifactBase reports whether name is one of DSO's rotation-artifact names
// ("<svc>_dso_backup_*", "<svc>_dso_new_*", "<svc>_old_<ts>") and its service.
func ArtifactBase(name string) (string, bool) {
	if i := strings.Index(name, "_dso_backup_"); i > 0 {
		return name[:i], true
	}
	if i := strings.Index(name, "_dso_new_"); i > 0 {
		return name[:i], true
	}
	if loc := oldSuffix.FindStringIndex(name); loc != nil && loc[0] > 0 {
		return name[:loc[0]], true
	}
	return "", false
}

func (s Snapshot) byID() map[string]CView {
	m := make(map[string]CView, len(s.Containers))
	for _, c := range s.Containers {
		m[c.ID] = c
	}
	return m
}

// serviceContainers returns the containers of service owned by owner: the real name
// holder and its rotation artifacts.
func (s Snapshot) serviceContainers(service, owner string) []CView {
	var out []CView
	for _, c := range s.Containers {
		if c.Owner != owner {
			continue
		}
		if base, ok := ArtifactBase(c.Name); (ok && base == service) || c.Name == service {
			out = append(out, c)
		}
	}
	return out
}

// RecordFor returns the journal record of the interrupted rotation of service (the
// most recently updated one). Records that name the service win; records with no
// service identity (a rotation that failed before reaching a strategy) are used
// only when none names it.
func (s Snapshot) RecordFor(service string) (RotationRecord, bool) {
	pick := func(match func(RotationRecord) bool) (RotationRecord, bool) {
		var best RotationRecord
		found := false
		for _, r := range s.Journal {
			if !match(r) {
				continue
			}
			if !found || r.LastUpdate.After(best.LastUpdate) {
				best, found = r, true
			}
		}
		return best, found
	}
	if r, ok := pick(func(r RotationRecord) bool { return r.ServiceID == service }); ok {
		return r, true
	}
	return pick(func(r RotationRecord) bool { return r.ServiceID == "" })
}

// committedFor reports whether the journal shows the ORIGINAL as legitimately
// retired-or-retirable for this strategy.
func committedFor(st Strategy, r RotationRecord) bool {
	switch st {
	case Rolling:
		if r.Committed {
			return true
		}
		switch r.Transition {
		case "swap_committed", "intent_cleanup", "completed":
			return true
		}
	case Restart:
		switch r.Transition {
		case "intent_cleanup", "completed":
			return true
		}
	}
	return false // signal never retires the original
}

// CheckRecovery applies the safety invariants to the state observed after a crash
// (crash) and after the restarted agent finished recovery (final).
func CheckRecovery(w World, p Point, crash, final Snapshot) []Violation {
	var vs []Violation
	add := func(rule, f string, a ...any) { vs = append(vs, Violation{rule, fmt.Sprintf(f, a...)}) }
	fin := final.byID()
	rec, haveRec := final.RecordFor(w.Service)

	// 1. A recoverable workload for the service survives; if none is RUNNING that
	//    must be reported (critical_error), never silently.
	svc := final.serviceContainers(w.Service, w.OwnerID)
	if len(svc) == 0 {
		add("workload-survives", "no container of service %q remains: the only recoverable workload was deleted", w.Service)
	}
	running := 0
	for _, c := range svc {
		if c.Running {
			running++
		}
	}
	crashRunning := 0
	for _, c := range crash.serviceContainers(w.Service, w.OwnerID) {
		if c.Running {
			crashRunning++
		}
	}
	if running == 0 && len(svc) > 0 && (!haveRec || rec.Status != "critical_error") {
		add("stopped-workload-reported", "no service container is running after recovery and the journal does not flag it for manual recovery (status=%q)", rec.Status)
	}
	if crashRunning > 0 && running == 0 {
		add("recovery-did-not-stop-service", "%d service container(s) were running after the crash and none is running after recovery", crashRunning)
	}

	// 2. Unrelated and foreign-owned containers are untouched.
	for _, group := range []struct {
		kind string
		ids  []string
	}{{"unrelated", w.Bystanders}, {"foreign-owned", w.Foreign}} {
		for _, id := range group.ids {
			before, hadBefore := crash.byID()[id]
			after, ok := fin[id]
			switch {
			case !ok:
				add("bystander-untouched", "%s container %s was removed", group.kind, short(id))
			case hadBefore && (after.Name != before.Name || after.Running != before.Running):
				add("bystander-untouched", "%s container %s changed (name %q->%q, running %v->%v)", group.kind, short(id), before.Name, after.Name, before.Running, after.Running)
			}
		}
	}

	// 3. The original is not retired before the journal shows the swap committed.
	if w.OriginalID != "" && !committedFor(w.Strategy, rec) {
		if _, ok := fin[w.OriginalID]; !ok && haveRec {
			add("original-not-retired", "the original %s was removed although the journal (transition=%q committed=%v) does not show it retired", short(w.OriginalID), rec.Transition, rec.Committed)
		}
	}

	// 4. Removals by recovery: only artifact-named containers. After the proxy handoff
	//    started recovery may remove one ONLY with live-routing evidence: the agent now
	//    wires a RoutingVerifier over its own proxy table (M0-04 3.1), so a removal is
	//    legitimate exactly when the surviving real-name holder is the container that
	//    answers traffic afterwards. The measured routing of the final snapshot is the
	//    independent evidence; a persisted handoff acknowledgement alone is not.
	holderRouted := false
	routedSet := map[string]bool{}
	for _, id := range final.RoutedTo {
		routedSet[id] = true
	}
	for _, c := range svc {
		if c.Running && c.Name == w.Service && routedSet[c.ID] {
			holderRouted = true
		}
	}
	for _, c := range crash.Containers {
		if _, still := fin[c.ID]; still {
			continue
		}
		if c.Owner != w.OwnerID {
			continue // already reported by the bystander rule where relevant
		}
		if _, isArt := ArtifactBase(c.Name); !isArt {
			add("only-artifacts-removed", "recovery removed %s (%q), which is not a rotation artifact", short(c.ID), c.Name)
		}
		if p.HandoffStarted && w.Proxied && !holderRouted {
			add("no-removal-after-handoff", "recovery removed %s (%q) after the proxy handoff had started but the surviving name holder is not observed receiving traffic (no live-routing evidence)", short(c.ID), c.Name)
		}
	}

	// 5. An interrupted operation is never marked successful.
	if haveRec {
		if rec.Status == "completed" {
			add("interrupted-not-successful", "the interrupted rotation %s is marked completed", rec.RotationID)
		}
		if rec.OperationState == "reload_requested_adoption_unverified" {
			add("no-adoption-claim", "journal claims a finished reload for an interrupted rotation")
		}
		if rec.Status == "recovered" {
			holder := false
			for _, c := range svc {
				if c.Name == w.Service && c.Running {
					holder = true
				}
			}
			if !holder && w.Strategy != Signal {
				add("recovered-needs-holder", "rotation marked recovered but no running container holds the name %q", w.Service)
			}
			if p.NoNameHolder {
				add("no-holder-must-be-manual", "after %s nothing held the real name, yet the rotation was marked recovered", p.Name)
			}
		}
		if p.NoNameHolder && rec.Status != "critical_error" && running == 0 {
			add("no-holder-must-be-manual", "after %s no workload is running and status=%q", p.Name, rec.Status)
		}
	}

	// 6. Routing consistent with durable state (proxied).
	if w.Proxied {
		ok := map[string]bool{}
		for _, c := range svc {
			if c.Running {
				ok[c.ID] = true
			}
		}
		allowed := map[string]bool{}
		if w.OriginalID != "" {
			allowed[w.OriginalID] = true
		}
		if p.HandoffStarted || committedFor(w.Strategy, rec) {
			for _, id := range rec.CandidateContainerIDs {
				allowed[id] = true
			}
			for _, c := range svc { // a promoted candidate may no longer be in the journal's list under its old name
				if c.Name == w.Service {
					allowed[c.ID] = true
				}
			}
		}
		if len(final.RoutedTo) == 0 && running > 0 {
			add("service-reachable", "the service has a running container but no traffic was answered after recovery")
		}
		for _, id := range final.RoutedTo {
			switch {
			case !ok[id]:
				add("routing-consistent", "traffic was answered by %s, which is not a running owned container of the service", short(id))
			case !allowed[id]:
				add("routing-consistent", "traffic was answered by %s, which the durable state (transition=%q committed=%v) does not authorise", short(id), rec.Transition, rec.Committed)
			}
		}
	}
	return vs
}

// CheckSecondPass requires a second recovery pass over the same world to change
// nothing: same containers (ID, name, running state) and the same journal digest.
func CheckSecondPass(first, second Snapshot) []Violation {
	var vs []Violation
	if a, b := containerDigest(first), containerDigest(second); a != b {
		vs = append(vs, Violation{"second-pass-idempotent", "the container set changed on the second recovery pass:\n" + diff(a, b)})
	}
	if a, b := Digest(first.Journal), Digest(second.Journal); a != b {
		vs = append(vs, Violation{"second-pass-idempotent", "the journal changed on the second recovery pass:\n" + diff(a, b)})
	}
	return vs
}

// CheckCorruptJournal: with an unreadable journal recovery must be conservative:
// nothing removed, nothing started or stopped, the unreadable file preserved.
func CheckCorruptJournal(before, after Snapshot, quarantined []string) []Violation {
	var vs []Violation
	if a, b := containerDigest(before), containerDigest(after); a != b {
		vs = append(vs, Violation{"corrupt-journal-conservative", "containers changed although the journal was unreadable:\n" + diff(a, b)})
	}
	if len(quarantined) == 0 {
		vs = append(vs, Violation{"corrupt-journal-preserved", "the unreadable journal was not preserved as evidence"})
	}
	return vs
}

// SignalEvidence is what CheckSignal needs beyond the container snapshots.
type SignalEvidence struct {
	ReloadsAtCrash, ReloadsAfter int               // application reload counter; -1 when unknown
	FilesAtCrash, FilesAfter     map[string]string // secret file name -> content (as seen in the container)
}

// CheckSignal: an interrupted signal rotation stays UNCERTAIN: the container is not
// recreated, the reload is not blindly repeated, files are not blindly re-delivered
// and the operation is flagged for manual recovery.
func CheckSignal(w World, crash, final Snapshot, ev SignalEvidence) []Violation {
	var vs []Violation
	add := func(rule, f string, a ...any) { vs = append(vs, Violation{rule, fmt.Sprintf(f, a...)}) }
	c, ok := final.byID()[w.OriginalID]
	if !ok || !c.Running {
		add("signal-container-intact", "the signalled container %s is not running after recovery (present=%v)", short(w.OriginalID), ok)
	}
	if ev.ReloadsAtCrash >= 0 && ev.ReloadsAfter >= 0 && ev.ReloadsAfter != ev.ReloadsAtCrash {
		add("reload-not-repeated", "the application reload counter went %d -> %d across recovery: the reload was blindly repeated", ev.ReloadsAtCrash, ev.ReloadsAfter)
	}
	if len(ev.FilesAtCrash) > 0 && fmt.Sprint(sortedKV(ev.FilesAtCrash)) != fmt.Sprint(sortedKV(ev.FilesAfter)) {
		add("no-blind-redelivery", "secret files changed across recovery (before=%v after=%v): partial delivery was silently completed or reverted", fileNames(ev.FilesAtCrash), fileNames(ev.FilesAfter))
	}
	// Was a signal rotation durably in flight at the crash? (An Nth-hit persistence
	// fault can land before the intent became durable; then there is nothing to be
	// uncertain about and only the "nothing changed" checks above apply.)
	if cr, durable := crash.RecordFor(w.Service); durable && cr.Strategy == "signal" {
		rec, have := final.RecordFor(w.Service)
		switch {
		case !have:
			add("signal-uncertain", "the journal record of the interrupted signal rotation disappeared")
		case rec.Status != "critical_error":
			add("signal-uncertain", "an interrupted signal rotation must stay flagged for manual recovery, status=%q transition=%q", rec.Status, rec.Transition)
		}
	}
	return vs
}

func containerDigest(s Snapshot) string {
	lines := make([]string, 0, len(s.Containers))
	for _, c := range s.Containers {
		lines = append(lines, fmt.Sprintf("%s %s running=%v", short(c.ID), c.Name, c.Running))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func diff(a, b string) string {
	as, bs := set(a), set(b)
	var out []string
	for l := range as {
		if !bs[l] {
			out = append(out, "    - "+l)
		}
	}
	for l := range bs {
		if !as[l] {
			out = append(out, "    + "+l)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func set(s string) map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Split(s, "\n") {
		m[l] = true
	}
	return m
}

func sortedKV(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func fileNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
