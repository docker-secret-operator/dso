//go:build linux

package ledger

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// errRaceLost is internal: the no-replace rename found the destination present.
var errRaceLost = errors.New("destination exists")

func renameNoReplaceAt(dirfd int, oldName, newName string) error {
	return unix.Renameat2(dirfd, oldName, dirfd, newName, unix.RENAME_NOREPLACE)
}

// openDir opens the key directory without following a final symlink and checks
// the open descriptor. A missing directory returns ErrKeyAbsent (nothing can
// predate it). With create, a missing directory is created 0700 and re-checked.
func (p *FileKeyProvider) openDir(create bool) (d *os.File, created bool, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		fd, oerr := unix.Open(p.dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		switch {
		case oerr == nil:
			f := os.NewFile(uintptr(fd), p.dir) // #nosec G115 -- fd is a small non-negative OS value
			if cerr := p.checkDir(f, created); cerr != nil {
				_ = f.Close()
				return nil, false, cerr
			}
			return f, created, nil
		case errors.Is(oerr, unix.ENOENT):
			if !create || attempt > 0 {
				return nil, false, keyErr(ErrKeyAbsent, "key directory does not exist", nil)
			}
			if merr := os.MkdirAll(p.dir, keyDirMode); merr != nil {
				return nil, false, keyErr(ErrKeyCreate, "cannot create the key directory", merr)
			}
			created = true
		case errors.Is(oerr, unix.ELOOP), errors.Is(oerr, unix.ENOTDIR):
			return nil, false, keyErr(ErrKeyUnsafe, "key directory is a symbolic link or not a directory", oerr)
		default:
			return nil, false, keyErr(ErrKeyUnsafe, "key directory cannot be opened", oerr)
		}
	}
	return nil, false, keyErr(ErrKeyCreate, "cannot create the key directory", nil)
}

func (p *FileKeyProvider) trustedOwner(fi os.FileInfo) bool {
	uid, ok := p.deps.ownerUID(fi)
	return ok && (uid == 0 || int(uid) == p.deps.euid())
}

func (p *FileKeyProvider) checkDir(f *os.File, created bool) error {
	if created {
		// We just made it: make the mode exact whatever the umask was.
		if err := f.Chmod(keyDirMode); err != nil {
			return keyErr(ErrKeyUnsafe, "cannot set the key directory mode", err)
		}
	}
	fi, err := f.Stat()
	if err != nil || !fi.IsDir() {
		return keyErr(ErrKeyUnsafe, "key directory cannot be examined", err)
	}
	if err := securefs.CheckDirChain(p.dir); err != nil {
		return keyErr(ErrKeyUnsafe, "key directory chain is unsafe", err)
	}
	if !p.trustedOwner(fi) {
		return keyErr(ErrKeyUnsafe, "key directory is owned by neither root nor the effective user", nil)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return keyErr(ErrKeyUnsafe, fmt.Sprintf("key directory is writable by group or others (mode %04o)", fi.Mode().Perm()), nil)
	}
	return nil
}

