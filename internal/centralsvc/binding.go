package centralsvc

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/store"
)

// Database-to-root-key binding.
//
// Per-project key ids alone cannot stop a second root key from being used on
// the same database: nothing ties the DATABASE to a key, so a different key
// could create new projects whose data keys it wraps itself (a mixed-key
// database, or a substituted key file quietly taking custody of new secrets).
//
// The binding is a canary: a random value wrapped by the root key with an
// AAD that names the key id, stored in store_meta. Verification does NOT trust
// any stored text: it asks the configured KeyProvider to unwrap the canary
// under ITS OWN id. Editing the stored key-id field therefore cannot make a
// wrong key pass (and cannot make a right key fail); that field exists only to
// give a better error message. An attacker with write access can at worst
// replace the binding with one for their own key, which makes the legitimate
// key fail closed (denial of service) -- they cannot read or add data under
// the real key.
const (
	metaBindingVersion = "binding.version"
	metaBindingKeyID   = "binding.root_key_id"
	metaBindingCanary  = "binding.canary"
	bindingFormat      = "1"
)

// ErrBindingCorrupt means the key-binding metadata is incomplete or does not
// verify under a key that claims to match it. The service refuses to operate.
var ErrBindingCorrupt = errors.New("centralsvc: database key-binding metadata is missing parts or corrupted; refusing to operate (restore from a backup, or investigate tampering)")

// Bind verifies that the configured root key is the one this database is bound
// to, creating the binding atomically when the database has none. Every other
// Service method calls it first, so a wrong key is rejected before any read or
// write; callers (such as `central init`) may call it directly to establish
// the binding deliberately.
//
// An unbound database is only bound when the key can actually unwrap EVERY
// project data key already in it, so binding can never adopt a database on
// behalf of the wrong key.
func (s *Service) Bind(ctx context.Context) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if s.bound {
		return nil
	}
	if err := s.verifyOrCreateBinding(ctx); err != nil {
		return err
	}
	s.bound = true
	return nil
}

func (s *Service) verifyOrCreateBinding(ctx context.Context) error {
	state, err := s.readBinding(ctx)
	if err != nil {
		return err
	}
	switch state.parts {
	case 0:
		if err := s.createBinding(ctx); err != nil {
			return err
		}
		// Whether we created it or lost a race to another initialiser, the
		// stored binding is now authoritative: verify against it.
		state, err = s.readBinding(ctx)
		if err != nil {
			return err
		}
		if state.parts != 3 {
			return ErrBindingCorrupt
		}
		return s.verifyBinding(state)
	case 3:
		return s.verifyBinding(state)
	default:
		return ErrBindingCorrupt
	}
}

type bindingState struct {
	parts   int // how many of the three rows exist
	version []byte
	keyID   []byte
	canary  []byte
}

func (s *Service) readBinding(ctx context.Context) (bindingState, error) {
	// One statement => one consistent snapshot. Three separate reads could
	// interleave with a concurrent first initialiser and see a half-written
	// binding that does not exist at any single moment.
	entries, err := s.store.ListMeta(ctx, "binding.")
	if err != nil {
		return bindingState{}, fmt.Errorf("reading database metadata: %w", err)
	}
	var st bindingState
	for _, e := range entries {
		switch e.Key {
		case metaBindingVersion:
			st.version = e.Value
		case metaBindingKeyID:
			st.keyID = e.Value
		case metaBindingCanary:
			st.canary = e.Value
		default:
			continue
		}
		st.parts++
	}
	return st, nil
}

func (s *Service) verifyBinding(st bindingState) error {
	if string(st.version) != bindingFormat {
		return fmt.Errorf("%w (unsupported binding format)", ErrBindingCorrupt)
	}
	id := s.keys.ID()
	canary, err := s.keys.Unwrap(context.Background(), st.canary, crypto.BindingAAD(id))
	if err == nil && len(canary) == crypto.KeySize {
		crypto.Zero(canary)
		return nil
	}
	crypto.Zero(canary)
	if string(st.keyID) == id {
		// The metadata names this very key but the canary does not open: the
		// canary was altered or damaged, not a different key presented.
		return ErrBindingCorrupt
	}
	return fmt.Errorf("%w: this database is bound to root key %q but the configured key is %q",
		ErrWrongRootKey, string(st.keyID), id)
}

func (s *Service) createBinding(ctx context.Context) error {
	id := s.keys.ID()
	// Adopt an unbound database only if this key owns everything already in it.
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if p.DEKKeyID != id {
			return fmt.Errorf("%w: project %q is protected by root key %q, not %q, and the database has no key binding",
				ErrWrongRootKey, p.Slug, p.DEKKeyID, id)
		}
		dek, err := s.keys.Unwrap(ctx, p.WrappedDEK, crypto.DEKAAD(p.ID, p.DEKKeyID, p.DEKVersion))
		if err != nil {
			return fmt.Errorf("%w: project %q's data key does not unwrap with the configured key", ErrWrongRootKey, p.Slug)
		}
		crypto.Zero(dek)
	}
	canary, err := crypto.GenerateKey()
	if err != nil {
		return err
	}
	defer crypto.Zero(canary)
	wrapped, err := s.keys.Wrap(ctx, canary, crypto.BindingAAD(id))
	if err != nil {
		return fmt.Errorf("wrapping key-binding canary: %w", err)
	}
	// All three rows or none, in one transaction: a concurrent first
	// initialisation has exactly one winner.
	_, err = s.store.PutMetaIfAbsent(ctx, []store.MetaEntry{
		{Key: metaBindingVersion, Value: []byte(bindingFormat)},
		{Key: metaBindingKeyID, Value: []byte(id)},
		{Key: metaBindingCanary, Value: wrapped},
	})
	return err
}
