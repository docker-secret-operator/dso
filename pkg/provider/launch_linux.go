//go:build linux

package provider

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// maxPluginBytes bounds the in-memory (memfd) copy; real plugins are 10-40 MB.
const maxPluginBytes = 512 << 20

// prepareVerifiedLaunch closes the verify-then-exec TOCTOU window (G012).
//
// Hashing a path and later exec'ing the path are two independent path
// resolutions; anything that can replace the file or swap a directory entry in
// between gets unverified code executed. Here the binary is opened once
// (O_NOFOLLOW, fstat-checked), its bytes are streamed through SHA-256 into a
// memfd, the digest is compared to the manifest, the memfd is sealed
// (write/grow/shrink/seal) and re-opened read-only, and that descriptor is
// what the child executes (/proc/self/fd/3 via ExtraFiles). The executed
// bytes are by construction the hashed bytes; the on-disk path is not
// consulted again.
func prepareVerifiedLaunch(manifestPath, pluginPath string) (*launchTarget, error) {
	pluginName := filepath.Base(pluginPath)

	if err := checkDirChainTrust(filepath.Dir(manifestPath)); err != nil {
		return nil, fmt.Errorf("manifest location untrusted: %w", err)
	}
	mf, err := openNoFollow(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open hash manifest %s: %w", manifestPath, err)
	}
	if _, err := checkFileTrust(mf, "hash manifest "+manifestPath, false); err != nil {
		_ = mf.Close()
		return nil, err
	}
	expected, err := manifestHashFor(mf, manifestPath, pluginName)
	_ = mf.Close()
	if err != nil {
		return nil, err
	}

	if err := checkDirChainTrust(filepath.Dir(pluginPath)); err != nil {
		return nil, fmt.Errorf("plugin location untrusted: %w", err)
	}
	bin, err := openNoFollow(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open plugin binary: %w", err)
	}
	defer func() { _ = bin.Close() }()
	fi, err := checkFileTrust(bin, "plugin binary "+pluginPath, true)
	if err != nil {
		return nil, err
	}
	if fi.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("plugin must be executable")
	}

	mfd, err := unix.MemfdCreate("dso-plugin-"+pluginName, unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, fmt.Errorf("memfd_create unavailable, cannot launch verified plugin: %w", err)
	}
	wf := os.NewFile(uintptr(mfd), "memfd:dso-plugin") // #nosec G115 -- mfd is a small non-negative descriptor returned by memfd_create
	defer func() { _ = wf.Close() }()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(wf, h), io.LimitReader(bin, maxPluginBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin binary: %w", err)
	}
	if n > maxPluginBytes {
		return nil, fmt.Errorf("plugin binary exceeds %d bytes", maxPluginBytes)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return nil, fmt.Errorf("plugin %s hash mismatch: manifest=%s actual=%s", pluginName, expected, actual)
	}

	const seals = unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	if _, err := unix.FcntlInt(wf.Fd(), unix.F_ADD_SEALS, seals); err != nil {
		return nil, fmt.Errorf("cannot seal plugin memfd: %w", err)
	}
	// A writable descriptor on the inode makes execve fail with ETXTBSY, so the
	// child gets a fresh read-only open of the same (sealed) memfd.
	// #nosec G304 -- /proc/self/fd of our own memfd
	ro, err := os.Open(fmt.Sprintf("/proc/self/fd/%d", wf.Fd()))
	if err != nil {
		return nil, fmt.Errorf("cannot reopen sealed plugin memfd (is /proc mounted?): %w", err)
	}

	if afterVerifyHook != nil {
		afterVerifyHook()
	}
	return &launchTarget{
		path:       "/proc/self/fd/3", // ExtraFiles[0] is fd 3 in the child
		extraFiles: []*os.File{ro},
		cleanup:    func() { _ = ro.Close() },
	}, nil
}
