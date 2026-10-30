package cli

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/term"

	"github.com/docker-secret-operator/dso/internal/centralsvc"
	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// `dso central` is the M1 local administration surface of the central secrets
// store. Security assumptions (also in docs/central-store-m1.md):
//
//   - It opens the database file and root key directly, as the invoking OS
//     user. There is no network listener and no authentication or RBAC: whoever
//     can read the key file and database can read every secret. File
//     permissions are the only access control in M1.
//   - Secret values are never accepted as command-line arguments (they would
//     land in shell history and the process list). They come from a hidden
//     terminal prompt or from stdin.
//   - Plaintext is printed only by `secret get --reveal`.
type centralFlags struct {
	db             string
	provider       string
	keyFile        string
	passphraseFile string
}

func NewCentralCmd() *cobra.Command {
	f := &centralFlags{}
	cmd := &cobra.Command{
		Use:          "central",
		Short:        "Local administration of the central secrets store (experimental, local-only, no authentication)",
		SilenceUsage: true,
		Long: `Experimental M1 interface to the central secrets store.

This command operates directly on a local SQLite database and root key. It
exposes no network API and performs no authentication or authorization: access
control is the file permissions of the database and key file. Do not use it as
a production secrets workflow yet.`,
	}
	cmd.PersistentFlags().StringVar(&f.db, "db", "", "path to the SQLite database (required)")
	cmd.PersistentFlags().StringVar(&f.provider, "key-provider", "", "root key provider: file or passphrase (required)")
	cmd.PersistentFlags().StringVar(&f.keyFile, "key-file", "", "path to the root key file (required)")
	cmd.PersistentFlags().StringVar(&f.passphraseFile, "passphrase-file", "", "read the passphrase from this file (mode 0600) instead of prompting")

	cmd.AddCommand(centralInitCmd(f), centralProjectCmd(f), centralEnvCmd(f), centralSecretCmd(f),
		centralBackupCmd(f), centralRestoreCmd(f), centralAuditCmd(f))
	return cmd
}

func (f *centralFlags) requireCommon() error {
	if f.db == "" || f.keyFile == "" {
		return errors.New("--db and --key-file are required")
	}
	if f.provider != "file" && f.provider != "passphrase" {
		return errors.New("--key-provider must be 'file' or 'passphrase' (there is no default: choose deliberately, see docs/central-store-m1.md)")
	}
	return nil
}

// readPassphraseFile reads a passphrase from a private file. It applies the
// same rules as a key file (securefs): the directory chain must not be
// writable by others, the leaf must not be a symlink, and -- judged on the open
// descriptor, so a swap after the check cannot change what is read -- it must be
// a regular file owned by the current user with no group/other access.
func readPassphraseFile(path string) ([]byte, error) {
	b, err := securefs.ReadPrivateFile(path, 4096)
	if err != nil {
		return nil, fmt.Errorf("passphrase file: %w", err)
	}
	return bytes.TrimRight(b, "\r\n"), nil
}

func promptHidden(cmd *cobra.Command, prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd()) // #nosec G115 -- stdin fd is small.
	if !term.IsTerminal(fd) {
		return nil, errors.New("no terminal available for a hidden prompt; use --passphrase-file or stdin options")
	}
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(cmd.ErrOrStderr())
	return b, err
}

func (f *centralFlags) passphrase(cmd *cobra.Command, confirm bool) ([]byte, error) {
	if f.passphraseFile != "" {
		return readPassphraseFile(f.passphraseFile)
	}
	p, err := promptHidden(cmd, "Passphrase: ")
	if err != nil {
		return nil, err
	}
	if confirm {
		p2, err := promptHidden(cmd, "Repeat passphrase: ")
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(p, p2) {
			return nil, errors.New("passphrases do not match")
		}
	}
	return p, nil
}

func (f *centralFlags) keyProvider(cmd *cobra.Command) (crypto.KeyProvider, error) {
	switch f.provider {
	case "file":
		return crypto.NewFileKeyProvider(f.keyFile)
	default:
		p, err := f.passphrase(cmd, false)
		if err != nil {
			return nil, err
		}
		defer crypto.Zero(p)
		return crypto.NewPassphraseKeyProvider(f.keyFile, p)
	}
}

