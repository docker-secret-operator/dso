//go:build unix

package sqlite

// Tests for PreserveActiveFiles (see preserve.go). The sources are plain files
// with arbitrary bytes wherever the primitive must not care what they contain;
// crash-state fixtures come from helper processes that SIGKILL themselves so the
// -wal is left exactly as an interrupted process leaves it. A "concurrent writer"
// is a test hook that runs at an exact point of an attempt (deterministic) or a
// helper process that writes without pause (real). Every failure asserts that no
// preservation or ".partial" directory is left behind and that the originals are
// untouched.

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

// ---- helper process ---------------------------------------------------------------------------

// TestHelperProcessPreserve is not a real test: it runs only when
// DSO_TEST_PRESERVE_MODE is set.
//
//	wal-kill       write N committed rows through the WAL with autocheckpoint off,
//	               then SIGKILL itself (no close, no checkpoint)
//	hammer         rewrite 8 bytes at the start of a file, forever
//	sqlite-writer  insert rows, one commit each, until asked to pause (it then keeps
//	               the database OPEN but idle) or until a bound is reached
//	preserve-kill  run the preservation itself and SIGKILL at DSO_TEST_PRESERVE_KILLPOINT
func TestHelperProcessPreserve(t *testing.T) {
	mode := os.Getenv("DSO_TEST_PRESERVE_MODE")
	if mode == "" {
		t.Skip("helper process")
	}
	path := os.Getenv("DSO_TEST_PRESERVE_PATH")
	switch mode {
	case "hammer":
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		var buf [8]byte
		for i := uint64(1); ; i++ {
			for b := 0; b < 8; b++ {
				buf[b] = byte(i >> (8 * b))
			}
			if _, err := f.WriteAt(buf[:], 0); err != nil {
				os.Exit(2)
			}
		}
	case "preserve-kill":
		pvPreserveKillChild(path, os.Getenv("DSO_TEST_PRESERVE_KILLPOINT"))
	case "wal-kill", "sqlite-writer":
		// Pre-create the file private, as sqlite.Open does, so the preservation's
		// private-file rules accept it whatever the umask.
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
			_ = f.Close()
		}
		// wal-kill keeps every frame in the -wal; the continuous writer lets SQLite
		// checkpoint normally so the files stay a bounded size.
		autockpt := "wal_autocheckpoint(0)"
		if mode == "sqlite-writer" {
			autockpt = "wal_autocheckpoint(1000)"
		}
		db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma="+autockpt+"&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		db.SetMaxOpenConns(1)
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS t(id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if mode == "wal-kill" {
			tx, _ := db.Begin()
			for i := 0; i < 200; i++ {
				if _, err := tx.Exec(`INSERT INTO t(v) VALUES (?)`, strings.Repeat("x", 100)); err != nil {
					os.Exit(2)
				}
			}
			if err := tx.Commit(); err != nil {
				os.Exit(2)
			}
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			select {}
		}
		// pauseIfAsked: while <path>.pause exists the writer is idle (database still
		// open) and says so through <path>.paused.
		pauseIfAsked := func() {
			if _, err := os.Lstat(path + ".pause"); err != nil {
				return
			}
			_ = os.WriteFile(path+".paused", nil, 0o600)
			for {
				if _, err := os.Lstat(path + ".pause"); err != nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			_ = os.Remove(path + ".paused")
		}
		// A paced, bounded writer: it must be active for the whole test, not fill the disk.
		for i := 0; i < 150000; i++ {
			pauseIfAsked()
			if _, err := db.Exec(`INSERT INTO t(v) VALUES (?)`, strings.Repeat("y", 200)); err != nil {
				os.Exit(2)
			}
			time.Sleep(50 * time.Microsecond)
		}
		for {
			pauseIfAsked()
			time.Sleep(time.Millisecond)
		}
	}
}

func pvHelper(t *testing.T, mode, path string) *exec.Cmd {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessPreserve$") // #nosec G204 -- this test binary
	c.Env = append(os.Environ(), "DSO_TEST_PRESERVE_MODE="+mode, "DSO_TEST_PRESERVE_PATH="+path)
	c.Stderr = os.Stderr
	return c
}

// ---- fixtures and assertions -----------------------------------------------------------------------

func pvDir(t *testing.T) string { return securefstest.TempDir(t) }

func pvBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func pvWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// pvDB creates <dir>/c.db with random bytes and the requested companions.
func pvDB(t *testing.T, wal, shm, journal bool) (dir, path string) {
	t.Helper()
	dir = pvDir(t)
	path = filepath.Join(dir, "c.db")
	pvWrite(t, path, pvBytes(20000))
	if wal {
		pvWrite(t, path+"-wal", pvBytes(9000))
	}
	if shm {
		pvWrite(t, path+"-shm", pvBytes(4000))
	}
	if journal {
		pvWrite(t, path+"-journal", pvBytes(3000))
	}
	return dir, path
}

func pvHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type pvState struct {
	exists bool
	hash   string
	mtime  time.Time
	ino    uint64
}

// pvStates records the original files so a later call can prove nothing changed.
func pvStates(t *testing.T, path string) map[string]pvState {
	t.Helper()
	out := map[string]pvState{}
	for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
		fi, err := os.Lstat(path + sfx)
		if err != nil {
			out[sfx] = pvState{}
			continue
		}
		var ino uint64
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			ino = uint64(st.Ino)
		}
		out[sfx] = pvState{exists: true, hash: pvHash(t, path+sfx), mtime: fi.ModTime(), ino: ino}
	}
	return out
}

func pvSame(t *testing.T, before, after map[string]pvState) {
	t.Helper()
	for k, b := range before {
		a := after[k]
		if a.exists != b.exists || a.hash != b.hash || !a.mtime.Equal(b.mtime) || a.ino != b.ino {
			t.Fatalf("the original %q changed: before %+v, after %+v", k, b, a)
		}
	}
}

// pvLeftovers lists preservation and partial directories in dir.
func pvLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.Contains(e.Name(), ".pre-restore-raw-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func pvNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	if l := pvLeftovers(t, dir); len(l) != 0 {
		t.Fatalf("directories left behind: %v", l)
	}
}

func pvTest() *preserver {
	pv := newPreserver()
	pv.retryDelay = time.Millisecond
	return pv
}

