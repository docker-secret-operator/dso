package ledger

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// Open opens the ledger in dir and applies the recovery modes of design §7 in
// their precedence order:
//
//  1. unsupported platform, unsafe state directory, unreadable ledger or lock
//     held elsewhere;
//  2. an unsafe or malformed key (checked before anything is renamed or created,
//     so Generate is never called for such a key);
//  3. a newer schema (the file is left byte-for-byte untouched);
//  4. a corrupt ledger (preserved, then a new ledger);
//  5. an absent or mismatching key (key-loss recovery), or an empty
//     hmac_key_id with a valid key (Option B: adopt when the document holds no
//     key-dependent identity, otherwise key mismatch);
//  6. normal load.
//
// Conditions 1 to 3 start the store in ModeMemoryOnly with every object on
// disk untouched. Open returns an error only for an unusable Config or a
// failure of the random source; every state-directory condition is reported
// in the OpenResult. The caller must Close the store.
func Open(dir string, cfg Config) (*Store, OpenResult, error) {
	if cfg.Keys == nil {
		return nil, OpenResult{}, errors.New("ledger: Config.Keys is required")
	}
	if dir == "" {
		return nil, OpenResult{}, errors.New("ledger: empty state directory")
	}
	s := newStore(dir, cfg)
	res, evs, err := s.open()
	if err != nil {
		_ = s.Close()
		return nil, res, err
	}
	s.emit(evs)
	return s, res, nil
}

// memoryOnly puts the store in a memory-only mode with a fresh in-memory ledger.
func (s *Store) memoryOnly(res *OpenResult, evs *[]Event, r Reason, detail, keyID string) error {
	l, err := newLedger(keyID, s.utcNow(), s.random)
	if err != nil {
		return err
	}
	s.mode, s.reason = ModeMemoryOnly, r
	s.healthy = false
	s.cur.Store(l)
	res.Mode, res.Reason, res.Detail, res.KeyID = ModeMemoryOnly, r, detail, keyID
	*evs = append(*evs, Event{Kind: EventRecovery, Reason: r, Detail: detail, At: s.utcNow()})
	return nil
}

func (s *Store) open() (res OpenResult, evs []Event, err error) {
	s.mode = ModeDurable
	if !securefs.Supported {
		return res, evs, s.memoryOnly(&res, &evs, ReasonUnsupportedPlatform, "this platform cannot verify ownership and permissions", "")
	}
	if err := s.ops.ensureDir(s.dir); err != nil {
		return res, evs, s.memoryOnly(&res, &evs, ReasonUnsafeDir, err.Error(), "")
	}
	release, err := s.ops.lock(s.dir)
	if err != nil {
		r := ReasonUnsafeDir
		if errors.Is(err, ErrLocked) {
			r = ReasonLocked
		}
		return res, evs, s.memoryOnly(&res, &evs, r, err.Error(), "")
	}
	s.release = release
	res.RemovedTemps = s.cleanStaleTemps()
	for _, n := range res.RemovedTemps {
		evs = append(evs, Event{Kind: EventStaleTempRemoved, Detail: n, At: s.utcNow()})
	}

	data, rerr := s.ops.readFile(s.path, fileSizeLimit)
	absent := errors.Is(rerr, fs.ErrNotExist)
	switch {
	case rerr != nil && errors.Is(rerr, ErrTooLarge):
		return res, evs, s.memoryOnly(&res, &evs, ReasonTooLarge, rerr.Error(), "")
	case rerr != nil && !absent:
		return res, evs, s.memoryOnly(&res, &evs, ReasonLedgerUnreadable, rerr.Error(), "")
	}

	ki := s.cfg.Keys.Inspect()
	validID := ""
	if ki.State == KeyValid {
		validID = ki.KeyID
	}
	switch ki.State {
	case KeyUnsafe:
		return res, evs, s.memoryOnly(&res, &evs, ReasonKeyUnsafe, ki.Reason, "")
	case KeyMalformed:
		return res, evs, s.memoryOnly(&res, &evs, ReasonKeyMalformed, ki.Reason, "")
	}

	if absent {
		return s.startFresh(res, evs, ki, ReasonNone)
	}
	l, perr := Unmarshal(data)
	var sv *SchemaVersionError
	switch {
	case errors.As(perr, &sv):
		return res, evs, s.memoryOnly(&res, &evs, ReasonNewerSchema, perr.Error(), validID)
	case perr != nil:
		return s.recoverAndStart(res, evs, ki, ReasonCorrupt, "corrupt")
	}

	switch {
	case ki.State == KeyAbsent:
		return s.recoverAndStart(res, evs, ki, ReasonKeyMissing, "keyloss")
	case ki.KeyID == l.HMACKeyID:
		s.adopt(l, true)
		res.Mode, res.KeyID = ModeDurable, ki.KeyID
	case l.HMACKeyID == "" && !hasHMACIdentity(l):
		// Option B: nothing in the document depends on a key, so the valid key's
		// id is adopted. The write is a normal durable update.
		s.adopt(l, true)
		next := l.Clone()
		next.HMACKeyID = ki.KeyID
		next.Generation = l.Generation + 1
		next.UpdatedAt = s.utcNow()
		s.mu.Lock()
		more, cerr := s.commitLocked(next, len(retryDelays))
		s.mu.Unlock()
		evs = append(evs, more...)
		res.Mode, res.Reason, res.KeyID = ModeDurable, ReasonKeyAdopted, ki.KeyID
		if cerr != nil {
			res.Detail = "adopting the key id could not be persisted: " + cerr.Error()
		}
		evs = append(evs, Event{Kind: EventRecovery, Reason: ReasonKeyAdopted, At: s.utcNow()})
	default:
		return s.recoverAndStart(res, evs, ki, ReasonKeyMismatch, "keyloss")
	}
	s.cancelOnOpen(&res, &evs)
	return res, evs, nil
}

