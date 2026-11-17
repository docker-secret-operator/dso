package audit

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// DurableSink is a local, append-only, hash-chained JSON-lines audit file.
//
// It is deliberately NOT the central vault database. The chain is the same
// plain (unkeyed) SHA-256 construction ratified for the central store: it is
// tamper-EVIDENT for in-place edits and reordering of the records present, and
// it does NOT detect deletion of the tail or replacement of the whole file by
// an older valid copy, and anyone who can write the file can rebuild a valid
// chain. A keyed chain (G025) is a separate decision and is not claimed here.
//
// Records hold metadata only (action, actor, provider, secret NAME, container
// id, status); never a secret value, credential, password or address.
type DurableSink struct {
	mu     sync.Mutex
	f      auditFile
	path   string
	seq    uint64
	prev   [sha256.Size]byte
	off    int64 // byte length of the verified, acknowledged prefix
	broken error // set when a failed append could not be rolled back
}

// auditFile is the subset of *os.File the sink needs (a seam for fault tests).
type auditFile interface {
	io.Writer
	Sync() error
	Truncate(int64) error
	Seek(int64, int) (int64, error)
	Close() error
}

// Record is one line of the durable audit file.
type Record struct {
	Seq       uint64 `json:"seq"`
	Time      string `json:"time"`
	Event     string `json:"event"`
	User      string `json:"user"`
	Provider  string `json:"provider,omitempty"`
	Secret    string `json:"secret_name,omitempty"` // #nosec G117 -- holds the secret NAME (metadata), never a value
	Container string `json:"container_id,omitempty"`
	Status    string `json:"status"`
	Prev      string `json:"prev_hash"`
	Hash      string `json:"hash"`
}

// ErrChain reports a record that fails chain verification.
var ErrChain = errors.New("audit chain verification failed")

func field(b *bytes.Buffer, s string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(s)))
	b.Write(n[:])
	b.WriteString(s)
}

func recordHash(prev [sha256.Size]byte, r Record) [sha256.Size]byte {
	var b bytes.Buffer
	b.WriteString("dso-audit-v1\x00")
	b.Write(prev[:])
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], r.Seq)
	b.Write(n[:])
	for _, s := range []string{r.Time, r.Event, r.User, r.Provider, r.Secret, r.Container, r.Status} {
		field(&b, s)
	}
	return sha256.Sum256(b.Bytes())
}

// parseChain reads and verifies the file content. It returns the verified
// records, the byte length of the verified prefix, and the chain tail. A final
// line without a terminating newline that does not parse is a torn write from
// a crash (never acknowledged to a caller) and is excluded from the prefix; any
// other malformed or mismatching line is ErrChain.
func parseChain(r io.Reader) (recs []Record, good int64, seq uint64, prev [sha256.Size]byte, err error) {
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) == 0 && rerr != nil {
			return recs, good, seq, prev, nil
		}
		terminated := len(line) > 0 && line[len(line)-1] == '\n'
		var rec Record
		if jerr := json.Unmarshal(bytes.TrimSpace(line), &rec); jerr != nil {
			if !terminated {
				return recs, good, seq, prev, nil // torn tail
			}
			return nil, 0, 0, prev, fmt.Errorf("%w: malformed record after seq %d", ErrChain, seq)
		}
		if rec.Seq != seq+1 {
			return nil, 0, 0, prev, fmt.Errorf("%w: sequence gap at %d", ErrChain, rec.Seq)
		}
		gotPrev, e1 := hex.DecodeString(rec.Prev)
		gotHash, e2 := hex.DecodeString(rec.Hash)
		if e1 != nil || e2 != nil || len(gotPrev) != sha256.Size || len(gotHash) != sha256.Size ||
			subtle.ConstantTimeCompare(gotPrev, prev[:]) != 1 {
			return nil, 0, 0, prev, fmt.Errorf("%w: bad linkage at seq %d", ErrChain, rec.Seq)
		}
		want := recordHash(prev, rec)
		if subtle.ConstantTimeCompare(gotHash, want[:]) != 1 {
			return nil, 0, 0, prev, fmt.Errorf("%w: hash mismatch at seq %d", ErrChain, rec.Seq)
		}
		if !terminated {
			return recs, good, seq, prev, nil // unterminated: never acknowledged, discard
		}
		recs = append(recs, rec)
		good += int64(len(line))
		seq, prev = rec.Seq, want
		if rerr != nil {
			return recs, good, seq, prev, nil
		}
	}
}

