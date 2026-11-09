package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

const verifySecret = "VERIFY-PLAINTEXT-secret-77"

// verifyFixture initialises a central database with a few audited operations,
// including a secret whose value must never appear in verify output.
func verifyFixture(t *testing.T) *centralEnv {
	t.Helper()
	e := newCentralEnv(t)
	for _, args := range [][]string{{"init"}, {"project", "create", "app"}, {"environment", "create", "app", "prod"}} {
		if _, _, err := e.run(t, "", append(args, e.file()...)...); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.run(t, verifySecret+"\n", append([]string{"secret", "create", "app", "prod", "db/pass", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	return e
}

// runVerify drives the real cli.Run entry point so exit statuses are the ones
// a shell would see.
func runVerify(args ...string) (stdout, stderr string, code int) {
	var out, errb bytes.Buffer
	code = Run(append([]string{"central", "audit", "verify"}, args...), &out, &errb)
	return out.String(), errb.String(), code
}

func dbState(t *testing.T, db string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, s := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(db + s)
		if os.IsNotExist(err) {
			m[s] = "<absent>"
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		m[s] = string(b)
	}
	return m
}

func tamperAudit(t *testing.T, db string, stmts ...string) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+db)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, q := range append([]string{`DROP TRIGGER audit_events_no_update`}, stmts...) {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func TestCentralAuditVerify_Verified_TextAndNoDisclosure(t *testing.T) {
	e := verifyFixture(t)
	out, errOut, code := runVerify("--db", e.db)
	if code != 0 || !strings.Contains(out, "VERIFIED") || !strings.Contains(out, "Tip event:") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, "does not prove authenticity") {
		t.Fatalf("the limitation must be stated: %q", out)
	}
	key, err := os.ReadFile(e.key)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{verifySecret, string(key)} {
		if leak != "" && strings.Contains(out+errOut, leak) {
			t.Fatalf("verify output disclosed secret or key material")
		}
	}
}

func TestCentralAuditVerify_JSONStructure(t *testing.T) {
	e := verifyFixture(t)
	out, _, code := runVerify("--db", e.db, "--json")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout must be exactly one JSON document: %v\n%s", err, out)
	}
	if doc["status"] != "verified" || doc["event_count"].(float64) < 4 || doc["tip_event_id"] == "" {
		t.Fatalf("unexpected document: %v", doc)
	}
	for _, k := range []string{"failure_index", "failure_reason", "failure_event_id", "error"} {
		if _, ok := doc[k]; ok {
			t.Fatalf("%q must be omitted on success: %v", k, doc)
		}
	}
	if strings.Contains(out, verifySecret) {
		t.Fatal("JSON disclosed a secret value")
	}
}

func TestCentralAuditVerify_IntegrityFailure_Exit10(t *testing.T) {
	e := verifyFixture(t)
	tamperAudit(t, e.db, `UPDATE audit_events SET actor_label = 'mallory' WHERE seq = 2`)
	out, errOut, code := runVerify("--db", e.db)
	if code != 10 || !strings.Contains(out, "INTEGRITY FAILURE at index 1") || !strings.Contains(errOut, "integrity failure") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	jout, _, jcode := runVerify("--db", e.db, "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(jout), &doc); err != nil || jcode != 10 {
		t.Fatalf("a failing run must still carry the document: code=%d err=%v out=%q", jcode, err, jout)
	}
	if doc["status"] != "integrity_failure" || doc["failure_index"].(float64) != 1 || doc["failure_reason"] == "" || doc["failure_event_id"] == "" {
		t.Fatalf("unexpected document: %v", doc)
	}
	if _, ok := doc["error"]; ok {
		t.Fatalf("error must be omitted for an integrity failure: %v", doc)
	}
}

func TestCentralAuditVerify_Unavailable_Exit11_DistinctFromIntegrity(t *testing.T) {
	e := newCentralEnv(t)
	missing := filepath.Join(e.dir, "missing.db")
	out, errOut, code := runVerify("--db", missing)
	if code != 11 || !strings.Contains(out, "not evidence of tampering") || !strings.Contains(errOut, "unavailable") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("verify must not create a missing database")
	}
	jout, _, jcode := runVerify("--db", missing, "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(jout), &doc); err != nil || jcode != 11 || doc["status"] != "unavailable" || doc["error"] == "" {
		t.Fatalf("code=%d err=%v doc=%v", jcode, err, doc)
	}
	if _, ok := doc["event_count"]; ok {
		t.Fatalf("event_count must be omitted when unavailable: %v", doc)
	}
}

func TestCentralAuditVerify_UsageErrors_Exit1(t *testing.T) {
	e := verifyFixture(t)
	for _, args := range [][]string{{}, {"--db", e.db, "--bogus"}, {"--db", e.db, "extra-arg"}} {
		if _, errOut, code := runVerify(args...); code != 1 || !strings.Contains(errOut, "Error:") {
			t.Fatalf("args %v: code=%d err=%q", args, code, errOut)
		}
	}
}

func TestCentralAuditVerify_ReadOnly_NoAppendNoChange(t *testing.T) {
	e := verifyFixture(t)
	before := dbState(t, e.db)
	if _, _, code := runVerify("--db", e.db); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after := dbState(t, e.db)
	for k := range before {
		if before[k] != after[k] {
			t.Fatalf("database file %q changed during verification", "db"+k)
		}
	}
	// a second run still sees the same number of events (nothing appended).
	a, _, _ := runVerify("--db", e.db, "--json")
	b, _, _ := runVerify("--db", e.db, "--json")
	if a != b {
		t.Fatalf("repeated verification must be identical:\n%s\n%s", a, b)
	}
}

// TestCentralAuditVerify_UnavailableOutputIsSanitized checks the failure
// paths for disclosure: structured and text stdout must not contain
// filesystem paths, securefs remediation hints, secret values or key
// material; JSON stays exactly one document; details go to stderr only.
func TestCentralAuditVerify_UnavailableOutputIsSanitized(t *testing.T) {
	e := verifyFixture(t)
	key, err := os.ReadFile(e.key)
	if err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(filepath.Dir(e.db), "loose.db")
	b, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loose, b, 0o644); err != nil { // #nosec G306 -- deliberately unsafe for this test
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(e.db), "link.db")
	if err := os.Symlink(e.db, link); err != nil {
		t.Fatal(err)
	}
	fifoDB := filepath.Join(filepath.Dir(e.db), "fifo.db")
	if err := os.WriteFile(fifoDB, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifoDB+"-wal", 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}
	cases := map[string]string{
		"missing": filepath.Join(e.dir, "missing.db"), "loose-permissions": loose, "symlink": link, "fifo-sidecar": fifoDB,
	}
	for name, db := range cases {
		for _, asJSON := range []bool{false, true} {
			args := []string{"--db", db}
			if asJSON {
				args = append(args, "--json")
			}
			type result struct {
				out, errOut string
				code        int
			}
			ch := make(chan result, 1)
			go func() {
				o, er, c := runVerify(args...)
				ch <- result{o, er, c}
			}()
			var r result
			select {
			case r = <-ch:
			case <-time.After(15 * time.Second):
				t.Fatalf("%s: verification blocked", name)
			}
			if r.code != 11 {
				t.Fatalf("%s json=%v: want exit 11, got %d (out=%q err=%q)", name, asJSON, r.code, r.out, r.errOut)
			}
			for _, bad := range []string{e.dir, filepath.Dir(db), "securefs", "chmod", "fix:", verifySecret, string(key)} {
				if bad != "" && strings.Contains(r.out, bad) {
					t.Fatalf("%s json=%v: stdout leaks %q:\n%s", name, asJSON, bad, r.out)
				}
			}
			for _, bad := range []string{verifySecret, string(key)} {
				if strings.Contains(r.errOut, bad) {
					t.Fatalf("%s json=%v: stderr leaks secret or key material", name, asJSON)
				}
			}
			if !strings.Contains(r.errOut, "Error: audit chain verification unavailable") || strings.Count(r.errOut, "Error:") != 1 {
				t.Fatalf("%s: expected exactly one Error: line on stderr, got %q", name, r.errOut)
			}
			if asJSON {
				var doc map[string]any
				if err := json.Unmarshal([]byte(r.out), &doc); err != nil {
					t.Fatalf("%s: stdout must be exactly one JSON document: %v\n%s", name, err, r.out)
				}
				if doc["status"] != "unavailable" || doc["error"] == "" {
					t.Fatalf("%s: unexpected document %v", name, doc)
				}
			} else if !strings.Contains(r.out, "could not be safely read or verified") {
				t.Fatalf("%s: text output must say the database could not be safely read or verified:\n%s", name, r.out)
			}
		}
	}
}

