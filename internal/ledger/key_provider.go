package ledger

import (
	crand "crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// keyDeps is the injection seam of the key provider: tests replace the CSPRNG,
// the owner lookup and the failure-prone filesystem steps. Production values
// are set by NewFileKeyProvider.
type keyDeps struct {
	random    io.Reader
	ownerUID  func(fi os.FileInfo) (uint32, bool)
	euid      func() int
	fsyncFile func(f *os.File) error
	fsyncDir  func(d *os.File) error
	// renameNoReplace renames old to new inside the directory dirfd and must fail
	// with EEXIST rather than replace new.
	renameNoReplace func(dirfd int, oldName, newName string) error
}

func defaultKeyDeps() keyDeps {
	return keyDeps{
		random:          crand.Reader,
		ownerUID:        ownerUID,
		euid:            os.Geteuid,
		fsyncFile:       func(f *os.File) error { return f.Sync() },
		fsyncDir:        func(d *os.File) error { return d.Sync() },
		renameNoReplace: renameNoReplaceAt,
	}
}

// FileKeyProvider is the production host-local HMAC key provider. It implements
// KeyProvider (Inspect, Generate) and adds a read-only material accessor for S4.
// It holds no key material itself: every call re-verifies and re-reads the file
// through a descriptor, so a tampered file is detected rather than served from
// a cache. It is safe for concurrent use.
type FileKeyProvider struct {
	dir  string
	deps keyDeps
}

var _ KeyProvider = (*FileKeyProvider)(nil)

// NewFileKeyProvider returns a provider for the key directory dir (use
// DefaultKeyDir in production). It touches nothing on disk. A directory under
// /run/dso is refused.
func NewFileKeyProvider(dir string) (*FileKeyProvider, error) {
	return newFileKeyProvider(dir, defaultKeyDeps())
}

func newFileKeyProvider(dir string, deps keyDeps) (*FileKeyProvider, error) {
	if dir == "" {
		return nil, keyErr(ErrKeyDirectory, "directory is empty", nil)
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, keyErr(ErrKeyDirectory, "cannot resolve the directory path", err)
	}
	if err := refuseVolatileDir(abs); err != nil {
		return nil, err
	}
	return &FileKeyProvider{dir: abs, deps: deps}, nil
}

// Dir returns the key directory.
func (p *FileKeyProvider) Dir() string { return p.dir }

func (p *FileKeyProvider) describe() string { return "ledger.FileKeyProvider{dir:" + p.dir + "}" }

// String implements fmt.Stringer.
func (p *FileKeyProvider) String() string { return p.describe() }

// GoString implements fmt.GoStringer.
func (p *FileKeyProvider) GoString() string { return p.describe() }

// Format implements fmt.Formatter so no verb exposes provider internals.
func (p *FileKeyProvider) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, p.describe()) }

// Inspect classifies the key without modifying anything. A lost key (a ledger or
// other evidence of a prior key exists but the key file is missing) and an
// unsafe key are both reported as KeyUnsafe so the store fails closed and never
// preserves, renames or regenerates; KeyMalformed is a safe but invalid file.
// KeyAbsent is reported only for a verified first run.
func (p *FileKeyProvider) Inspect() KeyInspection {
	k, err := p.LoadKey()
	switch {
	case err == nil:
		defer k.Zero()
		return KeyInspection{State: KeyValid, KeyID: k.ID()}
	case isKind(err, ErrKeyAbsent):
		return KeyInspection{State: KeyAbsent}
	case isKind(err, ErrKeyCorrupt):
		return KeyInspection{State: KeyMalformed, Reason: reasonOf(err)}
	default:
		return KeyInspection{State: KeyUnsafe, Reason: reasonOf(err)}
	}
}

// KeyMaterial returns the key id and a COPY of the key bytes for S4. The caller
// owns the copy and must never log it.
func (p *FileKeyProvider) KeyMaterial() (keyID string, key []byte, err error) {
	k, err := p.LoadKey()
	if err != nil {
		return "", nil, err
	}
	defer k.Zero()
	return k.ID(), k.Bytes(), nil
}

// Generate creates the key on a verified first run only. It never replaces,
// renames or deletes anything: if a usable key exists it returns ErrKeyExists
// (use EnsureKey for idempotent initialisation), and for a lost, unsafe or
// corrupt key it returns the matching typed error without writing. A caller
// that loses a creation race receives the winner's key id after the key was
// re-read and verified.
func (p *FileKeyProvider) Generate() (string, error) { return p.create(false) }

// EnsureKey is the idempotent initialiser: it returns the id of the existing
// valid key, or creates the key on a verified first run. It never regenerates
// over a lost, unsafe or corrupt key.
func (p *FileKeyProvider) EnsureKey() (string, error) { return p.create(true) }

func isKind(err error, kind error) bool {
	ke, ok := err.(*KeyError) //nolint:errorlint // concrete type is produced only by this package
	return ok && ke.Kind == kind
}

func reasonOf(err error) string {
	if ke, ok := err.(*KeyError); ok { //nolint:errorlint // see isKind
		return ke.Kind.Error() + ": " + ke.Reason
	}
	return err.Error()
}
