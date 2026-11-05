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
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

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
	// class is the active-database finding derived from the same evidence
	// (see classifyActive). It is set only for ReasonNoSchema and
	// ReasonNoBindingNoProjects; its zero value is stateInconclusive, so an
	// unset class fails closed. It never affects Error, Is or Unwrap.
	class activeState
	// classWhy explains a stateInconclusive class (why the evidence is
	// ambiguous); empty otherwise. Internal, like class.
	classWhy string
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
	// columns is the structure signature (see tableSignature) of every dsoTables
	// table that exists. It is evidence for classifyActive only; classify (and so
	// CheckInitialized) never reads it.
	columns map[string]string
	// views are the user-defined views. DSO's schema defines none, so any view is
	// unexpected. Like columns, only classifyActive reads it.
	views []string
}

type inspectFunc func(path string, probe func(sqlite.Querier) error) (sqlite.InspectionResult, error)

// CheckInitialized classifies the database at path; see the file comment.
func CheckInitialized(path string) error {
	return checkInitializedWith(sqlite.InspectDatabase, path)
}

func checkInitializedWith(inspect inspectFunc, path string) error {
	_, err := checkInitializedEvidence(inspect, path)
	return err
}

// checkInitializedEvidence is checkInitializedWith plus the evidence the
// classification was made from. The evidence is nil exactly when no usable
// evidence was collected (the file is empty or not SQLite, the inspection failed,
// or the winning probe could not read the database); otherwise it is the evidence
// behind the returned error, which may be nil (initialized) or any finding.
func checkInitializedEvidence(inspect inspectFunc, path string) (*initEvidence, error) {
	latest, err := sqlite.LatestSchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("determining supported schema version: %w", err)
	}
	migs, err := sqlite.EmbeddedMigrations()
	if err != nil {
		return nil, fmt.Errorf("loading embedded migrations: %w", err)
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
			return nil, &NotInitializedError{Path: path, Reason: ReasonEmptyFile, cause: err}
		case errors.Is(err, sqlite.ErrNotSQLite):
			return nil, &NotInitializedError{Path: path, Reason: ReasonNotSQLite, cause: err}
		case errors.Is(err, sqlite.ErrInspectionFailed):
			return nil, &InconclusiveError{Path: path, Reason: "the database could not be inspected safely: " + err.Error(), cause: err}
		}
		return nil, err // securefs refusals and other errors pass through unchanged
	}
	if queryErr != nil {
		// The winning attempt could not read the database: never classify the
		// partial evidence, and never report "not initialized".
		return nil, &InconclusiveError{
			Path:       path,
			Reason:     "reading the database failed: " + queryErr.Error(),
			WALPresent: res.WALPresent,
			cause:      queryErr,
		}
	}
	return &ev, classify(path, ev, res, latest, embedded)
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
		columns:        map[string]string{},
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

	vrows, err := q.QueryContext(bg, `SELECT name FROM sqlite_master WHERE type='view' ORDER BY name`)
	if err != nil {
		return initEvidence{}, err
	}
	for vrows.Next() {
		var n string
		if err := vrows.Scan(&n); err != nil {
			vrows.Close()
			return initEvidence{}, err
		}
		ev.views = append(ev.views, n)
	}
	if err := vrows.Close(); err != nil {
		return initEvidence{}, err
	}
	if err := vrows.Err(); err != nil {
		return initEvidence{}, err
	}

	for _, t := range dsoTables {
		if !ev.tables[t] {
			continue
		}
		sig, err := tableSignature(q, t)
		if err != nil {
			return initEvidence{}, err
		}
		ev.columns[t] = sig
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
		c, why := ev.unboundClass()
		return withClass(negative(path, ReasonNoSchema, false, res), c, why)
	}
	if ev.totalProjects > 0 {
		if ev.wellFormed != ev.totalProjects {
			return fmt.Errorf("%w (project rows with empty key data)", ErrBindingCorrupt)
		}
		return nil // adoptable: Bind verifies every project's data key
	}
	c, why := ev.unboundClass()
	return withClass(negative(path, ReasonNoBindingNoProjects, ev.provenEmpty(embedded), res), c, why)
}

