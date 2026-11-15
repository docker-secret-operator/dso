package runtimedir

// UnitDirectives is the single source of the systemd [Service] lines that
// declare the runtime directory. The shipped bootstrap unit, the setup planner
// unit and the repair unit all embed it, so the three writers of
// dso-agent.service cannot disagree about /run/dso.
//
//   - RuntimeDirectory=dso / RuntimeDirectoryMode=0750: systemd creates
//     /run/dso before ExecStart with the mode the agent enforces itself
//     (DirMode). Owner and group come from the unit's User=/Group= (root:root
//     for the shipped unit); the agent corrects the group to dso through its
//     directory descriptor on every start, so systemd's group is only ever
//     more restrictive than the final state.
//   - RuntimeDirectoryPreserve=restart: the directory survives a restart
//     (including Restart=on-failure) but is removed on an explicit stop and
//     does not survive a reboot (/run is a tmpfs). Nothing durable lives
//     there: the rotation journal, locks, vault, cache and audit data are
//     under /var/lib/dso. The only persisted content is the operational event
//     log events.jsonl, which the event store recreates empty when absent.
//
// Units deliberately carry no ExecStartPre "install -d /run/dso": systemd has
// already created the directory, and the agent re-establishes the invariant
// before it creates anything in it.
const UnitDirectives = "RuntimeDirectory=dso\n" +
	"RuntimeDirectoryMode=0750\n" +
	"RuntimeDirectoryPreserve=restart\n"
