package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CentralChecks covers DSO-DOCTOR-026 through DSO-DOCTOR-031: file-system
// custody of the local encrypted central store's root key and database. See
// internal/crypto/keyprovider.go (InstallKeyFile / InstallPassphraseKeyFile,
// via installExclusive) and internal/store/sqlite/sqlite.go (Open) for the
// permission contract these checks hold the live files to: a root key or
// passphrase KDF-parameter file is installed 0400 in a 0700 directory; the
// database file is created 0600 in a 0700 directory.
//
// The central store has no persisted configuration of its own -- every `dso
// central` invocation takes --db/--key-file/--key-provider explicitly -- so
// these checks are opt-in, driven by DoctorOptions.CentralDB/CentralKeyFile/
// CentralKeyProvider (wired from `dso doctor`'s own --central-* flags). When
// neither path is configured, run returns a single informational check
// rather than failing or silently doing nothing.
//
// Like StateDirChecks, these checks never read file contents -- only
// os.Lstat-based mode/type checks -- and never follow a symlink.
const (
	centralKeyFileMode = os.FileMode(0o400)
	centralKeyDirMode  = os.FileMode(0o700)
	centralDBFileMode  = os.FileMode(0o600)
	centralDBDirMode   = os.FileMode(0o700)
)

type CentralChecks struct {
	db          string
	keyFile     string
	keyProvider string // "", "file", or "passphrase" -- see internal/cli/central.go's --key-provider
	lstat       func(string) (os.FileInfo, error)
}

func newCentralChecks(opts DoctorOptions) *CentralChecks {
	return &CentralChecks{
		db:          opts.CentralDB,
		keyFile:     opts.CentralKeyFile,
		keyProvider: opts.CentralKeyProvider,
		lstat:       os.Lstat,
	}
}

func (c *CentralChecks) run(_ context.Context) []DoctorCheck {
	if c.db == "" && c.keyFile == "" {
		return []DoctorCheck{infoCheck(
			"DSO-DOCTOR-026", "Central store custody",
			"Whether the local encrypted central store's root key and database have safe file permissions",
			"skipped: neither --central-db nor --central-key-file was given to `dso doctor` -- "+
				"the central store has no persisted configuration of its own, so this check is opt-in",
			DoctorCatSecurity,
		)}
	}

	var checks []DoctorCheck
	if c.keyFile != "" {
		checks = append(checks, c.checkKeyFile(), c.checkKeyDir())
	}
	if c.db != "" {
		checks = append(checks, c.checkDBFile(), c.checkDBDir())
	}
	if c.db != "" && c.keyFile != "" {
		checks = append(checks, c.checkColocation())
	}
	return checks
}

// checkKeyFile is DSO-DOCTOR-027. In passphrase mode --central-key-file names
// the KDF-parameter file rather than a raw key, but it is installed under the
// identical 0400/0700 contract (InstallPassphraseKeyFile also calls
// installExclusive), so the same mode check applies -- only the displayed
// name/description changes, never the check itself.
func (c *CentralChecks) checkKeyFile() DoctorCheck {
	const id = "DSO-DOCTOR-027"
	name := "Root key file custody"
	desc := "The root key file must be 0400, readable only by its owner"
	if c.keyProvider == "passphrase" {
		name = "KDF parameter file custody"
		desc = "The passphrase KDF-parameter file must be 0400, readable only by its owner " +
			"(it holds no secret itself, but a tampered copy can downgrade the KDF or desynchronise the key id)"
	}
	return c.checkFileMode(id, name, desc, c.keyFile, centralKeyFileMode)
}

// checkKeyDir is DSO-DOCTOR-028.
func (c *CentralChecks) checkKeyDir() DoctorCheck {
	const id = "DSO-DOCTOR-028"
	const name = "Root key directory custody"
	const desc = "The directory holding the root key (or KDF-parameter file) must be 0700, accessible only to its owner"
	return c.checkDirMode(id, name, desc, filepath.Dir(c.keyFile), centralKeyDirMode)
}

// checkDBFile is DSO-DOCTOR-029.
func (c *CentralChecks) checkDBFile() DoctorCheck {
	const id = "DSO-DOCTOR-029"
	const name = "Central database file custody"
	const desc = "The central store database file must be 0600, accessible only to its owner"
	return c.checkFileMode(id, name, desc, c.db, centralDBFileMode)
}

// checkDBDir is DSO-DOCTOR-030.
func (c *CentralChecks) checkDBDir() DoctorCheck {
	const id = "DSO-DOCTOR-030"
	const name = "Central database directory custody"
	const desc = "The directory holding the central store database must be 0700, accessible only to its owner"
	return c.checkDirMode(id, name, desc, filepath.Dir(c.db), centralDBDirMode)
}