func pvIdentity(t *testing.T, path string) FileIdentity {
	t.Helper()
	id, err := StatIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func pvMustPreserve(t *testing.T, pv *preserver, path string) *Preservation {
	t.Helper()
	p, err := pv.preserve(path, pvIdentity(t, path))
	if err != nil {
		t.Fatalf("preserve: %v", err)
	}
	return p
}

func pvAppend(t *testing.T, path string, b []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
}

// ---- identity -----------------------------------------------------------------------------------------

func TestStatIdentity(t *testing.T) {
	t.Parallel()
	dir := pvDir(t)
	a := filepath.Join(dir, "a")
	pvWrite(t, a, []byte("a"))
	id1, err := StatIdentity(a)
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := StatIdentity(a)
	fi, _ := os.Stat(a)
	if !id1.matches(fi) || !id2.matches(fi) || !id1.valid() {
		t.Fatal("the identity of a file must match that file")
	}
	// A different file does not match; neither does a replaced one.
	b := filepath.Join(dir, "b")
	pvWrite(t, b, []byte("a"))
	fb, _ := os.Stat(b)
	if id1.matches(fb) {
		t.Fatal("a different file must not match")
	}
	if err := os.Rename(b, a); err != nil {
		t.Fatal(err)
	}
	fa, _ := os.Stat(a)
	if id1.matches(fa) {
		t.Fatal("a file renamed over the path must not match the old identity")
	}
	if (FileIdentity{}).valid() || (FileIdentity{}).matches(fa) {
		t.Fatal("the zero identity is invalid and matches nothing")
	}
	// Links, directories and missing files are refused.
	if err := os.Symlink(a, filepath.Join(dir, "l")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, p := range []string{filepath.Join(dir, "l"), dir, filepath.Join(dir, "absent")} {
		if _, err := StatIdentity(p); err == nil {
			t.Fatalf("StatIdentity(%s) must fail", p)
		}
	}
}

// ---- the happy path: every companion combination ---------------------------------------------------------

var pvDirName = regexp.MustCompile(`\.pre-restore-raw-\d{8}T\d{6}Z-[0-9a-f]{8}$`)

func TestPreserve_EveryCompanionCombination(t *testing.T) {
	t.Parallel()
	for mask := 0; mask < 8; mask++ {
		wal, shm, journal := mask&1 != 0, mask&2 != 0, mask&4 != 0
		t.Run(fmt.Sprintf("wal=%v,shm=%v,journal=%v", wal, shm, journal), func(t *testing.T) {
			t.Parallel()
			dir, path := pvDB(t, wal, shm, journal)
			before := pvStates(t, path)
			p := pvMustPreserve(t, pvTest(), path)
			pvSame(t, before, pvStates(t, path))

			if !pvDirName.MatchString(p.Dir()) || filepath.Dir(p.Dir()) != dir {
				t.Fatalf("unexpected directory %s", p.Dir())
			}
			want := map[string]string{"main": path}
			if wal {
				want["wal"] = path + "-wal"
			}
			if shm {
				want["shm"] = path + "-shm"
			}
			if journal {
				want["journal"] = path + "-journal"
			}
			ents, _ := os.ReadDir(p.Dir())
			var names []string
			for _, e := range ents {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			exp := []string{"manifest.json"}
			for n := range want {
				exp = append(exp, n)
			}
			sort.Strings(exp)
			if strings.Join(names, ",") != strings.Join(exp, ",") {
				t.Fatalf("directory holds %v, want %v", names, exp)
			}
			for n, src := range want {
				if pvHash(t, filepath.Join(p.Dir(), n)) != pvHash(t, src) {
					t.Fatalf("%s: the copy differs from the original", n)
				}
				fi, _ := os.Stat(filepath.Join(p.Dir(), n))
				if fi.Mode().Perm() != 0o600 {
					t.Fatalf("%s has mode %v", n, fi.Mode().Perm())
				}
			}
			if fi, _ := os.Stat(p.Dir()); fi.Mode().Perm() != 0o700 {
				t.Fatalf("directory mode %v", fi.Mode().Perm())
			}
			if len(p.Files()) != len(want) {
				t.Fatalf("record has %d files, want %d", len(p.Files()), len(want))
			}
			for _, f := range p.Files() {
				if f.SHA256 != pvHash(t, want[f.Name]) || f.Advisory != (f.Name == "shm") || f.Source != want[f.Name] {
					t.Fatalf("bad record %+v", f)
				}
			}
			if err := p.Unchanged(); err != nil {
				t.Fatalf("Unchanged right after preserving: %v", err)
			}
			if l := pvLeftovers(t, dir); len(l) != 1 || strings.HasSuffix(l[0], ".partial") {
				t.Fatalf("only the published directory may remain: %v", l)
			}
		})
	}
}

func TestPreserve_ManifestDescribesTheCopy(t *testing.T) {
	t.Parallel()
	_, path := pvDB(t, true, true, false)
	p := pvMustPreserve(t, pvTest(), path)
	b, err := os.ReadFile(filepath.Join(p.Dir(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || m.Source != path || len(m.Files) != 3 || !strings.Contains(m.Note, "not an atomic snapshot") {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if !sort.SliceIsSorted(m.Files, func(i, j int) bool { return m.Files[i].Name < m.Files[j].Name }) {
		t.Fatal("manifest files must be sorted")
	}
	for _, f := range m.Files {
		if f.SHA256 != pvHash(t, filepath.Join(p.Dir(), f.Name)) || f.Advisory != (f.Name == "shm") {
			t.Fatalf("manifest entry %+v does not describe the copy", f)
		}
	}
	if _, ino := m.Device, m.Inode; ino == 0 {
		t.Fatal("the manifest should record the file identity on this platform")
	}
	if fi, _ := os.Stat(filepath.Join(p.Dir(), "manifest.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode %v", fi.Mode().Perm())
	}
}

func TestPreserve_FilesReturnsACopy(t *testing.T) {
	t.Parallel()
	_, path := pvDB(t, false, false, false)
	p := pvMustPreserve(t, pvTest(), path)
	f := p.Files()
	f[0].SHA256 = "tampered"
	if p.Files()[0].SHA256 == "tampered" {
		t.Fatal("Files must return a copy")
	}
}

func TestPreserveActiveFiles_PublicEntryPoint(t *testing.T) {
	t.Parallel()
	_, path := pvDB(t, true, false, false)
	p, err := PreserveActiveFiles(path, pvIdentity(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Dir(), "wal")); err != nil {
		t.Fatal(err)
	}
	if _, err := PreserveActiveFiles(path, FileIdentity{}); err == nil {
		t.Fatal("the zero identity must be refused")
	}
	pv := newPreserver()
	if pv.attempts != 3 || pv.retryDelay <= 0 || pv.mkdir == nil || pv.createFile == nil || pv.syncDir == nil || pv.freeSpace != nil {
		t.Fatalf("unexpected production defaults: %+v", pv)
	}
}

// ---- the source is read as bytes, never opened as a database -------------------------------------------------

func TestPreserve_DoesNotNeedASQLiteFile(t *testing.T) {
	t.Parallel()
	for name, content := range map[string][]byte{
		"zero-byte":   {},
		"garbage":     pvBytes(5000),
		"text":        []byte(strings.Repeat("not a database\n", 50)),
		"bad-header":  append([]byte("SQLite format 3\x00"), pvBytes(4000)...),
		"tiny-sqlite": append([]byte("SQLite format 3\x00"), make([]byte, 100)...),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := pvDir(t)
			path := filepath.Join(dir, "c.db")
			pvWrite(t, path, content)
			before := pvStates(t, path)
			entriesBefore, _ := os.ReadDir(dir)
			p := pvMustPreserve(t, pvTest(), path)
			pvSame(t, before, pvStates(t, path))
			got, _ := os.ReadFile(filepath.Join(p.Dir(), "main"))
			if string(got) != string(content) {
				t.Fatal("the copy is not byte-identical")
			}
			// Nothing but the preservation directory was added: no -wal, -shm, -journal
			// or lock file, which opening the file through SQLite could have created.
			entriesAfter, _ := os.ReadDir(dir)
			if len(entriesAfter) != len(entriesBefore)+1 {
				t.Fatalf("directory entries went from %d to %d", len(entriesBefore), len(entriesAfter))
			}
		})
	}
}

func TestPreserve_ReadOnlySourceIsFine(t *testing.T) {
	t.Parallel()
	_, path := pvDB(t, true, false, false)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path+"-wal", 0o400); err != nil {
		t.Fatal(err)
	}
	before := pvStates(t, path)
	pvMustPreserve(t, pvTest(), path)
	pvSame(t, before, pvStates(t, path))
}

// ---- stability: concurrent changes -----------------------------------------------------------------------------

func TestPreserve_ChangeDuringTheFirstAttemptIsRetriedAndTheFinalCopyIsConsistent(t *testing.T) {
	t.Parallel()
	dir, path := pvDB(t, true, false, false)
	pv := pvTest()
	calls := 0
	pv.afterCopy = func(n int) {
		calls++
		if n == 1 {
			pvAppend(t, path, []byte("late write"))
			pvAppend(t, path+"-wal", []byte("late frame"))
		}
	}
	p := pvMustPreserve(t, pv, path)
	if calls != 2 {
		t.Fatalf("expected a retry (2 attempts), got %d", calls)
	}
	if pvHash(t, filepath.Join(p.Dir(), "main")) != pvHash(t, path) || pvHash(t, filepath.Join(p.Dir(), "wal")) != pvHash(t, path+"-wal") {
		t.Fatal("the kept copy must be the final, stable state of the files")
	}
	if l := pvLeftovers(t, dir); len(l) != 1 {
		t.Fatalf("the failed attempt's partial directory must be gone: %v", l)
	}
}

func TestPreserve_ChangeOnEveryAttemptFailsClosed(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"", "-wal", "-journal"} {
		t.Run("target"+target, func(t *testing.T) {
			t.Parallel()
			dir, path := pvDB(t, true, false, true)
			pv := pvTest()
			calls := 0
			pv.afterCopy = func(int) { calls++; pvAppend(t, path+target, []byte{byte(calls)}) }
			_, err := pv.preserve(path, pvIdentity(t, path))
			if !errors.Is(err, ErrPreserveUnstable) {
				t.Fatalf("want ErrPreserveUnstable, got %v", err)
			}
			if calls != pv.attempts {
				t.Fatalf("%d attempts made, want %d", calls, pv.attempts)
			}
			pvNoLeftovers(t, dir)
		})
	}
}

func TestPreserve_ModifiedThenRevertedIsDetected(t *testing.T) {
	t.Parallel()
	if !ctimeAvailable {
		t.Skip("needs the platform change time")
	}
	dir, path := pvDB(t, false, false, false)
	pv := pvTest()
	pv.afterCopy = func(int) {
		orig, _ := os.ReadFile(path)
		fi, _ := os.Stat(path)
		mod := append([]byte(nil), orig...)
		mod[100] ^= 0xff
		pvWrite(t, path, mod)                          // the intermediate state
		pvWrite(t, path, orig)                         // reverted: same bytes ...
		_ = os.Chtimes(path, time.Now(), fi.ModTime()) // ... and the same modification time
	}
	if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveUnstable) {
		t.Fatalf("a write that was made and reverted must still be detected through the change time: %v", err)
	}
	pvNoLeftovers(t, dir)
}

func TestPreserve_ContentChangeIsCaughtByTheHashAlone(t *testing.T) {
	t.Parallel()
	// The stat signature is made to match again, so only the second hash can see it.
	_, path := pvDB(t, false, false, false)
	pv := pvTest()
	srcs, err := pv.openSources(path, pvIdentity(t, path))
	if err != nil {
		t.Fatal(err)
	}
	defer srcs[0].f.Close()
	partial := filepath.Join(pvDir(t), "p")
	if err := os.Mkdir(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := pv.copyOne(partial, srcs[0]); err != nil {
		t.Fatal(err)
	}
	if ae := verifyStable(path, srcs, pvIdentity(t, path)); ae != nil {
		t.Fatalf("unchanged files must verify: %v", ae)
	}
	b, _ := os.ReadFile(path)
	b[10] ^= 1 // same size
	pvWrite(t, path, b)
	fi, _ := srcs[0].f.Stat()
	srcs[0].sig = statSigOf(fi) // pretend the signature did not move
	ae := verifyStable(path, srcs, pvIdentity(t, path))
	if ae == nil || !ae.retry || !strings.Contains(ae.Error(), "content changed") {
		t.Fatalf("the hash must catch a same-size change: %v", ae)
	}
}

func TestPreserve_PathReplacementIsCaughtByThePathCheckAlone(t *testing.T) {
	t.Parallel()
	// Renaming a file over the path also moves the old inode's change time, so the
	// signature would catch it on Linux. Here the signature is made to match again,
	// leaving only the check that the path still names the open file.
	_, path := pvDB(t, false, false, false)
	id := pvIdentity(t, path)
	pv := pvTest()
	srcs, err := pv.openSources(path, id)
	if err != nil {
		t.Fatal(err)
	}
	defer srcs[0].f.Close()
	partial := filepath.Join(pvDir(t), "p")
	if err := os.Mkdir(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := pv.copyOne(partial, srcs[0]); err != nil {
		t.Fatal(err)
	}
	other := path + ".other"
	pvWrite(t, other, pvBytes(100))
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	fi, _ := srcs[0].f.Stat()
	srcs[0].sig = statSigOf(fi) // neutralise the signature
	ae := verifyStable(path, srcs, id)
	if ae == nil || ae.retry || !errors.Is(ae, ErrPreserveIdentityChanged) {
		t.Fatalf("the path check must report that the identified file was replaced: %v", ae)
	}
	// A companion replaced the same way is a retryable change, not an identity failure.
	_, path2 := pvDB(t, true, false, false)
	srcs2, err := pv.openSources(path2, pvIdentity(t, path2))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, s := range srcs2 {
			_ = s.f.Close()
		}
	}()
	partial2 := filepath.Join(pvDir(t), "p2")
	if err := os.Mkdir(partial2, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs2 {
		if err := pv.copyOne(partial2, s); err != nil {
			t.Fatal(err)
		}
	}
	pvWrite(t, path2+"-wal.other", pvBytes(100))
	if err := os.Rename(path2+"-wal.other", path2+"-wal"); err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs2 {
		fi, _ := s.f.Stat()
		s.sig = statSigOf(fi)
	}
	if ae := verifyStable(path2, srcs2, pvIdentity(t, path2)); ae == nil || !ae.retry {
		t.Fatalf("a replaced -wal is a retryable change: %v", ae)
	}
}

func TestPreserve_SignatureChangeAloneIsDetected(t *testing.T) {
	t.Parallel()
	// Identical bytes, but the file was touched: the signature differs.
	dir, path := pvDB(t, false, false, false)
	pv := pvTest()
	pv.afterCopy = func(int) { _ = os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)) }
	if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveUnstable) {
		t.Fatalf("want ErrPreserveUnstable, got %v", err)
	}
	pvNoLeftovers(t, dir)
}

func TestPreserve_SidecarsAppearingAndDisappearing(t *testing.T) {
	t.Parallel()
	t.Run("wal-appears", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.afterCopy = func(n int) {
			if n == 1 {
				pvWrite(t, path+"-wal", pvBytes(500))
			}
		}
		p := pvMustPreserve(t, pv, path)
		if _, ok := p.record("wal"); !ok {
			t.Fatal("the retry must include the -wal that appeared")
		}
	})
	t.Run("journal-appears", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.afterCopy = func(n int) {
			if n == 1 {
				pvWrite(t, path+"-journal", pvBytes(500))
			}
		}
		if _, ok := pvMustPreserve(t, pv, path).record("journal"); !ok {
			t.Fatal("the retry must include the -journal that appeared")
		}
	})
	t.Run("wal-disappears", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, true, false, false)
		pv := pvTest()
		pv.afterCopy = func(n int) {
			if n == 1 {
				_ = os.Remove(path + "-wal")
			}
		}
		p := pvMustPreserve(t, pv, path)
		if _, ok := p.record("wal"); ok {
			t.Fatal("the retry must not claim a -wal that is gone")
		}
	})
	t.Run("wal-appears-on-every-attempt", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.afterCopy = func(int) {
			if _, err := os.Lstat(path + "-wal"); err != nil {
				pvWrite(t, path+"-wal", pvBytes(100))
			} else {
				_ = os.Remove(path + "-wal")
			}
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveUnstable) {
			t.Fatalf("want ErrPreserveUnstable, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("wal-vanishes-before-it-can-be-opened", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, true, false, false)
		pv := pvTest()
		pv.beforeOpen = func(n int) {
			if n == 1 {
				// An entry that Lstat sees but whose open fails is a race; simulate the
				// closest observable thing: it is gone by the time the attempt looks.
				_ = os.Remove(path + "-wal")
			}
		}
		p := pvMustPreserve(t, pv, path)
		if _, ok := p.record("wal"); ok {
			t.Fatal("no -wal exists any more")
		}
	})
}

