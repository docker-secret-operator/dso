package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func setVersion(doc, v string) string {
	return strings.Replace(doc, `"schema_version":2`, `"schema_version":`+v, 1)
}

func replaceNth(s, old, repl string, n int) string {
	idx := -1
	from := 0
	for i := 0; i < n; i++ {
		j := strings.Index(s[from:], old)
		if j < 0 {
			return s
		}
		idx = from + j
		from = idx + len(old)
	}
	return s[:idx] + repl + s[idx+len(old):]
}

func mustChange(t *testing.T, before, after string) string {
	t.Helper()
	if before == after {
		t.Fatal("test bug: the mutation did not change the document")
	}
	return after
}

func expectInvalid(t *testing.T, name, doc string, wantSub ...string) {
	t.Helper()
	_, err := Unmarshal([]byte(doc))
	if err == nil {
		t.Errorf("%s: expected an error", name)
		return
	}
	var sv *SchemaVersionError
	if errors.As(err, &sv) {
		t.Errorf("%s: reported as a newer schema, want corruption: %v", name, err)
		return
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("%s: must wrap ErrInvalid: %v", name, err)
		return
	}
	for _, s := range wantSub {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%s: error %q does not contain %q", name, err, s)
		}
	}
}

func expectNewer(t *testing.T, name, doc string) *SchemaVersionError {
	t.Helper()
	_, err := Unmarshal([]byte(doc))
	var sv *SchemaVersionError
	if !errors.As(err, &sv) {
		t.Errorf("%s: expected a SchemaVersionError, got %v", name, err)
		return nil
	}
	if errors.Is(err, ErrInvalid) {
		t.Errorf("%s: a newer schema must not match ErrInvalid", name)
	}
	if sv.Supported != SchemaVersion || sv.Found <= SchemaVersion {
		t.Errorf("%s: unexpected versions %+v", name, sv)
	}
	return sv
}

func expectAccepted(t *testing.T, name, doc, wantCanonical string) {
	t.Helper()
	l, err := Unmarshal([]byte(doc))
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return
	}
	if out, err := Marshal(l); err != nil || string(out) != wantCanonical {
		t.Errorf("%s: does not re-marshal to the canonical document (%v)", name, err)
	}
}

// T-U50 (design §28, §36.2 P1-5 to P1-8): schema versions.
func TestSchemaVersion_Boundary(t *testing.T) {
	if SchemaVersion != 2 {
		t.Fatalf("SchemaVersion = %d, the approved format is version 2 (design D5-4, §28)", SchemaVersion)
	}
	expectAccepted(t, "version 2", vecV1Doc, vecV1Doc)
	for name, v := range map[string]string{"zero": "0", "version 1": "1", "negative": "-1", "negative zero": "-0"} {
		expectInvalid(t, name, setVersion(vecV1Doc, v), "schema_version", "unsupported")
	}
	for _, v := range []string{"3", "4", "100", "2147483647", "9223372036854775807"} {
		_ = expectNewer(t, "version "+v, setVersion(vecV1Doc, v))
	}
	// An integer beyond 64 bits is greater than any supported version when
	// positive and below it when negative.
	for _, v := range []string{"9223372036854775808", "99999999999999999999999999"} {
		if sv := expectNewer(t, "huge "+v, setVersion(vecV1Doc, v)); sv != nil && sv.Found != math.MaxInt {
			t.Errorf("a huge version must saturate, got %d", sv.Found)
		}
	}
	expectInvalid(t, "huge negative", setVersion(vecV1Doc, "-99999999999999999999"), "schema_version")
	// Marshal writes only the supported version.
	for _, v := range []int{0, 1, 3} {
		l := buildV1()
		l.SchemaVersion = v
		if _, err := Marshal(l); !errors.Is(err, ErrInvalid) {
			t.Errorf("Marshal must refuse schema version %d, got %v", v, err)
		}
	}
}

