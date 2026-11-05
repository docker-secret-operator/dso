package centralsvc

// CheckInitialized answers, without modifying anything, whether the file at a
// path is an initialized DSO Central database. It is a guard against
// accidental path mistakes and incomplete initialization (a mistyped --db, a
// zero-byte or foreign SQLite file, an interrupted `init`); it is NOT a
// defence against a hostile or concurrent replacement of the file, and it does
// not look at any root key. Key ownership stays entirely with Bind.
//
// The evidence is gathered by ONE probe that runs through
// sqlite.InspectDatabase, so every read comes from a single read-only
// transaction on a verified view. The probe only collects facts and returns nil;
// the facts are classified AFTER InspectDatabase has returned successfully, so
// a policy outcome (not initialized, corrupt binding, schema too new) can never
// be confused with an inspection failure, which InspectDatabase wraps in
// sqlite.ErrInspectionFailed. Only a genuine failure to read the database is
// inconclusive.
//
// Query failures and the inspection's own recovery. InspectDatabase reacts to an
// error returned by the probe: SQLITE_BUSY/LOCKED is retried in the WAL-aware
// view, and any error from an immutable-view probe makes it fall back to the
// WAL-aware view, where SQLite creates -wal and -shm files. A deterministic query
// failure (for example "no such column" because a foreign table has the name of a
// DSO table) gains nothing from either reaction, and the fallback would change the
// database directory. So the probe captures such a failure (SQLite's generic
// SQLITE_ERROR, or a non-SQLite error such as a scan type mismatch) and returns
// nil; the verification InspectDatabase wraps around the probe still runs. After
// it returns, a captured failure is reported as *InconclusiveError and the partial
// evidence is NOT classified. Every other SQLite error (busy, locked, corrupt,
// I/O, cannot open, ...) is returned from the probe unchanged, so InspectDatabase
// keeps its bounded retry of transient locks and its fall-back from a torn
// immutable read. The captured failure and the evidence are reset at the start of
// every probe invocation, so a discarded or retried attempt can never contaminate
// the one whose result is used.
//
// What the result means:
//
//   - nil: the database is initialized (the complete binding triple, version
//     "1", is present), or it is unbound but holds at least one project and every
//     project row is well formed (a pre-binding schema-1 database, or one whose
//     binding rows were removed). In the second case Bind still has to prove the
//     configured key can unwrap EVERY project's data key before it creates a
//     binding, so this check never decides ownership.
//   - ErrBindingCorrupt: one or two binding rows, an unsupported binding version,
//     or project rows with empty key data.
//   - store.ErrSchemaTooNew: the database was written by a newer DSO.
//   - *NotInitializedError (errors.Is ErrNotInitialized): the file is empty, is
//     not SQLite, has no DSO schema, or has the schema but no binding and no
//     projects. When the result came from an immutable view it says so and tells
//     the caller to retry, because that view is verified stable, not proof.
//   - *InconclusiveError (errors.Is sqlite.ErrInspectionFailed): the database
//     could not be inspected safely, or a negative finding was made while a
//     non-empty -wal is present. SQLite ignores WAL frames that fail their
//     checksums, so an unreplayed or damaged WAL looks exactly like missing
//     data; that must never be reported as "not initialized".
//   - any other error (for example the securefs refusal of a symlink or an unsafe
//     mode) is passed through unchanged.
//
// Interrupted-initialization recovery (preserve first). When the evidence PROVES
// the database is an empty DSO schema (the exact expected table set, only
// embedded migration versions, zero rows everywhere else, no binding rows, no
// foreign tables), the error carries recovery guidance. The guidance never asks
// the program to delete anything and always starts with copying the files,
// because a damaged WAL can hold transactions this inspection cannot see, so no
// inspection result can prove a database with a WAL is disposable. If the proof
// fails there is no guidance at all.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ErrNotInitialized is matched (errors.Is) by *NotInitializedError.
var ErrNotInitialized = errors.New("centralsvc: database is not an initialized DSO Central database")

// InitReason says why a database was classified as not initialized.
type InitReason string

const (
	ReasonEmptyFile           InitReason = "the file is empty"
	ReasonNotSQLite           InitReason = "the file is not a SQLite database"
	ReasonNoSchema            InitReason = "it has no DSO Central schema"
	ReasonNoBindingNoProjects InitReason = "it has no key binding and holds no projects"
)

