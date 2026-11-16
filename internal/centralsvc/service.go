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

// reasonCategory maps an error to the closed set of audit-event failure
// categories (Phase 2 Step 5.3, approved design §5.1/§5.2): never the raw
// error string itself, which could echo implementation detail or, in a
// poorly-constructed message, a fragment of sensitive input.
func reasonCategory(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, store.ErrNotFound):
		return "not_found"
	case errors.Is(err, store.ErrConflict):
		return "conflict"
	case errors.Is(err, store.ErrInvalid), errors.Is(err, ErrInvalidName):
		return "validation_error"
	case errors.Is(err, store.ErrVersionDisabled), errors.Is(err, store.ErrLatestVersionDisabled):
		return "version_disabled"
	case errors.Is(err, ErrIntegrity):
		return "integrity_error"
	case errors.Is(err, ErrWrongRootKey):
		return "wrong_root_key"
	default:
		return "db_error"
	}
}

// appendAuditBestEffort appends ev as a standalone, non-transactional audit
// write (Phase 2 Step 5.3, approved Gate 6: log-only, availability-first).
// Used for: GetValue's reveal events (a read has no business mutation to
// roll back alongside), and for failure events recorded after a transactional
// attempt has already aborted, when there is no longer an active transaction
// for the failure event to share. A failure to persist the audit event itself
// is logged at error level and never changes, blocks, or delays the result
// the caller has already decided -- log-only reporting depends on the
// configured log sink being available and retained; it is not durable or
// tamper-proof evidence on its own.
func (s *Service) appendAuditBestEffort(ctx context.Context, ev store.AuditEvent) {
	id, err := newID()
	if err != nil {
		s.log.Error("audit event id generation failed", zap.String("event_type", string(ev.EventType)), zap.Error(err))
		return
	}
	ev.ID = id
	if _, err := s.store.AppendAuditEvent(ctx, ev); err != nil {
		s.log.Error("audit event persistence failed; business operation result is unaffected (availability-first policy)",
			zap.String("event_type", string(ev.EventType)), zap.String("outcome", string(ev.Outcome)), zap.Error(err))
	}
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
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditProjectCreate, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ProjectSlug: slug,
		})
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
	evID, err := newID()
	if err != nil {
		return store.Project{}, err
	}
	// Business mutation and its success audit event commit or roll back
	// together (Gate 2, approved): CreateProject is now one of the three
	// operations extended into WithTx specifically for this atomicity.
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateProject(ctx, p); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditProjectCreate, Outcome: store.AuditSuccess, ProjectSlug: slug,
		})
		return err
	})
	if txErr != nil {
		// The transaction above rolled back entirely, so there is nothing left
		// for a failure event to roll back alongside: record it standalone.
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditProjectCreate, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(txErr), ProjectSlug: slug,
		})
		return store.Project{}, txErr
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
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditEnvironmentCreate, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ProjectSlug: projectSlug, EnvironmentSlug: slug,
		})
		return store.Environment{}, err
	}
	p, err := s.store.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditEnvironmentCreate, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ProjectSlug: projectSlug, EnvironmentSlug: slug,
		})
		return store.Environment{}, err
	}
	id, err := newID()
	if err != nil {
		return store.Environment{}, err
	}
	evID, err := newID()
	if err != nil {
		return store.Environment{}, err
	}
	// CreateEnvironment is one of the three operations extended into WithTx
	// (Gate 2, approved) so its business mutation and success audit event
	// commit or roll back together.
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateEnvironment(ctx, store.Environment{ID: id, ProjectID: p.ID, Slug: slug, Name: name}); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditEnvironmentCreate, Outcome: store.AuditSuccess,
			ProjectSlug: projectSlug, EnvironmentSlug: slug,
		})
		return err
	})
	if txErr != nil {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditEnvironmentCreate, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(txErr), ProjectSlug: projectSlug, EnvironmentSlug: slug,
		})
		return store.Environment{}, txErr
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
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditSecretCreate, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor,
			ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name,
		})
		return err
	}
	if err := ValidateSecretName(name); err != nil {
		return fail(err)
	}
	if err := checkValue(value); err != nil {
		return fail(err)
	}
	sc, err := s.resolve(ctx, projectSlug, envSlug)
	if err != nil {
		return fail(err)
	}
	dek, err := s.unwrapDEK(ctx, sc.project)
	if err != nil {
		return fail(err)
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
	evID, err := newID()
	if err != nil {
		return err
	}
	// CreateSecret already ran inside Store.WithTx (M1); the success audit
	// event now shares that same transaction (Gate 2/§7.1, approved).
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateSecret(ctx, store.NewSecret{
			ID: id, ProjectID: sc.project.ID, EnvironmentID: sc.env.ID, Name: name, CreatedBy: actor, Comment: comment,
			DEKVersion: sc.project.DEKVersion, Ciphertext: ct, Nonce: nonce,
		}); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditSecretCreate, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: 1,
		})
		return err
	})
	if txErr != nil {
		return fail(txErr)
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
	fail := func(err error, version int) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditSecretAddVersion, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor,
			ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: version,
		})
		return err
	}
	if err := checkValue(value); err != nil {
		return 0, fail(err, 0)
	}
	sc, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return 0, fail(err, 0)
	}
	dek, err := s.unwrapDEK(ctx, sc.project)
	if err != nil {
		return 0, fail(err, 0)
	}
	defer crypto.Zero(dek)
	evID, err := newID()
	if err != nil {
		return 0, err
	}
	var v int
	// AddVersion already ran inside Store.WithTx (M1); the success audit
	// event now shares that same transaction (Gate 2/§7.1, approved).
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		ver, err := tx.AddVersion(ctx, sec.ID, actor, comment, func(version int) ([]byte, []byte, int, error) {
			aad, err := crypto.SecretAAD(sc.project.ID, sc.env.ID, sec.ID, name, version, sc.project.DEKVersion)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("building secret context: %w", err)
			}
			nonce, ct, err := crypto.Seal(dek, value, aad)
			return ct, nonce, sc.project.DEKVersion, err
		})
		if err != nil {
			return err
		}
		v = ver
		_, err = tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditSecretAddVersion, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: ver,
		})
		return err
	})
	if txErr != nil {
		return 0, fail(txErr, 0)
	}
	s.log.Info("secret version added", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.Int("version", v), zap.String("actor", actor))
	return v, nil
}