// withClass records the active-database class on a not-initialized finding. Any
// other error (an inconclusive WAL-present negative) is returned unchanged.
func withClass(err error, c activeState, why string) error {
	var ni *NotInitializedError
	if errors.As(err, &ni) {
		ni.class, ni.classWhy = c, why
	}
	return err
}

// hasForeignTable reports a user table outside DSO's known table set.
// SQLite's reserved sqlite_* internal tables (sqlite_sequence, sqlite_stat1, ...)
// are never user tables and are ignored; "schema_migrations" is part of the
// known set. It looks at names only: no identity pragma is used or assumed.
func (ev *initEvidence) hasForeignTable() bool {
	known := make(map[string]bool, len(dsoTables))
	for _, t := range dsoTables {
		known[t] = true
	}
	for t := range ev.tables {
		if strings.HasPrefix(strings.ToLower(t), "sqlite_") {
			continue
		}
		if !known[t] {
			return true
		}
	}
	return false
}

// unboundClass is the class of an unbound database (no binding rows and no
// projects), whether or not its core DSO schema is complete, plus the reason
// when that class is Inconclusive. It is reached only after classify has applied
// schema-version, binding and project evidence, so none of the rules below can
// override valid binding or well-formed project evidence. In order:
//
//  1. A user table outside DSO's table set (sqlite_* internals ignored) makes it
//     Foreign (the approved D7 rule).
//  2. Any user view makes it Inconclusive. DSO defines no views, but a view is
//     not a table, so it is ambiguous evidence, not a Foreign finding.
//  3. A table that has a DSO name but not the DSO structure is not DSO evidence:
//     the name alone proves nothing, so it is Inconclusive.
//  4. A schema_migrations table with no migration-defined table beside it is the
//     signature of an interrupted initialization ONLY while it is empty: DSO
//     creates schema_migrations before the migration transaction, and records a
//     version in that same transaction as the schema it applies. Rows with no
//     schema to go with them cannot come from an interrupted initialization, so
//     the table alone proves nothing and the result is Inconclusive.
//  5. Otherwise an unbound DSO database (possibly an incomplete fragment, which
//     DSO's own transactional migrations cannot normally leave behind).
//
// Ownership only: whether the database is structurally sound is a separate
// question answered by structure().
func (ev *initEvidence) unboundClass() (activeState, string) {
	if ev.hasForeignTable() {
		return stateForeign, ""
	}
	if len(ev.views) > 0 {
		return stateInconclusive, "it contains user-defined views (DSO Central defines none): " + strings.Join(ev.views, ", ")
	}
	if bad := ev.dsoNamedTablesWithoutDSOStructure(); len(bad) > 0 {
		return stateInconclusive, "tables with DSO Central's names do not have its structure: " + strings.Join(bad, ", ")
	}
	if ev.onlySchemaMigrations() && len(ev.versions) > 0 {
		return stateInconclusive, fmt.Sprintf("schema_migrations holds %d row(s) but no DSO Central table exists; an interrupted initialization leaves it empty, so the table alone does not show that DSO Central created this file", len(ev.versions))
	}
	return stateDSOUnbound, ""
}

// onlySchemaMigrations reports that schema_migrations is the only DSO-named
// table present (sqlite_* internals ignored; any other table was handled as a
// foreign table before this is asked).
func (ev *initEvidence) onlySchemaMigrations() bool {
	if !ev.tables["schema_migrations"] {
		return false
	}
	for _, t := range dsoTables {
		if t != "schema_migrations" && ev.tables[t] {
			return false
		}
	}
	return true
}

// dsoNamedTablesWithoutDSOStructure lists the DSO-named tables present that do
// not have the DSO structure (see tablesWithoutDSOStructure). It fails closed: if
// the expected structures cannot be derived, every present DSO-named table is
// listed.
func (ev *initEvidence) dsoNamedTablesWithoutDSOStructure() []string {
	sch, err := dsoStructures()
	if err != nil {
		var bad []string
		for _, t := range dsoTables {
			if ev.tables[t] {
				bad = append(bad, t)
			}
		}
		return bad
	}
	return ev.tablesWithoutDSOStructure(sch)
}

