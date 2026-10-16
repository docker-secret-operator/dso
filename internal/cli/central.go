package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/docker-secret-operator/dso/internal/centralsvc"
	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs"
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

	cmd.AddCommand(centralInitCmd(f), centralProjectCmd(f), centralEnvCmd(f), centralSecretCmd(f))
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
				_ = st.Close()
				return fail(err)
			}
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
			if err == nil && term.IsTerminal(int(os.Stdout.Fd())) { // #nosec G115
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
