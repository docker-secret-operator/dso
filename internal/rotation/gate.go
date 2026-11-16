package rotation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/strslice"

	"github.com/docker-secret-operator/dso/internal/injector"
)

// N19 managed file delivery: the startup gate.
//
// A candidate container created by a rotation for a config-driven `inject.type:
// file` secret is created with its ENTRYPOINT wrapped by a fixed POSIX shell gate.
// The gate does not start the application until a release flag (and every expected
// secret file) exists on the secrets tmpfs. The agent writes secret files and, last,
// the flag through the hardened exec injector only after delivery was verified, so
// the application can never run, or consume a secret, before delivery evidence
// exists. Nothing about delivery goes through the archive endpoint (Docker may write
// an archive upload into a created container's writable layer instead of a tmpfs:
// RUNTIME-UNVERIFIED, the reason for this design).
//
// This reuses the local-mode `dsofile://` idea (wait for the secret files, then
// exec) but is a separate, rotation-time builder: the compose-time wrapper
// (resolver.wrapCommandWithWait) rewrites compose `command:` nodes and cannot be
// applied to a container.Config. Nothing here is a sidecar, daemon or runtime.

const (
	// GateLabel marks a gated container. The value is a non-secret marker (digest of
	// the sorted target paths and file names).
	GateLabel = "dso.gate"
	// GateFlagName is the release flag written (last) into the first target
	// directory. It holds no secret.
	GateFlagName = ".dso-gate-release"
	// DefaultGateTimeout bounds how long the gate waits before it exits non-zero
	// (exit 75), turning "stuck forever" into a visible, restartable exit.
	DefaultGateTimeout = 120 * time.Second
	// GateExitTimeout is the gate's exit status when the wait expires.
	GateExitTimeout = 75

	gateShell = "/bin/sh"
)

// ErrDeliveryRefused: managed file delivery cannot be done safely for this
// container/configuration. Raised BEFORE any container is created or any workload is
// touched (a deterministic refusal, not a transient failure): retrying or falling back
// to another strategy cannot help, and no plaintext fallback exists.
var ErrDeliveryRefused = errors.New("rotation: managed file delivery refused (fail closed, nothing was changed)")

// gateScript is a FIXED program: nothing user- or secret-derived is interpolated into
// it. Arguments: $1 timeout seconds, $2 release-flag path, $3 N, then N secret file
// paths, then the original argv (original ENTRYPOINT followed by Docker's CMD).
//
//   - TERM/INT are trapped so `docker stop` of a gated PID 1 does not wait for SIGKILL.
//   - It waits for the flag AND every file (the flag alone is what the agent controls;
//     the files are defence in depth against a flag written without them).
//   - `exec "$@"` REPLACES the shell with the original process (no fork: the
//     application becomes PID 1, receives signals directly, argument boundaries are
//     preserved exactly).
//   - The wait is bounded (exit 75).
const gateScript = `trap 'exit 143' TERM
trap 'exit 130' INT
t=$1; flag=$2; n=$3; shift 3
ready() {
  [ -f "$flag" ] || return 1
  k=0
  for f in "$@"; do
    [ "$k" -lt "$n" ] || break
    [ -f "$f" ] || return 1
    k=$((k+1))
  done
  return 0
}
max=$((t*10)); i=0; nap=0.1; step=1
until ready "$@"; do
  if [ "$i" -ge "$max" ]; then echo "dso-gate: secret delivery did not complete in time" >&2; exit 75; fi
  if ! sleep "$nap" 2>/dev/null; then nap=1; step=10; sleep 1; fi
  i=$((i+step))
done
shift "$n"
exec "$@"
`

// GateScript returns the fixed gate program text (for tests and documentation).
func GateScript() string { return gateScript }

// gatePrefixLen is the number of leading Entrypoint elements before the variable part.
const gatePrefixLen = 4 // /bin/sh -c SCRIPT dso-gate