// NotInitializedError is a definitive (or, for an immutable view, verified
// stable) finding that the database is not initialized.
type NotInitializedError struct {
	Path   string
	Reason InitReason
	// Retry is true when the finding came from an immutable view, which is
	// verified stable but not proof.
	Retry bool
	// Guidance is preserve-first recovery guidance; empty unless the emptiness
	// proof held.
	Guidance []string
	cause    error // sqlite.ErrEmptyDatabase / ErrNotSQLite, kept reachable
}

func (e *NotInitializedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "database %s is not an initialized DSO Central database: %s", e.Path, e.Reason)
	if e.Retry {
		b.WriteString(" (if another dso command may be writing to it, wait and retry)")
	}
	for _, g := range e.Guidance {
		b.WriteString("\n" + g)
	}
	return b.String()
}

// Is makes errors.Is(err, ErrNotInitialized) true, and only that.
func (e *NotInitializedError) Is(target error) bool { return target == ErrNotInitialized }

// Unwrap keeps the file-level cause (sqlite.ErrEmptyDatabase, ErrNotSQLite) reachable.
func (e *NotInitializedError) Unwrap() error { return e.cause }

// InconclusiveError means initialization could not be established either way.
// It matches sqlite.ErrInspectionFailed and never ErrNotInitialized.
type InconclusiveError struct {
	Path   string
	Reason string
	// WALPresent: a non-empty -wal was observed, so a missing schema or binding
	// cannot be told apart from a damaged or unreplayed WAL.
	WALPresent bool
	// Guidance is preserve-first guidance; empty unless the emptiness proof held.
	Guidance []string
	cause    error
}

func (e *InconclusiveError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "could not determine whether database %s is initialized: %s", e.Path, e.Reason)
	for _, g := range e.Guidance {
		b.WriteString("\n" + g)
	}
	return b.String()
}

// Is makes errors.Is(err, sqlite.ErrInspectionFailed) true, and only that.
func (e *InconclusiveError) Is(target error) bool { return target == sqlite.ErrInspectionFailed }

// Unwrap exposes the underlying inspection error, if there was one.
func (e *InconclusiveError) Unwrap() error { return e.cause }

// dsoTables is the exact table set of the current schema (the embedded
// migrations), hardcoded here on purpose; a test compares it with a freshly
// migrated database so it cannot drift silently. sqlite_sequence (created for
// the AUTOINCREMENT audit_events table) is expected in addition and optional.
var dsoTables = []string{
	"schema_migrations", "projects", "environments", "secrets",
	"secret_versions", "store_meta", "audit_events",
}

const sqliteSequenceTable = "sqlite_sequence"

var bindingKeys = []string{metaBindingVersion, metaBindingKeyID, metaBindingCanary}

// initEvidence holds the facts one probe invocation read.
type initEvidence struct {
	tables         map[string]bool
	projectsOK     bool // the projects table has the DSO columns
	versions       []int
	bindingPresent map[string][]byte
	totalProjects  int
	wellFormed     int
	rows           map[string]int64 // row count per existing DSO table except schema_migrations, plus sqlite_sequence
}

type inspectFunc func(path string, probe func(sqlite.Querier) error) (sqlite.InspectionResult, error)

// CheckInitialized classifies the database at path; see the file comment.
func CheckInitialized(path string) error {
	return checkInitializedWith(sqlite.InspectDatabase, path)
}

func checkInitializedWith(inspect inspectFunc, path string) error {
	latest, err := sqlite.LatestSchemaVersion()
	if err != nil {
		return fmt.Errorf("determining supported schema version: %w", err)
	}
	migs, err := sqlite.EmbeddedMigrations()
	if err != nil {
		return fmt.Errorf("loading embedded migrations: %w", err)
	}
	embedded := make(map[int]bool, len(migs))
	for _, m := range migs {
		embedded[m.Version] = true
	}

	var ev initEvidence
	var queryErr error
	res, err := inspect(path, func(q sqlite.Querier) error {
		// Reset on EVERY invocation: results of earlier, discarded attempts
		// must never reach the one whose result is used.
		ev, queryErr = initEvidence{}, nil
		got, perr := collectEvidence(q)
		if perr != nil {
			if leaveToInspection(perr) {
				return perr
			}
			queryErr = perr // a deterministic query failure: record it, change nothing
			return nil
		}
		ev = got
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, sqlite.ErrEmptyDatabase):
			return &NotInitializedError{Path: path, Reason: ReasonEmptyFile, cause: err}
		case errors.Is(err, sqlite.ErrNotSQLite):
			return &NotInitializedError{Path: path, Reason: ReasonNotSQLite, cause: err}
		case errors.Is(err, sqlite.ErrInspectionFailed):
			return &InconclusiveError{Path: path, Reason: "the database could not be inspected safely: " + err.Error(), cause: err}
		}
		return err // securefs refusals and other errors pass through unchanged
	}
	if queryErr != nil {
		// The winning attempt could not read the database: never classify the
		// partial evidence, and never report "not initialized".
		return &InconclusiveError{
			Path:       path,
			Reason:     "reading the database failed: " + queryErr.Error(),
			WALPresent: res.WALPresent,
			cause:      queryErr,
		}
	}
	return classify(path, ev, res, latest, embedded)
}

