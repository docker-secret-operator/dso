package cli

import (
	"errors"
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/runtimedir"
)

// ensureRuntimeDirectory is replaced in tests so they never touch the real
// runtime directory.
var ensureRuntimeDirectory = runtimedir.Ensure

// establishAgentRuntimeDir enforces the runtime-directory invariant before the
// agent creates anything inside it (trigger engine, IPC socket, driver socket,
// event store).
//
// Production agent execution is root-only. A root agent must end up with
// /run/dso owned by root, group dso (root if the group does not exist), mode
// exactly 0750: the directory is created if missing, its group and mode are
// corrected when it is safe to do so, and a symlink, a non-directory or a
// directory owned by anyone else is refused. If the invariant cannot be
// established the agent does not start, because the IPC socket would live in a
// directory other users could replace entries in.
//
// A non-root process never modifies the canonical directory (it could not
// chown it anyway). That mode is for development with custom socket paths
// only; the canonical directory is left exactly as found.
func establishAgentRuntimeDir(logger *zap.Logger, euid int) error {
	if euid != 0 {
		logger.Warn("Agent is not running as root: the runtime directory "+runtimedir.CanonicalDir+" is not managed by this process. "+
			"Production agents must run as root; use custom --socket/--driver-socket paths for development",
			zap.Int("euid", euid))
		return nil
	}

	d, err := ensureRuntimeDirectory(runtimedir.CanonicalDir, runtimedir.CanonicalPolicyFor(euid))
	if err != nil {
		if errors.Is(err, runtimedir.ErrUnsupported) {
			// Only Linux can enforce the invariant. Say so loudly instead of
			// pretending it was enforced.
			logger.Warn("Runtime directory permissions cannot be enforced on this platform; Linux is the only supported production platform",
				zap.String("path", runtimedir.CanonicalDir), zap.Error(err))
			return nil
		}
		return fmt.Errorf("cannot establish the runtime directory invariant (owner root, group %s, mode %04o) on %s: %w",
			runtimedir.GroupName, uint32(runtimedir.DirMode), runtimedir.CanonicalDir, err)
	}
	if d != nil {
		// Only validation and correction were needed here; later users of the
		// directory open it themselves, so no descriptor is kept open.
		if cerr := d.Close(); cerr != nil {
			logger.Warn("Closing the runtime directory descriptor failed", zap.Error(cerr))
		}
	}
	return nil
}

// currentEUID is replaced in tests.
var currentEUID = os.Geteuid
