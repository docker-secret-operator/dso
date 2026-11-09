package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func writeContent(s string) func(io.Writer) error {
	return func(w io.Writer) error { _, err := io.WriteString(w, s); return err }
}

func failingWriter(partial string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, _ = io.WriteString(w, partial)
		return errors.New("injected write failure")
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestExport_NewFileIs0600EvenWithPermissiveUmask(t *testing.T) {
	old := syscall.Umask(0) // would yield 0666 under the old os.Create
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), ".env.local")
	if err := writeExportFile(path, false, writeContent("A=1\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(path); string(b) != "A=1\n" {
		t.Errorf("content = %q", b)
	}
}

func TestExport_RefusesToOverwriteByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("PRECIOUS=data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeExportFile(path, false, writeContent("NEW=1\n"))
	if err == nil || !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("expected refusal mentioning --overwrite, got %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "PRECIOUS=data\n" {
		t.Errorf("existing file was modified: %q", b)
	}
}

func TestExport_DoesNotFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("do not touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(victim, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := writeExportFile(link, false, writeContent("X=1\n")); err == nil {
		t.Fatal("export through a symlink must be refused")
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := writeExportFile(dangling, false, writeContent("X=1\n")); err == nil {
		t.Fatal("export through a dangling symlink must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Error("dangling symlink target was created")
	}
	if b, _ := os.ReadFile(victim); string(b) != "do not touch" {
		t.Errorf("symlink target modified: %q", b)
	}
}

func TestExport_OverwriteReplacesAtomicallyWith0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("OLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeExportFile(path, true, writeContent("NEW=2\n")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "NEW=2\n" {
		t.Errorf("content = %q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode after overwrite = %o, want 600", fi.Mode().Perm())
	}
	if names := dirEntries(t, filepath.Dir(path)); len(names) != 1 {
		t.Errorf("leftover files: %v", names)
	}
}

func TestExport_OverwriteReplacesSymlinkWithoutWritingThroughIt(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	_ = os.WriteFile(victim, []byte("do not touch"), 0o600)
	link := filepath.Join(dir, "out")
	if err := os.Symlink(victim, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := writeExportFile(link, true, writeContent("S=1\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "do not touch" {
		t.Errorf("overwrite wrote through the symlink: %q", b)
	}
	if b, _ := os.ReadFile(link); string(b) != "S=1\n" {
		t.Errorf("link replaced content = %q", b)
	}
}

func TestExport_FailedExportRemovesIncompleteNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")
	if err := writeExportFile(path, false, failingWriter("PARTIAL=1\n")); err == nil {
		t.Fatal("expected error")
	}
	if names := dirEntries(t, dir); len(names) != 0 {
		t.Errorf("incomplete export left behind: %v", names)
	}
}

func TestExport_FailedOverwriteKeepsOriginalAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")
	if err := os.WriteFile(path, []byte("ORIGINAL=1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeExportFile(path, true, failingWriter("PARTIAL=1\n")); err == nil {
		t.Fatal("expected error")
	}
	if b, _ := os.ReadFile(path); string(b) != "ORIGINAL=1\n" {
		t.Errorf("original destroyed by failed overwrite: %q", b)
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("temp files left: %v", names)
	}
}

func TestExport_FailedExportNeverDeletesPreexistingFile(t *testing.T) {
	// The refusal path must not "clean up" a file it did not create.
	path := filepath.Join(t.TempDir(), ".env.local")
	_ = os.WriteFile(path, []byte("MINE=1\n"), 0o600)
	_ = writeExportFile(path, false, failingWriter("x"))
	if b, err := os.ReadFile(path); err != nil || string(b) != "MINE=1\n" {
		t.Errorf("pre-existing file was removed or changed: %v %q", err, b)
	}
}

func TestExport_MissingDirectoryIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", ".env.local")
	if err := writeExportFile(path, false, writeContent("A=1\n")); err == nil {
		t.Fatal("expected error")
	}
	if err := writeExportFile(path, true, writeContent("A=1\n")); err == nil {
		t.Fatal("expected error (overwrite)")
	}
}

// Race-safe creation: many concurrent exporters, exactly one wins, and the
// winner's file is complete and uncorrupted.
func TestExport_ConcurrentCreateExactlyOneWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	const n = 24
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- writeExportFile(path, false, writeContent(strings.Repeat(fmt.Sprintf("W%d=x\n", i), 200)))
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d exporters succeeded, want exactly 1", wins)
	}
	b, _ := os.ReadFile(path)
	first := strings.SplitN(string(b), "\n", 2)[0]
	if string(b) != strings.Repeat(first+"\n", 200) {
		t.Error("winning file is interleaved/corrupted")
	}
}

func TestExportCmd_HasOverwriteFlagDefaultingOff(t *testing.T) {
	f := NewExportCmd().Flags().Lookup("overwrite")
	if f == nil || f.DefValue != "false" {
		t.Fatalf("--overwrite flag missing or defaults on: %+v", f)
	}
}

// ---- dso logs --api client -------------------------------------------------

func TestFetchEvents_SendsBearerTokenWhenConfigured(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth != "Bearer test-token-0123456789" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[{"message":"hello"}]`))
	}))
	defer srv.Close()

	t.Setenv("DSO_AUTH_TOKEN", "test-token-0123456789")
	if ev := fetchEvents(srv.URL); len(ev) != 1 {
		t.Fatalf("authenticated fetch returned %v (auth header %q)", ev, gotAuth)
	}
	t.Setenv("DSO_AUTH_TOKEN", "")
	if ev := fetchEvents(srv.URL); ev != nil {
		t.Fatalf("unauthenticated fetch should yield nothing, got %v", ev)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header sent without a configured token: %q", gotAuth)
	}
}