// T-U37 (design §36.4): the two-phase decoder.
func TestStrictDecoder_TwoPhases(t *testing.T) {
	sealed := string(mustMarshal(t, sampleLedger(t)))
	newerV2 := setVersion(vecV2Doc, "3")

	t.Run("a-e newer schema is recognised before any structural check", func(t *testing.T) {
		for name, doc := range map[string]string{
			"a unknown top-level field": strings.Replace(newerV2, `{"schema_version":3,`, `{"schema_version":3,"surprise":true,`, 1),
			"b unknown nested field":    mustChange(t, newerV2, strings.Replace(newerV2, `"project":""`, `"project":"","bogus":1`, 1)),
			"c nested duplicate key":    mustChange(t, newerV2, strings.Replace(newerV2, `"service":"svc"`, `"service":"x","service":"svc"`, 1)),
			"d case-variant names": mustChange(t, newerV2, strings.Replace(strings.Replace(newerV2, `"entries":`, `"Entries":`, 1),
				`"key":{`, `"Key":{`, 1)),
			"e all of them": strings.Replace(strings.Replace(strings.Replace(strings.Replace(newerV2, `{"schema_version":3,`, `{"schema_version":3,"surprise":true,`, 1),
				`"project":""`, `"project":"","bogus":1`, 1), `"service":"svc"`, `"service":"x","service":"svc"`, 1), `"entries":`, `"Entries":`, 1),
			"entirely different structure": `{"schema_version":3,"records":[1,2,{"x":null}],"schema":"v3"}`,
		} {
			_ = expectNewer(t, name, doc)
		}
	})

	t.Run("f duplicate top-level schema_version is corruption", func(t *testing.T) {
		for name, ins := range map[string]string{
			"2 then 1":        `"schema_version":2,"schema_version":1,`,
			"1 then 2":        `"schema_version":1,"schema_version":2,`,
			"1 then 1":        `"schema_version":1,"schema_version":1,`,
			"2 then 2":        `"schema_version":2,"schema_version":2,`,
			"3 then 3":        `"schema_version":3,"schema_version":3,`,
			"escaped spelled": `"schema_version":2,"schema_version":2,`,
		} {
			expectInvalid(t, name, strings.Replace(vecV1Doc, `"schema_version":2,`, ins, 1), "schema_version", "duplicate key")
		}
	})

	t.Run("g missing schema_version", func(t *testing.T) {
		expectInvalid(t, "absent", strings.Replace(vecV1Doc, `"schema_version":2,`, ``, 1), "schema_version", "missing field")
		expectInvalid(t, "only a case variant", strings.Replace(vecV1Doc, `"schema_version"`, `"Schema_Version"`, 1), "schema_version", "missing field")
	})

	t.Run("h i schema_version is an integer literal", func(t *testing.T) {
		for name, v := range map[string]string{"string": `"2"`, "float": `2.0`, "exponent": `1e0`, "fraction": `2.5`, "null": `null`, "true": `true`, "array": `[2]`, "object": `{}`} {
			expectInvalid(t, name, setVersion(vecV1Doc, v), "schema_version")
		}
	})

	t.Run("k supported schema with nested duplicate keys", func(t *testing.T) {
		for name, doc := range map[string]string{
			"entries[0].key.service": mustChange(t, sealed, strings.Replace(sealed, `"service":"legacy-batch"`, `"service":"x","service":"legacy-batch"`, 1)),
			"entries[0].state":       mustChange(t, sealed, strings.Replace(sealed, `"state":"in_progress"`, `"state":"failed","state":"in_progress"`, 1)),
			"replicas[0].applied":    mustChange(t, sealed, replaceNth(sealed, `"applied":{"kind":"hmac"`, `"applied":{"kind":"hmac","kind":"hmac"`, 2)),
			"rotation.strategy":      mustChange(t, sealed, strings.Replace(sealed, `"strategy":"restart"`, `"strategy":"a","strategy":"restart"`, 1)),
			"top-level first bogus":  mustChange(t, sealed, strings.Replace(sealed, `"generation":42`, `"generation":7,"generation":42`, 1)),
			"top-level last bogus":   mustChange(t, sealed, strings.Replace(sealed, `"generation":42`, `"generation":42,"generation":7`, 1)),
			"top-level ledger_id":    mustChange(t, sealed, strings.Replace(sealed, `"ledger_id"`, `"ledger_id":"`+strings.Repeat("b", 32)+`","ledger_id"`, 1)),
		} {
			expectInvalid(t, name, doc, "duplicate key")
		}
	})

	t.Run("l supported schema with unknown fields", func(t *testing.T) {
		for name, doc := range map[string]string{
			"top level":       mustChange(t, sealed, strings.Replace(sealed, `"generation"`, `"surprise":1,"generation"`, 1)),
			"entry key":       mustChange(t, sealed, strings.Replace(sealed, `"project":""`, `"project":"","bogus":1`, 1)),
			"intent":          mustChange(t, sealed, strings.Replace(sealed, `"strategy":"restart"`, `"strategy":"restart","bogus":1`, 1)),
			"replica version": mustChange(t, sealed, replaceNth(sealed, `"applied":{"kind":"hmac"`, `"applied":{"bogus":1,"kind":"hmac"`, 2)),
			"recovery action": mustChange(t, sealed, strings.Replace(sealed, `"op":"stop"`, `"op":"stop","bogus":1`, 1)),
			"unknown object":  mustChange(t, sealed, strings.Replace(sealed, `"generation":42`, `"generation":42,"extra":{"a":1}`, 1)),
		} {
			expectInvalid(t, name, doc, "unknown field")
		}
	})

	t.Run("m supported schema with case-variant names", func(t *testing.T) {
		for name, doc := range map[string]string{
			"LEDGER_ID":   mustChange(t, sealed, strings.Replace(sealed, `"ledger_id"`, `"LEDGER_ID"`, 1)),
			"nested Key":  mustChange(t, sealed, strings.Replace(sealed, `"key":{`, `"Key":{`, 1)),
			"Desired_Seq": mustChange(t, sealed, strings.Replace(sealed, `"desired_seq"`, `"Desired_Seq"`, 1)),
			"Entries":     mustChange(t, sealed, strings.Replace(sealed, `"entries"`, `"Entries"`, 1)),
		} {
			expectInvalid(t, name, doc, "case variant")
		}
	})

	t.Run("n escaped duplicate key", func(t *testing.T) {
		expectInvalid(t, "state", strings.Replace(vecV2Doc, `"state":"idle"`, `"state":"failed","state":"idle"`, 1), "duplicate key")
		expectInvalid(t, "same key spelled twice with escapes", strings.Replace(vecV2Doc, `"state":"idle"`, `"state":"idle","state":"idle"`, 1), "duplicate key")
	})

	t.Run("o invalid UTF-8", func(t *testing.T) {
		expectInvalid(t, "supported", strings.Replace(vecV2Doc, `"svc"`, "\"s\xffvc\"", 1), "syntax error")
		expectInvalid(t, "newer schema", strings.Replace(newerV2, `"svc"`, "\"s\xffvc\"", 1), "syntax error")
		expectInvalid(t, "outside strings", vecV2Doc+"\xff", "syntax error")
	})

	t.Run("p byte-order mark", func(t *testing.T) {
		expectInvalid(t, "supported", "\xef\xbb\xbf"+vecV2Doc, "byte-order mark")
		expectInvalid(t, "newer schema", "\xef\xbb\xbf"+newerV2, "byte-order mark")
	})

	t.Run("q trailing JSON", func(t *testing.T) {
		for _, tail := range []string{`{}`, vecV1Doc, `[]`, `x`, `,`, `}`} {
			expectInvalid(t, "supported + "+tail[:min(len(tail), 12)], vecV2Doc+tail, "syntax error")
			expectInvalid(t, "newer + "+tail[:min(len(tail), 12)], newerV2+tail, "syntax error")
		}
	})

	t.Run("r truncated newer-schema document cannot be classified", func(t *testing.T) {
		for _, doc := range []string{`{"schema_version":3,"surprise":`, `{"schema_version":3`, `{"schema_version":3,"records":[1,2`} {
			expectInvalid(t, doc, doc, "syntax error")
		}
	})

	t.Run("s not an object", func(t *testing.T) {
		for _, doc := range []string{`[]`, ``, "  \n\t ", `"x"`, `123`, `null`, `true`, `[{"schema_version":2}]`} {
			expectInvalid(t, fmt.Sprintf("%q", doc), doc)
		}
	})

	t.Run("t harmless formatting is accepted", func(t *testing.T) {
		var indented, tabbed bytes.Buffer
		if err := json.Indent(&indented, []byte(vecV10Doc), "", "  "); err != nil {
			t.Fatal(err)
		}
		if err := json.Indent(&tabbed, []byte(vecV10Doc), "\n\n", "\t"); err != nil {
			t.Fatal(err)
		}
		expectAccepted(t, "indented", indented.String(), vecV10Doc)
		expectAccepted(t, "CRLF line ends", strings.ReplaceAll(indented.String(), "\n", "\r\n"), vecV10Doc)
		expectAccepted(t, "tabs and blank lines", tabbed.String(), vecV10Doc)
		expectAccepted(t, "leading and trailing blank lines", "\n\n \t"+vecV10Doc+"\r\n\r\n\n", vecV10Doc)
		expectAccepted(t, "trailing newline", vecV1Doc+"\n", vecV1Doc)
		// Key order inside an object is irrelevant.
		reordered := `{"recoveries":[],"entries":[],"body_sha256":"` + vecV1SHA + `","updated_at":"2026-11-12T08:00:00Z","created_at":"2026-11-12T08:00:00Z",` +
			`"hmac_key_id":"k-0123456789abcdef","generation":1,"ledger_id":"` + strings.Repeat("a", 32) + `","schema_version":2}`
		expectAccepted(t, "reordered keys", reordered, vecV1Doc)
		// A different order of entries and replicas is accepted and canonicalized.
		expectAccepted(t, "reversed order file", vecV10ReversedFile, vecV10Doc)
		expectAccepted(t, "zero-offset timestamp spelling", strings.Replace(vecV1Doc, `"created_at":"2026-11-12T08:00:00Z"`, `"created_at":"2026-11-12T08:00:00+00:00"`, 1), vecV1Doc)
	})

	t.Run("u a newer schema is reported without touching the input", func(t *testing.T) {
		doc := []byte(strings.Replace(newerV2, `"service":"svc"`, `"service":"x","service":"svc","extra":{"Entries":[1]}`, 1))
		keep := append([]byte(nil), doc...)
		_, err := Unmarshal(doc)
		var sv *SchemaVersionError
		if !errors.As(err, &sv) || *sv != (SchemaVersionError{Found: 3, Supported: SchemaVersion}) {
			t.Fatalf("expected exactly the two integers, got %v", err)
		}
		if !bytes.Equal(doc, keep) {
			t.Fatal("Unmarshal modified its input")
		}
		if msg := err.Error(); strings.Contains(msg, "svc") || strings.Contains(msg, "extra") {
			t.Errorf("the message must carry only the two integers: %q", msg)
		}
	})
}