// open returns a ready Service and a closer. Missing or invalid key material
// fails here, before the database is touched for secret operations.
func (f *centralFlags) open(cmd *cobra.Command) (*centralsvc.Service, func(), error) {
	if err := f.requireCommon(); err != nil {
		return nil, nil, err
	}
	kp, err := f.keyProvider(cmd)
	if err != nil {
		return nil, nil, err
	}
	st, err := sqlite.Open(cmd.Context(), f.db, sqlite.Options{})
	if err != nil {
		return nil, nil, err
	}
	return centralsvc.New(st, kp, nil), func() { _ = st.Close() }, nil
}

func actorName() string {
	if u, err := user.Current(); err == nil {
		return "local-cli:" + u.Username
	}
	return "local-cli:unknown"
}

// centralInitAfterKey, when non-nil, runs after the root key is in place and
// before the database is created. Tests use it to simulate a failure at the
// one point that leaves key material without a database.
var centralInitAfterKey func() error

// centralAuditLoggerOverride, when non-nil, replaces the real zap.Logger
// centralAuditLogger would otherwise construct. Tests use it to capture
// init's audit-failure log output deterministically (see
// internal/cli/central_audit_logger_test.go), without constructing a real
// zap.NewProduction logger (which writes to the process's actual stderr)
// on every test run.
var centralAuditLoggerOverride *zap.Logger

// centralAuditLogger returns the logger `dso central init` uses to report
// audit-write failures (F1, approved): the same structured logging
// convention internal/centralsvc's appendAuditBestEffort already uses for
// every other operation, not a bespoke stderr format. A minimal
// zap.NewProduction logger is this CLI's own default (no shared
// `dso`-wide logger construction exists to reuse for a short-lived,
// one-shot command like `init`); falling back to zap.NewNop() if even that
// fails is a deliberate choice to never let logger construction itself
// block or fail the init command.
func centralAuditLogger() *zap.Logger {
	if centralAuditLoggerOverride != nil {
		return centralAuditLoggerOverride
	}
	l, err := zap.NewProduction()
	if err != nil {
		return zap.NewNop()
	}
	return l
}

