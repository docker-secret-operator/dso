package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/vault"
)

// isolatedVault points HOME at a temp dir and initialises a throwaway vault.
// All values used by these tests are synthetic.
func isolatedVault(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DSO_MASTER_KEY", "synthetic-test-master-key-not-real")
	if err := vault.InitDefault(); err != nil {
		t.Fatalf("InitDefault: %v", err)
	}
}

func runSecretSet(t *testing.T, key string, stdin []byte, extra ...string) error {
	t.Helper()
	cmd := newSecretSetCmd()
	cmd.SetIn(bytes.NewReader(stdin))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{key}, extra...))
	return cmd.Execute()
}

func runSecretGet(t *testing.T, key string) string {
	t.Helper()
	cmd := newSecretGetCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{key, "--reveal"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret get: %v", err)
	}
	return out.String()
}

func TestSecretSet_PreservesExactBytes(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nQUJDREVGRw==\nSElKS0xNTg==\n-----END PRIVATE KEY-----\n"
	cases := map[string]string{
		"leading spaces":       "   value",
		"trailing spaces":      "value   ",
		"leading newlines":     "\n\nvalue",
		"trailing newline":     "value\n",
		"multiple trailing nl": "value\n\n\n",
		"pem with final nl":    pem,
		"tabs":                 "\tva\tlue\t",
		"carriage returns":     "a\r\nb\r\n",
		"unicode":              "pässwörd-密码-🔐",
		"whitespace only":      "  \n\t ",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			isolatedVault(t)
			if err := runSecretSet(t, "proj/key", []byte(want)); err != nil {
				t.Fatalf("set: %v", err)
			}
			if got := runSecretGet(t, "proj/key"); got != want {
				t.Fatalf("round trip altered value\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestSecretSet_FileRedirectEqualsStdin(t *testing.T) {
	isolatedVault(t)
	want := "line1\nline2\n\n"
	f := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(f, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	fh, err := os.Open(f) // #nosec G304 -- temp file created by this test
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()

	cmd := newSecretSetCmd()
	cmd.SetIn(fh) // regular file: not a terminal, read verbatim
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"proj/filekey"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := runSecretGet(t, "proj/filekey"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSecretSet_EmptyRejected(t *testing.T) {
	isolatedVault(t)
	if err := runSecretSet(t, "proj/key", nil); err == nil {
		t.Fatal("expected empty value to be rejected")
	}
}

func TestSecretSet_InvalidUTF8Rejected(t *testing.T) {
	isolatedVault(t)
	err := runSecretSet(t, "proj/key", []byte{0x61, 0xff, 0xfe, 0x62})
	if err == nil {
		t.Fatal("expected invalid UTF-8 to be rejected, not silently rewritten")
	}
	if strings.Contains(err.Error(), "\xff") {
		t.Error("error must not echo the value")
	}
}

func TestSecretSet_TooLargeRejected(t *testing.T) {
	isolatedVault(t)
	if err := runSecretSet(t, "proj/key", bytes.Repeat([]byte("a"), maxSecretSize+1)); err == nil {
		t.Fatal("expected oversize value to be rejected")
	}
	if err := runSecretSet(t, "proj/ok", bytes.Repeat([]byte("a"), maxSecretSize)); err != nil {
		t.Fatalf("exactly max size should be accepted: %v", err)
	}
}

func TestSecretSet_TrimNewlineIsExplicitAndMinimal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"value\n", "value"},
		{"value\r\n", "value"},
		{"value\n\n", "value\n"}, // only ONE terminator
		{"  value  \n", "  value  "},
		{"value", "value"},
	}
	for _, c := range cases {
		isolatedVault(t)
		if err := runSecretSet(t, "proj/key", []byte(c.in), "--trim-newline"); err != nil {
			t.Fatal(err)
		}
		if got := runSecretGet(t, "proj/key"); got != c.want {
			t.Errorf("in %q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestSecretSet_ErrorsDoNotLeakValue(t *testing.T) {
	// no vault initialised -> load failure; the value must not appear in output
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DSO_MASTER_KEY", "synthetic-test-master-key-not-real")
	err := runSecretSet(t, "proj/key", []byte("SYNTHETIC-CANARY-VALUE"))
	if err == nil {
		t.Fatal("expected error without initialised vault")
	}
	if strings.Contains(err.Error(), "SYNTHETIC-CANARY-VALUE") {
		t.Fatal("error leaked secret value")
	}
}

// M0-06: `secret get` must never print plaintext unless --reveal is given.
func TestSecretGet_RefusesWithoutReveal_AndNeverReadsTheVault(t *testing.T) {
	isolatedVault(t)
	const synthetic = "SYNTHETIC-PLAINTEXT-SENTINEL-8841"
	if err := runSecretSet(t, "proj/token", []byte(synthetic)); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"proj/token"},
		{"proj/token", "-n"},
		{"proj/token", "--newline"},
		{"missing-project/missing"}, // refusal must not even reveal existence
	} {
		code, out, errOut := runCLI(append([]string{"secret", "get"}, args...)...)
		if code == 0 {
			t.Fatalf("%v: must exit non-zero without --reveal", args)
		}
		if strings.Contains(out+errOut, synthetic) {
			t.Fatalf("%v: plaintext leaked: out=%q err=%q", args, out, errOut)
		}
		if out != "" {
			t.Fatalf("%v: stdout must stay empty, got %q", args, out)
		}
		if !strings.Contains(errOut, "--reveal") {
			t.Fatalf("%v: error must tell the user how to proceed: %q", args, errOut)
		}
		if strings.Contains(errOut, "not found") {
			t.Fatalf("%v: refusal must not disclose whether the secret exists: %q", args, errOut)
		}
	}
}

func TestSecretGet_RevealPrintsExactValue_AndErrorsStayRedacted(t *testing.T) {
	isolatedVault(t)
	const synthetic = "SYNTHETIC-PLAINTEXT-SENTINEL-1207"
	if err := runSecretSet(t, "proj/token", []byte(synthetic)); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI("secret", "get", "proj/token", "--reveal")
	if code != 0 || out != synthetic {
		t.Fatalf("reveal: code=%d out=%q", code, out)
	}
	code, out, _ = runCLI("secret", "get", "proj/token", "--reveal", "-n")
	if code != 0 || out != synthetic+"\n" {
		t.Fatalf("reveal -n: code=%d out=%q", code, out)
	}
	// A wrong key must not echo any stored value.
	_, out, errOut := runCLI("secret", "get", "proj/other", "--reveal")
	if strings.Contains(out+errOut, synthetic) {
		t.Fatalf("error path leaked a stored value: %q %q", out, errOut)
	}
	// `secret list` must not print values either.
	_, out, errOut = runCLI("secret", "list", "proj")
	if strings.Contains(out+errOut, synthetic) {
		t.Fatalf("secret list leaked a value: %q %q", out, errOut)
	}
}