// recordedVersion is the database's own schema version: the newest version
// recorded in schema_migrations, provided that it is an embedded migration.
// Missing, zero, unrecognised or newer-than-supported evidence is not a valid
// version (ok is false), and nothing falls back to the latest schema.
func (ev *initEvidence) recordedVersion(sch dsoSchema) (version int, ok bool) {
	if len(ev.versions) == 0 {
		return 0, false
	}
	for _, v := range ev.versions {
		if v > version {
			version = v
		}
	}
	_, ok = sch.byVersion[version]
	return version, ok
}

// tablesWithoutDSOStructure lists the DSO-named tables present whose structure is
// not the expected one, for ownership purposes:
//
//   - schema_migrations is version-independent. No embedded migration creates or
//     alters it, so it is compared with its fixed DDL whatever versions are
//     recorded. That keeps an interrupted initialization (only an empty
//     schema_migrations table) an unbound DSO database.
//   - every other (migration-defined) table is compared with the structure of the
//     database's OWN recorded version. If there is no valid recorded version, or
//     the table does not exist at that version, the name alone proves nothing and
//     the table is listed. Today's latest schema is never used as a fallback, so a
//     future migration cannot change how a legitimate older database is read.
//
// It looks only at tables that are present: missing tables and an incomplete
// migration history are structural findings (structure), not ownership ones.
func (ev *initEvidence) tablesWithoutDSOStructure(sch dsoSchema) []string {
	version, valid := ev.recordedVersion(sch)
	var bad []string
	for _, t := range dsoTables {
		if !ev.tables[t] {
			continue
		}
		if t == "schema_migrations" {
			if ev.columns[t] == "" || ev.columns[t] != sch.schemaMigrations {
				bad = append(bad, t)
			}
			continue
		}
		want, defined := sch.byVersion[version][t]
		if !valid || !defined || ev.columns[t] == "" || ev.columns[t] != want {
			bad = append(bad, t)
		}
	}
	return bad
}

// structuralFinding is the structural-integrity assessment of an active
// database. It is independent of ownership: a valid binding says a database
// belongs to DSO Central, not that its schema is intact. The zero value is
// structureUnknown, so an unset finding is never mistaken for clean.
//
// structureClean means ONLY that the implemented checks passed: the expected
// DSO tables for the database's own schema version exist with the expected
// ordered columns, declared types, NOT NULL flags, primary-key positions and
// generated-column indicators, and there are no other user tables or views.
// It does NOT verify CHECK/UNIQUE/foreign-key constraints, defaults,
// collations, triggers, indexes, a generated column's expression, SQLite file
// integrity, or the absence of other tampering.
type structuralFinding int

const (
	// structureUnknown: no reliable structural evidence (no evidence collected,
	// an incomplete inspection, a schema version this binary cannot attribute).
	structureUnknown structuralFinding = iota
	structureClean
	structureMismatch
)

func (f structuralFinding) String() string {
	switch f {
	case structureUnknown:
		return "unknown"
	case structureClean:
		return "clean"
	case structureMismatch:
		return "mismatch"
	}
	return fmt.Sprintf("structuralFinding(%d)", int(f))
}

// structure assesses the evidence against the structures the embedded
// migrations give the database's own (newest recorded) schema version.
func (ev *initEvidence) structure() (structuralFinding, []string) {
	sch, err := dsoStructures()
	if err != nil {
		return structureUnknown, []string{"the expected DSO structures could not be derived: " + err.Error()}
	}
	return ev.structureAgainst(sch)
}