func centralInitCmd(f *centralFlags) *cobra.Command {
	var allowColocated, useExistingKey bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the root key and an empty, key-bound database",
		Long: `Create the root key, an empty database, and bind the database to that key.

Everything that can be checked is checked BEFORE any key material is written
(directory safety, an existing database, an existing key), so the usual
failures leave nothing behind. If the key is installed but the database cannot
be created, the key is kept (it is never deleted or overwritten) and the
database files this run created are removed; fix the cause and re-run with
--use-existing-key to build the database around the key that is already there.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := f.requireCommon(); err != nil {
				return err
			}
			// ---- preflight: no key material is written until all of this passes ----
			dbDir, keyDir := filepath.Dir(f.db), filepath.Dir(f.keyFile)
			for _, d := range []string{dbDir, keyDir} {
				if err := securefs.EnsureDir(d); err != nil {
					return fmt.Errorf("refusing to initialise: %w", err)
				}
			}
			if sameDir(dbDir, keyDir) && !allowColocated {
				return errors.New("the key file is in the same directory as the database; a copy of that directory (backup, snapshot) would contain both. " +
					"Store the key in a different directory (ideally on separate storage -- DSO cannot verify that), or pass --allow-colocated-key to accept this for testing")
			}
			dbExisted := false
			if fi, err := os.Lstat(f.db); err == nil {
				if fi.Size() > 0 || !fi.Mode().IsRegular() {
					return fmt.Errorf("%s already exists; refusing to initialise over it", f.db)
				}
				dbExisted = true // empty leftover of an earlier failed init; reuse it
			}
			if stale := crypto.StaleTempKeyFiles(keyDir); len(stale) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: leftover temporary key file(s) from an interrupted installation: %s\n"+
					"They contain key material and are not removed automatically. Delete them yourself once you are sure no database depends on them.\n",
					strings.Join(stale, ", "))
			}
			_, keyErr := os.Lstat(f.keyFile)
			keyExists := keyErr == nil
			if keyExists && !useExistingKey {
				return fmt.Errorf("key file %s already exists and will not be overwritten.\n"+
					"  - If it was left behind by an `init` that failed after installing the key and NO database was created from it, "+
					"re-run with --use-existing-key to build the database around it.\n"+
					"  - If a database already uses it, do not re-initialise: pass the same --db to the other commands.\n"+
					"  - To start over with a new key, move this key file away yourself first", f.keyFile)
			}
			if !keyExists && useExistingKey {
				return fmt.Errorf("--use-existing-key was given but %s does not exist", f.keyFile)
			}

			// ---- key ----
			var kp crypto.KeyProvider
			var err error
			if keyExists {
				if kp, err = f.keyProvider(cmd); err != nil { // validates it loads, with the passphrase if applicable
					return fmt.Errorf("existing key cannot be used: %w", err)
				}
			} else if f.provider == "file" {
				if _, err = crypto.InstallKeyFile(f.keyFile); err != nil {
					return err
				}
				kp, err = crypto.NewFileKeyProvider(f.keyFile)
			} else {
				p, perr := f.passphrase(cmd, true)
				if perr != nil {
					return perr
				}
				defer crypto.Zero(p)
				if _, err = crypto.InstallPassphraseKeyFile(f.keyFile, p); err != nil {
					return err
				}
				kp, err = crypto.NewPassphraseKeyProvider(f.keyFile, p)
			}
			if err != nil {
				return keptKeyError(f.keyFile, err)
			}

			// ---- database + binding ----
			// recordInitEvent is best-effort (Gate 6, approved: log/report-only,
			// availability-first): a failure to persist this event never changes
			// the result init has already decided. It is only ever called once
			// a *sqlite.Store exists and opened successfully -- a failure before
			// that (preflight, key install, sqlite.Open itself) has no database to
			// write an event into and is therefore, by construction, never
			// recorded; docs/central-store-audit-log.md states this limitation
			// explicitly (F2). Any failure to persist the event itself -- or to
			// even generate its id -- is reported synchronously at error level
			// through the same structured (zap) logging convention the rest of
			// the audit subsystem uses (internal/centralsvc's
			// appendAuditBestEffort), not a bespoke stderr format: F1's
			// remediation found that an earlier version of this fix used plain
			// stderr text, which fixed the "silently discarded" defect but did
			// not satisfy the approved "existing structured logging
			// conventions" requirement. Never a secret value or key material
			// in the logged fields.
			initLogger := centralAuditLogger()
			recordInitEvent := func(st *sqlite.Store, outcome store.AuditOutcome, reason string) {
				if st == nil {
					return
				}
				var idBuf [16]byte
				if _, err := rand.Read(idBuf[:]); err != nil {
					initLogger.Error("init audit event id generation failed",
						zap.String("outcome", string(outcome)), zap.Error(err))
					return
				}
				if _, err := st.AppendAuditEvent(cmd.Context(), store.AuditEvent{
					ID: hex.EncodeToString(idBuf[:]), EventType: store.AuditInit, Outcome: outcome, ReasonCategory: reason,
				}); err != nil {
					initLogger.Error("init audit event persistence failed; command result is unaffected (availability-first policy); "+
						"log-only reporting is not durable or tamper-proof evidence on its own",
						zap.String("outcome", string(outcome)), zap.Error(err))
				}
			}
			fail := func(err error) error {
				if !dbExisted {
					for _, p := range []string{f.db, f.db + "-wal", f.db + "-shm"} {
						_ = os.Remove(p) // only files this run created
					}
				}
				return keptKeyError(f.keyFile, err)
			}
			if centralInitAfterKey != nil {
				if err := centralInitAfterKey(); err != nil {
					return fail(err)
				}
			}
			st, err := sqlite.Open(cmd.Context(), f.db, sqlite.Options{})
			if err != nil {
				return fail(err)
			}
			defer st.Close()
			if err := centralsvc.New(st, kp, nil).Bind(cmd.Context()); err != nil {
				recordInitEvent(st, store.AuditFailure, "db_error")
				_ = st.Close()
				return fail(err)
			}
			recordInitEvent(st, store.AuditSuccess, "")
			fmt.Fprintf(cmd.OutOrStdout(), "Initialised %s, bound to root key id %s (%s provider).\n", f.db, kp.ID(), f.provider)
			fmt.Fprintln(cmd.OutOrStdout(), "Back up the root key separately from the database. Without it the data is unrecoverable.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&allowColocated, "allow-colocated-key", false, "allow the key file in the same directory as the database (testing only)")
	cmd.Flags().BoolVar(&useExistingKey, "use-existing-key", false, "build the database around a key file that already exists (recovery after a failed init); the key is never modified")
	return cmd
}

// keptKeyError explains the state after a failure that happened once the key
// existed, so the operator never has to guess whether to delete it.
func keptKeyError(keyFile string, err error) error {
	return fmt.Errorf("%w\n\nThe root key at %s was NOT deleted or changed. No usable database exists yet. "+
		"After fixing the cause, re-run `dso central init ... --use-existing-key` to continue with the same key. "+
		"Do not delete the key unless you are certain no database was ever created with it", err, keyFile)
}

// sameDir compares directories after resolving symbolic links, so a symlink
// cannot be used to put both files in one place while appearing separate.
func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		ra, _ = filepath.Abs(a)
		rb, _ = filepath.Abs(b)
	}
	return ra == rb
}

func tabs(cmd *cobra.Command) *tabwriter.Writer {
	return tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
}

func centralProjectCmd(f *centralFlags) *cobra.Command {
	parent := &cobra.Command{Use: "project", Short: "Manage projects"}
	var name string
	create := &cobra.Command{
		Use: "create <slug>", Args: cobra.ExactArgs(1), Short: "Create a project (generates its data key)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			p, err := svc.CreateProject(cmd.Context(), args[0], name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created project %s\n", p.Slug)
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "display name")
	list := &cobra.Command{
		Use: "list", Short: "List projects", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			ps, err := svc.ListProjects(cmd.Context())
			if err != nil {
				return err
			}
			w := tabs(cmd)
			fmt.Fprintln(w, "SLUG\tNAME\tCREATED")
			for _, p := range ps {
				fmt.Fprintf(w, "%s\t%s\t%s\n", p.Slug, p.Name, p.CreatedAt.Format("2006-01-02 15:04:05Z"))
			}
			return w.Flush()
		},
	}
	parent.AddCommand(create, list)
	return parent
}

func centralEnvCmd(f *centralFlags) *cobra.Command {
	parent := &cobra.Command{Use: "environment", Aliases: []string{"environments"}, Short: "Manage environments"}
	var name string
	create := &cobra.Command{
		Use: "create <project> <slug>", Args: cobra.ExactArgs(2), Short: "Create an environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			e, err := svc.CreateEnvironment(cmd.Context(), args[0], args[1], name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created environment %s/%s\n", args[0], e.Slug)
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "display name")
	list := &cobra.Command{
		Use: "list <project>", Args: cobra.ExactArgs(1), Short: "List environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			es, err := svc.ListEnvironments(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			w := tabs(cmd)
			fmt.Fprintln(w, "SLUG\tNAME")
			for _, e := range es {
				fmt.Fprintf(w, "%s\t%s\n", e.Slug, e.Name)
			}
			return w.Flush()
		},
	}
	parent.AddCommand(create, list)
	return parent
}

// readValue obtains a secret value without ever taking it from argv.
func readValue(cmd *cobra.Command, fromStdin bool) ([]byte, error) {
	if fromStdin {
		b, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), crypto.MaxPlaintextSize+1))
		if err != nil {
			return nil, err
		}
		// A single trailing newline from `echo`/heredocs is not part of the value.
		b = bytes.TrimSuffix(b, []byte("\n"))
		return b, nil
	}
	b, err := promptHidden(cmd, "Value: ")
	if err != nil {
		return nil, fmt.Errorf("%w (or pass --value-stdin)", err)
	}
	return b, nil
}

func centralSecretCmd(f *centralFlags) *cobra.Command {
	parent := &cobra.Command{Use: "secret", Short: "Manage versioned secrets"}

	var comment string
	var stdin bool
	valueCmd := func(use, short string, add bool) *cobra.Command {
		c := &cobra.Command{
			Use: use + " <project> <environment> <name>", Args: cobra.ExactArgs(3), Short: short,
			RunE: func(cmd *cobra.Command, args []string) error {
				svc, closeFn, err := f.open(cmd)
				if err != nil {
					return err
				}
				defer closeFn()
				v, err := readValue(cmd, stdin)
				if err != nil {
					return err
				}
				defer crypto.Zero(v)
				if add {
					n, err := svc.AddVersion(cmd.Context(), args[0], args[1], args[2], v, actorName(), comment)
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Created version %d of %s\n", n, args[2])
					return nil
				}
				if err := svc.CreateSecret(cmd.Context(), args[0], args[1], args[2], v, actorName(), comment); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Created secret %s (version 1)\n", args[2])
				return nil
			},
		}
		c.Flags().BoolVar(&stdin, "value-stdin", false, "read the value from stdin instead of a hidden prompt")
		c.Flags().StringVar(&comment, "comment", "", "free-text note stored with the version (not secret)")
		return c
	}

	var version int
	var reveal bool
	get := &cobra.Command{
		Use: "get <project> <environment> <name>", Args: cobra.ExactArgs(3),
		Short: "Print a secret value (requires --reveal)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !reveal {
				return errors.New("refusing to print a secret value without --reveal")
			}
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			v, err := svc.GetValue(cmd.Context(), args[0], args[1], args[2], version)
			if err != nil {
				return err
			}
			defer v.Zero()
			_, err = cmd.OutOrStdout().Write(v.Plaintext)
			if err == nil && term.IsTerminal(int(os.Stdout.Fd())) { // #nosec G115 -- a terminal file descriptor is a small non-negative OS value
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return err
		},
	}
	get.Flags().IntVar(&version, "version", 0, "version number (default: the newest version; fails if it is disabled)")
	get.Flags().BoolVar(&reveal, "reveal", false, "print the plaintext value")

	list := &cobra.Command{
		Use: "list <project> <environment>", Args: cobra.ExactArgs(2), Short: "List secret names and metadata (no values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			ms, err := svc.ListSecrets(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			w := tabs(cmd)
			fmt.Fprintln(w, "NAME\tLATEST\tVERSIONS\tCREATED BY\tCREATED")
			for _, m := range ms {
				latest := fmt.Sprint(m.LatestVersion)
				if m.LatestVersionDisabled {
					latest += " (DISABLED)"
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", m.Name, latest, m.VersionCount, m.CreatedBy, m.CreatedAt.Format("2006-01-02 15:04:05Z"))
			}
			return w.Flush()
		},
	}

	versions := &cobra.Command{
		Use: "versions <project> <environment> <name>", Args: cobra.ExactArgs(3), Short: "List version metadata (no values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			vs, err := svc.ListVersions(cmd.Context(), args[0], args[1], args[2])
			if err != nil {
				return err
			}
			w := tabs(cmd)
			fmt.Fprintln(w, "VERSION\tCREATED BY\tCREATED\tSTATE\tCOMMENT")
			for _, v := range vs {
				state := "enabled"
				if v.DisabledAt != nil {
					state = "disabled"
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", v.Version, v.CreatedBy, v.CreatedAt.Format("2006-01-02 15:04:05Z"), state, v.Comment)
			}
			return w.Flush()
		},
	}

	disable := &cobra.Command{
		Use: "disable-version <project> <environment> <name> <version>", Args: cobra.ExactArgs(4), Short: "Disable a version (kept, but no longer readable)",
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[3])
			if err != nil || n < 1 {
				return errors.New("version must be a positive integer")
			}
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			if err := svc.DisableVersion(cmd.Context(), args[0], args[1], args[2], n, actorName()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Disabled version %d of %s\n", n, args[2])
			return nil
		},
	}

	del := &cobra.Command{
		Use: "delete <project> <environment> <name>", Args: cobra.ExactArgs(3), Short: "Soft-delete a secret (versions retained)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, closeFn, err := f.open(cmd)
			if err != nil {
				return err
			}
			defer closeFn()
			if err := svc.DeleteSecret(cmd.Context(), args[0], args[1], args[2], actorName()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted secret %s\n", args[2])
			return nil
		},
	}

	parent.AddCommand(valueCmd("create", "Create a secret (value from a hidden prompt or stdin)", false),
		valueCmd("add-version", "Add a new immutable version (value from a hidden prompt or stdin)", true),
		get, list, versions, disable, del)
	return parent
}

// centralBackupCmd adds `dso central backup` (Phase 2 Step 5.4, approved
// design/decision-resolution). It requires only --db: backup never needs or
// touches key material, since it copies ciphertext and wrapped keys as-is
// without decrypting anything.
func centralBackupCmd(f *centralFlags) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write a verified, on-demand snapshot of the central database",
		Long: `Writes a complete, self-contained, independently verified snapshot of
the central database to --out, using the same consistency mechanism (SQLite's
own VACUUM INTO) and verification (PRAGMA integrity_check plus a schema
version check) as the existing migration-time backup. It never overwrites an
existing file at --out.

The backup contains every table, including the full audit history. It never
contains the root key or passphrase: preserve that separately, or this backup
cannot be used to recover any secret value. Backups are never pruned
automatically; managing accumulated backup files is the operator's
responsibility.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.db == "" {
				return errors.New("--db is required")
			}
			if out == "" {
				return errors.New("--out is required")
			}
			st, err := sqlite.Open(cmd.Context(), f.db, sqlite.Options{})
			if err != nil {
				return err
			}
			defer st.Close()
			path, err := st.Backup(cmd.Context(), out)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup written to %s\n", path)
			fmt.Fprintln(cmd.OutOrStdout(), "Reminder: this backup does not include the root key or passphrase; preserve that material separately.")
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "destination path for the backup (required; must not already exist)")
	return cmd
}