func TestPreserve_ShmIsAdvisory(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(t *testing.T, path string){
		"appears":    func(t *testing.T, path string) { pvWrite(t, path+"-shm", pvBytes(300)) },
		"rewritten":  func(t *testing.T, path string) { pvWrite(t, path+"-shm", pvBytes(4000)) },
		"disappears": func(t *testing.T, path string) { _ = os.Remove(path + "-shm") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, path := pvDB(t, true, name != "appears", false)
			pv := pvTest()
			calls := 0
			pv.afterCopy = func(int) { calls++; change(t, path) }
			p := pvMustPreserve(t, pv, path)
			if calls != 1 {
				t.Fatalf("a -shm change must not force a retry (%d attempts)", calls)
			}
			// ... and the recheck ignores it too.
			if err := p.Unchanged(); err != nil {
				t.Fatalf("Unchanged must ignore the -shm: %v", err)
			}
		})
	}
}

// ---- file identity and path replacement --------------------------------------------------------------------------

func TestPreserve_IdentityChanges(t *testing.T) {
	t.Parallel()
	replace := func(t *testing.T, path string) {
		other := path + ".other"
		pvWrite(t, other, pvBytes(100))
		if err := os.Rename(other, path); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("replaced-before-the-call", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		id := pvIdentity(t, path)
		replace(t, path)
		before := pvStates(t, path)
		_, err := pvTest().preserve(path, id)
		if !errors.Is(err, ErrPreserveIdentityChanged) {
			t.Fatalf("want ErrPreserveIdentityChanged, got %v", err)
		}
		pvSame(t, before, pvStates(t, path))
		pvNoLeftovers(t, dir)
	})
	t.Run("replaced-before-the-first-open", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.beforeOpen = func(n int) {
			if n == 1 {
				replace(t, path)
			}
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveIdentityChanged) {
			t.Fatalf("want ErrPreserveIdentityChanged, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("replaced-while-it-is-being-copied", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, true, false, false)
		pv := pvTest()
		pv.afterCopy = func(int) { replace(t, path) }
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveIdentityChanged) {
			t.Fatalf("want ErrPreserveIdentityChanged, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("removed", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		id := pvIdentity(t, path)
		_ = os.Remove(path)
		if _, err := pvTest().preserve(path, id); !errors.Is(err, ErrPreserveIdentityChanged) {
			t.Fatalf("want ErrPreserveIdentityChanged, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("swapped-for-a-symlink", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		id := pvIdentity(t, path)
		real := path + ".real"
		if err := os.Rename(path, real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		before := pvHash(t, real)
		_, err := pvTest().preserve(path, id)
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("a link must be refused, got %v", err)
		}
		if pvHash(t, real) != before {
			t.Fatal("the link target changed")
		}
		pvNoLeftovers(t, dir)
	})
}

func TestPreserve_RefusesUnsafeSources(t *testing.T) {
	t.Parallel()
	t.Run("main-readable-by-others", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := pvTest().preserve(path, pvIdentity(t, path)); err == nil || !strings.Contains(err.Error(), "must not be accessible") {
			t.Fatalf("want the private-file refusal, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("wal-is-a-symlink", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		target := filepath.Join(dir, "elsewhere")
		pvWrite(t, target, pvBytes(50))
		if err := os.Symlink(target, path+"-wal"); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := pvTest().preserve(path, pvIdentity(t, path)); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("a linked companion must be refused, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("journal-is-a-directory", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		if err := os.Mkdir(path+"-journal", 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := pvTest().preserve(path, pvIdentity(t, path)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("a non-regular companion must be refused, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("unsafe-directory", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		id := pvIdentity(t, path)
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if _, err := pvTest().preserve(path, id); err == nil || !strings.Contains(err.Error(), "not safe") {
			t.Fatalf("want the directory refusal, got %v", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		pvNoLeftovers(t, dir)
	})
}

// ---- failures: space, permissions, injected faults, cleanup ---------------------------------------------------------

func TestPreserve_DiskFull(t *testing.T) {
	t.Parallel()
	t.Run("enospc-while-writing-the-main-copy", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, true, false, false)
		before := pvStates(t, path)
		pv := pvTest()
		pv.wrapWriter = func(name string, w io.Writer) io.Writer {
			if name == "main" {
				return &pvFailingWriter{w: w, after: 100, err: syscall.ENOSPC}
			}
			return w
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveNoSpace) {
			t.Fatalf("want ErrPreserveNoSpace, got %v", err)
		}
		pvSame(t, before, pvStates(t, path))
		pvNoLeftovers(t, dir)
	})
	t.Run("enospc-on-a-later-file", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, true, true, true)
		pv := pvTest()
		pv.wrapWriter = func(name string, w io.Writer) io.Writer {
			if name == "shm" {
				return &pvFailingWriter{w: w, after: 10, err: syscall.ENOSPC}
			}
			return w
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveNoSpace) {
			t.Fatalf("want ErrPreserveNoSpace, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("quota-exceeded", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.wrapWriter = func(_ string, w io.Writer) io.Writer { return &pvFailingWriter{w: w, after: 0, err: syscall.EDQUOT} }
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveNoSpace) {
			t.Fatalf("want ErrPreserveNoSpace, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("enospc-creating-the-directory", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.mkdir = func(string, os.FileMode) error { return syscall.ENOSPC }
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveNoSpace) {
			t.Fatalf("want ErrPreserveNoSpace, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("precheck-insufficient-creates-nothing", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, true, false, false)
		pv := pvTest()
		pv.freeSpace = func(string) (uint64, bool, error) { return 1000, true, nil }
		pv.mkdir = func(string, os.FileMode) error {
			t.Error("nothing may be created when the space check fails")
			return nil
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveNoSpace) {
			t.Fatalf("want ErrPreserveNoSpace, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("precheck-unknown-proceeds", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.freeSpace = func(string) (uint64, bool, error) { return 0, false, nil }
		pvMustPreserve(t, pv, path)
	})
	t.Run("precheck-sufficient-proceeds", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.freeSpace = func(string) (uint64, bool, error) { return 1 << 40, true, nil }
		pvMustPreserve(t, pv, path)
	})
	t.Run("precheck-error", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.freeSpace = func(string) (uint64, bool, error) { return 0, false, errors.New("statfs failed") }
		if _, err := pv.preserve(path, pvIdentity(t, path)); err == nil || !strings.Contains(err.Error(), "statfs failed") {
			t.Fatalf("got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
}

type pvFailingWriter struct {
	w       io.Writer
	after   int
	written int
	err     error
}

func (f *pvFailingWriter) Write(p []byte) (int, error) {
	if f.written+len(p) > f.after {
		n := f.after - f.written
		if n < 0 {
			n = 0
		}
		if n > 0 {
			_, _ = f.w.Write(p[:n])
		}
		f.written += n
		return n, f.err
	}
	f.written += len(p)
	return f.w.Write(p)
}

func TestPreserve_PermissionFailures(t *testing.T) {
	t.Parallel()
	t.Run("directory-not-writable", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("permissions do not bind root")
		}
		dir, path := pvDB(t, false, false, false)
		id := pvIdentity(t, path)
		before := pvStates(t, path)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		_, err := pvTest().preserve(path, id)
		if err == nil {
			t.Fatal("preserving into a read-only directory must fail")
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Logf("failure (not a permission error as such): %v", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		pvSame(t, before, pvStates(t, path))
		pvNoLeftovers(t, dir)
	})
	t.Run("mkdir-denied", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.mkdir = func(string, os.FileMode) error { return os.ErrPermission }
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("file-creation-denied-after-the-first-file", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, true, true, false)
		pv := pvTest()
		real := pv.createFile
		n := 0
		pv.createFile = func(p string, m os.FileMode) (*os.File, error) {
			n++
			if n == 2 {
				return nil, os.ErrPermission
			}
			return real(p, m)
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("got %v", err)
		}
		pvNoLeftovers(t, dir) // the first copy was written; the partial directory is gone
	})
	t.Run("manifest-cannot-be-created", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		real := pv.createFile
		pv.createFile = func(p string, m os.FileMode) (*os.File, error) {
			if strings.HasSuffix(p, "manifest.json") {
				return nil, os.ErrPermission
			}
			return real(p, m)
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("copy-does-not-read-back-identically", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.wrapWriter = func(name string, w io.Writer) io.Writer { return &pvCorruptingWriter{w: w} }
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveVerification) {
			t.Fatalf("want ErrPreserveVerification, got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("directory-sync-of-the-partial-fails", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.syncDir = func(d string) error {
			if strings.HasSuffix(d, ".partial") {
				return errors.New("fsync failed")
			}
			return nil
		}
		if _, err := pv.preserve(path, pvIdentity(t, path)); err == nil || !strings.Contains(err.Error(), "fsync failed") {
			t.Fatalf("got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
}

type pvCorruptingWriter struct {
	w io.Writer
}

func (c *pvCorruptingWriter) Write(p []byte) (int, error) {
	q := append([]byte(nil), p...)
	if len(q) > 0 {
		q[0] ^= 0xff
	}
	return c.w.Write(q)
}

func TestPreserve_ParentDirectorySyncFailureReturnsAValidPreservation(t *testing.T) {
	t.Parallel()
	dir, path := pvDB(t, true, false, false)
	pv := pvTest()
	pv.syncDir = func(d string) error {
		if d == dir {
			return errors.New("fsync of the parent failed")
		}
		return nil
	}
	p, err := pv.preserve(path, pvIdentity(t, path))
	var de *PreserveDurabilityError
	if !errors.As(err, &de) || p == nil {
		t.Fatalf("want a preservation together with *PreserveDurabilityError, got %v / %v", p, err)
	}
	if pvHash(t, filepath.Join(p.Dir(), "main")) != pvHash(t, path) {
		t.Fatal("the returned preservation must be complete")
	}
	if l := pvLeftovers(t, dir); len(l) != 1 || strings.HasSuffix(l[0], ".partial") {
		t.Fatalf("the published directory must be kept: %v", l)
	}
}

func TestPreserve_NeverOverwritesOrDeletesAnExistingPreservation(t *testing.T) {
	t.Parallel()
	dir, path := pvDB(t, false, false, false)
	first := pvMustPreserve(t, pvTest(), path)
	marker := filepath.Join(first.Dir(), "main")
	before := pvHash(t, marker)

	// A later failure leaves the earlier, completed directory alone.
	pv := pvTest()
	pv.wrapWriter = func(_ string, w io.Writer) io.Writer { return &pvFailingWriter{w: w, after: 5, err: syscall.ENOSPC} }
	if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveNoSpace) {
		t.Fatalf("got %v", err)
	}
	if pvHash(t, marker) != before || len(pvLeftovers(t, dir)) != 1 {
		t.Fatal("a completed preservation must never be removed or changed")
	}

	// A name collision with an existing directory is refused, not overwritten.
	pv2 := pvTest()
	fixed := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	pv2.now = func() time.Time { return fixed }
	pv2.random = func(b []byte) (int, error) { copy(b, []byte{1, 2, 3, 4}); return len(b), nil }
	existing := fmt.Sprintf("%s.pre-restore-raw-%s-%s", path, "20300102T030405Z", "01020304")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	pvWrite(t, filepath.Join(existing, "keep"), []byte("keep"))
	if _, err := pv2.preserve(path, pvIdentity(t, path)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(existing, "keep")); string(b) != "keep" {
		t.Fatal("the existing directory was touched")
	}
	if _, err := os.Stat(existing + ".partial"); !os.IsNotExist(err) {
		t.Fatal("the partial directory of the refused attempt must be removed")
	}
}

func TestRemovePartial_OnlyRemovesItsOwnPartialDirectory(t *testing.T) {
	t.Parallel()
	dir := pvDir(t)
	keep := filepath.Join(dir, "x.pre-restore-raw-1")
	if err := os.Mkdir(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	removePartial(keep, dir) // no .partial suffix: refused
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("a directory without the .partial suffix must not be removed")
	}
	nested := filepath.Join(dir, "sub")
	_ = os.Mkdir(nested, 0o700)
	deep := filepath.Join(nested, "y.partial")
	_ = os.Mkdir(deep, 0o700)
	removePartial(deep, dir) // not directly inside dir: refused
	if _, err := os.Stat(deep); err != nil {
		t.Fatal("a .partial directory elsewhere must not be removed")
	}
	own := filepath.Join(dir, "z.partial")
	_ = os.Mkdir(own, 0o700)
	pvWrite(t, filepath.Join(own, "f"), []byte("x"))
	removePartial(own, dir)
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatal("its own partial directory must be removed")
	}
}

// ---- Unchanged -----------------------------------------------------------------------------------------------------

func TestUnchanged(t *testing.T) {
	t.Parallel()
	t.Run("modified-main", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		p := pvMustPreserve(t, pvTest(), path)
		pvAppend(t, path, []byte("x"))
		if err := p.Unchanged(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("same-size-change", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		p := pvMustPreserve(t, pvTest(), path)
		b, _ := os.ReadFile(path)
		b[5] ^= 1
		pvWrite(t, path, b)
		if err := p.Unchanged(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("main-replaced-by-identical-bytes", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		p := pvMustPreserve(t, pvTest(), path)
		b, _ := os.ReadFile(path)
		other := path + ".copy"
		pvWrite(t, other, b)
		if err := os.Rename(other, path); err != nil {
			t.Fatal(err)
		}
		if err := p.Unchanged(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("a different file with the same bytes is still not the identified file: %v", err)
		}
	})
	t.Run("wal-changes-appears-disappears", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, true, false, false)
		p := pvMustPreserve(t, pvTest(), path)
		pvAppend(t, path+"-wal", []byte("x"))
		if err := p.Unchanged(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("modified -wal: %v", err)
		}
		_ = os.Remove(path + "-wal")
		if err := p.Unchanged(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("vanished -wal: %v", err)
		}
		_, path2 := pvDB(t, false, false, false)
		p2 := pvMustPreserve(t, pvTest(), path2)
		pvWrite(t, path2+"-wal", pvBytes(10))
		if err := p2.Unchanged(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("new -wal: %v", err)
		}
	})
	t.Run("journal-appears", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, false, false)
		p := pvMustPreserve(t, pvTest(), path)
		pvWrite(t, path+"-journal", pvBytes(10))
		if err := p.Unchanged(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("shm-is-ignored", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, false, true, false)
		p := pvMustPreserve(t, pvTest(), path)
		pvWrite(t, path+"-shm", pvBytes(77))
		if err := p.Unchanged(); err != nil {
			t.Fatalf("the -shm is advisory: %v", err)
		}
		_ = os.Remove(path + "-shm")
		if err := p.Unchanged(); err != nil {
			t.Fatalf("the -shm is advisory: %v", err)
		}
	})
	t.Run("does-not-modify-anything", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, true, true, true)
		p := pvMustPreserve(t, pvTest(), path)
		before := pvStates(t, path)
		_ = p.Unchanged()
		pvSame(t, before, pvStates(t, path))
	})
}

// ---- a preserved main/WAL pair reopens to the same committed data -----------------------------------------------------

func pvOpenCopy(t *testing.T, files map[string]string) (rows int, integrity string) {
	t.Helper()
	dir := pvDir(t)
	for sfx, src := range files {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		pvWrite(t, filepath.Join(dir, "x.db"+sfx), b)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "x.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return -1, err.Error()
	}
	if err := db.QueryRow(`SELECT count(*) FROM t`).Scan(&rows); err != nil {
		return 0, integrity
	}
	return rows, integrity
}

func TestPreserve_CrashedWALPairReopensToTheCommittedData(t *testing.T) {
	t.Parallel()
	dir := pvDir(t)
	path := filepath.Join(dir, "c.db")
	c := pvHelper(t, "wal-kill", path)
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the helper should have been killed: %v", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", err)
	}
	wfi, err := os.Stat(path + "-wal")
	if err != nil || wfi.Size() == 0 {
		t.Fatalf("the crash must leave a non-empty -wal (%v)", err)
	}
	before := pvStates(t, path)

	p := pvMustPreserve(t, pvTest(), path)
	pvSame(t, before, pvStates(t, path))

	// The original pair, copied elsewhere and recovered by SQLite, is the reference.
	refRows, refIntegrity := pvOpenCopy(t, map[string]string{"": path, "-wal": path + "-wal"})
	if refRows != 200 || refIntegrity != "ok" {
		t.Fatalf("reference: %d rows, integrity %q", refRows, refIntegrity)
	}
	rows, integrity := pvOpenCopy(t, map[string]string{"": filepath.Join(p.Dir(), "main"), "-wal": filepath.Join(p.Dir(), "wal")})
	if rows != refRows || integrity != "ok" {
		t.Fatalf("the preserved pair reopens to %d rows (integrity %q), the original to %d", rows, integrity, refRows)
	}
	// The main file alone does not hold the committed rows: the -wal is what matters.
	if mainOnly, _ := pvOpenCopy(t, map[string]string{"": filepath.Join(p.Dir(), "main")}); mainOnly >= refRows {
		t.Fatalf("the fixture is not WAL-resident (%d rows in the main file alone)", mainOnly)
	}
}

// ---- real concurrent writers -----------------------------------------------------------------------------------------

func TestPreserve_RealWriterHammeringTheMainFileFailsClosed(t *testing.T) {
	t.Parallel()
	dir := pvDir(t)
	path := filepath.Join(dir, "c.db")
	pvWrite(t, path, pvBytes(4<<20))
	c := pvHelper(t, "hammer", path)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	first, _ := os.ReadFile(path)
	deadline := time.Now().Add(5 * time.Second)
	for {
		now, _ := os.ReadFile(path)
		if string(now[:8]) != string(first[:8]) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper never wrote")
		}
	}
	pv := pvTest()
	_, err := pv.preserve(path, pvIdentity(t, path))
	if !errors.Is(err, ErrPreserveUnstable) {
		t.Fatalf("a file being rewritten without pause must not be preserved, got %v", err)
	}
	pvNoLeftovers(t, dir)
}

// This test is about CONTENTION: against a process that never pauses, a
// preservation is either refused as unstable or, when it happens to be accepted,
// is recoverable. It does not require any acceptance (that depends on load); the
// quiet-moment test below is the one that requires an accepted preservation.
func TestPreserve_RealSQLiteWriter_Contention_RefusesCleanlyOrKeepsARecoverableCopy(t *testing.T) {
	t.Parallel()
	dir := pvDir(t)
	path := filepath.Join(dir, "c.db")
	c := pvHelper(t, "sqlite-writer", path)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() > 20000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper never produced a -wal")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var ok, unstable int
	lastRows := 0
	for i := 0; i < 20; i++ {
		id, err := StatIdentity(path)
		if err != nil {
			t.Fatal(err)
		}
		p, err := pvTest().preserve(path, id)
		switch {
		case err == nil:
			ok++
			files := map[string]string{"": filepath.Join(p.Dir(), "main")}
			if _, has := p.record("wal"); has {
				files["-wal"] = filepath.Join(p.Dir(), "wal")
			}
			rows, integrity := pvOpenCopy(t, files)
			if integrity != "ok" || rows < lastRows {
				t.Fatalf("a preservation that was accepted is not a consistent database: %d rows (previous %d), integrity %q", rows, lastRows, integrity)
			}
			lastRows = rows
		case errors.Is(err, ErrPreserveUnstable):
			unstable++
		default:
			t.Fatalf("unexpected failure: %v", err)
		}
	}
	for _, l := range pvLeftovers(t, dir) {
		if strings.HasSuffix(l, ".partial") {
			t.Fatalf("a partial directory was left behind: %s", l)
		}
	}
	t.Logf("against a real SQLite writer: %d preservations accepted (each verified recoverable), %d refused as unstable", ok, unstable)
}

func TestPreserve_ConcurrentCallsDoNotCollide(t *testing.T) {
	t.Parallel()
	_, path := pvDB(t, true, true, true)
	id := pvIdentity(t, path)
	var wg sync.WaitGroup
	dirs := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := pvTest().preserve(path, id)
			if err != nil {
				errs <- err
				return
			}
			dirs <- p.Dir()
		}()
	}
	wg.Wait()
	close(dirs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for d := range dirs {
		if seen[d] {
			t.Fatalf("two preservations share %s", d)
		}
		seen[d] = true
		if pvHash(t, filepath.Join(d, "main")) != pvHash(t, path) {
			t.Fatalf("%s is not a faithful copy", d)
		}
	}
	if len(seen) != 8 {
		t.Fatalf("%d distinct directories, want 8", len(seen))
	}
}

// ---- Phase 2B: synchronization -------------------------------------------------------------------------------

// pvPublishedDirs lists directories in dir that carry a final (non-".partial")
// preservation name.
func pvPublishedDirs(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, l := range pvLeftovers(t, dir) {
		if !strings.HasSuffix(l, ".partial") {
			out = append(out, l)
		}
	}
	return out
}

func TestPreserve_EveryCopyAndTheManifestAreSyncedBeforeAnythingIsPublished(t *testing.T) {
	t.Parallel()
	dir, path := pvDB(t, true, true, true)
	pv := pvTest()
	var events []string
	real := pv.syncFile
	pv.syncFile = func(f *os.File) error {
		// At the moment of each file sync nothing may be published yet.
		if pub := pvPublishedDirs(t, dir); len(pub) != 0 {
			t.Errorf("a final directory %v exists before %s was synced", pub, f.Name())
		}
		events = append(events, "file:"+filepath.Base(f.Name()))
		return real(f)
	}
	realDir := pv.syncDir
	pv.syncDir = func(d string) error {
		switch {
		case strings.HasSuffix(d, ".partial"):
			events = append(events, "dir:partial")
			if pub := pvPublishedDirs(t, dir); len(pub) != 0 {
				t.Errorf("published before the partial directory was synced: %v", pub)
			}
		case d == dir:
			events = append(events, "dir:parent")
			if pub := pvPublishedDirs(t, dir); len(pub) != 1 {
				t.Errorf("the parent is synced after the rename, so exactly one final directory must exist: %v", pub)
			}
		}
		return realDir(d)
	}
	before := pvStates(t, path)
	pvMustPreserve(t, pv, path)
	pvSame(t, before, pvStates(t, path))

	// Every copy and the manifest were synced; the manifest last of the files; then
	// the partial directory; then (after the rename) the parent.
	want := []string{"file:main", "file:wal", "file:journal", "file:shm", "file:manifest.json", "dir:partial", "dir:parent"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("synchronization order:\n got %v\nwant %v", events, want)
	}
}

func TestPreserve_SyncFailureStopsPublicationAndCleansUp(t *testing.T) {
	t.Parallel()
	for _, failing := range []string{"main", "wal", "journal", "shm", "manifest.json"} {
		t.Run(failing, func(t *testing.T) {
			t.Parallel()
			dir, path := pvDB(t, true, true, true)
			before := pvStates(t, path)
			pv := pvTest()
			real := pv.syncFile
			pv.syncFile = func(f *os.File) error {
				if filepath.Base(f.Name()) == failing {
					return errors.New("sync boom")
				}
				return real(f)
			}
			_, err := pv.preserve(path, pvIdentity(t, path))
			if err == nil || !strings.Contains(err.Error(), "sync boom") {
				t.Fatalf("got %v", err)
			}
			pvSame(t, before, pvStates(t, path))
			pvNoLeftovers(t, dir)
		})
	}
	t.Run("out-of-space-while-syncing", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.syncFile = func(*os.File) error { return syscall.ENOSPC }
		if _, err := pv.preserve(path, pvIdentity(t, path)); !errors.Is(err, ErrPreserveNoSpace) {
			t.Fatalf("got %v", err)
		}
		pvNoLeftovers(t, dir)
	})
	t.Run("a-skipped-sync-is-visible-to-the-order-test", func(t *testing.T) {
		t.Parallel()
		// Guard for the test above: if the production code did not call syncFile for
		// a file, the recorded events would lack it. Here a no-op seam proves the
		// seam is what the production path calls.
		_, path := pvDB(t, false, false, false)
		pv := pvTest()
		calls := 0
		pv.syncFile = func(f *os.File) error { calls++; return nil }
		pvMustPreserve(t, pv, path)
		if calls != 2 { // main + manifest
			t.Fatalf("syncFile was called %d times, want 2", calls)
		}
	})
}

// ---- Phase 2B: a real SQLite process holding the database -------------------------------------------------------

// pvPause asks the sqlite-writer helper to stop writing and waits until it says
// it has. The helper keeps the database OPEN while paused, so this is a quiet
// moment of a live external SQLite process, not a closed database.
func pvPause(t *testing.T, path string) {
	t.Helper()
	pvWrite(t, path+".pause", nil)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Lstat(path + ".paused"); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper never acknowledged the pause")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestPreserve_RealSQLiteProcessHoldingTheDatabase_QuietMomentIsAcceptedAndRecoverable(t *testing.T) {
	t.Parallel()
	dir := pvDir(t)
	path := filepath.Join(dir, "c.db")
	c := pvHelper(t, "sqlite-writer", path)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	deadline := time.Now().Add(15 * time.Second)
	for {
		if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() > 20000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper never produced a -wal")
		}
		time.Sleep(5 * time.Millisecond)
	}
	pvPause(t, path)

	// The process is alive and still has the database open (its -shm exists).
	if c.ProcessState != nil {
		t.Fatal("the helper exited")
	}
	if _, err := os.Lstat(path + "-shm"); err != nil {
		t.Fatalf("the helper should still have the database open: %v", err)
	}
	refRows, refIntegrity := pvOpenCopy(t, map[string]string{"": path, "-wal": path + "-wal"})
	if refRows <= 0 || refIntegrity != "ok" {
		t.Fatalf("reference: %d rows, integrity %q", refRows, refIntegrity)
	}
	mainBefore, walBefore := pvHash(t, path), pvHash(t, path+"-wal")

	pv := pvTest()
	attempts := 0
	pv.afterCopy = func(int) { attempts++ }
	p := pvMustPreserve(t, pv, path)
	if attempts != 1 {
		t.Fatalf("a quiet database should be accepted on the first attempt (%d attempts)", attempts)
	}
	if pvHash(t, path) != mainBefore || pvHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed")
	}
	files := map[string]string{"": filepath.Join(p.Dir(), "main")}
	if _, has := p.record("wal"); !has {
		t.Fatal("the -wal must have been preserved")
	}
	files["-wal"] = filepath.Join(p.Dir(), "wal")
	rows, integrity := pvOpenCopy(t, files)
	if rows != refRows || integrity != "ok" {
		t.Fatalf("the accepted preservation reopens to %d rows (integrity %q), the live database held %d", rows, integrity, refRows)
	}
	if err := p.Unchanged(); err != nil {
		t.Fatalf("Unchanged while the database is quiet: %v", err)
	}

	// Resume the writer: it was really paused, and writing again makes the files differ.
	if err := os.Remove(path + ".pause"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for p.Unchanged() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the writer never resumed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---- Phase 2B: SIGKILL while preserving -----------------------------------------------------------------------------

type pvKillWriter struct {
	w       io.Writer
	after   int
	written int
	kill    func()
}

func (k *pvKillWriter) Write(p []byte) (int, error) {
	n, err := k.w.Write(p)
	k.written += n
	if k.written >= k.after {
		k.kill()
	}
	return n, err
}

// pvPreserveKillChild runs inside the helper process: it preserves path and
// SIGKILLs itself at the requested point.
func pvPreserveKillChild(path, point string) {
	kill := func() {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	}
	id, err := StatIdentity(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	pv := newPreserver()
	switch point {
	case "copy": // part of the way through copying the main file
		pv.wrapWriter = func(name string, w io.Writer) io.Writer {
			if name != "main" {
				return w
			}
			return &pvKillWriter{w: w, after: 1 << 20, kill: kill}
		}
	case "aftercopy": // every file copied and synced, nothing verified or published
		pv.afterCopy = func(int) { kill() }
	case "presync": // manifest written and synced, partial directory about to be synced and renamed
		real := pv.syncDir
		pv.syncDir = func(d string) error {
			if strings.HasSuffix(d, ".partial") {
				kill()
			}
			return real(d)
		}
	case "afterpublish": // the publication rename has completed; nothing further happens
		real := pv.publish
		pv.publish = func(partial, final string) error {
			if err := real(partial, final); err != nil {
				return err
			}
			kill()
			return nil // unreachable: kill never returns
		}
	default:
		os.Exit(2)
	}
	_, err = pv.preserve(path, id)
	fmt.Fprintln(os.Stderr, "the child was supposed to be killed, but finished:", err)
	os.Exit(3)
}

func TestPreserve_SIGKILLBeforePublication(t *testing.T) {
	t.Parallel()
	for _, point := range []string{"copy", "aftercopy", "presync"} {
		t.Run(point, func(t *testing.T) {
			t.Parallel()
			dir := pvDir(t)
			path := filepath.Join(dir, "c.db")
			pvWrite(t, path, pvBytes(8<<20))
			pvWrite(t, path+"-wal", pvBytes(100000))

			// Things that must survive untouched: an unrelated file, an unrelated
			// directory, and an earlier completed preservation.
			pvWrite(t, filepath.Join(dir, "unrelated.txt"), []byte("keep me"))
			if err := os.Mkdir(filepath.Join(dir, "unrelated.d"), 0o700); err != nil {
				t.Fatal(err)
			}
			earlier := path + ".pre-restore-raw-20200101T000000Z-aaaaaaaa"
			if err := os.Mkdir(earlier, 0o700); err != nil {
				t.Fatal(err)
			}
			pvWrite(t, filepath.Join(earlier, "main"), []byte("earlier preservation"))
			before := pvStates(t, path)
			sentinels := map[string]string{
				"unrelated.txt":                  pvHash(t, filepath.Join(dir, "unrelated.txt")),
				filepath.Base(earlier) + "/main": pvHash(t, filepath.Join(earlier, "main")),
			}

			c := pvHelper(t, "preserve-kill", path)
			c.Env = append(c.Env, "DSO_TEST_PRESERVE_KILLPOINT="+point)
			err := c.Run()
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("the child should have been killed: %v", err)
			}
			if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
				t.Fatalf("expected SIGKILL, got %v", err)
			}

			// The originals are untouched.
			pvSame(t, before, pvStates(t, path))
			// Nothing was published: the only new entry is this run's own .partial directory.
			var partials, published []string
			for _, l := range pvLeftovers(t, dir) {
				switch {
				case l == filepath.Base(earlier):
				case strings.HasSuffix(l, ".partial"):
					partials = append(partials, l)
				default:
					published = append(published, l)
				}
			}
			if len(published) != 0 || len(partials) != 1 {
				t.Fatalf("published %v, partial %v; want no published directory and exactly one .partial", published, partials)
			}
			// Unrelated entries are intact.
			for rel, h := range sentinels {
				if pvHash(t, filepath.Join(dir, rel)) != h {
					t.Fatalf("%s changed", rel)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "unrelated.d")); err != nil {
				t.Fatal("an unrelated directory disappeared")
			}

			// What the interrupted run left behind, per kill point.
			part := filepath.Join(dir, partials[0])
			ents, _ := os.ReadDir(part)
			have := map[string]int64{}
			for _, e := range ents {
				fi, _ := e.Info()
				have[e.Name()] = fi.Size()
			}
			switch point {
			case "copy":
				if sz, ok := have["main"]; !ok || sz == 0 || sz >= 8<<20 || len(have) != 1 {
					t.Fatalf("expected a partly written main file only, got %v", have)
				}
			case "aftercopy":
				if have["main"] != 8<<20 || have["wal"] != 100000 || len(have) != 2 {
					t.Fatalf("expected complete copies and no manifest, got %v", have)
				}
			case "presync":
				if have["main"] != 8<<20 || have["wal"] != 100000 || have["manifest.json"] == 0 {
					t.Fatalf("expected complete copies and a manifest, got %v", have)
				}
			}
			stale := map[string]int64{}
			for k, v := range have {
				stale[k] = v
			}

			// A later preservation works, leaves the stale .partial alone, and is the
			// only completed directory besides the earlier one.
			p := pvMustPreserve(t, pvTest(), path)
			if filepath.Base(p.Dir()) == partials[0] || strings.HasSuffix(p.Dir(), ".partial") {
				t.Fatalf("a new run must not reuse the stale directory: %s", p.Dir())
			}
			ents2, _ := os.ReadDir(part)
			if len(ents2) != len(stale) {
				t.Fatalf("the stale .partial directory was modified by a later run")
			}
			for _, e := range ents2 {
				fi, _ := e.Info()
				if stale[e.Name()] != fi.Size() {
					t.Fatalf("the stale .partial file %s changed", e.Name())
				}
			}
			if err := p.Unchanged(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// ---- Phase 2B: atomic no-replace publication ------------------------------------------------------------------------

// pvRenameSupported reports whether the real publishDir can run here. On
// platforms without an atomic no-replace rename it must fail closed instead.
func pvRenameSupported(t *testing.T) bool {
	t.Helper()
	switch runtime.GOOS {
	case "linux", "darwin":
		return true
	}
	dir := pvDir(t)
	src := filepath.Join(dir, "a.partial")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := publishDir(src, filepath.Join(dir, "a")); !errors.Is(err, ErrPreserveNoReplaceUnsupported) {
		t.Fatalf("an unsupported platform must fail closed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Fatal("nothing may be published on an unsupported platform")
	}
	return false
}

func pvMkdirWith(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	for n, c := range files {
		pvWrite(t, filepath.Join(path, n), []byte(c))
	}
}

func TestPublishDir_RealKernelNoReplace(t *testing.T) {
	t.Parallel()
	if !pvRenameSupported(t) {
		t.Skip("no atomic no-replace rename on this platform; the fail-closed behavior was checked above")
	}
	type setup func(t *testing.T, final string) (check func(t *testing.T))
	cases := map[string]setup{
		"empty-directory": func(t *testing.T, final string) func(*testing.T) {
			pvMkdirWith(t, final, nil)
			return func(t *testing.T) {
				ents, err := os.ReadDir(final)
				if err != nil || len(ents) != 0 {
					t.Fatalf("the existing empty directory was changed (%v, %d entries)", err, len(ents))
				}
			}
		},
		"non-empty-directory": func(t *testing.T, final string) func(*testing.T) {
			pvMkdirWith(t, final, map[string]string{"keep": "existing preservation"})
			return func(t *testing.T) {
				if b, _ := os.ReadFile(filepath.Join(final, "keep")); string(b) != "existing preservation" {
					t.Fatal("the existing directory's content changed")
				}
			}
		},
		"regular-file": func(t *testing.T, final string) func(*testing.T) {
			pvWrite(t, final, []byte("a file"))
			return func(t *testing.T) {
				if b, _ := os.ReadFile(final); string(b) != "a file" {
					t.Fatal("the existing file changed")
				}
			}
		},
		"dangling-symlink": func(t *testing.T, final string) func(*testing.T) {
			if err := os.Symlink(final+".nowhere", final); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return func(t *testing.T) {
				if tgt, err := os.Readlink(final); err != nil || tgt != final+".nowhere" {
					t.Fatalf("the existing link changed (%q, %v)", tgt, err)
				}
			}
		},
	}
	for name, mk := range cases {
		t.Run("refuses-"+name, func(t *testing.T) {
			t.Parallel()
			dir := pvDir(t)
			partial, final := filepath.Join(dir, "x.partial"), filepath.Join(dir, "x")
			pvMkdirWith(t, partial, map[string]string{"main": "new"})
			check := mk(t, final)
			if err := publishDir(partial, final); !errors.Is(err, ErrPreserveDestinationExists) {
				if errors.Is(err, ErrPreserveNoReplaceUnsupported) {
					t.Skipf("this filesystem does not support the flag: %v", err)
				}
				t.Fatalf("want ErrPreserveDestinationExists, got %v", err)
			}
			check(t)
			if b, _ := os.ReadFile(filepath.Join(partial, "main")); string(b) != "new" {
				t.Fatal("the source directory must be left for the caller to clean up")
			}
		})
	}
	t.Run("publishes-when-absent", func(t *testing.T) {
		t.Parallel()
		dir := pvDir(t)
		partial, final := filepath.Join(dir, "x.partial"), filepath.Join(dir, "x")
		pvMkdirWith(t, partial, map[string]string{"main": "new"})
		if err := publishDir(partial, final); err != nil {
			if errors.Is(err, ErrPreserveNoReplaceUnsupported) {
				t.Skipf("this filesystem does not support the flag: %v", err)
			}
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(final, "main")); string(b) != "new" {
			t.Fatal("the content was not moved")
		}
		if _, err := os.Lstat(partial); !os.IsNotExist(err) {
			t.Fatal("the source name must be gone")
		}
	})
}

func TestPublishDirWith_ClassifiesTheRenameResult(t *testing.T) {
	t.Parallel()
	mk := func(t *testing.T) (partial, final string) {
		dir := pvDir(t)
		partial, final = filepath.Join(dir, "x.partial"), filepath.Join(dir, "x")
		pvMkdirWith(t, partial, nil)
		return
	}
	for name, tc := range map[string]struct {
		err        error
		wantExists bool
		wantUnsupp bool
	}{
		"EEXIST":       {err: syscall.EEXIST, wantExists: true},
		"ENOTEMPTY":    {err: syscall.ENOTEMPTY, wantExists: true},
		"ENOSYS":       {err: syscall.ENOSYS, wantUnsupp: true},
		"EINVAL":       {err: syscall.EINVAL, wantUnsupp: true},
		"ENOTSUP":      {err: syscall.ENOTSUP, wantUnsupp: true},
		"EOPNOTSUPP":   {err: syscall.EOPNOTSUPP, wantUnsupp: true},
		"link-wrapped": {err: &os.LinkError{Op: "rename", Err: syscall.EEXIST}, wantExists: true},
		"EACCES":       {err: syscall.EACCES},
		"EIO":          {err: syscall.EIO},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			partial, final := mk(t)
			err := publishDirWith(func(string, string) error { return tc.err }, partial, final)
			if err == nil {
				t.Fatal("an error must be reported")
			}
			if errors.Is(err, ErrPreserveDestinationExists) != tc.wantExists || errors.Is(err, ErrPreserveNoReplaceUnsupported) != tc.wantUnsupp {
				t.Fatalf("misclassified: %v", err)
			}
			if !tc.wantExists && !tc.wantUnsupp && !errors.Is(err, tc.err) {
				t.Fatalf("the cause must stay reachable: %v", err)
			}
			if errors.Is(err, ErrPreserveOutcomeUnknown) {
				t.Fatalf("a definite failure must not look unknown: %v", err)
			}
		})
	}
	t.Run("sentinels-pass-through", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		err := publishDirWith(func(string, string) error { return fmt.Errorf("%w: platform", ErrPreserveNoReplaceUnsupported) }, partial, final)
		if !errors.Is(err, ErrPreserveNoReplaceUnsupported) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("missing-source", func(t *testing.T) {
		t.Parallel()
		dir := pvDir(t)
		if err := publishDirWith(func(string, string) error { t.Error("must not rename"); return nil }, filepath.Join(dir, "absent"), filepath.Join(dir, "x")); err == nil {
			t.Fatal("a missing source must be an error")
		}
	})
}

func TestPublishDirWith_InterruptedRenameIsResolvedFromIdentity(t *testing.T) {
	t.Parallel()
	mk := func(t *testing.T) (partial, final string) {
		dir := pvDir(t)
		partial, final = filepath.Join(dir, "x.partial"), filepath.Join(dir, "x")
		pvMkdirWith(t, partial, map[string]string{"main": "new"})
		return
	}
	t.Run("interrupted-after-it-happened-is-success-and-not-retried", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		calls := 0
		err := publishDirWith(func(o, n string) error {
			calls++
			if e := os.Rename(o, n); e != nil {
				t.Fatal(e)
			}
			return syscall.EINTR
		}, partial, final)
		if err != nil || calls != 1 {
			t.Fatalf("err %v after %d calls; an interrupted rename that took effect is a success and must not be re-issued", err, calls)
		}
		if b, _ := os.ReadFile(filepath.Join(final, "main")); string(b) != "new" {
			t.Fatal("the published content is missing")
		}
	})
	t.Run("interrupted-before-it-happened-is-reissued-once-verified", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		calls := 0
		err := publishDirWith(func(o, n string) error {
			calls++
			if calls == 1 {
				return syscall.EINTR // nothing happened
			}
			return os.Rename(o, n)
		}, partial, final)
		if err != nil || calls != 2 {
			t.Fatalf("err %v after %d calls", err, calls)
		}
	})
	t.Run("interrupted-every-time-without-effect-fails-and-leaves-everything", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		calls := 0
		err := publishDirWith(func(string, string) error { calls++; return syscall.EINTR }, partial, final)
		if err == nil || calls != publishTries || !errors.Is(err, syscall.EINTR) || errors.Is(err, ErrPreserveOutcomeUnknown) {
			t.Fatalf("err %v after %d calls", err, calls)
		}
		if _, e := os.Stat(filepath.Join(partial, "main")); e != nil {
			t.Fatal("the source must still be there for the caller to clean up")
		}
		if _, e := os.Lstat(final); !os.IsNotExist(e) {
			t.Fatal("nothing may exist at the destination")
		}
	})
	t.Run("interrupted-while-a-competitor-appears-is-refused-and-untouched", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		err := publishDirWith(func(string, string) error {
			pvMkdirWith(t, final, map[string]string{"competitor": "mine"})
			return syscall.EINTR
		}, partial, final)
		if !errors.Is(err, ErrPreserveDestinationExists) {
			t.Fatalf("got %v", err)
		}
		if b, _ := os.ReadFile(filepath.Join(final, "competitor")); string(b) != "mine" {
			t.Fatal("the competitor was touched")
		}
	})
	t.Run("interrupted-and-the-destination-is-not-ours-is-unknown-and-untouched", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		err := publishDirWith(func(o, n string) error {
			if e := os.Rename(o, n); e != nil { // it happened ...
				t.Fatal(e)
			}
			// ... and then someone replaced the published directory with their own.
			if e := os.RemoveAll(n); e != nil {
				t.Fatal(e)
			}
			pvMkdirWith(t, n, map[string]string{"theirs": "x"})
			return syscall.EINTR
		}, partial, final)
		if !errors.Is(err, ErrPreserveOutcomeUnknown) {
			t.Fatalf("got %v", err)
		}
		if b, _ := os.ReadFile(filepath.Join(final, "theirs")); string(b) != "x" {
			t.Fatal("the other directory was touched")
		}
	})
	t.Run("interrupted-and-both-names-are-gone-is-unknown", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		err := publishDirWith(func(o, n string) error { _ = os.RemoveAll(o); return syscall.EINTR }, partial, final)
		if !errors.Is(err, ErrPreserveOutcomeUnknown) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("interrupted-and-the-source-was-swapped-is-unknown", func(t *testing.T) {
		t.Parallel()
		partial, final := mk(t)
		err := publishDirWith(func(o, n string) error {
			_ = os.RemoveAll(o)
			pvMkdirWith(t, o, map[string]string{"swapped": "x"})
			return syscall.EINTR
		}, partial, final)
		if !errors.Is(err, ErrPreserveOutcomeUnknown) {
			t.Fatalf("got %v", err)
		}
		if b, _ := os.ReadFile(filepath.Join(partial, "swapped")); string(b) != "x" {
			t.Fatal("the swapped-in directory was touched")
		}
	})
}

func TestPreserve_PublicationRefusesToReplace(t *testing.T) {
	t.Parallel()
	if !pvRenameSupported(t) {
		t.Skip("no atomic no-replace rename on this platform")
	}
	fixed := func(pv *preserver) string {
		pv.now = func() time.Time { return time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC) }
		pv.random = func(b []byte) (int, error) { copy(b, []byte{9, 8, 7, 6}); return len(b), nil }
		return "20310203T040506Z-09080706"
	}
	for name, mk := range map[string]func(t *testing.T, final string){
		"existing-empty-directory":     func(t *testing.T, final string) { pvMkdirWith(t, final, nil) },
		"existing-non-empty-directory": func(t *testing.T, final string) { pvMkdirWith(t, final, map[string]string{"keep": "existing"}) },
		"existing-file":                func(t *testing.T, final string) { pvWrite(t, final, []byte("existing")) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, path := pvDB(t, true, false, false)
			pv := pvTest()
			suffix := fixed(pv)
			final := path + ".pre-restore-raw-" + suffix
			mk(t, final)
			before := pvStates(t, path)
			_, err := pv.preserve(path, pvIdentity(t, path))
			if errors.Is(err, ErrPreserveNoReplaceUnsupported) {
				t.Skipf("this filesystem does not support the flag: %v", err)
			}
			if !errors.Is(err, ErrPreserveDestinationExists) {
				t.Fatalf("got %v", err)
			}
			pvSame(t, before, pvStates(t, path))
			if _, e := os.Stat(final + ".partial"); !os.IsNotExist(e) {
				t.Fatal("the refused attempt's own partial directory must be removed")
			}
			if _, e := os.Lstat(final); e != nil {
				t.Fatal("the existing entry disappeared")
			}
			if name == "existing-non-empty-directory" {
				if b, _ := os.ReadFile(filepath.Join(final, "keep")); string(b) != "existing" {
					t.Fatal("the existing directory's content changed")
				}
			}
			if name == "existing-empty-directory" {
				if ents, _ := os.ReadDir(final); len(ents) != 0 {
					t.Fatal("the existing empty directory was filled")
				}
			}
			_ = dir
		})
	}
}

func TestPreserve_CompetitorCreatedImmediatelyBeforePublication(t *testing.T) {
	t.Parallel()
	if !pvRenameSupported(t) {
		t.Skip("no atomic no-replace rename on this platform")
	}
	for name, create := range map[string]func(t *testing.T, final string){
		"empty-directory": func(t *testing.T, final string) { pvMkdirWith(t, final, nil) },
		"directory-with-content": func(t *testing.T, final string) {
			pvMkdirWith(t, final, map[string]string{"theirs": "x"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, path := pvDB(t, false, false, false)
			// A stale partial directory of some other run must also survive.
			stale := path + ".pre-restore-raw-20200101T000000Z-deadbeef.partial"
			pvMkdirWith(t, stale, map[string]string{"main": "stale"})
			before := pvStates(t, path)
			pv := pvTest()
			var competitor string
			pv.publish = func(partial, final string) error {
				competitor = final
				create(t, final) // appears between the last check and the rename
				return publishDir(partial, final)
			}
			_, err := pv.preserve(path, pvIdentity(t, path))
			if errors.Is(err, ErrPreserveNoReplaceUnsupported) {
				t.Skipf("this filesystem does not support the flag: %v", err)
			}
			if !errors.Is(err, ErrPreserveDestinationExists) {
				t.Fatalf("got %v", err)
			}
			pvSame(t, before, pvStates(t, path))
			if fi, e := os.Stat(competitor); e != nil || !fi.IsDir() {
				t.Fatal("the competing directory is gone")
			}
			if name == "directory-with-content" {
				if b, _ := os.ReadFile(filepath.Join(competitor, "theirs")); string(b) != "x" {
					t.Fatal("the competitor's content changed")
				}
			} else if ents, _ := os.ReadDir(competitor); len(ents) != 0 {
				t.Fatal("the competing empty directory was filled")
			}
			if b, _ := os.ReadFile(filepath.Join(stale, "main")); string(b) != "stale" {
				t.Fatal("a different run's stale partial directory was touched")
			}
			if _, e := os.Stat(competitor + ".partial"); !os.IsNotExist(e) {
				t.Fatal("this run's own partial directory must be removed")
			}
			var own []string
			for _, l := range pvLeftovers(t, dir) {
				if strings.HasSuffix(l, ".partial") {
					own = append(own, l)
				}
			}
			if len(own) != 1 || own[0] != filepath.Base(stale) {
				t.Fatalf("only the unrelated stale partial may remain: %v", own)
			}
		})
	}
}

func TestPreserve_PublicationGoesThroughTheSeamExactlyOnce(t *testing.T) {
	t.Parallel()
	dir, path := pvDB(t, true, false, false)
	pv := pvTest()
	var calls [][2]string
	pv.publish = func(partial, final string) error {
		calls = append(calls, [2]string{partial, final})
		return publishDir(partial, final)
	}
	p := pvMustPreserve(t, pv, path)
	if len(calls) != 1 || !strings.HasSuffix(calls[0][0], ".partial") || calls[0][1] != p.Dir() || strings.HasSuffix(calls[0][1], ".partial") || filepath.Dir(calls[0][1]) != dir {
		t.Fatalf("unexpected publication calls: %v", calls)
	}
}

func TestPreserve_PublicationFailureKinds(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err         error
		wantCleaned bool
		check       func(error) bool
	}{
		"unsupported": {fmt.Errorf("%w (EINVAL)", ErrPreserveNoReplaceUnsupported), true, func(e error) bool { return errors.Is(e, ErrPreserveNoReplaceUnsupported) }},
		"permission":  {fmt.Errorf("rename: %w", os.ErrPermission), true, func(e error) bool { return errors.Is(e, os.ErrPermission) }},
		"unknown":     {fmt.Errorf("%w: interrupted", ErrPreserveOutcomeUnknown), false, func(e error) bool { return errors.Is(e, ErrPreserveOutcomeUnknown) }},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, path := pvDB(t, true, false, false)
			earlier := pvMustPreserve(t, pvTest(), path)
			earlierHash := pvHash(t, filepath.Join(earlier.Dir(), "main"))
			before := pvStates(t, path)
			pv := pvTest()
			pv.publish = func(string, string) error { return tc.err }
			_, err := pv.preserve(path, pvIdentity(t, path))
			if err == nil || !tc.check(err) {
				t.Fatalf("got %v", err)
			}
			pvSame(t, before, pvStates(t, path))
			if pvHash(t, filepath.Join(earlier.Dir(), "main")) != earlierHash {
				t.Fatal("an earlier completed preservation changed")
			}
			var partials []string
			for _, l := range pvLeftovers(t, dir) {
				if strings.HasSuffix(l, ".partial") {
					partials = append(partials, l)
				}
			}
			if tc.wantCleaned && len(partials) != 0 {
				t.Fatalf("this run's partial directory must be cleaned: %v", partials)
			}
			if !tc.wantCleaned && len(partials) != 1 {
				t.Fatalf("with an unknown outcome the partial directory must be kept: %v", partials)
			}
			if len(pvPublishedDirs(t, dir)) != 1 {
				t.Fatalf("only the earlier completed preservation may exist: %v", pvPublishedDirs(t, dir))
			}
		})
	}
}

func TestPreserve_InterruptedPublicationEndToEnd(t *testing.T) {
	t.Parallel()
	t.Run("it-happened", func(t *testing.T) {
		t.Parallel()
		_, path := pvDB(t, true, false, false)
		pv := pvTest()
		pv.publish = func(partial, final string) error {
			return publishDirWith(func(o, n string) error {
				if e := os.Rename(o, n); e != nil {
					t.Fatal(e)
				}
				return syscall.EINTR
			}, partial, final)
		}
		p := pvMustPreserve(t, pv, path)
		if pvHash(t, filepath.Join(p.Dir(), "main")) != pvHash(t, path) {
			t.Fatal("the published preservation is not a faithful copy")
		}
		if err := p.Unchanged(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("outcome-cannot-be-established", func(t *testing.T) {
		t.Parallel()
		dir, path := pvDB(t, false, false, false)
		pv := pvTest()
		pv.publish = func(partial, final string) error {
			return publishDirWith(func(o, n string) error { _ = os.RemoveAll(o); return syscall.EINTR }, partial, final)
		}
		_, err := pv.preserve(path, pvIdentity(t, path))
		if !errors.Is(err, ErrPreserveOutcomeUnknown) {
			t.Fatalf("got %v", err)
		}
		_ = dir
	})
}

// A creator racing the publisher can never have its directory replaced: of the
// two operations on the final name, exactly one wins, and the loser leaves the
// winner's entry untouched. (A check followed by an ordinary rename would
// occasionally let both "succeed"; this stress test states the invariant that
// the kernel's single operation guarantees. A deterministic test cannot place a
// competitor between a check and a rename, so the invariant is also pinned by the
// real-kernel tests above, which use an existing EMPTY directory that an ordinary
// rename would replace.)
func TestPublishDir_RacingCreatorIsNeverReplaced(t *testing.T) {
	t.Parallel()
	if !pvRenameSupported(t) {
		t.Skip("no atomic no-replace rename on this platform")
	}
	dir := pvDir(t)
	var published, refused int
	for i := 0; i < 400; i++ {
		partial := filepath.Join(dir, fmt.Sprintf("p%d.partial", i))
		final := filepath.Join(dir, fmt.Sprintf("p%d", i))
		pvMkdirWith(t, partial, map[string]string{"main": "x"})
		var created atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			if i%2 == 0 {
				runtime.Gosched()
			}
			if err := os.Mkdir(final, 0o700); err == nil {
				created.Store(true)
			}
		}()
		err := publishDir(partial, final)
		<-done
		switch {
		case err == nil && created.Load():
			t.Fatalf("iteration %d: both the creator and the publisher succeeded, so the creator's directory was replaced", i)
		case err == nil:
			published++
			if b, _ := os.ReadFile(filepath.Join(final, "main")); string(b) != "x" {
				t.Fatalf("iteration %d: published content missing", i)
			}
		case errors.Is(err, ErrPreserveNoReplaceUnsupported):
			t.Skipf("this filesystem does not support the flag: %v", err)
		case errors.Is(err, ErrPreserveDestinationExists):
			refused++
			if !created.Load() {
				t.Fatalf("iteration %d: refused although nothing was created", i)
			}
			if ents, _ := os.ReadDir(final); len(ents) != 0 {
				t.Fatalf("iteration %d: the creator's directory was filled", i)
			}
		default:
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
	t.Logf("racing creator: %d published, %d refused", published, refused)
}

// ---- Phase 3a: ActiveSidecars (read-only) ----------------------------------------------------------------------------

// pvSidecarFixture writes the three companions in the requested state:
// size < 0 absent, 0 empty, > 0 that many bytes.
func pvSidecarFixture(t *testing.T, path string, wal, journal, shm int64) {
	t.Helper()
	for sfx, n := range map[string]int64{"-wal": wal, "-journal": journal, "-shm": shm} {
		if n < 0 {
			continue
		}
		pvWrite(t, path+sfx, pvBytes(int(n)))
	}
}

func TestActiveSidecars_EveryCombination(t *testing.T) {
	t.Parallel()
	sizes := []int64{-1, 0, 123}
	for _, mainExists := range []bool{true, false} {
		for _, wal := range sizes {
			for _, jr := range sizes {
				for _, shm := range sizes {
					name := fmt.Sprintf("main=%v/wal=%d/journal=%d/shm=%d", mainExists, wal, jr, shm)
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						dir := pvDir(t)
						path := filepath.Join(dir, "c.db")
						if mainExists {
							pvWrite(t, path, pvBytes(500))
						}
						pvSidecarFixture(t, path, wal, jr, shm)
						before := pvStates(t, path)
						entries, _ := os.ReadDir(dir)

						got, err := ActiveSidecars(path)
						if err != nil {
							t.Fatal(err)
						}
						want := func(n int64) SidecarFile {
							if n < 0 {
								return SidecarFile{}
							}
							return SidecarFile{Exists: true, Size: n}
						}
						if got.MainExists != mainExists || got.WAL != want(wal) || got.Journal != want(jr) || got.SHM != want(shm) {
							t.Fatalf("got %+v", got)
						}
						for label, c := range map[string]struct {
							f  SidecarFile
							ne bool
						}{"wal": {got.WAL, wal > 0}, "journal": {got.Journal, jr > 0}, "shm": {got.SHM, shm > 0}} {
							if c.f.NonEmpty() != c.ne {
								t.Fatalf("%s NonEmpty() = %v", label, c.f.NonEmpty())
							}
						}
						// Orphans exist only when the main file is missing, only for non-empty
						// companions, and in the fixed order -wal, -journal, -shm.
						var wantOrphans []string
						if !mainExists {
							if wal > 0 {
								wantOrphans = append(wantOrphans, "-wal")
							}
							if jr > 0 {
								wantOrphans = append(wantOrphans, "-journal")
							}
							if shm > 0 {
								wantOrphans = append(wantOrphans, "-shm")
							}
						}
						if strings.Join(got.OrphanNonEmpty(), ",") != strings.Join(wantOrphans, ",") {
							t.Fatalf("orphans = %v, want %v", got.OrphanNonEmpty(), wantOrphans)
						}
						// Read-only: nothing was created, removed or changed.
						pvSame(t, before, pvStates(t, path))
						after, _ := os.ReadDir(dir)
						if len(after) != len(entries) {
							t.Fatalf("the directory changed: %d entries before, %d after", len(entries), len(after))
						}
					})
				}
			}
		}
	}
}

func TestActiveSidecars_RefusesNonRegularFiles(t *testing.T) {
	t.Parallel()
	type tc struct {
		name  string
		build func(t *testing.T, dir, path string) (target string)
		kind  string
	}
	cases := []tc{
		{"wal is a symlink to a file", func(t *testing.T, dir, path string) string {
			other := filepath.Join(dir, "other")
			pvWrite(t, other, pvBytes(10))
			if err := os.Symlink(other, path+"-wal"); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return path + "-wal"
		}, "symbolic link"},
		{"journal is a dangling symlink", func(t *testing.T, dir, path string) string {
			if err := os.Symlink(filepath.Join(dir, "nowhere"), path+"-journal"); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return path + "-journal"
		}, "symbolic link"},
		{"shm is a directory", func(t *testing.T, dir, path string) string {
			if err := os.Mkdir(path+"-shm", 0o700); err != nil {
				t.Fatal(err)
			}
			return path + "-shm"
		}, "directory"},
		{"wal is a named pipe", func(t *testing.T, dir, path string) string {
			if err := syscall.Mkfifo(path+"-wal", 0o600); err != nil {
				t.Skipf("mkfifo unavailable: %v", err)
			}
			return path + "-wal"
		}, "named pipe"},
		{"main is a symlink", func(t *testing.T, dir, path string) string {
			real := filepath.Join(dir, "real.db")
			pvWrite(t, real, pvBytes(10))
			if err := os.Symlink(real, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return path
		}, "symbolic link"},
		{"main is a directory", func(t *testing.T, dir, path string) string {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			return path
		}, "directory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := pvDir(t)
			path := filepath.Join(dir, "c.db")
			if !strings.HasPrefix(c.name, "main") {
				pvWrite(t, path, pvBytes(10))
			}
			target := c.build(t, dir, path)
			before, _ := os.ReadDir(dir)
			_, err := ActiveSidecars(path)
			if !errors.Is(err, ErrSidecarNotRegular) {
				t.Fatalf("want ErrSidecarNotRegular, got %v", err)
			}
			if !strings.Contains(err.Error(), target) || !strings.Contains(err.Error(), c.kind) {
				t.Fatalf("the error should name the path and the kind (%q, %q): %v", target, c.kind, err)
			}
			after, _ := os.ReadDir(dir)
			if len(before) != len(after) {
				t.Fatal("the directory changed")
			}
			if fi, e := os.Lstat(target); e != nil || (c.kind == "symbolic link" && fi.Mode()&os.ModeSymlink == 0) {
				t.Fatalf("the offending entry was altered: %v", e)
			}
		})
	}
}

func TestActiveSidecars_OtherFailuresAreErrorsNeverMissing(t *testing.T) {
	t.Parallel()
	t.Run("a path component that is a file", func(t *testing.T) {
		t.Parallel()
		dir := pvDir(t)
		file := filepath.Join(dir, "plain")
		pvWrite(t, file, []byte("x"))
		info, err := ActiveSidecars(filepath.Join(file, "c.db"))
		if err == nil || !errors.Is(err, syscall.ENOTDIR) || errors.Is(err, ErrSidecarNotRegular) {
			t.Fatalf("ENOTDIR must be reported as such, not as a missing file: %+v, %v", info, err)
		}
		if info != (ActiveSidecarsInfo{}) {
			t.Fatalf("no partial result on error: %+v", info)
		}
	})
	t.Run("a directory that cannot be searched", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("permissions do not bind root")
		}
		dir := pvDir(t)
		sub := filepath.Join(dir, "sub")
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		pvWrite(t, filepath.Join(sub, "c.db"), pvBytes(10))
		if err := os.Chmod(sub, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })
		_, err := ActiveSidecars(filepath.Join(sub, "c.db"))
		if err == nil || !errors.Is(err, os.ErrPermission) {
			t.Fatalf("a permission failure must be an error, not 'missing': %v", err)
		}
	})
	t.Run("a genuinely absent file is not an error", func(t *testing.T) {
		t.Parallel()
		info, err := ActiveSidecars(filepath.Join(pvDir(t), "c.db"))
		if err != nil || info != (ActiveSidecarsInfo{}) {
			t.Fatalf("%+v, %v", info, err)
		}
	})
	t.Run("an unnormalised path is resolved", func(t *testing.T) {
		t.Parallel()
		dir := pvDir(t)
		path := filepath.Join(dir, "c.db")
		pvWrite(t, path, pvBytes(10))
		pvWrite(t, path+"-wal", pvBytes(20))
		info, err := ActiveSidecars(dir + "/./sub/../c.db")
		if err == nil {
			// "sub" does not exist, so the lexical cleanup makes this the real path.
			if !info.MainExists || !info.WAL.NonEmpty() {
				t.Fatalf("%+v", info)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	})
}

func TestActiveSidecars_AgreesWithWhatIsPreserved(t *testing.T) {
	t.Parallel()
	_, path := pvDB(t, true, true, true)
	info, err := ActiveSidecars(path)
	if err != nil || !info.MainExists || !info.WAL.NonEmpty() || !info.Journal.NonEmpty() || !info.SHM.NonEmpty() {
		t.Fatalf("%+v, %v", info, err)
	}
	before := pvStates(t, path)
	p := pvMustPreserve(t, pvTest(), path)
	pvSame(t, before, pvStates(t, path))
	for name, f := range map[string]SidecarFile{"wal": info.WAL, "journal": info.Journal, "shm": info.SHM} {
		rec, ok := p.record(name)
		if !ok || rec.Size != f.Size {
			t.Fatalf("%s: ActiveSidecars says %d bytes, the preservation recorded %+v (%v)", name, f.Size, rec, ok)
		}
	}
}

func TestActiveSidecars_ConcurrentCallsUnderRace(t *testing.T) {
	t.Parallel()
	_, path := pvDB(t, true, false, true)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if info, err := ActiveSidecars(path); err != nil || !info.WAL.NonEmpty() {
					t.Errorf("%+v, %v", info, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
