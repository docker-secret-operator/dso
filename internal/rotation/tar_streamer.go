package rotation

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// FileSecretSpec describes one secret that must be delivered into a
// container's filesystem via StreamSecretToContainer rather than merged into
// Config.Env. Path is the target directory inside the container
// (SecretMapping.Inject.Path); Data maps provider field name -> secret value,
// mirroring the shape StreamSecretToContainer already expects.
type FileSecretSpec struct {
	Path string
	Data map[string]string
	UID  int
	GID  int
	// Version is the keyed identity (hmac:<key id>:<mac>) of Data, computed by the
	// caller from the exact values it hands over; "" when no ledger key is available.
	// Journalled by managed delivery after verification; never plaintext.
	Version string
}

// StreamSecretToContainer uploads secrets into a container's filesystem using an
// in-memory tar stream. Entries are created with mode 0400 and the requested
// owner in the tar header, so the daemon creates them with those attributes (there
// is no permissive-mode window and nothing touches the host filesystem).
//
// Guarantees and limits (distinct from injector.InjectFiles):
//   - Intended for a container that has NOT started serving yet (rolling
//     replacement before handoff). The daemon extracts entries in place; it does not
//     publish via rename, so a concurrent reader in a RUNNING container could observe
//     a missing or partial file. Use injector.InjectFiles for live replacement.
//   - Names are validated: a name that reduces to "", ".", ".." or contains a path
//     separator or NUL is rejected, as is a non-absolute/unclean target path or "/"
//     (AllowOverwriteDirWithFile would otherwise let such an entry replace a
//     directory with a file).
//   - Symlinks that already exist INSIDE the container's filesystem are resolved by
//     the daemon within the container's root; DSO cannot prevent a container-controlled
//     link from redirecting a write inside that container.
func StreamSecretToContainer(ctx context.Context, cli *client.Client, containerID string, targetPath string, data map[string]string, uid, gid int) error {
	buf, err := buildSecretTar(targetPath, data, uid, gid)
	if err != nil {
		return err
	}
	// Header names are absolute paths inside the container, so copy at the root.
	if err := cli.CopyToContainer(ctx, containerID, "/", bytes.NewReader(buf), container.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	}); err != nil {
		return fmt.Errorf("failed to copy tar to container: %w", err)
	}
	return nil
}

// buildSecretTar validates inputs and builds the archive (separated for tests).
func buildSecretTar(targetPath string, data map[string]string, uid, gid int) ([]byte, error) {
	if uid < 0 || gid < 0 {
		return nil, fmt.Errorf("invalid uid/gid %d:%d", uid, gid)
	}
	if !path.IsAbs(targetPath) || path.Clean(targetPath) != targetPath || targetPath == "/" || strings.ContainsRune(targetPath, 0) {
		return nil, fmt.Errorf("invalid secret target path %q: must be an absolute, clean, non-root directory", targetPath)
	}
	var names []string
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]string{}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range names {
		content := data[name]
		safe := path.Base(strings.ReplaceAll(name, "\\", "/"))
		if safe == "" || safe == "." || safe == ".." || safe == "/" || strings.ContainsRune(name, 0) {
			return nil, fmt.Errorf("invalid secret file name %q", name)
		}
		if prev, dup := seen[safe]; dup {
			return nil, fmt.Errorf("secret names %q and %q collide on %q", prev, name, safe)
		}
		seen[safe] = name
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     path.Join(targetPath, safe),
			Mode:     0o400,
			Size:     int64(len(content)),
			Uid:      uid,
			Gid:      gid,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("failed to write tar header for %s: %w", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, fmt.Errorf("failed to write tar content for %s: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	return buf.Bytes(), nil
}

// InjectedSecretSync is a helper for syncing a single secret's data
func InjectedSecretSync(ctx context.Context, cli *client.Client, containerID string, targetPath string, data map[string]string, uid, gid int) error {
	return StreamSecretToContainer(ctx, cli, containerID, targetPath, data, uid, gid)
}
