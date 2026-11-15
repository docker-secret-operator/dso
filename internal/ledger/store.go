package ledger

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Store is the single writer of ledger.json (design §8.1). Reads are lock-free
// copies of an immutable published document; every mutation goes through update
// (durable) or updateMemoryOnly (memory-only modes), on a deep clone, and the
// published document is replaced only after the new one is durable. Both are
// unexported on purpose (design section 9, O-9): they mutate the whole
// document, so production code gets only the narrow Versions facade.
type Store struct {
	dir, path string
	ops       *fileOps
	cfg       Config
	now       func() time.Time
	random    io.Reader
	after     func(time.Duration) <-chan time.Time

	mode   Mode
	reason Reason

	mu  sync.Mutex // serializes all mutation, including retries
	cur atomic.Pointer[Ledger]

	hmu          sync.Mutex // guards the fields below
	persisted    bool       // published document equals the file
	healthy      bool
	failures     int
	lastErr      string
	lastSuccess  time.Time
	probing      bool
	closed       bool
	liveRequests map[string]bool // request ids created by this process

	done    chan struct{}
	wg      sync.WaitGroup
	release func() error
}

// Persistence attempts per update and their delays (design §8.3 R3, D-S2-6).
var retryDelays = [3]time.Duration{0, 250 * time.Millisecond, time.Second}

const (
	probeMin = 5 * time.Second
	probeMax = 60 * time.Second
)

