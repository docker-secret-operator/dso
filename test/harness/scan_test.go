package harness

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSentinel = "dso-it-0123456789ab-v2"

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanFindsEveryEncodingAndNeverEchoesTheValue(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "clean.log"), "agent started\nrotation finished\n")
	write(t, filepath.Join(dir, "plain.log"), "value="+testSentinel+"\n")
	write(t, filepath.Join(dir, "state", "hex.json"), hex.EncodeToString([]byte(testSentinel)))
	write(t, filepath.Join(dir, "b64.txt"), "x:"+base64.StdEncoding.EncodeToString([]byte(testSentinel)))
	// base64 at the other two alignments
	write(t, filepath.Join(dir, "b64b.txt"), base64.StdEncoding.EncodeToString([]byte("a"+testSentinel+"zz")))
	write(t, filepath.Join(dir, "b64c.txt"), base64.StdEncoding.EncodeToString([]byte("ab"+testSentinel+"zzz")))

	got, err := ScanTree(dir, testSentinel, nil)
	if err != nil {
		t.Fatal(err)
	}
	hit := map[string]bool{}
	for _, f := range got {
		hit[filepath.Base(f.Path)] = true
		if strings.Contains(f.String(), testSentinel) {
			t.Fatalf("a finding echoed the secret: %q", f.String())
		}
	}
	for _, want := range []string{"plain.log", "hex.json", "b64.txt", "b64b.txt", "b64c.txt"} {
		if !hit[want] {
			t.Errorf("%s: leak not detected", want)
		}
	}
	if hit["clean.log"] {
		t.Error("false positive on a clean file")
	}
}

func TestScanTreeSkipAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "secrets", "v"), testSentinel) // legitimately holds the value
	write(t, filepath.Join(dir, "agent.log"), "ok")
	got, err := ScanTree(dir, testSentinel, func(p string) bool {
		return strings.Contains(p, string(filepath.Separator)+"secrets"+string(filepath.Separator))
	})
	if err != nil || len(got) != 0 {
		t.Fatalf("skip not honoured: %v %v", got, err)
	}
	if _, err := ScanFiles(testSentinel, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file must be an error, not a clean result")
	}
	if os.Getuid() != 0 {
		unread := filepath.Join(dir, "locked.log")
		write(t, unread, testSentinel)
		if err := os.Chmod(unread, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := ScanTree(dir, testSentinel, func(p string) bool { return strings.Contains(p, "secrets") }); err == nil {
			t.Fatal("an unreadable file must be an error: unscanned is not clean")
		}
	}
}

func TestScanRejectsTooShortSentinel(t *testing.T) {
	if _, err := ScanReader(strings.NewReader("abc"), "abc", "x"); err == nil {
		t.Fatal("a 3-byte sentinel would match everything")
	}
}
