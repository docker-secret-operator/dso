package injector

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// validFileName restricts secret file names to characters that are safe to
// interpolate into the shell command built by buildInjectCmd. Names come from
// resolver.ServiceSecrets, which derives them from compose service names —
// untrusted input from the compose file — so this is the last line of
// defense against shell injection (SEC-6), not just path traversal.
var validFileName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// secretDir is the in-container directory secret files are delivered to.
const secretDir = "/run/secrets/dso"

// Options tunes file injection.
type Options struct {
	// Dir is the in-container directory to deliver into. Empty means the
	// default /run/secrets/dso. It must be an absolute, clean, non-root path.
	Dir string
	// RequireTmpfs makes injection fail unless secretDir is on a tmpfs mount,
	// so plaintext never lands on the container's writable (host-disk) layer.
	// The DSO compose rewrite mounts a tmpfs there; a container started some
	// other way may not have one.
	RequireTmpfs bool
}

// InjectFiles writes secret files into the container's live filesystem using
// docker exec with stdin delivery. This correctly targets tmpfs mounts which
// are only visible inside the container's mount namespace (not accessible via
// CopyToContainer from the host overlay layer).
//
// Guarantee: each individual file is published atomically (a reader sees the
// previous complete file or the new complete file, never a partial one) and a
// failed delivery leaves the previous file untouched and no temporary file
// behind. Guarantee NOT given: a *set* of files is not replaced atomically as a
// group; applications needing a consistent multi-file snapshot must tolerate a
// brief window where files are from different versions.
func InjectFiles(ctx context.Context, cli *client.Client, containerID string, files map[string]string, uid, gid int) error {
	return InjectFilesWithOptions(ctx, cli, containerID, files, uid, gid, Options{})
}

// InjectFilesWithOptions is InjectFiles with explicit options.
func InjectFilesWithOptions(ctx context.Context, cli *client.Client, containerID string, files map[string]string, uid, gid int, opts Options) error {
	if len(files) == 0 {
		return nil
	}
	if _, err := resolveDir(opts.Dir); err != nil {
		return err
	}
	// Validate every name before any write so a bad name cannot leave a
	// partially delivered set.
	for fileName := range files {
		if _, err := checkFileName(fileName); err != nil {
			return err
		}
	}
	for fileName, content := range files {
		if err := injectOneFileOpts(ctx, cli, containerID, fileName, content, uid, gid, opts); err != nil {
			return err
		}
	}
	return nil
}

// CheckFileName reports whether name is an acceptable secret file name for the
// exec injector (the same rule the injector applies before any write). Exported
// so managed callers can refuse an unsafe name BEFORE any container is created.
func CheckFileName(name string) error {
	_, err := checkFileName(name)
	return err
}

// CheckDir reports whether dir is an acceptable delivery directory (absolute,
// clean, non-root, no NUL; empty means the default). Exported for the same
// reason as CheckFileName.
func CheckDir(dir string) error {
	_, err := resolveDir(dir)
	return err
}

func injectOneFile(ctx context.Context, cli *client.Client, containerID, fileName, content string, uid, gid int) error {
	return injectOneFileOpts(ctx, cli, containerID, fileName, content, uid, gid, Options{})
}

func checkFileName(fileName string) (string, error) {
	// Sanitize fileName to prevent path traversal inside the container.
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || fileName == ".." || strings.ContainsRune(fileName, '/') {
		return "", fmt.Errorf("invalid secret file name %q: must be a plain filename with no path components", fileName)
	}
	if !validFileName.MatchString(fileName) {
		return "", fmt.Errorf("invalid secret file name %q: only letters, digits, '.', '_', '-' are allowed", fileName)
	}
	return fileName, nil
}