// structureAgainst is structure with the expected schema supplied. Besides the
// tables and views, it checks that the recorded migration history is the embedded
// sequence up to the newest recorded version: a missing embedded version, or a
// recorded version that is not an embedded migration, is a mismatch. It uses the
// embedded sequence itself, never an assumption that versions are consecutive.
func (ev *initEvidence) structureAgainst(sch dsoSchema) (structuralFinding, []string) {
	if len(ev.versions) == 0 {
		return structureUnknown, []string{"no applied schema version is recorded"}
	}
	newest, ok := ev.recordedVersion(sch)
	if !ok {
		highest := 0
		for _, v := range ev.versions {
			if v > highest {
				highest = v
			}
		}
		return structureUnknown, []string{fmt.Sprintf("schema version %d is not one this binary can attribute", highest)}
	}
	want := sch.byVersion[newest]
	var reasons []string

	recorded := make(map[int]bool, len(ev.versions))
	for _, v := range ev.versions {
		recorded[v] = true
	}
	for _, v := range sch.versions {
		if v <= newest && !recorded[v] {
			reasons = append(reasons, fmt.Sprintf("migration version %d is not recorded in schema_migrations (the history up to version %d is incomplete)", v, newest))
		}
	}
	for _, v := range ev.versions {
		if _, embedded := sch.byVersion[v]; !embedded {
			reasons = append(reasons, fmt.Sprintf("recorded schema version %d is not an embedded migration", v))
		}
	}

	for _, t := range dsoTables {
		w, expected := want[t]
		switch {
		case expected && !ev.tables[t]:
			reasons = append(reasons, fmt.Sprintf("%s: table is missing for schema version %d", t, newest))
		case !expected && ev.tables[t]:
			reasons = append(reasons, fmt.Sprintf("%s: table does not belong to schema version %d", t, newest))
		case expected && ev.columns[t] != w:
			reasons = append(reasons, fmt.Sprintf("%s: columns differ from the DSO schema at version %d", t, newest))
		}
	}
	known := make(map[string]bool, len(dsoTables))
	for _, t := range dsoTables {
		known[t] = true
	}
	var extra []string
	for t := range ev.tables {
		if !known[t] && !strings.HasPrefix(strings.ToLower(t), "sqlite_") {
			extra = append(extra, t)
		}
	}
	sort.Strings(extra)
	for _, t := range extra {
		reasons = append(reasons, "unexpected table "+t)
	}
	for _, v := range ev.views {
		reasons = append(reasons, "unexpected view "+v)
	}
	if len(reasons) > 0 {
		return structureMismatch, reasons
	}
	return structureClean, nil
}

