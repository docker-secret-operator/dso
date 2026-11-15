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

// HardeningDirectives is the single source of the systemd [Service] sandboxing
// lines that do not depend on the host filesystem layout. The shipped
// bootstrap unit, the setup planner unit and the repair unit all embed it, so
// the three writers of dso-agent.service cannot disagree about them.
//
// Each line is justified by what the agent does (see SECURITY.md, "Service
// hardening"); none changes User=/Group= (the agent stays root: its Docker
// socket access is root-equivalent regardless of the UID, and it owns the
// root:dso runtime directory, state directories and privileged proxy ports).
//
//   - NoNewPrivileges=true: no DSO process needs a setuid/setgid or file-
//     capability transition. Provider plugins are plain go-plugin binaries
//     exec'd from a verified path or a hashed memfd copy.
//   - PrivateTmp=yes: the agent keeps no state or hand-off files in /tmp (state
//     is under /var/lib/dso, sockets under /run).
//   - ProtectClock/Hostname/KernelLogs/KernelModules/KernelTunables/
//     ControlGroups: the agent never sets the clock, hostname, sysctls, loads
//     modules, reads the kernel log or writes cgroups (it reads /proc/self
//     only; read-only /proc/sys, /sys and cgroupfs stay readable).
//   - RestrictNamespaces/RestrictRealtime/RestrictSUIDSGID/LockPersonality:
//     the agent and plugins do not create namespaces, use realtime scheduling,
//     create setuid files or change execution domain.
//   - RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6: agent/driver/plugin
//     sockets are AF_UNIX; providers and the proxy use TCP over IPv4/IPv6. No
//     netlink use was found in the agent.
//   - SystemCallArchitectures=native: the agent and plugins are native
//     binaries.
//
// Deliberately absent (see SECURITY.md): MemoryDenyWriteExecute (Go runtime
// and plugins), UMask (the socket path relies on a scoped umask helper plus
// explicit chmod), CapabilityBoundingSet (a safe minimum cannot be proven
// without a real systemd host: plugins run as children with the same bounding
// set), and the filesystem sandbox (ProtectSystem/ProtectHome/PrivateDevices/
// ReadWritePaths), which depends on paths only the bootstrap unit creates and
// is kept there.
const HardeningDirectives = "NoNewPrivileges=true\n" +
	"PrivateTmp=yes\n" +
	"ProtectClock=yes\n" +
	"ProtectHostname=yes\n" +
	"ProtectKernelLogs=yes\n" +
	"ProtectKernelModules=yes\n" +
	"ProtectKernelTunables=yes\n" +
	"ProtectControlGroups=yes\n" +
	"RestrictNamespaces=yes\n" +
	"RestrictRealtime=yes\n" +
	"RestrictSUIDSGID=yes\n" +
	"LockPersonality=yes\n" +
	"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6\n" +
	"SystemCallArchitectures=native\n"
