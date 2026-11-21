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
	// RotationID is the dso.rotation.id label: the identity of the rotation operation
	// that created the container. A candidate carries it even when the crash preceded
	// the journal recording its container ID.
	RotationID string
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
	// A record names its service by the identifier the strategy used: restart and rolling
	// journal the container NAME, while the signal strategy journals whatever the controller
	// derived (the compose service label, else the container ID). A container without a
	// compose label therefore yields a record whose service_id is the container's ID, so also
	// match the ID of the container carrying the service name. Without this a signal
	// record was invisible to every journal-dependent invariant (they ran vacuously).
	ids := map[string]bool{}
	for _, c := range s.Containers {
		if c.Name == service {
			ids[c.ID] = true
		}
	}
	if r, ok := pick(func(r RotationRecord) bool { return r.ServiceID == service || (r.ServiceID != "" && ids[r.ServiceID]) }); ok {
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
		if _, isArt := ArtifactBase(c.Name); !isArt && !removalIsRolledBackCandidate(w, crash, final, c) {
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
		// A documented manual-recovery point (a crash inside the rolling swap) is expected
		// to leave the service unreachable until an operator acts; CheckManualRecovery
		// asserts that outcome instead. Every other proxied point must serve traffic.
		if len(final.RoutedTo) == 0 && running > 0 && !p.DocumentedManual {
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

// rollbackScope is the set of restart-journal transitions the N21 controlled rollback
// acts on: every step before candidate_ready (mirrors rollbackTransition in
// internal/agent/recovery_rollback.go; TestRollbackScopeMatchesRotationConstants pins the
// names). candidate_ready and later steps are deliberately absent.
var rollbackScope = map[string]bool{
	"intent_rename_original": true, "intent_create_candidate": true, "candidate_created": true,
	"intent_stop_original": true, "original_stopped": true,
	"intent_start_replacement": true, "replacement_started": true, "intent_rollback": true,
	"gate_started": true, "intent_deliver_secrets": true, "delivery_partial": true,
	"secrets_delivered": true, "intent_release_gate": true, "gate_released": true,
}

// removalIsRolledBackCandidate reports whether a container that recovery removed, and
// that was not artifact-named at the crash, is demonstrably the interrupted restart
// rotation's own candidate retired after the N21 controlled rollback.
//
// N21 stops the unverified candidate and RENAMES it <service>_dso_new_<ts> before a later
// pass retires it as an artifact, so at the crash (the name this harness sees) the
// candidate still holds the real service name. Name alone therefore cannot tell it from
// an unrelated service container; identity can. Every one of these must hold, otherwise
// the removal is still reported by only-artifacts-removed:
//
//  1. identity: the container carries the interrupted record's rotation ID label, or its
//     ID is in the record's candidate list; a container labelled with a DIFFERENT
//     rotation never qualifies, even if its ID is listed
//  2. it is not an original (record's original IDs and World.OriginalID)
//  3. it is owned by this agent
//  4. the strategy is Restart
//  5. the crash-time journal transition is inside the N21 rollback scope
//  6. the original survives in the final snapshot, running, holding the real name
//  7. the final record of the same operation is rolled_back with an operation state of
//     rollback_container_restored_*
//
// Any missing record or empty identity fails closed.
func removalIsRolledBackCandidate(w World, crash, final Snapshot, removed CView) bool {
	if w.Strategy != Restart || removed.Owner != w.OwnerID || w.OriginalID == "" || removed.ID == w.OriginalID {
		return false
	}
	crashRec, haveCrash := crash.RecordFor(w.Service)
	finalRec, haveFinal := final.RecordFor(w.Service)
	if !haveCrash || !haveFinal || !rollbackScope[crashRec.Transition] {
		return false
	}
	rid := finalRec.RotationID
	if rid == "" || (crashRec.RotationID != "" && crashRec.RotationID != rid) {
		return false // no operation identity, or the crash and final records are different operations
	}
	if finalRec.Transition != "rolled_back" || !strings.HasPrefix(finalRec.OperationState, "rollback_container_restored_") {
		return false
	}
	if removed.RotationID != "" && removed.RotationID != rid {
		return false // another (stale) rotation's container
	}
	listed := false
	for _, ids := range [][]string{crashRec.CandidateContainerIDs, finalRec.CandidateContainerIDs} {
		for _, id := range ids {
			if id == removed.ID {
				listed = true
			}
		}
	}
	if removed.RotationID != rid && !listed {
		return false // not demonstrably this rotation's candidate
	}
	for _, recd := range []RotationRecord{crashRec, finalRec} {
		for _, id := range recd.Originals() {
			if id == removed.ID {
				return false
			}
		}
	}
	orig, ok := final.byID()[w.OriginalID]
	return ok && orig.Running && orig.Name == w.Service
}

// CheckManualRecovery asserts the documented outcome of a crash inside the rolling swap
// (Point.DocumentedManual): recovery stays fail-closed. It does NOT replace any other
// invariant; CheckRecovery still runs in full, so bystander, ownership, workload and
// routing-consistency rules stay active.
//
//   - the interrupted rotation is reported as requiring manual recovery: status
//     critical_error
//   - recovery did not commit, roll back or finish the operation on the operator's
//     behalf: the journal transition is exactly the one at the crash
//   - nothing was deleted or moved aside: every container that existed at the crash
//     (original, candidate, backup, bystanders) still exists with the same name
//
// A repeated recovery pass changing nothing is asserted separately by CheckSecondPass.
func CheckManualRecovery(w World, p Point, crash, final Snapshot) []Violation {
	if !p.DocumentedManual {
		return nil
	}
	var vs []Violation
	add := func(f string, a ...any) {
		vs = append(vs, Violation{"manual-recovery-fail-closed", fmt.Sprintf(f, a...)})
	}

	crashRec, haveCrash := crash.RecordFor(w.Service)
	finalRec, haveFinal := final.RecordFor(w.Service)
	switch {
	case !haveFinal:
		add("%s: no journal record of the interrupted rotation after recovery", p.Name)
	case finalRec.Status != "critical_error":
		add("%s: the rotation is %q, not critical_error: a documented manual-recovery state must be reported as requiring an operator", p.Name, finalRec.Status)
	case haveCrash && finalRec.Transition != crashRec.Transition:
		add("%s: recovery moved the journal from %q to %q; it must not decide the operation on the operator's behalf", p.Name, crashRec.Transition, finalRec.Transition)
	}
	fin := final.byID()
	for _, c := range crash.Containers {
		after, ok := fin[c.ID]
		switch {
		case !ok:
			add("%s: container %s (%q) was deleted by recovery in a state that must stay manual", p.Name, short(c.ID), c.Name)
		case after.Name != c.Name:
			add("%s: container %s was renamed %q -> %q (moved aside or retired) in a state that must stay manual", p.Name, short(c.ID), c.Name, after.Name)
		}
	}
	return vs
}