// RollbackSecret creates a brand-new, immutable version whose plaintext
// equals an existing version's (toVersion): it is never an in-place revert,
// and the version being rolled back from is never modified or removed.
//
// toVersion must resolve through the same path GetValue uses for an explicit
// version number: store.Tx.GetVersion. That call already fails closed with
// store.ErrVersionDisabled for a disabled source version, so rollback from a
// disabled version is rejected with nothing appended (Review Focus #1) with
// no extra check needed here. Rolling back from the current latest (enabled)
// version succeeds exactly like any other source version (Review Focus #2):
// GetVersion applies no "latest" special-casing (that rule lives only in
// GetLatestVersion, which this method never calls).
//
// The historical plaintext is decrypted using exactly the sequence GetValue
// uses (unwrapDEK + crypto.SecretAAD keyed on the OLD version's own number
// and DEK version + crypto.Open) and re-sealed using exactly the sequence
// AddVersion uses (the same tx.AddVersion call, whose SealFunc builds
// crypto.SecretAAD keyed on the NEW version's number and the project's
// current DEK version, then crypto.Seal) -- both run inside one
// s.store.WithTx so the read, the re-seal, the append and the audit event
// commit or roll back together. The decrypted plaintext is never returned,
// logged, or left un-zeroed: it exists only inside this method's
// transaction closure and is zeroed before the closure returns.
func (s *Service) RollbackSecret(ctx context.Context, projectSlug, envSlug, name string, toVersion int, actor, comment string) (int, error) {
	if err := s.Bind(ctx); err != nil {
		return 0, err
	}
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditSecretRollback, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor,
			ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: toVersion,
		})
		return err
	}
	sc, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return 0, fail(err)
	}
	dek, err := s.unwrapDEK(ctx, sc.project)
	if err != nil {
		return 0, fail(err)
	}
	defer crypto.Zero(dek)
	evID, err := newID()
	if err != nil {
		return 0, err
	}
	var v int
	// RollbackSecret runs the read of the old version, the re-seal, the new
	// version's append, and its success audit event all inside one
	// transaction (Gate 2/§7.1 pattern, as for CreateSecret/AddVersion/
	// DisableVersion/DeleteSecret): a failure at any step leaves nothing
	// behind, including no partially-applied rollback.
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		// Mirrors GetValue's exact decrypt sequence for an explicit version
		// number (never GetLatestVersion: rollback always names its source
		// version explicitly).
		rec, err := tx.GetVersion(ctx, sec.ID, toVersion)
		if err != nil {
			return err
		}
		aad, err := crypto.SecretAAD(sc.project.ID, sc.env.ID, sec.ID, name, rec.Version, rec.DEKVersion)
		if err != nil {
			return fmt.Errorf("building secret context: %w", err)
		}
		pt, err := crypto.Open(dek, rec.Nonce, rec.Ciphertext, aad)
		if err != nil {
			s.log.Warn("secret failed integrity verification", zap.String("project", projectSlug),
				zap.String("environment", envSlug), zap.String("secret", name), zap.Int("version", rec.Version))
			return ErrIntegrity
		}
		defer crypto.Zero(pt)
		// Mirrors AddVersion's exact seal/append sequence: same tx.AddVersion
		// call, same AAD construction, just the new version's own number.
		ver, err := tx.AddVersion(ctx, sec.ID, actor, comment, func(version int) ([]byte, []byte, int, error) {
			aad, err := crypto.SecretAAD(sc.project.ID, sc.env.ID, sec.ID, name, version, sc.project.DEKVersion)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("building secret context: %w", err)
			}
			nonce, ct, err := crypto.Seal(dek, pt, aad)
			return ct, nonce, sc.project.DEKVersion, err
		})
		if err != nil {
			return err
		}
		v = ver
		_, err = tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditSecretRollback, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: ver,
		})
		return err
	})
	if txErr != nil {
		return 0, fail(txErr)
	}
	s.log.Info("secret version rolled back", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.Int("from_version", toVersion), zap.Int("new_version", v), zap.String("actor", actor))
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
	// GetValue is a read: there is no business mutation for its audit event to
	// roll back alongside, so a FAILURE outcome is appended standalone,
	// non-blocking (approved event-coverage matrix: every reveal call, not
	// sampled). The SUCCESS outcome is different, per the ratified REVEAL-B
	// policy: it is appended blocking, and its result gates whether the
	// plaintext is ever returned -- see the end of this function. This is
	// the highest-value event in the whole design, since it is the one
	// place a secret value is disclosed.
	fail := func(err error, recordedVersion int) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditSecretGetReveal, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err),
			ProjectSlug:    projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: recordedVersion,
		})
		return err
	}
	sc, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return Value{}, fail(err, version)
	}
	var rec store.VersionRecord
	if version == 0 {
		rec, err = s.store.GetLatestVersion(ctx, sec.ID)
	} else {
		rec, err = s.store.GetVersion(ctx, sec.ID, version)
	}
	if err != nil {
		return Value{}, fail(err, version)
	}
	dek, err := s.unwrapDEK(ctx, sc.project)
	if err != nil {
		return Value{}, fail(err, rec.Version)
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
		return Value{}, fail(ErrIntegrity, rec.Version)
	}
	s.log.Info("secret read", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("secret", name), zap.Int("version", rec.Version))
	// REVEAL-B (ratified): the reveal event must be durably persisted BEFORE
	// the plaintext is returned, unlike every other best-effort audit event
	// in this service. A disclosure with no corresponding record is exactly
	// the gap this policy closes; appendAuditBestEffort's log-only,
	// availability-first behavior is deliberately not used here. If the
	// append fails, the plaintext already decrypted into pt is zeroed and
	// never returned to the caller.
	evID, err := newID()
	if err != nil {
		crypto.Zero(pt)
		return Value{}, fmt.Errorf("generating reveal audit event id: %w (secret not disclosed)", err)
	}
	if _, err := s.store.AppendAuditEvent(ctx, store.AuditEvent{
		ID: evID, EventType: store.AuditSecretGetReveal, Outcome: store.AuditSuccess,
		ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: rec.Version,
	}); err != nil {
		crypto.Zero(pt)
		return Value{}, fmt.Errorf("persisting reveal audit event: %w (secret not disclosed)", err)
	}
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
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditSecretDisableVersion, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor,
			ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: version,
		})
		return err
	}
	_, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return fail(err)
	}
	evID, err := newID()
	if err != nil {
		return err
	}
	// DisableVersion already ran inside Store.WithTx (M1); the success audit
	// event now shares that same transaction (Gate 2/§7.1, approved).
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.DisableVersion(ctx, sec.ID, version, actor); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditSecretDisableVersion, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name, Version: version,
		})
		return err
	})
	if txErr != nil {
		return fail(txErr)
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
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditSecretDelete, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor,
			ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name,
		})
		return err
	}
	_, sec, err := s.lookup(ctx, projectSlug, envSlug, name)
	if err != nil {
		return fail(err)
	}
	evID, err := newID()
	if err != nil {
		return err
	}
	// DeleteSecret is one of the three operations extended into WithTx
	// (Gate 2, approved) so its business mutation and success audit event
	// commit or roll back together.
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.DeleteSecret(ctx, sec.ID, actor); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditSecretDelete, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug, EnvironmentSlug: envSlug, SecretName: name,
		})
		return err
	})
	if txErr != nil {
		return fail(txErr)
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

// RenameProject changes a project's slug and name. It resolves projectSlug to
// an id first (the same pattern every other mutator uses), then wraps the
// store's RenameProject in s.store.WithTx together with its success audit
// event (Gate 2 pattern). A slug collision comes back from the store as
// store.ErrConflict (internal/store/sqlite's mapErr already keeps the raw
// SQLite unique-constraint text out of it); reasonCategory's existing
// errors.Is(err, store.ErrConflict) case turns that into the "conflict"
// category here, so no SQLite-flavored message ever reaches the caller.
func (s *Service) RenameProject(ctx context.Context, projectSlug, newSlug, newName, actor string) error {
	if err := s.Bind(ctx); err != nil {
		return err
	}
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditProjectRename, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor, ProjectSlug: projectSlug,
		})
		return err
	}
	p, err := s.store.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		return fail(err)
	}
	evID, err := newID()
	if err != nil {
		return err
	}
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.RenameProject(ctx, p.ID, newSlug, newName); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditProjectRename, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug,
		})
		return err
	})
	if txErr != nil {
		return fail(txErr)
	}
	s.log.Info("project renamed", zap.String("project", projectSlug), zap.String("new_slug", newSlug), zap.String("actor", actor))
	return nil
}