// splitGated recognises an Entrypoint produced by ArmGate and returns the original
// Entrypoint it wraps. wrapped is false for any other value.
func splitGated(ep []string) (orig []string, wrapped bool) {
	if len(ep) < gatePrefixLen+3 || ep[0] != gateShell || ep[1] != "-c" || ep[2] != gateScript || ep[3] != "dso-gate" {
		return ep, false
	}
	n, err := strconv.Atoi(ep[gatePrefixLen+2])
	if err != nil || n < 0 || len(ep) < gatePrefixLen+3+n {
		return ep, false
	}
	return ep[gatePrefixLen+3+n:], true
}

// gateExpectedFiles returns the secret file paths the gate in ep waits for (nil when ep
// is not a gate produced by ArmGate).
func gateExpectedFiles(ep []string) []string {
	if _, ok := splitGated(ep); !ok {
		return nil
	}
	n, _ := strconv.Atoi(ep[gatePrefixLen+2])
	return append([]string(nil), ep[gatePrefixLen+3:gatePrefixLen+3+n]...)
}

// GateFlagPath is the release-flag path used for specs (the first sorted directory).
func GateFlagPath(specs []FileSecretSpec) string {
	dirs := specDirs(specs)
	if len(dirs) == 0 {
		return ""
	}
	return path.Join(dirs[0], GateFlagName)
}

// GateFlagDir is the directory holding the release flag for specs.
func GateFlagDir(specs []FileSecretSpec) string {
	dirs := specDirs(specs)
	if len(dirs) == 0 {
		return ""
	}
	return dirs[0]
}

func specDirs(specs []FileSecretSpec) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, s := range specs {
		if !seen[s.Path] {
			seen[s.Path] = true
			dirs = append(dirs, s.Path)
		}
	}
	sort.Strings(dirs)
	return dirs
}

// gateFiles returns the sorted absolute file paths the gate waits for.
func gateFiles(specs []FileSecretSpec) []string {
	var files []string
	for _, s := range specs {
		for name := range s.Data {
			files = append(files, path.Join(s.Path, name))
		}
	}
	sort.Strings(files)
	return files
}

// forbiddenDeliveryRoots are never valid secret delivery directories.
var forbiddenDeliveryRoots = []string{"/proc", "/sys", "/dev"}