// readSafeFile opens name inside d (O_NOFOLLOW, non-blocking so a FIFO cannot
// hang the caller) and applies every descriptor-based check before one bounded
// read. A missing file returns (nil, os.ErrNotExist).
func (p *FileKeyProvider) readSafeFile(d *os.File, name string, wantSize int) ([]byte, error) {
	fd, err := unix.Openat(int(d.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0) // #nosec G115 -- small OS-provided descriptor
	if err != nil {
		switch {
		case errors.Is(err, unix.ENOENT):
			return nil, os.ErrNotExist
		case errors.Is(err, unix.ELOOP):
			return nil, keyErr(ErrKeyUnsafe, name+" is a symbolic link", err)
		default:
			return nil, keyErr(ErrKeyUnsafe, name+" cannot be opened", err)
		}
	}
	f := os.NewFile(uintptr(fd), name) // #nosec G115 -- fd is a small non-negative OS value
	defer func() { _ = f.Close() }()   // read-only descriptor
	fi, err := f.Stat()
	if err != nil {
		return nil, keyErr(ErrKeyUnsafe, name+" cannot be examined", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, keyErr(ErrKeyUnsafe, name+" is not a regular file", nil)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink != 1 {
		return nil, keyErr(ErrKeyUnsafe, name+" must have exactly one hard link", nil)
	}
	if !p.trustedOwner(fi) {
		return nil, keyErr(ErrKeyUnsafe, name+" is owned by neither root nor the effective user", nil)
	}
	if perm := fi.Mode().Perm(); perm&^0o600 != 0 || perm&0o400 == 0 {
		return nil, keyErr(ErrKeyUnsafe, fmt.Sprintf("%s has mode %04o; only 0400 or 0600 is accepted", name, perm), nil)
	}
	if fi.Size() != int64(wantSize) {
		return nil, keyErr(ErrKeyCorrupt, fmt.Sprintf("%s has %d bytes, want %d", name, fi.Size(), wantSize), nil)
	}
	buf := make([]byte, wantSize+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, keyErr(ErrKeyUnsafe, name+" cannot be read", err)
	}
	if n != wantSize {
		return nil, keyErr(ErrKeyCorrupt, name+" changed size while being read", nil)
	}
	return buf[:n], nil
}

// priorKeyEvidence reports why a key must have existed: a ledger file (or a
// preserved one), a key-id marker or a preserved key. A bare ledger.lock or a
// temporary file is not evidence.
func priorKeyEvidence(d *os.File) (string, error) {
	if _, err := d.Seek(0, io.SeekStart); err != nil {
		return "", keyErr(ErrKeyUnsafe, "key directory cannot be listed", err)
	}
	ents, err := d.ReadDir(-1)
	if err != nil {
		return "", keyErr(ErrKeyUnsafe, "key directory cannot be listed", err)
	}
	for _, e := range ents {
		n := e.Name()
		switch {
		case n == ledgerFileName:
			return "a ledger file exists", nil
		case strings.HasPrefix(n, ledgerFileName+".") && !strings.HasPrefix(n, ledgerFileName+".tmp-"):
			return "a preserved ledger file exists", nil
		case n == keyMarkerName:
			return "a key-id marker exists", nil
		case strings.HasPrefix(n, keyLossPrefix):
			return "a preserved key exists", nil
		}
	}
	return "", nil
}

// LoadKey verifies and reads the key. Errors are *KeyError matching ErrKeyAbsent
// (verified first run), ErrKeyLost, ErrKeyUnsafe or ErrKeyCorrupt. It never
// writes.
func (p *FileKeyProvider) LoadKey() (HMACKey, error) {
	d, _, err := p.openDir(false)
	if err != nil {
		return HMACKey{}, err
	}
	defer func() { _ = d.Close() }() // read-only directory descriptor
	return p.loadFrom(d)
}

func (p *FileKeyProvider) loadFrom(d *os.File) (HMACKey, error) {
	raw, err := p.readSafeFile(d, keyFileName, keyFileSize)
	if errors.Is(err, os.ErrNotExist) {
		why, eerr := priorKeyEvidence(d)
		if eerr != nil {
			return HMACKey{}, eerr
		}
		if why != "" {
			// A concurrent initialiser may have installed the key between the
			// read above and the listing; look once more before declaring loss.
			raw, err = p.readSafeFile(d, keyFileName, keyFileSize)
			if errors.Is(err, os.ErrNotExist) {
				return HMACKey{}, keyErr(ErrKeyLost, "the key file is missing but "+why+"; it is never regenerated", nil)
			}
		} else {
			return HMACKey{}, keyErr(ErrKeyAbsent, "no key, ledger or key-id marker exists", nil)
		}
	}
	if err != nil {
		return HMACKey{}, err
	}
	id, key, kerr := decodeKeyFile(raw)
	for i := range raw {
		raw[i] = 0
	}
	if kerr != nil {
		return HMACKey{}, kerr
	}
	if merr := p.checkMarker(d, id); merr != nil {
		for i := range key {
			key[i] = 0
		}
		return HMACKey{}, merr
	}
	return HMACKey{id: id, key: key}, nil
}

// checkMarker verifies the key-id marker, when present, against the key id.
func (p *FileKeyProvider) checkMarker(d *os.File, id string) error {
	raw, err := p.readSafeFile(d, keyMarkerName, keyMarkerSize)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		var ke *KeyError
		if errors.As(err, &ke) {
			return &KeyError{Kind: ke.Kind, KeyID: id, Reason: "key-id marker: " + ke.Reason, Err: ke.Err}
		}
		return err
	}
	if string(raw) != id+"\n" {
		return &KeyError{Kind: ErrKeyCorrupt, KeyID: id, Reason: "the key-id marker names a different key"}
	}
	return nil
}

// create implements Generate (idempotent=false) and EnsureKey (true).
func (p *FileKeyProvider) create(idempotent bool) (string, error) {
	d, _, err := p.openDir(false)
	switch {
	case err == nil:
		defer func() { _ = d.Close() }()
		k, lerr := p.loadFrom(d)
		if lerr == nil {
			defer k.Zero()
			if !idempotent {
				return "", &KeyError{Kind: ErrKeyExists, KeyID: k.ID(), Reason: "Generate never replaces a key"}
			}
			return k.ID(), p.ensureMarker(d, k.ID())
		}
		if !isKind(lerr, ErrKeyAbsent) {
			return "", lerr // lost, unsafe or corrupt: never generate over it
		}
	case isKind(err, ErrKeyAbsent):
		// no directory: nothing can predate it
		d, _, err = p.openDir(true)
		if err != nil {
			return "", err
		}
		defer func() { _ = d.Close() }()
	default:
		return "", err
	}
	return p.install(d)
}

func (p *FileKeyProvider) install(d *os.File) (string, error) {
	key := make([]byte, HMACKeySize)
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	if _, err := io.ReadFull(p.deps.random, key); err != nil {
		return "", keyErr(ErrKeyCreate, "the CSPRNG failed; no key was written", err)
	}
	id := DeriveKeyID(key)
	err := p.installNoReplace(d, keyFileName, encodeKeyFile(id, key))
	if errors.Is(err, errRaceLost) {
		// Another initialiser won: re-read, verify and adopt its key id.
		k, lerr := p.loadFrom(d)
		if lerr != nil {
			return "", lerr
		}
		defer k.Zero()
		return k.ID(), p.ensureMarker(d, k.ID())
	}
	if err != nil {
		return "", err
	}
	// Confirm what is on disk before reporting success.
	k, lerr := p.loadFrom(d)
	if lerr != nil {
		return "", lerr
	}
	defer k.Zero()
	if k.ID() != id {
		return "", &KeyError{Kind: ErrKeyCorrupt, KeyID: id, Reason: "the installed key differs from the generated one"}
	}
	return id, p.ensureMarker(d, id)
}

// ensureMarker records the key id as durable evidence that a key existed. It is
// written after the key so a crash cannot leave a marker without a key.
func (p *FileKeyProvider) ensureMarker(d *os.File, id string) error {
	err := p.installNoReplace(d, keyMarkerName, []byte(id+"\n"))
	if errors.Is(err, errRaceLost) {
		return p.checkMarker(d, id)
	}
	return err
}

// installNoReplace writes content to a unique O_EXCL temporary file, fsyncs it,
// renames it onto name with RENAME_NOREPLACE and fsyncs the directory. It
// returns errRaceLost when name exists. A failure before the rename leaves no
// temporary file. If the directory fsync fails the installed file is kept (it is
// valid) and the error is returned.
func (p *FileKeyProvider) installNoReplace(d *os.File, name string, content []byte) error {
	var rb [8]byte
	if _, err := io.ReadFull(p.deps.random, rb[:]); err != nil {
		return keyErr(ErrKeyCreate, "the CSPRNG failed; no file was written", err)
	}
	tmp := keyTmpPrefix + hex.EncodeToString(rb[:])
	dirfd := int(d.Fd()) // #nosec G115 -- small OS-provided descriptor
	fd, err := unix.Openat(dirfd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, keyFileMode)
	if err != nil {
		return keyErr(ErrKeyCreate, "cannot create the temporary file", err)
	}
	f := os.NewFile(uintptr(fd), tmp) // #nosec G115 -- fd is a small non-negative OS value
	cleanup := func() { _ = unix.Unlinkat(dirfd, tmp, 0) }
	if err := f.Chmod(keyFileMode); err != nil {
		_ = f.Close()
		cleanup()
		return keyErr(ErrKeyCreate, "cannot set the temporary file mode", err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		cleanup()
		return keyErr(ErrKeyCreate, "cannot write the temporary file", err)
	}
	if err := p.deps.fsyncFile(f); err != nil {
		_ = f.Close()
		cleanup()
		return keyErr(ErrKeyCreate, "cannot fsync the temporary file", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return keyErr(ErrKeyCreate, "cannot close the temporary file", err)
	}
	for try := 0; ; try++ {
		err = p.deps.renameNoReplace(dirfd, tmp, name)
		if !errors.Is(err, unix.EINTR) || try >= 3 {
			break
		}
		var st unix.Stat_t
		if unix.Fstatat(dirfd, tmp, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
			err = nil // the temporary name is gone: the rename completed
			break
		}
	}
	switch {
	case err == nil:
	case errors.Is(err, unix.EEXIST):
		cleanup()
		return errRaceLost
	default:
		cleanup()
		return keyErr(ErrKeyCreate, "cannot install "+name+" without replacing", err)
	}
	if err := p.deps.fsyncDir(d); err != nil {
		return keyErr(ErrKeyCreate, "directory fsync failed after installing "+name+"; the file was kept", err)
	}
	return nil
}