func injectOneFileOpts(ctx context.Context, cli *client.Client, containerID, fileName, content string, uid, gid int, opts Options) error {
	fileName, err := checkFileName(fileName)
	if err != nil {
		return err
	}
	if uid < 0 || gid < 0 {
		return fmt.Errorf("invalid uid/gid %d:%d for %s", uid, gid, fileName)
	}
	dir, err := resolveDir(opts.Dir)
	if err != nil {
		return err
	}

	execID, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: false,
		AttachStderr: false,
		Cmd:          injectArgv(dir, fileName, uid, gid, opts.RequireTmpfs),
	})
	if err != nil {
		return fmt.Errorf("exec create failed for %s: %w", fileName, err)
	}

	// The secret is delivered on the exec's stdin stream and never appears in
	// /proc/<pid>/cmdline or `docker inspect` output.
	resp, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{Detach: false})
	if err != nil {
		return fmt.Errorf("exec attach failed for %s: %w", fileName, err)
	}
	defer resp.Close()

	if _, err := fmt.Fprint(resp.Conn, stdinPayload(content)); err != nil {
		return fmt.Errorf("failed to write secret to stdin for %s: %w", fileName, err)
	}
	_ = resp.CloseWrite()

	// Poll for exec exit. Only a completed, zero-exit command counts.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := cli.ContainerExecInspect(ctx, execID.ID)
		if err != nil {
			return fmt.Errorf("exec inspect failed for %s: %w", fileName, err)
		}
		if !info.Running {
			if info.ExitCode != 0 {
				return fmt.Errorf("secret write command exited %d for %s (%s)", info.ExitCode, fileName, exitReason(info.ExitCode))
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("secret write cancelled for %s: %w", fileName, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}

	return fmt.Errorf("exec timed out writing %s", fileName)
}

// resolveDir validates the delivery directory (default secretDir).
func resolveDir(dir string) (string, error) {
	if dir == "" {
		return secretDir, nil
	}
	if !path.IsAbs(dir) || path.Clean(dir) != dir || dir == "/" || strings.ContainsRune(dir, 0) {
		return "", fmt.Errorf("invalid secret directory %q: must be an absolute, clean, non-root path", dir)
	}
	return dir, nil
}

func exitReason(code int) string {
	switch code {
	case 64:
		return "invalid file name"
	case 65:
		return "secret directory is a symlink or not a directory"
	case 66:
		return "secret directory is not a tmpfs mount"
	case 67:
		return "destination is a directory"
	case 68:
		return "incomplete input"
	case 69:
		return "integrity check failed"
	default:
		return "see container shell/tool availability (sh, mktemp, base64, sha256sum)"
	}
}

// stdinPayload is what the in-container script reads: the base64 secret on
// line 1 and the hex SHA-256 of the plaintext on line 2. The script refuses to
// publish unless the decoded bytes match, so a truncated stream can never
// produce a published partial secret.
func stdinPayload(content string) string {
	sum := sha256.Sum256([]byte(content))
	return base64.StdEncoding.EncodeToString([]byte(content)) + "\n" + hex.EncodeToString(sum[:]) + "\n"
}

// injectScript is a fixed POSIX sh program. Nothing untrusted is interpolated
// into it: directory, name, uid, gid and the tmpfs flag arrive as positional
// parameters, so there is no shell-injection surface and the same script can be
// executed locally in tests.
//
// Properties: files are created via mktemp (O_EXCL, random name, mode 0600 under
// umask 077) so no plaintext is ever readable with a permissive mode; ownership
// and 0400 are applied BEFORE publication; publication is a single rename; a trap
// removes the temp file on any failure; symlinked/ directory destinations are
// refused.
const injectScript = `set -eu
umask 077
dir=$1; name=$2; uid=$3; gid=$4; req=$5
case "$name" in ''|.|..|*[!A-Za-z0-9._-]*) echo "invalid name" >&2; exit 64;; esac
if [ -L "$dir" ]; then echo "secret directory is a symlink" >&2; exit 65; fi
mkdir -p "$dir"
if [ ! -d "$dir" ] || [ -L "$dir" ]; then echo "secret directory invalid" >&2; exit 65; fi
if [ "$req" = 1 ]; then
  fs=$(awk -v d="$dir" 'BEGIN{b=-1} { m=$2; if ((d==m || index(d, m "/")==1 || m=="/") && length(m)>b) { b=length(m); t=$3 } } END{print t}' /proc/mounts)
  if [ "$fs" != tmpfs ]; then echo "secret directory is not a tmpfs mount" >&2; exit 66; fi
fi
dest=$dir/$name
if [ -d "$dest" ]; then echo "destination is a directory" >&2; exit 67; fi
tmp=$(mktemp "$dir/.inject.XXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM
IFS= read -r b64 || exit 68
IFS= read -r want || exit 68
printf '%s' "$b64" | base64 -d > "$tmp"
have=$(sha256sum < "$tmp" | cut -d' ' -f1)
if [ "$have" != "$want" ]; then echo "integrity check failed" >&2; exit 69; fi
if [ "$uid" != 0 ] || [ "$gid" != 0 ]; then chown "$uid:$gid" "$tmp"; fi
chmod 0400 "$tmp"
mv -f "$tmp" "$dest"
trap - EXIT HUP INT TERM
`

func injectArgv(dir, name string, uid, gid int, requireTmpfs bool) []string {
	req := "0"
	if requireTmpfs {
		req = "1"
	}
	return []string{"/bin/sh", "-c", injectScript, "dso-inject", dir, name, fmt.Sprint(uid), fmt.Sprint(gid), req}
}