// walkObjects calls f for every JSON object in the decoded document, with its
// path.
func walkObjects(v any, path string, f func(obj map[string]any, path string)) {
	switch x := v.(type) {
	case map[string]any:
		f(x, path)
		for k, c := range x {
			p := k
			if path != "" {
				p = path + "." + k
			}
			walkObjects(c, p, f)
		}
	case []any:
		for i, c := range x {
			walkObjects(c, fmt.Sprintf("%s[%d]", path, i), f)
		}
	}
}

func decodeGeneric(t *testing.T, doc string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(doc))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// T-U51 (design §29.1, §29.4): required, optional, null and empty fields, and
// the diagnostics.
func TestPresencePolicy(t *testing.T) {
	sealed := string(mustMarshal(t, sampleLedger(t)))
	optional := map[string]bool{"desired_observed_at": true, "finished_at": true}

	// Every required field is required: removing any one of them is a
	// "missing field" error that names the field, before any checksum is judged.
	removed := 0
	root := decodeGeneric(t, sealed)
	var targets []struct{ path, key string }
	walkObjects(root, "", func(obj map[string]any, path string) {
		for k := range obj {
			if !optional[k] {
				targets = append(targets, struct{ path, key string }{path, k})
			}
		}
	})
	for _, tg := range targets {
		doc := decodeGeneric(t, sealed)
		var found bool
		walkObjects(doc, "", func(obj map[string]any, path string) {
			if path == tg.path && !found {
				delete(obj, tg.key)
				found = true
			}
		})
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		full := tg.key
		if tg.path != "" {
			full = tg.path + "." + tg.key
		}
		_, uerr := Unmarshal(b)
		if !errors.Is(uerr, ErrInvalid) || errors.Is(uerr, ErrChecksum) || !strings.Contains(uerr.Error(), full+": missing field") {
			t.Errorf("removing %s: want a %q error for it, got %v", full, "missing field", uerr)
		}
		removed++
	}
	if removed < 100 {
		t.Fatalf("only %d required fields were exercised", removed)
	}

	// null is invalid for scalars, times and non-pointer objects; tolerated for
	// lists; allowed for the optional objects.
	nullErrors := 0
	for _, tg := range targets {
		doc := decodeGeneric(t, sealed)
		var kind string
		walkObjects(doc, "", func(obj map[string]any, path string) {
			if path == tg.path && kind == "" {
				switch obj[tg.key].(type) {
				case nil:
					kind = "null"
				case []any:
					kind = "list"
				case map[string]any:
					kind = "object"
				default:
					kind = "scalar"
				}
				// Only scalars and the non-pointer objects (key, scope and the
				// intent's own desired version) reject null; the pointer objects
				// (desired, applied, rotation, request, last_outcome, a replica's
				// applied) are required keys whose value may be null.
				nonPointerObject := kind == "object" && (tg.key == "key" || tg.key == "scope" || strings.HasSuffix(tg.path, "rotation") && tg.key == "desired")
				if kind == "scalar" || nonPointerObject {
					obj[tg.key] = nil
				} else {
					kind += "-skip"
				}
			}
		})
		if !strings.HasSuffix(kind, "-skip") && kind != "null" {
			b, _ := json.Marshal(doc)
			full := tg.key
			if tg.path != "" {
				full = tg.path + "." + tg.key
			}
			_, err := Unmarshal(b)
			if !errors.Is(err, ErrInvalid) || errors.Is(err, ErrChecksum) || !strings.Contains(err.Error(), full) || !strings.Contains(err.Error(), "type error") {
				t.Errorf("null for %s (%s): want a type error naming the field, got %v", full, kind, err)
			}
			nullErrors++
		}
	}
	if nullErrors < 50 {
		t.Fatalf("only %d null cases were exercised", nullErrors)
	}

	// Lists may be null and read as empty; the checksum is of the same content.
	expectAccepted(t, "entries null", strings.Replace(vecV1Doc, `"entries":[]`, `"entries":null`, 1), vecV1Doc)
	expectAccepted(t, "recoveries null", strings.Replace(vecV1Doc, `"recoveries":[]`, `"recoveries":null`, 1), vecV1Doc)
	expectAccepted(t, "replicas null", strings.Replace(vecV2Doc, `"replicas":[]`, `"replicas":null`, 1), vecV2Doc)
	expectAccepted(t, "planned and done null", strings.Replace(strings.Replace(vecV8Doc, `"planned":[]`, `"planned":null`, 2), `"done":[]`, `"done":null`, 2), vecV8Doc)
	// Optional objects are present and may be null; the optional times are
	// absent when unset, and null is read as absent.
	expectAccepted(t, "optional time null", strings.Replace(vecV2Doc, `"applied":null`, `"desired_observed_at":null,"applied":null`, 1), vecV2Doc)
	expectAccepted(t, "optional time absent", vecV2Doc, vecV2Doc)
	expectAccepted(t, "finished_at null", strings.Replace(vecV8Doc, `"started_at":"2026-11-12T08:00:01Z"`, `"started_at":"2026-11-12T08:00:01Z","finished_at":null`, 1), vecV8Doc)
	// The empty string is invalid for an optional time.
	expectInvalid(t, "desired_observed_at empty", strings.Replace(vecV2Doc, `"applied":null`, `"desired_observed_at":"","applied":null`, 1), "desired_observed_at", "invalid value")
	expectInvalid(t, "finished_at empty", strings.Replace(vecV8Doc, `"started_at":"2026-11-12T08:00:01Z"`, `"started_at":"2026-11-12T08:00:01Z","finished_at":""`, 1), "finished_at", "invalid value")
	expectInvalid(t, "finished_at not a time", strings.Replace(vecV8Doc, `"started_at":"2026-11-12T08:00:01Z"`, `"started_at":"2026-11-12T08:00:01Z","finished_at":"soon"`, 1), "finished_at", "invalid value")
	// Required empty strings are allowed where the validator allows them (the
	// vectors contain project "", number "", reason "", source "").
	expectAccepted(t, "empty strings where allowed", vecV10Doc, vecV10Doc)

	// Diagnostics name a field path and a reason class, and never echo content.
	marker := "S3CR3T-MARKER-IN-INPUT"
	diag := []struct {
		name, doc string
		want      []string
	}{
		{"unknown nested field", strings.Replace(vecV2Doc, `"project":""`, `"project":"","`+marker+`":1`, 1), []string{"entries[0].key", "unknown field"}},
		{"duplicate nested key", strings.Replace(vecV2Doc, `"service":"svc"`, `"service":"`+marker+`","service":"svc"`, 1), []string{"entries[0].key", "duplicate key"}},
		{"case variant", strings.Replace(vecV2Doc, `"service":"svc"`, `"Service":"`+marker+`"`, 1), []string{"entries[0].key", "case variant"}},
		{"missing field", strings.Replace(vecV2Doc, `"replicas":[],`, ``, 1), []string{"entries[0].replicas", "missing field"}},
		{"wrong type", strings.Replace(vecV2Doc, `"generation":1`, `"generation":"`+marker+`"`, 1), []string{"generation", "type error"}},
		{"invalid value", strings.Replace(vecV8Doc, `"started_at":"2026-11-12T08:00:01Z"`, `"started_at":"`+marker+`"`, 1), []string{"recoveries[0].started_at", "invalid value"}},
		{"negative sequence", strings.Replace(vecV10Doc, `"desired_seq":3`, `"desired_seq":-3`, 1), []string{"entries[0].desired_seq", "non-negative"}},
		{"syntax", `{"schema_version":2,"ledger_id":"` + marker, []string{"syntax error", "at byte"}},
		{"not an object", `["` + marker + `"]`, []string{"type error"}},
	}
	for _, d := range diag {
		_, err := Unmarshal([]byte(d.doc))
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", d.name, err)
			continue
		}
		for _, w := range d.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q lacks %q", d.name, err, w)
			}
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("%s: the error echoes document content: %v", d.name, err)
		}
	}
}