// printCandidateInfo shows a restore candidate's path and its validated,
// non-sensitive metadata -- schema version and its own audit-chain tip,
// where available -- so an operator has the information the approved design
// calls for before confirming a destructive restore (audit finding F8: the
// candidate's own path/filename is included, not just its contents, since
// the operator may be comparing this output against a log or a script's own
// record of which file it asked for).
func printCandidateInfo(cmd *cobra.Command, candidatePath string, info sqlite.CandidateInfo) {
	fmt.Fprintf(cmd.OutOrStdout(), "Candidate: %s\n", candidatePath)
	fmt.Fprintf(cmd.OutOrStdout(), "Candidate schema version: %d\n", info.SchemaVersion)
	fmt.Fprintf(cmd.OutOrStdout(), "Candidate audit events:   %d\n", info.EventCount)
	if info.LastEvent != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Candidate audit-chain tip: %s (%s, %s)\n",
			info.LastEvent.ID, info.LastEvent.EventType, info.LastEvent.CreatedAt.UTC().Format(time.RFC3339))
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Candidate audit chain: empty")
	}
}

// centralRestoreCmd adds `dso central restore` (Phase 2 Step 5.4, approved
// design/decision-resolution).
//
// Known, explicitly disclosed limitations (historical design references,
// NOT present in this checkout or its git history, so unverifiable here:
// docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md and
// docs/reviews/2026-10-29-central-store-phase2-backup-restore-decision-resolution.md):
//   - audit-chain verification proves the candidate's own internal
//     consistency, not that it is the newest legitimate state; restoring an
//     older valid backup over a database with more recent activity is NOT
//     detected by this command.
//   - successful validation proves SQLite structural integrity and that the
//     configured root key matches the candidate's binding; it does NOT
//     prove every secret version's ciphertext is decryptable.
//   - this command has no caller-specific authorization: anyone able to run
//     `dso central` as the owning OS user can run it, identically to every
//     other `dso central` command today.
func centralRestoreCmd(f *centralFlags) *cobra.Command {
	var from string
	var dryRun, force bool
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Validate, and optionally perform, a restore of the central database from a backup",
		Long: `Validates a backup candidate (SQLite integrity, schema compatibility, full
audit-chain self-consistency, and root-key binding) and, unless --dry-run is
given, replaces the active database with it after creating and verifying a
mandatory safety snapshot of the active database and obtaining explicit
confirmation.

Known limitations:
  - audit-chain verification cannot detect that an older, internally valid
    backup is being restored over a database with more recent activity.
  - successful validation does not prove every secret version's ciphertext
    in the candidate is decryptable; it proves SQLite structural integrity
    and that the configured root key matches the candidate's binding.
  - this command has no caller-specific authorization today (the authn/authz
    design document it was planned against is not available in this
    checkout): anyone able to run 'dso central' as the owning OS user can
    run it.

Restore is an OFFLINE operator action. Stop every other process that uses the
active database before restoring. Restore takes no cross-process lock, so a
write made by another process while it runs can be silently lost.

--dry-run performs every validation check and reports the result without
touching the active database, creating a safety snapshot, or recording a
restore event.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.requireCommon(); err != nil {
				return err
			}
			if from == "" {
				return errors.New("--from is required")
			}
			kp, err := f.keyProvider(cmd)
			if err != nil {
				return err
			}
			stopCleanup := sqlite.InstallStagingSignalCleanup()
			defer stopCleanup()
			result, err := centralsvc.Restore(cmd.Context(), centralsvc.RestoreOptions{
				CandidatePath: from,
				DBPath:        f.db,
				KeyProvider:   kp,
				DryRun:        dryRun,
				Actor:         actorName(),
				Confirm: func(info sqlite.CandidateInfo) bool {
					printCandidateInfo(cmd, from, info)
					if force {
						return true
					}
					return promptApprovalIO(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf(
						"This will replace the active database at %s. A safety snapshot will be kept. "+
							"Restore is offline: stop every other process using this database first "+
							"(no cross-process lock is taken; concurrent writes can be lost). Continue?", f.db))
				},
			})
			if errors.Is(err, centralsvc.ErrRestoreNotConfirmed) {
				fmt.Fprintln(cmd.OutOrStdout(), "Restore cancelled.")
				return nil
			}
			if err != nil {
				return err
			}
			if dryRun {
				printCandidateInfo(cmd, from, result.CandidateInfo)
				fmt.Fprintln(cmd.OutOrStdout(), "Dry run: candidate passed validation. The active database was not modified.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Restore complete. Safety snapshot: %s\n", result.SafetySnapshot)
			if result.RestoreEventID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Restore audit event: %s\n", result.RestoreEventID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "path to the backup to restore from (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate the candidate and report the result without modifying the active database")
	cmd.Flags().BoolVar(&force, "force", false, "skip the interactive confirmation prompt (non-interactive restore)")
	return cmd
}

// promptApprovalIO asks prompt on out and reads one line from in; only an
// explicit "y"/"yes" (case-insensitive) approves. EOF or any other answer
// declines. It uses the command's own streams, unlike promptForApproval
// (which is bound to the process's os.Stdin/os.Stdout), so the restore
// confirmation can be driven and captured through the Cobra command.
func promptApprovalIO(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// auditVerifyJSON is the stable --json document of `audit verify`. Optional
// fields are omitted when they do not apply; nothing is guessed.
type auditVerifyJSON struct {
	Status         string `json:"status"` // verified | integrity_failure | unavailable
	EventCount     *int   `json:"event_count,omitempty"`
	TipEventID     string `json:"tip_event_id,omitempty"`
	FailureIndex   *int   `json:"failure_index,omitempty"`
	FailureEventID string `json:"failure_event_id,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
	Error          string `json:"error,omitempty"`
}

