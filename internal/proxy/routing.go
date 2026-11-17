package proxy

// Routing inspection (M0-04 finding 3.1).
//
// The proxy is EMBEDDED in `dso agent` and its backend table is in-memory: when the
// agent dies the table dies with it, and nothing about it is persisted. A journalled
// "handoff done" acknowledgement therefore proves only that routing was switched by
// a process that no longer exists. What a freshly started agent can legitimately say
// about routing is what ITS OWN proxy has been told since it started: the table that
// startup reconstruction (ScanAndRegister) rebuilt from Docker, plus whatever swaps
// this process has performed since.
//
// RoutingSnapshot exposes exactly that table, and only claims it is "established"
// when the reconstruction that produced it is known to be complete. Recovery uses it
// to retire a workload only when the snapshot positively shows the replacement
// receiving traffic and the retiree not; anything else is "unknown" and preserves
// both workloads.

// reconstructionState records how far startup reconstruction got.
type reconstructionState int

const (
	// The zero value: no scan has run in this process (the table is empty
	// because nobody filled it, not because nothing is routed).
	_ reconstructionState = iota
	// reconstructionComplete: the Docker listing succeeded and every eligible
	// container with a host-port mapping was processed.
	reconstructionComplete
	// reconstructionIncomplete: the listing failed; the table is empty out of
	// ignorance.
	reconstructionIncomplete
)

// RoutingSnapshot returns the IDs of the backends that currently receive NEW
// connections (non-draining) in this process's routing table, and whether that
// table can be trusted as the complete routing state.
//
// established is false when no reconstruction has completed in this process or the
// scan could not list Docker: the table is then empty out of ignorance and an
// absent ID proves nothing. Once established, the table is by construction what
// this process routes (a container that failed to bind/register is not routed and
// is correctly absent). It says nothing about backend health; recovery verifies
// that separately.
func (m *Manager) RoutingSnapshot() (active []string, established bool) {
	m.rmu.Lock()
	st := m.reconstruct
	m.rmu.Unlock()
	for _, b := range m.registry.Active() {
		active = append(active, b.ID)
	}
	return active, st == reconstructionComplete
}