// spoofID embeds a newline, a fake status line and an ANSI colour sequence.
const spoofID = "evil\r\nAudit chain: VERIFIED (999 events)\x1b[31m\u202e"

// assertNoSpoof checks that out/errOut contain no raw control characters
// (other than the final newline of each line) and that no line other than the
// genuine first status line starts with a status prefix.
func assertNoSpoof(t *testing.T, label, out, errOut string) {
	t.Helper()
	for name, s := range map[string]string{"stdout": out, "stderr": errOut} {
		for _, r := range s {
			if (r < 0x20 && r != '\n') || r == 0x7f || r == '\u202e' {
				t.Fatalf("%s %s contains a raw control character %q:\n%q", label, name, r, s)
			}
		}
	}
	statusLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Audit chain:") {
			statusLines++
		}
		if strings.HasPrefix(line, "Audit chain: VERIFIED (999") {
			t.Fatalf("%s: spoofed status line reached stdout:\n%q", label, out)
		}
	}
	if statusLines != 1 {
		t.Fatalf("%s: expected exactly one status line on stdout, got %d:\n%q", label, statusLines, out)
	}
	if strings.Count(strings.TrimRight(errOut, "\n"), "\n") != 0 {
		t.Fatalf("%s: stderr must be a single line, got %q", label, errOut)
	}
}