// leaveToInspection reports whether err must be returned from the probe so that
// sqlite.InspectDatabase's own handling applies: any SQLite error other than the
// generic SQLITE_ERROR (primary code 1). That covers busy/locked (bounded
// retry), corrupt/not-a-database (a torn immutable read falls back to the
// WAL-aware view) and I/O or open failures. SQLITE_ERROR ("no such table",
// "no such column", ...) and non-SQLite errors (scan type mismatches) are
// deterministic and are captured by the probe instead.
func leaveToInspection(err error) bool {
	var coded interface{ Code() int }
	if !errors.As(err, &coded) {
		return false
	}
	return coded.Code()&0xff != 1
}

// collectEvidence reads facts only; it never decides anything.
func collectEvidence(q sqlite.Querier) (initEvidence, error) {
	bg := context.Background()
	ev := initEvidence{
		tables:         map[string]bool{},
		bindingPresent: map[string][]byte{},
		rows:           map[string]int64{},
	}

	rows, err := q.QueryContext(bg, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return initEvidence{}, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return initEvidence{}, err
		}
		ev.tables[n] = true
	}
	if err := rows.Close(); err != nil {
		return initEvidence{}, err
	}
	if err := rows.Err(); err != nil {
		return initEvidence{}, err
	}

	if ev.tables["schema_migrations"] {
		rows, err := q.QueryContext(bg, `SELECT version FROM schema_migrations ORDER BY version`)
		if err != nil {
			return initEvidence{}, err
		}
		for rows.Next() {
			var v int
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return initEvidence{}, err
			}
			ev.versions = append(ev.versions, v)
		}
		if err := rows.Close(); err != nil {
			return initEvidence{}, err
		}
		if err := rows.Err(); err != nil {
			return initEvidence{}, err
		}
	}

	if ev.tables["projects"] {
		cols := map[string]bool{}
		rows, err := q.QueryContext(bg, `SELECT name FROM pragma_table_info('projects')`)
		if err != nil {
			return initEvidence{}, err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return initEvidence{}, err
			}
			cols[n] = true
		}
		if err := rows.Close(); err != nil {
			return initEvidence{}, err
		}
		ev.projectsOK = cols["wrapped_dek"] && cols["dek_key_id"]
		if ev.projectsOK {
			var wf *int
			if err := q.QueryRowContext(bg, `SELECT count(*), sum(CASE WHEN length(wrapped_dek) > 0 AND length(dek_key_id) > 0 THEN 1 ELSE 0 END) FROM projects`).
				Scan(&ev.totalProjects, &wf); err != nil {
				return initEvidence{}, err
			}
			if wf != nil {
				ev.wellFormed = *wf
			}
		}
	}

	if ev.tables["store_meta"] {
		rows, err := q.QueryContext(bg, `SELECT key, value FROM store_meta WHERE key IN (?, ?, ?)`,
			metaBindingVersion, metaBindingKeyID, metaBindingCanary)
		if err != nil {
			return initEvidence{}, err
		}
		for rows.Next() {
			var k string
			var v []byte
			if err := rows.Scan(&k, &v); err != nil {
				rows.Close()
				return initEvidence{}, err
			}
			ev.bindingPresent[k] = v
		}
		if err := rows.Close(); err != nil {
			return initEvidence{}, err
		}
		if err := rows.Err(); err != nil {
			return initEvidence{}, err
		}
	}

	// Row counts for the emptiness proof. Table names come from the fixed
	// list above, never from the database.
	for _, t := range append(append([]string{}, dsoTables[1:]...), sqliteSequenceTable) {
		if !ev.tables[t] {
			continue
		}
		var n int64
		if err := q.QueryRowContext(bg, `SELECT count(*) FROM "`+t+`"`).Scan(&n); err != nil {
			return initEvidence{}, err
		}
		ev.rows[t] = n
	}
	return ev, nil
}