func newStore(dir string, cfg Config) *Store {
	s := &Store{
		dir: dir, path: filepath.Join(dir, "ledger.json"),
		ops: cfg.ops, cfg: cfg, now: cfg.Now, random: cfg.Random, after: cfg.After,
		healthy: true, liveRequests: map[string]bool{}, done: make(chan struct{}),
	}
	if s.ops == nil {
		s.ops = realOps()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.random == nil {
		s.random = rand.Reader
	}
	if s.after == nil {
		s.after = time.After
	}
	return s
}

func (s *Store) utcNow() time.Time { return s.now().UTC() }

// emit delivers events outside every store lock. A panicking callback is
// recovered: it never affects an update that already happened.
func (s *Store) emit(evs []Event) {
	if s.cfg.Events == nil {
		return
	}
	for _, e := range evs {
		func() {
			defer func() { _ = recover() }()
			s.cfg.Events(e)
		}()
	}
}

func (s *Store) isClosed() bool {
	s.hmu.Lock()
	defer s.hmu.Unlock()
	return s.closed
}

// Get returns an independent copy of one entry.
func (s *Store) Get(k Key) (Entry, bool) {
	l := s.cur.Load()
	for i := range l.Entries {
		if l.Entries[i].Key == k {
			return cloneEntry(l.Entries[i]), true
		}
	}
	return Entry{}, false
}

// Snapshot returns an independent copy of the published document and whether it
// is durable.
func (s *Store) Snapshot() Snapshot {
	s.hmu.Lock()
	durable := s.mode == ModeDurable && s.persisted
	s.hmu.Unlock()
	return Snapshot{Ledger: s.cur.Load().Clone(), Durable: durable, Mode: s.mode}
}

// PersistenceHealthy reports whether durable persistence is available.
func (s *Store) PersistenceHealthy() bool {
	s.hmu.Lock()
	defer s.hmu.Unlock()
	return s.mode == ModeDurable && s.healthy
}

// Gate returns nil while durable persistence is available. It is ADVISORY
// (owner decision O-3): it only selects durable versus memory-only handling. It
// is never a precondition for, and never gates, a Docker mutation: dispatch is
// decided by the M0 operation guard alone, and a ledger problem never stops a
// rotation that M0 would permit.
func (s *Store) Gate() error {
	if !s.PersistenceHealthy() {
		return ErrPersistenceUnavailable
	}
	return nil
}

// Health returns a snapshot of persistence health.
func (s *Store) Health() Health {
	s.hmu.Lock()
	defer s.hmu.Unlock()
	return Health{
		Mode: s.mode, Reason: s.reason, Healthy: s.mode == ModeDurable && s.healthy,
		Durable: s.mode == ModeDurable && s.persisted, ConsecutiveFailures: s.failures,
		LastError: s.lastErr, LastSuccess: s.lastSuccess,
		Generation: s.cur.Load().Generation, ProbeRunning: s.probing,
	}
}

// update applies fn to a deep clone of the published ledger, validates the
// result, writes it durably and only then publishes a fresh clone of what was
// written. fn runs exactly once per call and must be idempotent, because a
// caller may retry update after ErrPersistenceFailed. The store, not fn, sets
// generation and updated_at. Validation errors and fn's own errors write
// nothing, leave memory unchanged and do not affect health (M-S2-2); a failed
// durable write, including a size-cap refusal, does.
//
// Durability guarantee. The candidate is published in memory only after the
// whole write succeeded: temporary file, fsync, rename and directory fsync. A
// failed update therefore always leaves the published document unchanged, but it
// does NOT necessarily leave the previous document on disk. If the rename
// succeeded and the directory fsync then failed, the file may already hold the
// complete candidate, so memory and disk can temporarily represent different
// generations. After a crash either the previous or the complete candidate
// document may be what a restart finds; both are complete, valid documents. An
// update that failed is never reported as successful, and a later successful
// write (including the background probe) replaces whatever the uncertain write
// left. Because the candidate was never acknowledged, the later document is
// built from the published state, so two different documents can carry the same
// generation number; generation is a diagnostic counter.
//
// fn may return ErrNoChange to end the update without any write.
func (s *Store) update(fn func(*Ledger) error) error {
	if fn == nil {
		return errors.New("ledger: nil update function")
	}
	if s.isClosed() {
		return ErrClosed
	}
	if s.mode != ModeDurable {
		return ErrPersistenceUnavailable
	}
	s.mu.Lock()
	evs, err := s.updateLocked(fn, len(retryDelays))
	s.mu.Unlock()
	s.emit(evs)
	return err
}

// updateLocked is update with s.mu held. attempts bounds the durable write.
func (s *Store) updateLocked(fn func(*Ledger) error, attempts int) ([]Event, error) {
	base := s.cur.Load()
	next := base.Clone()
	if err := fn(next); err != nil {
		if errors.Is(err, ErrNoChange) {
			return nil, nil
		}
		return nil, err
	}
	next.Generation = base.Generation + 1
	next.UpdatedAt = s.utcNow()
	return s.commitLocked(next, attempts)
}

// commitLocked marshals, writes (with bounded retries) and publishes next.
func (s *Store) commitLocked(next *Ledger, attempts int) ([]Event, error) {
	data, err := Marshal(next)
	if err != nil {
		return nil, err // validation or encoding: not a persistence failure
	}
	published, err := Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("ledger: internal error: a marshalled document does not parse: %w", err)
	}
	var evs []Event
	if int64(len(data)) > fileSizeLimit {
		err := fmt.Errorf("%w: %w: %d bytes, limit %d", ErrPersistenceFailed, ErrTooLarge, len(data), fileSizeLimit)
		s.noteFailure(err, &evs)
		return evs, err
	}
	var last error
	for i := 0; i < attempts; i++ {
		if d := retryDelays[i]; d > 0 {
			select {
			case <-s.after(d):
			case <-s.done:
				return evs, ErrClosed
			}
		}
		if last = s.writeOnce(data); last == nil {
			s.cur.Store(published)
			s.noteSuccess(&evs)
			return evs, nil
		}
	}
	err = fmt.Errorf("%w: %w", ErrPersistenceFailed, last)
	s.noteFailure(err, &evs)
	return evs, err
}

func tempName(random io.Reader) (string, error) {
	var b [8]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", err
	}
	return "ledger.json.tmp-" + hex.EncodeToString(b[:]), nil
}

// writeOnce is one pass of the §8.2 write algorithm: exclusive temp file in the
// same directory, write, fsync, close, atomic rename over ledger.json, fsync of
// the directory. On any failure before the rename our own temp file is removed
// best-effort and the previous document stays on disk. If the directory fsync
// fails after the rename the file already holds the complete candidate (its
// content was fsynced before the rename; only the directory entry is uncertain),
// but the outcome is reported as a failure and memory is left unchanged, so the
// caller never claims durability it cannot show. See update for the guarantee.
func (s *Store) writeOnce(data []byte) error {
	name, err := tempName(s.random)
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, name)
	f, err := s.ops.createTemp(tmp)
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	fail := func(what string, err error) error {
		_ = f.Close()
		_ = s.ops.remove(tmp)
		return fmt.Errorf("%s: %w", what, err)
	}
	if n, err := f.Write(data); err != nil || n != len(data) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return fail("writing temporary file", err)
	}
	if err := f.Sync(); err != nil {
		return fail("fsync of temporary file", err)
	}
	if err := f.Close(); err != nil {
		_ = s.ops.remove(tmp)
		return fmt.Errorf("closing temporary file: %w", err)
	}
	if err := s.ops.rename(tmp, s.path); err != nil {
		_ = s.ops.remove(tmp)
		return fmt.Errorf("renaming temporary file: %w", err)
	}
	if err := s.ops.syncDir(s.dir); err != nil {
		return fmt.Errorf("fsync of directory (rename durability unknown): %w", err)
	}
	return nil
}