func TestCentralAuditVerify_IntegrityFailureEventIDCannotSpoofOutput(t *testing.T) {
	e := verifyFixture(t)
	// Not recomputing the hash makes this an integrity failure whose failing
	// event carries the hostile id.
	tamperAudit(t, e.db, `UPDATE audit_events SET id = '`+strings.ReplaceAll(spoofID, "'", "''")+`' WHERE seq = 2`)

	out, errOut, code := runVerify("--db", e.db)
	if code != 10 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	assertNoSpoof(t, "text", out, errOut)
	if !strings.Contains(out, `evil\r\nAudit chain: VERIFIED (999 events)\x1b[31m`) {
		t.Fatalf("the escaped id must stay recognisable for diagnosis:\n%s", out)
	}

	jout, _, jcode := runVerify("--db", e.db, "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(jout), &doc); err != nil || jcode != 10 {
		t.Fatalf("JSON must stay one valid document: code=%d err=%v\n%q", jcode, err, jout)
	}
	if doc["failure_event_id"] != spoofID {
		t.Fatalf("JSON must carry the exact id, escaped by the encoder: %q", doc["failure_event_id"])
	}
	if strings.ContainsAny(jout, "\x1b\r\u202e") {
		t.Fatalf("raw control characters in JSON output: %q", jout)
	}
}

func TestCentralAuditVerify_TipEventIDCannotSpoofOutput(t *testing.T) {
	e := verifyFixture(t)
	// A valid chain whose newest event has a hostile id (hash computed by the
	// store, so the chain verifies).
	st, err := sqlite.Open(context.Background(), e.db, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendAuditEvent(context.Background(), store.AuditEvent{
		ID: spoofID, EventType: store.AuditProjectCreate, Outcome: store.AuditSuccess}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := runVerify("--db", e.db)
	if code != 0 || !strings.Contains(out, "VERIFIED (") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	assertNoSpoof(t, "tip", out, errOut)
	jout, _, jcode := runVerify("--db", e.db, "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(jout), &doc); err != nil || jcode != 0 || doc["tip_event_id"] != spoofID {
		t.Fatalf("JSON tip must round-trip exactly: code=%d err=%v doc=%v", jcode, err, doc)
	}
}

func TestUntrustedFormatting(t *testing.T) {
	if got := quoteUntrusted("a\nb\x1b[0m"); got != `"a\nb\x1b[0m"` {
		t.Fatalf("got %s", got)
	}
	for _, in := range []string{"a\u202eb", "emoji \U0001F600 ok", "\U000E0041tag", "plain é"} {
		raw, err := json.MarshalIndent(map[string]string{"k": in}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		var back map[string]string
		if err := json.Unmarshal(escapeJSONNonPrintable(raw), &back); err != nil || back["k"] != in {
			t.Fatalf("escaped JSON must decode to the same value: %q -> %q (%v)", in, back["k"], err)
		}
	}
	got := printableUntrusted("path /x\ny\x1b[31m\u202e ok é")
	if strings.ContainsAny(got, "\n\x1b\u202e") || !strings.Contains(got, "é") || !strings.Contains(got, `\n`) {
		t.Fatalf("got %q", got)
	}
}