// RenameEnvironment changes an environment's slug and name, scoped to
// projectSlug. It resolves both to ids first via s.resolve (the same
// resolver AddVersion/DisableVersion/DeleteSecret use), then wraps the
// store's RenameEnvironment in s.store.WithTx together with its success
// audit event. A same-project slug collision surfaces as store.ErrConflict,
// mapped to the "conflict" reasonCategory exactly as RenameProject's does.
func (s *Service) RenameEnvironment(ctx context.Context, projectSlug, envSlug, newSlug, newName, actor string) error {
	if err := s.Bind(ctx); err != nil {
		return err
	}
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditEnvironmentRename, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor,
			ProjectSlug: projectSlug, EnvironmentSlug: envSlug,
		})
		return err
	}
	sc, err := s.resolve(ctx, projectSlug, envSlug)
	if err != nil {
		return fail(err)
	}
	evID, err := newID()
	if err != nil {
		return err
	}
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.RenameEnvironment(ctx, sc.env.ID, newSlug, newName); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditEnvironmentRename, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug, EnvironmentSlug: envSlug,
		})
		return err
	})
	if txErr != nil {
		return fail(txErr)
	}
	s.log.Info("environment renamed", zap.String("project", projectSlug), zap.String("environment", envSlug),
		zap.String("new_slug", newSlug), zap.String("actor", actor))
	return nil
}