// adopt publishes a document read from disk.
func (s *Store) adopt(l *Ledger, persisted bool) {
	s.cur.Store(l)
	s.hmu.Lock()
	s.persisted = persisted
	s.hmu.Unlock()
}

// recoverAndStart preserves the existing file (kind is the file-name infix) and
// starts a fresh ledger. A preservation failure of any kind leaves the ledger
// and key untouched and starts memory-only.
func (s *Store) recoverAndStart(res OpenResult, evs []Event, ki KeyInspection, why Reason, kind string) (OpenResult, []Event, error) {
	keep := ""
	if ki.State == KeyValid {
		keep = ki.KeyID
	}
	dst, perr := s.preserve(kind)
	if perr != nil {
		return res, evs, s.memoryOnly(&res, &evs, ReasonPreserveFailed, perr.Error(), keep)
	}
	res.Preserved = dst
	evs = append(evs, Event{Kind: EventPreserved, Reason: why, Detail: dst, At: s.utcNow()})
	if kind == "keyloss" {
		// §7: a new key is generated and installed. The ledger was preserved
		// first, so a refused preservation can never have replaced the key.
		ki = KeyInspection{State: KeyAbsent}
	}
	return s.startFresh(res, evs, ki, why)
}

// startFresh creates and persists an empty ledger. A valid key's id is used; an
// absent key is generated (never for unsafe or malformed keys, which were
// handled before).
func (s *Store) startFresh(res OpenResult, evs []Event, ki KeyInspection, why Reason) (OpenResult, []Event, error) {
	keyID := ki.KeyID
	if ki.State != KeyValid {
		id, err := s.cfg.Keys.Generate()
		if err != nil {
			return res, evs, s.memoryOnly(&res, &evs, ReasonKeyGenerateFailed, err.Error(), "")
		}
		keyID = id
	}
	l, err := newLedger(keyID, s.utcNow(), s.random)
	if err != nil {
		return res, evs, err
	}
	l.Generation = 1
	s.adopt(l, false)
	s.mu.Lock()
	more, cerr := s.commitLocked(l.Clone(), len(retryDelays))
	s.mu.Unlock()
	evs = append(evs, more...)
	res.Mode, res.Reason, res.KeyID = ModeDurable, why, keyID
	if cerr != nil {
		res.Detail = "the new ledger could not be persisted: " + cerr.Error()
	}
	if why != ReasonNone {
		evs = append(evs, Event{Kind: EventRecovery, Reason: why, Detail: res.Preserved, At: s.utcNow()})
	}
	s.cancelOnOpen(&res, &evs)
	return res, evs, nil
}

// cancelOnOpen applies the restart-time cancellation of §9.7.5 when the store
// can persist.
func (s *Store) cancelOnOpen(res *OpenResult, evs *[]Event) {
	if !s.PersistenceHealthy() {
		return
	}
	n, more, err := s.cancelUndispatched()
	*evs = append(*evs, more...)
	res.CancelledRequests = n
	if err != nil {
		res.Detail = fmt.Sprintf("cancelling undispatched requests failed: %v", err)
	}
}
