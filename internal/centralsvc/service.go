// Package centralsvc is the service layer of the central store: it composes a
// store.Store and a crypto.KeyProvider into versioned, envelope-encrypted
// secrets. It is an in-process API only; M1 exposes no network surface and
// performs no authorization (that is M2). The actor strings recorded here are
// attribution metadata for a future audit log, not an access-control decision.
package centralsvc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/store"
)

var (
	ErrInvalidName = errors.New("secrets: invalid name")
	// ErrWrongRootKey means the configured root key is not the one that
	// protects this project's data key.
	ErrWrongRootKey = errors.New("secrets: the configured root key does not match this project's key id")
	// ErrIntegrity means stored ciphertext failed authentication: tampering,
	// corruption, or a row moved to a different project/environment/secret/version.
	ErrIntegrity = errors.New("secrets: stored value failed integrity verification")
)

var (
	slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62})$`)
	nameRe = regexp.MustCompile(`^[A-Za-z0-9_](?:[A-Za-z0-9_./-]{0,254})$`)
)

// Service performs secret operations. Construct with New.
type Service struct {
	store store.Store
	keys  crypto.KeyProvider
	log   *zap.Logger

	bindMu sync.Mutex
	bound  bool // the database is verified (or newly bound) to keys's root key
}

// New returns a Service. logger may be nil. The service logs operation
// metadata (ids, names, versions) and never values, keys or ciphertext.
func New(s store.Store, kp crypto.KeyProvider, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{store: s, keys: kp, log: logger}
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func validateSlug(kind, s string) error {
	if !slugRe.MatchString(s) {
		return fmt.Errorf("%w: %s must match %s", ErrInvalidName, kind, slugRe)
	}
	return nil
}

// ValidateSecretName enforces the secret-name grammar: path-like, no empty or
// dot segments, so names can never alias each other or traverse.
func ValidateSecretName(n string) error {
	if !nameRe.MatchString(n) || strings.HasSuffix(n, "/") || strings.Contains(n, "//") {
		return fmt.Errorf("%w: secret names use letters, digits and _ . / -", ErrInvalidName)
	}
	for _, seg := range strings.Split(n, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: secret names must not contain . or .. segments", ErrInvalidName)
		}
	}
	return nil
}

// CreateProject creates a project with a fresh random DEK wrapped by the root key.
func (s *Service) CreateProject(ctx context.Context, slug, name string) (store.Project, error) {
	if err := s.Bind(ctx); err != nil {
		return store.Project{}, err
	}
	if err := validateSlug("project slug", slug); err != nil {
		return store.Project{}, err
	}
	id, err := newID()
	if err != nil {
		return store.Project{}, err
	}
	dek, err := crypto.GenerateKey()
	if err != nil {
		return store.Project{}, err
	}
	defer crypto.Zero(dek)
	const dekVersion = 1
	dekAAD, err := crypto.DEKAAD(id, s.keys.ID(), dekVersion)
	if err != nil {
		return store.Project{}, fmt.Errorf("building project key context: %w", err)
	}
	wrapped, err := s.keys.Wrap(ctx, dek, dekAAD)
	if err != nil {
		return store.Project{}, fmt.Errorf("wrapping project key: %w", err)
	}
	p := store.Project{ID: id, Slug: slug, Name: name, WrappedDEK: wrapped, DEKKeyID: s.keys.ID(), DEKVersion: dekVersion}
	if err := s.store.CreateProject(ctx, p); err != nil {
		return store.Project{}, err
	}
	s.log.Info("project created", zap.String("project", slug), zap.String("key_id", s.keys.ID()))
	created, err := s.store.GetProject(ctx, id)
	return stripKeyMaterial(created), err
}

// stripKeyMaterial removes the wrapped DEK from a project before it leaves the
// service: callers need identity and metadata, never key blobs.
func stripKeyMaterial(p store.Project) store.Project {
	p.WrappedDEK = nil
	return p
}

func (s *Service) CreateEnvironment(ctx context.Context, projectSlug, slug, name string) (store.Environment, error) {
	if err := s.Bind(ctx); err != nil {
		return store.Environment{}, err
	}
	if err := validateSlug("environment slug", slug); err != nil {
		return store.Environment{}, err
	}
	p, err := s.store.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		return store.Environment{}, err
	}
	id, err := newID()
	if err != nil {
		return store.Environment{}, err
	}
	if err := s.store.CreateEnvironment(ctx, store.Environment{ID: id, ProjectID: p.ID, Slug: slug, Name: name}); err != nil {
		return store.Environment{}, err
	}
	s.log.Info("environment created", zap.String("project", projectSlug), zap.String("environment", slug))
	return s.store.GetEnvironmentBySlug(ctx, p.ID, slug)
}

type scope struct {
	project store.Project
	env     store.Environment
}

func (s *Service) resolve(ctx context.Context, projectSlug, envSlug string) (scope, error) {
	p, err := s.store.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		return scope{}, err
	}
	e, err := s.store.GetEnvironmentBySlug(ctx, p.ID, envSlug)
	if err != nil {
		return scope{}, err
	}
	return scope{p, e}, nil
}

// unwrapDEK returns the project's DEK. Callers must crypto.Zero it.
func (s *Service) unwrapDEK(ctx context.Context, p store.Project) ([]byte, error) {
	if p.DEKKeyID != s.keys.ID() {
		return nil, ErrWrongRootKey
	}
	dekAAD, err := crypto.DEKAAD(p.ID, p.DEKKeyID, p.DEKVersion)
	if err != nil {
		return nil, fmt.Errorf("building project key context: %w", err)
	}
	dek, err := s.keys.Unwrap(ctx, p.WrappedDEK, dekAAD)
	if err != nil {
		return nil, fmt.Errorf("unwrapping project key: %w", err)
	}
	return dek, nil
}

func checkValue(v []byte) error {
	if len(v) == 0 {
		return fmt.Errorf("%w: empty value", store.ErrInvalid)
	}
	if len(v) > crypto.MaxPlaintextSize {
		return crypto.ErrPlaintextTooLarge
	}
	return nil
}

// CreateSecret creates a new secret with version 1. It fails with
// store.ErrConflict if a live secret of that name exists.
func (s *Service) CreateSecret(ctx context.Context, projectSlug, envSlug, name string, value []byte, actor, comment string) error {
	if err := s.Bind(ctx); err != nil {
		return err
	}
	if err := ValidateSecretName(name); err != nil {
		return err
	}
	if err := checkValue(value); err != nil {
		return err
	}
	sc, err := s.resolve(ctx, projectSlug, envSlug)
	if err != nil {
		return err
	}
	dek, err := s.unwrapDEK(ctx, sc.project)
	if err != nil {
		return err
	}
	defer crypto.Zero(dek)
	id, err := newID()
	if err != nil {
		return err
	}
	aad, err := crypto.SecretAAD(sc.project.ID, sc.env.ID, id, name, 1, sc.project.DEKVersion)
	if err != nil {
		return fmt.Errorf("building secret context: %w", err)
	}
	nonce, ct, err := crypto.Seal(dek, value, aad)
	if err != nil {
		return err
	}
	if err := s.store.CreateSecret(ctx, store.NewSecret{
		ID: id, ProjectID: sc.project.ID, EnvironmentID: sc.env.ID, Name: name, CreatedBy: actor, Comment: comment,
		DEKVersion: sc.project.DEKVersion, Ciphertext: ct, Nonce: nonce,
	}); err != nil {
		return err
	}
	s.log.Info("secret created", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.Int("version", 1), zap.String("actor", actor))
	return nil
}

// AddVersion appends a new immutable version and returns its number.
func (s *Service) AddVersion(ctx context.Context, projectSlug, envSlug, name string, value []byte, actor, comment string) (int, error) {
	if err := s.Bind(ctx); err != nil {
		return 0, err
	}
	if err := checkValue(value); err != nil {
		return 0, err
	}
	sc, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return 0, err
	}
	dek, err := s.unwrapDEK(ctx, sc.project)
	if err != nil {
		return 0, err
	}
	defer crypto.Zero(dek)
	v, err := s.store.AddVersion(ctx, sec.ID, actor, comment, func(version int) ([]byte, []byte, int, error) {
		aad, err := crypto.SecretAAD(sc.project.ID, sc.env.ID, sec.ID, name, version, sc.project.DEKVersion)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("building secret context: %w", err)
		}
		nonce, ct, err := crypto.Seal(dek, value, aad)
		return ct, nonce, sc.project.DEKVersion, err
	})
	if err != nil {
		return 0, err
	}
	s.log.Info("secret version added", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.Int("version", v), zap.String("actor", actor))
	return v, nil
}

func (s *Service) lookup(ctx context.Context, projectSlug, envSlug, name string) (scope, store.SecretMeta, error) {
	sc, err := s.resolve(ctx, projectSlug, envSlug)
	if err != nil {
		return scope{}, store.SecretMeta{}, err
	}
	sec, err := s.store.GetSecret(ctx, sc.env.ID, name)
	return sc, sec, err
}

// Value is a decrypted secret. It is a distinct type so it can never be
// confused with metadata, and String/GoString never reveal the contents.
type Value struct {
	Version   int
	Plaintext []byte
}

func (Value) String() string   { return "secrets.Value{redacted}" }
func (Value) GoString() string { return "secrets.Value{redacted}" }

// Zero overwrites the plaintext.
func (v *Value) Zero() { crypto.Zero(v.Plaintext) }

// GetValue returns and decrypts one version; version 0 means the latest
// enabled version. The ciphertext is verified against the row's own identity,
// so a row moved to another project, environment, secret or version fails
// with ErrIntegrity.
func (s *Service) GetValue(ctx context.Context, projectSlug, envSlug, name string, version int) (Value, error) {
	if err := s.Bind(ctx); err != nil {
		return Value{}, err
	}
	sc, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return Value{}, err
	}
	var rec store.VersionRecord
	if version == 0 {
		rec, err = s.store.GetLatestVersion(ctx, sec.ID)
	} else {
		rec, err = s.store.GetVersion(ctx, sec.ID, version)
	}
	if err != nil {
		return Value{}, err
	}
	dek, err := s.unwrapDEK(ctx, sc.project)
	if err != nil {
		return Value{}, err
	}
	defer crypto.Zero(dek)
	aad, err := crypto.SecretAAD(sc.project.ID, sc.env.ID, sec.ID, name, rec.Version, rec.DEKVersion)
	if err != nil {
		return Value{}, fmt.Errorf("building secret context: %w", err)
	}
	pt, err := crypto.Open(dek, rec.Nonce, rec.Ciphertext, aad)
	if err != nil {
		s.log.Warn("secret failed integrity verification", zap.String("project", projectSlug),
			zap.String("environment", envSlug), zap.String("secret", name), zap.Int("version", rec.Version))
		return Value{}, ErrIntegrity
	}
	s.log.Info("secret read", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.Int("version", rec.Version))
	return Value{Version: rec.Version, Plaintext: pt}, nil
}

// ListSecrets returns metadata only.
func (s *Service) ListSecrets(ctx context.Context, projectSlug, envSlug string) ([]store.SecretMeta, error) {
	if err := s.Bind(ctx); err != nil {
		return nil, err
	}
	sc, err := s.resolve(ctx, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	return s.store.ListSecrets(ctx, sc.env.ID)
}

// ListVersions returns version metadata only.
func (s *Service) ListVersions(ctx context.Context, projectSlug, envSlug, name string) ([]store.VersionMeta, error) {
	if err := s.Bind(ctx); err != nil {
		return nil, err
	}
	_, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return nil, err
	}
	return s.store.ListVersions(ctx, sec.ID)
}

func (s *Service) DisableVersion(ctx context.Context, projectSlug, envSlug, name string, version int, actor string) error {
	if err := s.Bind(ctx); err != nil {
		return err
	}
	_, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return err
	}
	if err := s.store.DisableVersion(ctx, sec.ID, version, actor); err != nil {
		return err
	}
	s.log.Info("secret version disabled", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.Int("version", version), zap.String("actor", actor))
	return nil
}

// DeleteSecret soft-deletes the secret (versions are retained; purge is deferred).
func (s *Service) DeleteSecret(ctx context.Context, projectSlug, envSlug, name, actor string) error {
	if err := s.Bind(ctx); err != nil {
		return err
	}
	_, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSecret(ctx, sec.ID, actor); err != nil {
		return err
	}
	s.log.Info("secret deleted", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.String("actor", actor))
	return nil
}

func (s *Service) ListProjects(ctx context.Context) ([]store.Project, error) {
	if err := s.Bind(ctx); err != nil {
		return nil, err
	}
	ps, err := s.store.ListProjects(ctx)
	for i := range ps {
		ps[i] = stripKeyMaterial(ps[i])
	}
	return ps, err
}

func (s *Service) ListEnvironments(ctx context.Context, projectSlug string) ([]store.Environment, error) {
	if err := s.Bind(ctx); err != nil {
		return nil, err
	}
	p, err := s.store.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		return nil, err
	}
	return s.store.ListEnvironments(ctx, p.ID)
}