// DeleteProject removes a project. It resolves projectSlug to an id first,
// then wraps the store's DeleteProject in s.store.WithTx together with its
// success audit event. The store rejects a non-empty project with
// store.ErrConflict; that rejection is itself audited here as a FAILURE
// event (ReasonCategory "conflict") even though nothing was removed, so a
// delete attempt against a project that still has environments leaves a
// record.
func (s *Service) DeleteProject(ctx context.Context, projectSlug, actor string) error {
	if err := s.Bind(ctx); err != nil {
		return err
	}
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditProjectDelete, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor, ProjectSlug: projectSlug,
		})
		return err
	}
	p, err := s.store.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		return fail(err)
	}
	evID, err := newID()
	if err != nil {
		return err
	}
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.DeleteProject(ctx, p.ID); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditProjectDelete, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug,
		})
		return err
	})
	if txErr != nil {
		return fail(txErr)
	}
	s.log.Info("project deleted", zap.String("project", projectSlug), zap.String("actor", actor))
	return nil
}

// DeleteEnvironment removes an environment, scoped to projectSlug. It
// resolves both to ids first via s.resolve, then wraps the store's
// DeleteEnvironment in s.store.WithTx together with its success audit event.
// The store rejects a non-empty environment (including one with only
// soft-deleted secrets still referencing it) with store.ErrConflict; that
// rejection is audited as a FAILURE event (ReasonCategory "conflict") even
// though nothing was removed.
func (s *Service) DeleteEnvironment(ctx context.Context, projectSlug, envSlug, actor string) error {
	if err := s.Bind(ctx); err != nil {
		return err
	}
	fail := func(err error) error {
		s.appendAuditBestEffort(ctx, store.AuditEvent{
			EventType: store.AuditEnvironmentDelete, Outcome: store.AuditFailure,
			ReasonCategory: reasonCategory(err), ActorLabel: actor,
			ProjectSlug: projectSlug, EnvironmentSlug: envSlug,
		})
		return err
	}
	sc, err := s.resolve(ctx, projectSlug, envSlug)
	if err != nil {
		return fail(err)
	}
	evID, err := newID()
	if err != nil {
		return err
	}
	txErr := s.store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.DeleteEnvironment(ctx, sc.env.ID); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, store.AuditEvent{
			ID: evID, EventType: store.AuditEnvironmentDelete, Outcome: store.AuditSuccess,
			ActorLabel: actor, ProjectSlug: projectSlug, EnvironmentSlug: envSlug,
		})
		return err
	})
	if txErr != nil {
		return fail(txErr)
	}
	s.log.Info("environment deleted", zap.String("project", projectSlug), zap.String("environment", envSlug), zap.String("actor", actor))
	return nil
}