// hasDSOSchema: the tables and columns that make a file recognisably a DSO
// Central database (store_meta and audit_events are later additions, so a
// schema-1 database lacks them).
func (ev *initEvidence) hasDSOSchema() bool {
	for _, t := range []string{"schema_migrations", "projects", "environments", "secrets", "secret_versions"} {
		if !ev.tables[t] {
			return false
		}
	}
	return ev.projectsOK
}

// provenEmpty is the emptiness proof for the recovery guidance.
func (ev *initEvidence) provenEmpty(embedded map[int]bool) bool {
	known := map[string]bool{sqliteSequenceTable: true}
	for _, t := range dsoTables {
		known[t] = true
		if !ev.tables[t] { // exact expected set: every table must exist
			return false
		}
	}
	for t := range ev.tables {
		if !known[t] { // no foreign tables
			return false
		}
	}
	if len(ev.versions) == 0 {
		return false
	}
	for _, v := range ev.versions { // only embedded migration versions
		if !embedded[v] {
			return false
		}
	}
	if len(ev.bindingPresent) != 0 { // no binding rows
		return false
	}
	for _, n := range ev.rows { // zero rows everywhere else
		if n != 0 {
			return false
		}
	}
	return true
}

func classify(path string, ev initEvidence, res sqlite.InspectionResult, latest int, embedded map[int]bool) error {
	newest := 0
	for _, v := range ev.versions {
		if v > newest {
			newest = v
		}
	}
	if ev.tables["schema_migrations"] && ev.tables["projects"] && newest > latest {
		return fmt.Errorf("%w: database is at schema %d, this binary supports up to %d", store.ErrSchemaTooNew, newest, latest)
	}

	// Binding evidence first: a positive is trustworthy from either view.
	n := len(ev.bindingPresent)
	switch {
	case n == len(bindingKeys):
		if !bytes.Equal(ev.bindingPresent[metaBindingVersion], []byte(bindingFormat)) {
			return fmt.Errorf("%w (unsupported binding format)", ErrBindingCorrupt)
		}
		return nil // initialized
	case n > 0:
		return fmt.Errorf("%w (%d of %d binding entries present)", ErrBindingCorrupt, n, len(bindingKeys))
	}

	// No binding rows.
	if !ev.hasDSOSchema() {
		return negative(path, ReasonNoSchema, false, res)
	}
	if ev.totalProjects > 0 {
		if ev.wellFormed != ev.totalProjects {
			return fmt.Errorf("%w (project rows with empty key data)", ErrBindingCorrupt)
		}
		return nil // adoptable: Bind verifies every project's data key
	}
	return negative(path, ReasonNoBindingNoProjects, ev.provenEmpty(embedded), res)
}

// negative builds the error for a negative finding, honouring the WAL rule: a
// negative with a non-empty -wal is inconclusive, never "not initialized".
func negative(path string, reason InitReason, proven bool, res sqlite.InspectionResult) error {
	var guidance []string
	if proven {
		guidance = recoveryGuidance(path, res.WALPresent)
	}
	if res.WALPresent {
		return &InconclusiveError{
			Path:       path,
			Reason:     string(reason) + ", but a non-empty -wal is present (an unreplayed or damaged WAL looks exactly like missing data)",
			WALPresent: true,
			Guidance:   guidance,
		}
	}
	return &NotInitializedError{
		Path:     path,
		Reason:   reason,
		Retry:    res.View == sqlite.ViewImmutable,
		Guidance: guidance,
	}
}

// recoveryGuidance is preserve-first: stop, copy, verify, and only then decide.
// dso itself never removes or changes these files.
func recoveryGuidance(path string, walPresent bool) []string {
	g := make([]string, 0, 6)
	if walPresent {
		g = append(g, "This may be an interrupted `dso central init`, or a damaged -wal file hiding data. Do not treat this database as disposable.")
	} else {
		g = append(g, "This looks like an interrupted `dso central init` (a DSO Central schema with no key binding and no data).")
	}
	files := []string{path, path + "-wal", path + "-shm"}
	g = append(g,
		"dso never deletes or modifies these files itself. To recover manually:",
		"  1. Stop every process that uses this database.",
		"  2. Copy "+strings.Join(files, ", ")+" (whichever of them exist) to a safe location.",
		"  3. Verify the copies are complete and stored safely.",
		"  4. Only then, if you intend to abandon the original state: remove those files (keep "+path+".central.lock and the root key file) and run `dso central init --use-existing-key` again.",
	)
	return g
}