// ValidateFileSecretSpecs refuses unsafe delivery targets BEFORE any container is
// created: a non-absolute/unclean/root path, a pseudo-filesystem path, an unsafe file
// name, duplicate destinations, or an empty secret. Refusals wrap ErrDeliveryRefused.
func ValidateFileSecretSpecs(specs []FileSecretSpec) error {
	if len(specs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, s := range specs {
		if s.Path == "" {
			return fmt.Errorf("%w: a file secret has no inject.path", ErrDeliveryRefused)
		}
		if err := injector.CheckDir(s.Path); err != nil {
			return fmt.Errorf("%w: unsafe destination: %v", ErrDeliveryRefused, err)
		}
		for _, root := range forbiddenDeliveryRoots {
			if s.Path == root || strings.HasPrefix(s.Path, root+"/") {
				return fmt.Errorf("%w: destination %q is on a pseudo filesystem", ErrDeliveryRefused, s.Path)
			}
		}
		if s.UID < 0 || s.GID < 0 {
			return fmt.Errorf("%w: invalid owner %d:%d", ErrDeliveryRefused, s.UID, s.GID)
		}
		if len(s.Data) == 0 {
			return fmt.Errorf("%w: file secret for %s has no files", ErrDeliveryRefused, s.Path)
		}
		for name := range s.Data {
			// The injector normalises a name with filepath.Base; the gate and the probe
			// must wait for/verify exactly the file that gets written, so a name that is
			// not already a plain base name is refused instead of normalised.
			if name != path.Base(name) || strings.ContainsAny(name, `/\`) {
				return fmt.Errorf("%w: invalid secret file name %q: must be a plain filename with no path components", ErrDeliveryRefused, name)
			}
			if err := injector.CheckFileName(name); err != nil {
				return fmt.Errorf("%w: %v", ErrDeliveryRefused, err)
			}
			if name == GateFlagName {
				return fmt.Errorf("%w: secret file name %q is reserved", ErrDeliveryRefused, name)
			}
			dest := path.Join(s.Path, name)
			if seen[dest] {
				return fmt.Errorf("%w: destination %s is delivered twice", ErrDeliveryRefused, dest)
			}
			seen[dest] = true
		}
	}
	return nil
}

// covers reports whether mount destination m covers path p (equal or ancestor).
func covers(m, p string) bool {
	return m == p || m == "/" || strings.HasPrefix(p, strings.TrimSuffix(m, "/")+"/")
}

// VerifyDeclaredTmpfs checks the DECLARED configuration of a candidate (before it is
// created): every target directory must be covered by a tmpfs declaration and no more
// specific bind mount or volume may shadow it. A tmpfs declared read-only is refused.
// The persistent writable layer is never an acceptable destination, and DSO does not
// add mounts or change runtime settings on the operator's behalf.
func VerifyDeclaredTmpfs(cfg *container.Config, hc *container.HostConfig, specs []FileSecretSpec) error {
	if len(specs) == 0 {
		return nil
	}
	if hc == nil {
		return fmt.Errorf("%w: host configuration unavailable", ErrDeliveryRefused)
	}
	for _, dir := range specDirs(specs) {
		best, bestTmpfs, bestRO := -1, false, false
		consider := func(dest string, isTmpfs, ro bool) {
			if covers(dest, dir) && len(dest) > best {
				best, bestTmpfs, bestRO = len(dest), isTmpfs, ro
			}
		}
		for dest, opts := range hc.Tmpfs {
			consider(path.Clean(dest), true, hasOpt(opts, "ro"))
		}
		for _, m := range hc.Mounts {
			consider(path.Clean(m.Target), m.Type == mount.TypeTmpfs, m.ReadOnly)
		}
		for _, b := range hc.Binds {
			parts := strings.Split(b, ":")
			if len(parts) >= 2 {
				ro := len(parts) >= 3 && hasOpt(parts[2], "ro")
				consider(path.Clean(parts[1]), false, ro)
			}
		}
		if cfg != nil {
			for v := range cfg.Volumes {
				consider(path.Clean(v), false, false)
			}
		}
		switch {
		case best < 0 || (best == 1 && !bestTmpfs):
			return fmt.Errorf("%w: %s is not covered by a tmpfs mount declared on the container (DSO will not write secret material to the container's writable layer; declare `tmpfs: [%s]`)", ErrDeliveryRefused, dir, dir)
		case !bestTmpfs:
			return fmt.Errorf("%w: %s is covered by a bind mount or volume, not a tmpfs (secret material would be persisted)", ErrDeliveryRefused, dir)
		case bestRO:
			return fmt.Errorf("%w: the tmpfs covering %s is read-only", ErrDeliveryRefused, dir)
		}
	}
	return nil
}

func hasOpt(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if strings.TrimSpace(o) == want {
			return true
		}
	}
	return false
}

// VerifyUserOwnership refuses, before any change, an owner the delivery exec cannot
// produce: the injector exec runs as the container's configured user, which can only
// chown to its own uid. A numeric non-root user must equal inject.uid; root and
// non-numeric (named) users are left to the injector, which fails closed on a chown
// it cannot perform.
func VerifyUserOwnership(cfg *container.Config, specs []FileSecretSpec) error {
	if cfg == nil || cfg.User == "" {
		return nil
	}
	uidPart, _, _ := strings.Cut(cfg.User, ":")
	uid, err := strconv.Atoi(uidPart)
	if err != nil || uid == 0 {
		return nil
	}
	for _, s := range specs {
		if s.UID != uid {
			return fmt.Errorf("%w: the container runs as uid %d but inject.uid is %d; the delivery exec runs as the container user and cannot chown to another uid (set inject.uid to %d)", ErrDeliveryRefused, uid, s.UID, uid)
		}
	}
	return nil
}

// ArmGate wraps cfg's ENTRYPOINT with the startup gate for specs, preserving the
// original contract: the effective argv is gate-prefix ++ original ENTRYPOINT ++ CMD,
// so `exec "$@"` re-creates exactly (original ENTRYPOINT ++ CMD) with every argument
// boundary intact. Cmd, User, WorkingDir, Env and every runtime setting are NOT touched.
//
// cfg must be a clone the caller owns (PrepareShadowConfig / cloneContainerConfig):
// ArmGate assigns NEW slices and never mutates the originals. An Entrypoint that is
// already gated is unwrapped first and re-wrapped for the current file set, so
// successive rotations never nest. A container with neither ENTRYPOINT nor CMD cannot
// be wrapped and is refused. The returned label value is also set as GateLabel.
func ArmGate(cfg *container.Config, specs []FileSecretSpec, timeout time.Duration) error {
	if cfg == nil {
		return fmt.Errorf("%w: no container configuration", ErrDeliveryRefused)
	}
	if err := ValidateFileSecretSpecs(specs); err != nil {
		return err
	}
	if len(specs) == 0 {
		return fmt.Errorf("%w: no file secrets to gate", ErrDeliveryRefused)
	}
	orig, _ := splitGated([]string(cfg.Entrypoint))
	if len(orig) == 0 && len(cfg.Cmd) == 0 {
		return fmt.Errorf("%w: the container has neither an ENTRYPOINT nor a CMD, so the original process cannot be preserved", ErrDeliveryRefused)
	}
	if timeout <= 0 {
		timeout = DefaultGateTimeout
	}
	files := gateFiles(specs)
	ep := make([]string, 0, gatePrefixLen+3+len(files)+len(orig))
	ep = append(ep, gateShell, "-c", gateScript, "dso-gate",
		strconv.Itoa(int(timeout/time.Second)), GateFlagPath(specs), strconv.Itoa(len(files)))
	ep = append(ep, files...)
	ep = append(ep, orig...)
	cfg.Entrypoint = strslice.StrSlice(ep)

	sum := sha256.Sum256([]byte(strings.Join(files, "\x00")))
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{}
	}
	cfg.Labels[GateLabel] = hex.EncodeToString(sum[:8])
	return nil
}

// OriginalCommand returns the (ENTRYPOINT, CMD) the gate preserves: for a gated config
// the wrapped original, otherwise the config's own. Exposed for tests and diagnostics.
func OriginalCommand(cfg *container.Config) (entrypoint, cmd []string) {
	ep, _ := splitGated([]string(cfg.Entrypoint))
	return ep, []string(cfg.Cmd)
}

// IsGated reports whether cfg's ENTRYPOINT is a DSO startup gate.
func IsGated(cfg *container.Config) bool {
	if cfg == nil {
		return false
	}
	_, w := splitGated([]string(cfg.Entrypoint))
	return w
}

// PrepareGatedCandidate is the single pre-create step every strategy runs for a
// candidate that must receive file secrets: it validates the targets, requires a
// declared tmpfs (never the writable layer), checks ownership feasibility and arms
// the gate on cfg (a caller-owned clone). It performs NO Docker call and changes no
// workload, so a refusal costs nothing. Errors wrap ErrDeliveryRefused.
func PrepareGatedCandidate(cfg *container.Config, hc *container.HostConfig, specs []FileSecretSpec, gateTimeout time.Duration) error {
	if err := ValidateFileSecretSpecs(specs); err != nil {
		return err
	}
	if err := VerifyDeclaredTmpfs(cfg, hc, specs); err != nil {
		return err
	}
	if err := VerifyUserOwnership(cfg, specs); err != nil {
		return err
	}
	return ArmGate(cfg, specs, gateTimeout)
}