// tableSignature is a table's ordered columns with declared type, NOT NULL flag,
// primary-key position and the hidden/generated indicator, from
// pragma_table_xinfo (pragma_table_info omits generated columns). It does not
// see CHECK, UNIQUE or foreign-key constraints, defaults, collations, triggers,
// indexes, or the text of a generated column's expression (only that the column
// is generated, and whether it is virtual or stored).
func tableSignature(q sqlite.Querier, table string) (string, error) {
	rows, err := q.QueryContext(context.Background(),
		`SELECT name, type, "notnull", pk, hidden FROM pragma_table_xinfo(?) ORDER BY cid`, table)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for rows.Next() {
		var name, typ string
		var notNull, pk, hidden int
		if err := rows.Scan(&name, &typ, &notNull, &pk, &hidden); err != nil {
			rows.Close()
			return "", err
		}
		fmt.Fprintf(&b, "%s %s %d %d %d;", strings.ToLower(name), strings.ToUpper(typ), notNull, pk, hidden)
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	return b.String(), rows.Err()
}

// schemaMigrationsDDL mirrors the statement (*Store).migrate runs for
// schema_migrations, which no embedded migration creates. A test compares the
// structure derived from it with a freshly opened database so it cannot drift.
const schemaMigrationsDDL = `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`

// dsoSchema is the expected structure of the DSO tables: byVersion[v] maps each
// table that exists after migration v to its signature, and latest is the entry
// for the newest embedded version. Deriving per version (rather than only the
// final schema) keeps legacy databases comparable if a future migration alters a
// table. Callers receive deep copies (see dsoStructures).
type dsoSchema struct {
	latest    map[string]string
	byVersion map[int]map[string]string
	// versions is the embedded migration sequence, ascending; it need not be
	// consecutive.
	versions []int
	// schemaMigrations is the structure of schema_migrations, which no migration
	// creates or alters, so it does not depend on any version.
	schemaMigrations string
}

var (
	dsoStructOnce sync.Once
	dsoStruct     dsoSchema
	dsoStructErr  error
)

// dsoStructures returns the expected structures, derived once from the embedded
// migrations. The cached value is never handed out: every caller gets its own
// deep copy, so mutating a result cannot affect any other caller.
func dsoStructures() (dsoSchema, error) {
	dsoStructOnce.Do(func() { dsoStruct, dsoStructErr = deriveDSOStructures() })
	if dsoStructErr != nil {
		return dsoSchema{}, dsoStructErr
	}
	return dsoStruct.clone(), nil
}

func (d dsoSchema) clone() dsoSchema {
	out := dsoSchema{
		latest:           make(map[string]string, len(d.latest)),
		byVersion:        make(map[int]map[string]string, len(d.byVersion)),
		versions:         append([]int(nil), d.versions...),
		schemaMigrations: d.schemaMigrations,
	}
	for k, v := range d.latest {
		out.latest[k] = v
	}
	for ver, m := range d.byVersion {
		c := make(map[string]string, len(m))
		for k, v := range m {
			c[k] = v
		}
		out.byVersion[ver] = c
	}
	return out
}

// deriveDSOStructures applies the embedded migrations, one at a time, to a
// private in-memory database and records the DSO tables' signatures after each,
// so the embedded migrations stay the single schema definition. No file is
// touched.
func deriveDSOStructures() (dsoSchema, error) {
	migs, err := sqlite.EmbeddedMigrations()
	if err != nil {
		return dsoSchema{}, err
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return dsoSchema{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // a private :memory: database lives on one connection
	bg := context.Background()
	if _, err := db.ExecContext(bg, schemaMigrationsDDL); err != nil {
		return dsoSchema{}, err
	}
	out := dsoSchema{byVersion: map[int]map[string]string{}}
	if out.schemaMigrations, err = tableSignature(db, "schema_migrations"); err != nil {
		return dsoSchema{}, err
	}
	for _, m := range migs {
		if _, err := db.ExecContext(bg, m.SQL); err != nil {
			return dsoSchema{}, fmt.Errorf("deriving the expected DSO structure from migration %d: %w", m.Version, err)
		}
		snap := map[string]string{}
		for _, t := range dsoTables {
			var n int
			if err := db.QueryRowContext(bg, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, t).Scan(&n); err != nil {
				return dsoSchema{}, err
			}
			if n == 0 {
				continue
			}
			sig, err := tableSignature(db, t)
			if err != nil {
				return dsoSchema{}, err
			}
			snap[t] = sig
		}
		out.byVersion[m.Version] = snap
		out.versions = append(out.versions, m.Version)
		out.latest = snap
	}
	for _, t := range dsoTables {
		if out.latest[t] == "" {
			return dsoSchema{}, fmt.Errorf("the embedded migrations do not create table %q", t)
		}
	}
	return out, nil
}

// activeState is the restore-oriented classification of an active database
// path. The zero value is stateInconclusive so that an unset or unknown value
// fails closed. It is not wired into Restore yet.
type activeState int

const (
	// stateInconclusive: the state could not be established (locked, damaged or
	// unreadable inspection, a non-empty -wal hiding a negative, a securefs
	// refusal, or an ambiguous schema). Never treated as safe to overwrite.
	stateInconclusive   activeState = iota
	stateMissing                    // no file at the path
	stateInitialized                // complete binding, or adoptable (well-formed projects)
	stateEmpty                      // a zero-byte file
	stateDSOUnbound                 // DSO schema or fragment, no binding, no projects, no foreign tables
	stateBindingCorrupt             // partial or invalid binding, or project rows without key data
	stateForeign                    // unrelated tables and no binding or projects
	stateTooNew                     // schema newer than this binary supports
	stateNonSQLite                  // not a SQLite file
)

func (s activeState) String() string {
	switch s {
	case stateInconclusive:
		return "inconclusive"
	case stateMissing:
		return "missing"
	case stateInitialized:
		return "initialized"
	case stateEmpty:
		return "empty"
	case stateDSOUnbound:
		return "dso-unbound"
	case stateBindingCorrupt:
		return "binding-corrupt"
	case stateForeign:
		return "foreign"
	case stateTooNew:
		return "too-new"
	case stateNonSQLite:
		return "non-sqlite"
	}
	return fmt.Sprintf("activeState(%d)", int(s))
}

// activeAssessment is what classifyActive learns about an active database,
// as two separate answers:
//
//   - State is the ownership classification: whether the file is a DSO Central
//     database, and in what condition (see activeState). Control flow uses State,
//     never the type of Err.
//   - Structure is the structural-integrity finding: whether the DSO tables,
//     views and extra objects match the schema for the database's own version.
//     It is filled in even when ownership evidence is strong, so a valid binding
//     cannot hide an altered table (see structuralFinding for what "clean" means).
//
// Err is the error CheckInitialized returns for the file (nil for Missing and
// Initialized), except that an Inconclusive State never carries an error that
// matches ErrNotInitialized: ambiguity is reported as *InconclusiveError.
type activeAssessment struct {
	State            activeState
	Structure        structuralFinding
	StructureReasons []string
	Err              error
}

// classifyActive classifies path for restore; see assessActive. It returns the
// ownership state and error only.
func classifyActive(path string) (activeState, error) {
	a := assessActive(path)
	return a.State, a.Err
}

// assessActive is classifyActive with the structural finding.
func assessActive(path string) activeAssessment {
	return assessActiveWith(sqlite.InspectDatabase, path)
}

func classifyActiveWith(inspect inspectFunc, path string) (activeState, error) {
	a := assessActiveWith(inspect, path)
	return a.State, a.Err
}

// assessActiveWith reuses CheckInitialized's inspection and evidence
// classification (one classifier, not two), so it adds no database access beyond
// CheckInitialized's and writes nothing. Anything unrecognised, including securefs
// refusals, is stateInconclusive with the original error. A state derived from an
// immutable-view negative (NotInitializedError.Retry) is verified-stable, not
// proof.
//
// Ownership precedence (the order classify already applies): a newer schema, then
// binding evidence (a complete binding is Initialized, a partial or invalid one
// is BindingCorrupt), then projects (well-formed is Initialized/adoptable,
// otherwise BindingCorrupt). Only when none of those decides, an unbound
// database is classified by unboundClass: a foreign table is Foreign, a user
// view or a DSO-named table without the DSO structure is Inconclusive, anything
// else is DSO-unbound. Table and view evidence therefore never overrides valid
// binding or project evidence.
//
// Structure is assessed from the same evidence and never changes State. It is
// structureUnknown whenever no reliable evidence exists: a missing, empty or
// non-SQLite file, a failed or incomplete inspection (including a negative hidden
// behind a non-empty -wal), or a schema version this binary cannot attribute.
func assessActiveWith(inspect inspectFunc, path string) activeAssessment {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return activeAssessment{State: stateMissing}
		}
		return activeAssessment{State: stateInconclusive, Err: err}
	}
	ev, err := checkInitializedEvidence(inspect, path)
	a := activeAssessment{State: stateInconclusive, Err: err}
	withEvidence := false
	switch {
	case err == nil:
		a.State, withEvidence = stateInitialized, true
	case errors.Is(err, store.ErrSchemaTooNew):
		a.State, withEvidence = stateTooNew, true
	case errors.Is(err, ErrBindingCorrupt):
		a.State, withEvidence = stateBindingCorrupt, true
	default:
		var ni *NotInitializedError
		if errors.As(err, &ni) {
			switch ni.Reason {
			case ReasonEmptyFile:
				a.State = stateEmpty
			case ReasonNotSQLite:
				a.State = stateNonSQLite
			default:
				a.State, withEvidence = ni.class, true
				if ni.class == stateInconclusive {
					why := ni.classWhy
					if why == "" {
						why = "the schema cannot be attributed to DSO Central"
					}
					// Deliberately no cause: wrapping the not-initialized finding would
					// make errors.Is(err, ErrNotInitialized) true for an ambiguous file.
					a.Err = &InconclusiveError{Path: path, Reason: why}
				}
			}
		}
	}
	if withEvidence && ev != nil {
		a.Structure, a.StructureReasons = ev.structure()
	}
	return a
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
