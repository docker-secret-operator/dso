package ledger

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

const (
	// maxPreserveAttempts bounds name-collision retries (D-S2-4); exhaustion
	// fails closed.
	maxPreserveAttempts = 16
	// maxInterruptedRetries bounds retries of a rename interrupted by a signal
	// whose outcome was established as "not renamed".
	maxInterruptedRetries = 8
)

// preserve moves the existing ledger file to ledger.json.<kind>-<UTC>, with an
// atomic no-replace rename so an existing preserved file can never be
// overwritten (design §7, owner decision D-S2-4). On a name collision it adds a
// random four-hex suffix and retries, a bounded number of times. After EINTR the
// outcome is established by file identity before any retry; if it cannot be
// established the call fails without retrying. Any other failure, including an
// unsupported platform, kernel or filesystem, leaves the ledger and key
// untouched and is returned for the caller to fail closed.
func (s *Store) preserve(kind string) (string, error) {
	before, err := s.ops.lstat(s.path)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrPreserveFailed, err)
	}
	base := s.path + "." + kind + "-" + s.utcNow().Format("20060102T150405Z")
	interrupted := 0
	for attempt := 0; attempt < maxPreserveAttempts; {
		dst := base
		if attempt > 0 {
			var b [2]byte
			if _, err := io.ReadFull(s.random, b[:]); err != nil {
				return "", fmt.Errorf("%w: %w", ErrPreserveFailed, err)
			}
			dst = base + "-" + hex.EncodeToString(b[:])
		}
		err := s.ops.renameNoReplace(s.path, dst)
		switch {
		case err == nil:
			_ = s.ops.syncDir(s.dir) // best effort; the rename itself is atomic
			return dst, nil
		case errors.Is(err, fs.ErrExist):
			attempt++
		case errors.Is(err, syscall.EINTR):
			renamed, known := s.renameOutcome(before, dst)
			switch {
			case known && renamed:
				_ = s.ops.syncDir(s.dir)
				return dst, nil
			case known && interrupted < maxInterruptedRetries:
				interrupted++
			default:
				return "", fmt.Errorf("%w: %w", ErrPreserveAmbiguous, err)
			}
		default:
			return "", fmt.Errorf("%w: %w", ErrPreserveFailed, err)
		}
	}
	return "", ErrPreserveExhausted
}

// renameOutcome decides, by file identity, whether an interrupted rename of the
// file described by before to dst happened. known is false when neither
// "renamed" nor "not renamed" can be shown.
func (s *Store) renameOutcome(before os.FileInfo, dst string) (renamed, known bool) {
	si, serr := s.ops.lstat(s.path)
	di, derr := s.ops.lstat(dst)
	switch {
	case errors.Is(serr, fs.ErrNotExist) && derr == nil && os.SameFile(before, di):
		return true, true
	case serr == nil && os.SameFile(before, si) && errors.Is(derr, fs.ErrNotExist):
		return false, true
	}
	return false, false
}

var staleTempRE = regexp.MustCompile(`^ledger\.json\.tmp-[0-9a-f]{16}$`)

// cleanStaleTemps removes leftover temporary files of our own naming. It runs
// only while the process lock is held and after the directory-chain check
// (D-S2-5). A candidate must match the exact name, be a regular file by Lstat
// (links are never followed or removed) and be owned by the effective uid;
// production runs as root, so this equals "owned by root" of design §8.2.
// Preserved .corrupt-, .keyloss- and .pre-v files and every unrelated name are
// never touched.
func (s *Store) cleanStaleTemps() []string {
	entries, err := s.ops.readDir(s.dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, e := range entries {
		name := e.Name()
		if !staleTempRE.MatchString(name) {
			continue
		}
		p := filepath.Join(s.dir, name)
		fi, err := s.ops.lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if uid, ok := s.ops.ownerUID(fi); !ok || int(uid) != s.ops.euid() {
			continue
		}
		if s.ops.remove(p) == nil {
			removed = append(removed, name)
		}
	}
	return removed
}