func (s *Store) noteSuccess(evs *[]Event) {
	s.hmu.Lock()
	defer s.hmu.Unlock()
	was := s.healthy
	s.healthy, s.persisted, s.failures, s.lastErr = true, true, 0, ""
	s.lastSuccess = s.utcNow()
	if !was {
		*evs = append(*evs, Event{Kind: EventPersistenceRestored, At: s.utcNow()})
	}
}

func (s *Store) noteFailure(err error, evs *[]Event) {
	s.hmu.Lock()
	was := s.healthy
	s.healthy = false
	s.failures++
	s.lastErr = err.Error()
	s.hmu.Unlock()
	if was {
		*evs = append(*evs, Event{Kind: EventPersistenceDegraded, Err: err, At: s.utcNow()})
	}
	s.startProbe()
}

// startProbe starts the single background probe if none runs (single-flight).
func (s *Store) startProbe() {
	s.hmu.Lock()
	defer s.hmu.Unlock()
	if s.probing || s.closed || s.mode != ModeDurable {
		return
	}
	s.probing = true
	s.wg.Add(1)
	go s.probeLoop()
}

// probeLoop runs while persistence is degraded, with capped backoff and no hot
// loop (design §8.3 R3).
func (s *Store) probeLoop() {
	defer s.wg.Done()
	delay := probeMin
	for {
		select {
		case <-s.done:
			s.hmu.Lock()
			s.probing = false
			s.hmu.Unlock()
			return
		case <-s.after(delay):
		}
		s.probeOnce()
		s.hmu.Lock()
		if s.healthy || s.closed {
			s.probing = false
			s.hmu.Unlock()
			return
		}
		s.hmu.Unlock()
		if delay *= 2; delay > probeMax {
			delay = probeMax
		}
	}
}

// probeOnce first runs the flush hook; with nothing held it makes a harmless
// write (a generation bump) under the store mutex.
func (s *Store) probeOnce() {
	if s.cfg.Flush != nil {
		flushed, err := s.cfg.Flush()
		if err != nil {
			s.emit([]Event{{Kind: EventProbe, Err: err, At: s.utcNow()}})
			return
		}
		if flushed {
			s.emit([]Event{{Kind: EventProbe, Detail: "held record flushed", At: s.utcNow()}})
			return
		}
	}
	if s.isClosed() {
		return
	}
	s.mu.Lock()
	evs, err := s.updateLocked(func(*Ledger) error { return nil }, 1)
	s.mu.Unlock()
	evs = append(evs, Event{Kind: EventProbe, Err: err, At: s.utcNow()})
	s.emit(evs)
}

// updateMemoryOnly applies fn to an in-memory working copy in a memory-only
// mode. It performs no disk access, does not bump the generation and never
// claims durability: Snapshot reports Durable=false (owner decision D-S2-7).
func (s *Store) updateMemoryOnly(fn func(*Ledger) error) error {
	if fn == nil {
		return errors.New("ledger: nil update function")
	}
	if s.isClosed() {
		return ErrClosed
	}
	if s.mode != ModeMemoryOnly {
		return ErrNotMemoryOnly
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cur.Load().Clone()
	if err := fn(next); err != nil {
		if errors.Is(err, ErrNoChange) {
			return nil
		}
		return err
	}
	probe := *next
	probe.BodySHA256 = ""
	if err := probe.Validate(); err != nil {
		return err
	}
	s.cur.Store(normalized(next).Clone())
	return nil
}

// Close stops the background probe, rejects further updates and releases the
// ledger lock. It is safe to call more than once.
func (s *Store) Close() error {
	s.hmu.Lock()
	if s.closed {
		s.hmu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	s.hmu.Unlock()
	s.wg.Wait()
	s.mu.Lock()   // wait for an in-flight update to end
	s.mu.Unlock() //nolint:staticcheck // SA2001: intentional barrier, waits for any in-flight update to finish
	if s.release != nil {
		return s.release()
	}
	return nil
}