func centralAuditCmd(f *centralFlags) *cobra.Command {
	parent := &cobra.Command{Use: "audit", Short: "Inspect the audit log"}
	var asJSON bool
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Verify the integrity of the audit hash chain (read-only)",
		Args:  cobra.NoArgs,
		Long: `Recomputes the audit log's SHA-256 hash chain and reports whether it is
internally consistent. It is strictly read-only: it needs no key, appends no
audit event, runs no migration and creates nothing. The database file is never
opened by SQLite; a private temporary copy of it (and of any -wal/-shm files)
is verified instead, so the original and its sidecars are not touched. The copy
needs temporary disk space about the size of the database and is removed when
the command finishes or is interrupted by SIGINT/SIGTERM. After SIGKILL or a
machine failure it cannot be removed automatically: a directory named
dso-restore-validate-* (mode 0700) can remain in the temporary directory and
should be deleted manually. A -wal or -shm file that is a symbolic link or not
a regular file makes verification unavailable.

An empty audit log (zero events) is reported as verified.

Exit status:
  0   verified
  1   usage error, or any other command failure
  10  integrity failure: a hash or link in the chain does not match
  11  verification unavailable: the database could not be verified (for
      example unreadable, unsafe, no audit log, unsupported schema); this is
      not evidence of tampering
Codes 10 and 11 apply only to this command. Scripts should treat any non-zero
exit status as failure.

--json prints exactly one document on stdout, also on failure:
  status            "verified", "integrity_failure" or "unavailable"
  event_count       number of events read (omitted when unavailable)
  tip_event_id      id of the last event (verified, non-empty chain only)
  failure_index     0-based position of the first failing event
  failure_event_id  id of the first failing event
  failure_reason    why it failed
  error             sanitized reason verification was unavailable (no paths;
                    details are written to stderr only)

Same-user access. The database is safety-checked and then copied by path, and
its -wal/-shm files are checked only for being regular files (not symbolic
links), not for owner or mode. A process running as the same operating-system
user with write access to the database directory can still race these checks;
they do not defend against that.

Concurrent changes. The verified copy is made with plain file reads, not a
transactionally consistent snapshot. If the database or its -wal file changes
while it is being copied, the copy is retried; if no stable copy can be
obtained the result is exit 11 (verification unavailable), which means the
check could not be done and is not evidence of tampering. Verify while no
process is writing to the database for the most reliable result.

Limitations. The chain is unkeyed: anyone who can modify the database can
recompute it, so a "verified" result does NOT prove authenticity. It also
cannot detect deletion of the newest events or replacement of the database by
an older, internally valid copy. This command is not authentication or
authorization, and not a tamper-proof guarantee.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.db == "" {
				return errors.New("--db is required")
			}
			stopCleanup := sqlite.InstallStagingSignalCleanup()
			defer stopCleanup()
			res := sqlite.VerifyAuditLog(cmd.Context(), f.db)
			doc := auditVerifyJSON{Status: string(res.Status)}
			var failure error
			switch res.Status {
			case sqlite.AuditVerified:
				n := res.EventCount
				doc.EventCount, doc.TipEventID = &n, res.TipEventID
			case sqlite.AuditIntegrityFailure:
				n, i := res.EventCount, res.FailureIndex
				doc.EventCount, doc.FailureIndex = &n, &i
				doc.FailureEventID, doc.FailureReason = res.FailureEventID, res.FailureReason
				failure = &exitCodeError{exitAuditIntegrityFailure, fmt.Errorf(
					"audit chain integrity failure at index %d (event %s): %s", res.FailureIndex, quoteUntrusted(res.FailureEventID), printableUntrusted(res.FailureReason))}
			default:
				// Structured output carries only the sanitized reason; the
				// underlying detail (which can contain paths) goes to stderr only.
				doc.Error = res.Reason
				failure = &exitCodeError{exitAuditUnavailable, fmt.Errorf("audit chain verification unavailable: %s (detail: %s)", printableUntrusted(res.Reason), printableUntrusted(res.Err.Error()))}
			}
			out := cmd.OutOrStdout()
			if asJSON {
				var buf bytes.Buffer
				enc := json.NewEncoder(&buf)
				enc.SetIndent("", "  ")
				if err := enc.Encode(doc); err != nil {
					return err
				}
				if _, err := out.Write(escapeJSONNonPrintable(buf.Bytes())); err != nil {
					return err
				}
				return failure
			}
			switch res.Status {
			case sqlite.AuditVerified:
				fmt.Fprintf(out, "Audit chain: VERIFIED (%d events)\n", res.EventCount)
				if res.TipEventID != "" {
					fmt.Fprintf(out, "Tip event: %s\n", quoteUntrusted(res.TipEventID))
				}
				fmt.Fprintln(out, "Note: the chain is unkeyed. This does not prove authenticity, and cannot detect deleted newest events or rollback to an older valid copy.")
			case sqlite.AuditIntegrityFailure:
				fmt.Fprintf(out, "Audit chain: INTEGRITY FAILURE at index %d (event %s)\n", res.FailureIndex, quoteUntrusted(res.FailureEventID))
				fmt.Fprintf(out, "Reason: %s\n", printableUntrusted(res.FailureReason))
			default:
				fmt.Fprintln(out, "Audit chain: VERIFICATION UNAVAILABLE - the database could not be safely read or verified (this is not evidence of tampering)")
				fmt.Fprintf(out, "Reason: %s\n", printableUntrusted(res.Reason))
			}
			return failure
		},
	}
	verify.Flags().BoolVar(&asJSON, "json", false, "print the result as one JSON document")
	parent.AddCommand(verify)
	return parent
}