// OpenDurable opens (creating if needed) the audit file at path with mode 0600,
// verifies the existing chain, discards a torn final line, and positions for
// append. It refuses a file whose chain does not verify and a file readable or
// writable by group/other.
func OpenDurable(path string) (*DurableSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- operator-configured state path
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*DurableSink, error) { _ = f.Close(); return nil, e }
	st, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fail(fmt.Errorf("audit file %s must not be accessible to group or other (mode %o)", path, st.Mode().Perm()))
	}
	recs, good, seq, prev, err := parseChain(f)
	if err != nil {
		return fail(err)
	}
	_ = recs // good already excludes a torn tail; truncating keeps appends line-aligned.
	if err := f.Truncate(good); err != nil {
		return fail(err)
	}
	if _, err := f.Seek(good, io.SeekStart); err != nil {
		return fail(err)
	}
	return &DurableSink{f: f, path: path, seq: seq, prev: prev, off: good}, nil
}

// Append writes one record and fsyncs before returning.
func (d *DurableSink) Append(event, user, provider, secret, container, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.broken != nil {
		return fmt.Errorf("audit sink disabled after unrecoverable write failure: %w", d.broken)
	}
	rec := Record{
		Seq: d.seq + 1, Time: time.Now().UTC().Format(time.RFC3339Nano),
		Event: event, User: user, Provider: provider, Secret: secret, Container: container, Status: status,
		Prev: hex.EncodeToString(d.prev[:]),
	}
	h := recordHash(d.prev, rec)
	rec.Hash = hex.EncodeToString(h[:])
	line, err := json.Marshal(rec) // #nosec G117 -- Record carries the secret name only
	if err != nil {
		return err
	}
	out := append(line, '\n')
	_, err = d.f.Write(out)
	if err == nil {
		err = d.f.Sync()
	}
	if err != nil {
		// A short write, ENOSPC or failed fsync may leave a partial or
		// unacknowledged line. Roll the file back to the last acknowledged
		// offset so later appends cannot sit behind garbage (which would make
		// the whole chain unverifiable). If the rollback itself fails, stop
		// writing rather than corrupt the chain.
		if terr := d.f.Truncate(d.off); terr != nil {
			d.broken = terr
		} else if _, serr := d.f.Seek(d.off, io.SeekStart); serr != nil {
			d.broken = serr
		}
		return err
	}
	d.seq, d.prev = rec.Seq, h
	d.off += int64(len(out))
	return nil
}

// Close closes the file.
func (d *DurableSink) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.f.Close()
}

// Path returns the file path.
func (d *DurableSink) Path() string { return d.path }

// ReadVerified returns every record of the file at path after verifying the
// whole chain. It never modifies the file.
func ReadVerified(path string) ([]Record, error) {
	f, err := os.Open(path) // #nosec G304 -- operator-configured state path
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recs, _, _, _, err := parseChain(f)
	return recs, err
}

var (
	durable        atomic.Pointer[DurableSink]
	durableFailure atomic.Uint64
)

// SetDurable installs (or, with nil, removes) the process-wide durable sink
// that Log appends every audit event to.
func SetDurable(d *DurableSink) { durable.Store(d) }

// DurablePath returns the active sink's file path, or "" when none is set.
func DurablePath() string {
	if d := durable.Load(); d != nil {
		return d.Path()
	}
	return ""
}

// DurableWriteFailures counts events the durable sink failed to persist.
func DurableWriteFailures() uint64 { return durableFailure.Load() }