// checkColocation is DSO-DOCTOR-031, reusing the same SameDirectory helper
// `dso central init` uses to refuse initialising a colocated key and
// database. A doctor run only warns here (never fails): an operator may
// already be aware and accepted it deliberately (`--allow-colocated-key`),
// and the risk is a documented backup/snapshot concern, not an active
// vulnerability.
func (c *CentralChecks) checkColocation() DoctorCheck {
	const id = "DSO-DOCTOR-031"
	const name = "Root key / database separation"
	const desc = "The root key and the database it protects should not sit in the same directory, " +
		"so a single backup or snapshot of one cannot also carry the other"

	dbDir, keyDir := filepath.Dir(c.db), filepath.Dir(c.keyFile)
	same, err := SameDirectory(dbDir, keyDir)
	if err != nil {
		return warnCheck(id, name, desc, "could not compare directories: "+err.Error(),
			"the key and database directories could not be resolved for comparison", DoctorCatSecurity,
			"Verify both --central-db and --central-key-file point at reachable paths")
	}
	if !same {
		return passCheck(id, name, desc, "the key and database are in different directories", DoctorCatSecurity)
	}
	return warnCheck(id, name, desc,
		fmt.Sprintf("both the key and the database live in %s", dbDir),
		"a backup or snapshot of this one directory would carry both the database and the key that decrypts it",
		DoctorCatSecurity,
		"Move the root key file to a separate directory, ideally separate storage -- "+
			"`dso central init --allow-colocated-key` is accepted only for throwaway testing, never production",
	)
}

// checkFileMode applies the shared file-custody rule: must exist, must not be
// a symlink (mirroring doctor_state.go's explicit symlink rejection), must be
// a regular file, and must be exactly `want` mode. Content is never read.
func (c *CentralChecks) checkFileMode(id, name, desc, path string, want os.FileMode) DoctorCheck {
	fi, err := c.lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return failCheck(id, name, desc, path+" does not exist",
				"doctor was told to check this path by --central-db/--central-key-file but nothing is there",
				DoctorHigh, DoctorCatSecurity,
				"Verify the path is correct, or run `dso central init` to create it")
		}
		return warnCheck(id, name, desc, "stat failed: "+err.Error(), path+" cannot be inspected",
			DoctorCatSecurity, "Check filesystem health and parent directory permissions")
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return failCheck(id, name, desc, path+" is a symbolic link",
			"a symlinked key or database file is never followed by this check, and is refused by securefs when `dso central` opens it",
			DoctorHigh, DoctorCatSecurity,
			"Replace the symlink with a real file")
	}
	if !fi.Mode().IsRegular() {
		return failCheck(id, name, desc, path+" is not a regular file", "a non-regular file exists at this path",
			DoctorHigh, DoctorCatSecurity, "Remove the conflicting path and restore the correct file")
	}
	perm := fi.Mode().Perm()
	if perm != want {
		return failCheck(id, name, desc, fmt.Sprintf("%s: mode %04o, expected %04o", path, perm, want),
			"the file was created by hand, by an older build, or has been modified since installation",
			DoctorHigh, DoctorCatSecurity,
			fmt.Sprintf("By hand: `chmod %04o %s`", want, path))
	}
	return passCheck(id, name, desc, fmt.Sprintf("%s: mode %04o", path, perm), DoctorCatSecurity)
}

// checkDirMode is checkFileMode's directory counterpart.
func (c *CentralChecks) checkDirMode(id, name, desc, dir string, want os.FileMode) DoctorCheck {
	fi, err := c.lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return failCheck(id, name, desc, dir+" does not exist",
				"the parent directory of a configured path is missing",
				DoctorHigh, DoctorCatSecurity,
				"Verify the path is correct, or run `dso central init` to create it")
		}
		return warnCheck(id, name, desc, "stat failed: "+err.Error(), dir+" cannot be inspected",
			DoctorCatSecurity, "Check filesystem health and parent directory permissions")
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return failCheck(id, name, desc, dir+" is a symbolic link",
			"a symlinked directory is never followed by this check, and is refused by securefs when `dso central` opens it",
			DoctorHigh, DoctorCatSecurity,
			"Replace the symlink with a real directory")
	}
	if !fi.IsDir() {
		return failCheck(id, name, desc, dir+" is not a directory", "a file exists at this directory path",
			DoctorHigh, DoctorCatSecurity, "Remove the conflicting file and recreate the directory")
	}
	perm := fi.Mode().Perm()
	if perm != want {
		return failCheck(id, name, desc, fmt.Sprintf("%s: mode %04o, expected %04o", dir, perm, want),
			"the directory was created by hand, by an older build, or has been modified since installation",
			DoctorHigh, DoctorCatSecurity,
			fmt.Sprintf("By hand: `chmod %04o %s`", want, dir))
	}
	return passCheck(id, name, desc, fmt.Sprintf("%s: mode %04o", dir, perm), DoctorCatSecurity)
}

// SameDirectory reports whether a and b resolve to the same directory. It
// mirrors `dso central init`'s own co-location check (formerly the unexported
// sameDir in internal/cli/central.go): paths are compared after resolving
// symlinks, falling back to absolute-path comparison when either side cannot
// be resolved that way (notably when a directory doesn't exist yet -- the
// normal case when `init` runs this before creating anything). An error is
// returned only when even that fallback fails for both sides (e.g. the
// current working directory cannot be determined).
func SameDirectory(a, b string) (bool, error) {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		var absErrA, absErrB error
		ra, absErrA = filepath.Abs(a)
		rb, absErrB = filepath.Abs(b)
		if absErrA != nil && absErrB != nil {
			return false, fmt.Errorf("cannot resolve directories for comparison: %v / %v", absErrA, absErrB)
		}
	}
	return ra == rb, nil
}
