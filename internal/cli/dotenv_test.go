package cli

import (
	"os"
	"testing"
)

// These tests exercise parseDotEnv directly against the fixture matrix,
// deliberately separate from migration-plan and compose-transformation
// tests (dotenv_test.go / migrate_plan_test.go / migrate_test.go): a
// failure in .env parsing should never be conflated with a failure in
// plan generation or Compose transformation.

func openFixture(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 -- fixed testdata path
	if err != nil {
		t.Fatalf("failed to open fixture %s: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseDotEnv_Basic(t *testing.T) {
	f := openFixture(t, "testdata/migrate/basic/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"DB_PASSWORD": "hunter2", "PORT": "8080", "LOG_LEVEL": "info"}
	for k, v := range want {
		if result.Values[k] != v {
			t.Errorf("key %s: got %q, want %q", k, result.Values[k], v)
		}
	}
	if len(result.DuplicateKeys) != 0 {
		t.Errorf("expected no duplicates, got %v", result.DuplicateKeys)
	}
}

func TestParseDotEnv_QuotedValues(t *testing.T) {
	f := openFixture(t, "testdata/migrate/quoted-values/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Values["DB_PASSWORD"] != "hunter2 with spaces" {
		t.Errorf("double-quote stripping failed: got %q", result.Values["DB_PASSWORD"])
	}
	if result.Values["API_TOKEN"] != "single-quoted-token" {
		t.Errorf("single-quote stripping failed: got %q", result.Values["API_TOKEN"])
	}
	if result.Values["UNQUOTED"] != "plain" {
		t.Errorf("unquoted value altered: got %q", result.Values["UNQUOTED"])
	}
}

func TestParseDotEnv_EmptyValues(t *testing.T) {
	f := openFixture(t, "testdata/migrate/empty-values/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := result.Values["DB_PASSWORD"]; !ok || v != "" {
		t.Errorf("expected DB_PASSWORD to be present with empty value, got %q ok=%v", v, ok)
	}
	if result.Values["LOG_LEVEL"] != "info" {
		t.Error("expected LOG_LEVEL to still parse correctly after an empty-valued key")
	}
}

func TestParseDotEnv_DuplicateVars_LastWins(t *testing.T) {
	f := openFixture(t, "testdata/migrate/duplicate-vars/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Values["DB_PASSWORD"] != "second-value" {
		t.Errorf("expected last occurrence to win, got %q", result.Values["DB_PASSWORD"])
	}
	if len(result.DuplicateKeys) != 1 || result.DuplicateKeys[0] != "DB_PASSWORD" {
		t.Errorf("expected DuplicateKeys=[DB_PASSWORD], got %v", result.DuplicateKeys)
	}
}

func TestParseDotEnv_Comments_NotTreatedAsSecrets(t *testing.T) {
	f := openFixture(t, "testdata/migrate/comments/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Values) != 2 {
		t.Fatalf("expected exactly 2 parsed keys (comments excluded), got %d: %v", len(result.Values), result.Values)
	}
	for k := range result.Values {
		if k[0] == '#' {
			t.Errorf("a comment line was parsed as a key: %q", k)
		}
	}
}

// TestParseDotEnv_MultilinePreservesFirstLine documents the existing,
// line-oriented parser's actual behavior on a quoted multi-line value: it
// is NOT joined across lines (bufio.Scanner is line-oriented), so only the
// first line is captured, with its opening quote retained since the line
// itself has no matching closing quote. This is pre-existing `env import`
// behavior, preserved exactly by the extraction -- not a new limitation
// introduced by migrate. Subsequent lines of the multi-line value are
// skipped as malformed (no '=' separator).
func TestParseDotEnv_MultilinePreservesFirstLine(t *testing.T) {
	f := openFixture(t, "testdata/migrate/multiline/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Values["DB_PASSWORD"] != "hunter2" {
		t.Errorf("expected DB_PASSWORD to still parse correctly, got %q", result.Values["DB_PASSWORD"])
	}
	if len(result.SkippedLines) == 0 {
		t.Error("expected the unterminated multi-line continuation lines to be reported as skipped")
	}
}

// TestParseDotEnv_ExportPrefix documents that "export KEY=value" is NOT
// specially unwrapped: the key becomes the literal token before '=',
// including the "export " prefix. This is pre-existing behavior from
// `env import`'s parser, preserved as-is rather than silently changed by
// this extraction.
func TestParseDotEnv_ExportPrefix(t *testing.T) {
	f := openFixture(t, "testdata/migrate/export-prefix/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Values["DB_PASSWORD"]; ok {
		t.Error("did not expect a bare DB_PASSWORD key -- 'export ' prefix is not stripped by this parser")
	}
	if result.Values["export DB_PASSWORD"] != "hunter2" {
		t.Errorf("expected the literal key 'export DB_PASSWORD', got keys: %v", result.Values)
	}
}

func TestParseDotEnv_SkippedLinesNeverContainValues(t *testing.T) {
	f := openFixture(t, "testdata/migrate/multiline/.env")
	result, err := parseDotEnv(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, skip := range result.SkippedLines {
		if skip == "" {
			continue
		}
		// The skip messages are line-number + reason only; they must never
		// echo the offending line's content, which could itself be (part
		// of) a secret value.
		if len(skip) > 60 {
			t.Errorf("skip message looks like it may contain line content, not just a reason: %q", skip)
		}
	}
}
