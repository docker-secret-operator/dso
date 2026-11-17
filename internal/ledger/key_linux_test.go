//go:build linux

package ledger

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

type countingReader struct {
	mu    sync.Mutex
	reads int
	fail  int // fail the n-th read (1-based); 0 = never
	r     io.Reader
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.fail != 0 && c.reads == c.fail {
		return 0, errors.New("injected csprng failure")
	}
	return c.r.Read(p)
}

func newKP(t *testing.T, dir string, mut func(*keyDeps)) *FileKeyProvider {
	t.Helper()
	d := defaultKeyDeps()
	if mut != nil {
		mut(&d)
	}
	p, err := newFileKeyProvider(dir, d)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// keyDir returns an existing 0700 directory.
func safeTemp(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func keyDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(safeTemp(t), "ledger")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func keyNames(t *testing.T, dir string) []string {
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

func mustKind(t *testing.T, err, kind error) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Fatalf("error = %v, want kind %v", err, kind)
	}
}

func goodKeyFile(t *testing.T) (string, []byte, []byte) {
	t.Helper()
	key := bytes.Repeat([]byte{0x5a}, HMACKeySize)
	id := DeriveKeyID(key)
	return id, key, encodeKeyFile(id, key)
}

func writeKeyFile(t *testing.T, path string, b []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, b, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestKey_DeriveKeyIDVector(t *testing.T) {
	key := make([]byte, HMACKeySize)
	id := DeriveKeyID(key)
	if !ValidKeyID(id) {
		t.Fatalf("bad id %q", id)
	}
	// Independent computation of "k-" + hex(sha256(domain||key)[:8]).
	sum := sha256Sum([]byte("dso-ledger-key-id-v1" + string(key)))
	if want := "k-" + hex.EncodeToString(sum[:8]); id != want {
		t.Fatalf("id = %s want %s", id, want)
	}
	if DeriveKeyID(append([]byte{1}, key[1:]...)) == id {
		t.Fatal("different keys gave the same id")
	}
}

func TestKey_FirstRunCreatesWithExactPermissions(t *testing.T) {
	dir := filepath.Join(safeTemp(t), "state", "ledger") // does not exist
	p := newKP(t, dir, nil)
	if got := p.Inspect(); got.State != KeyAbsent {
		t.Fatalf("pre-create inspect = %+v", got)
	}
	id, err := p.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidKeyID(id) {
		t.Fatalf("id %q", id)
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %04o", di.Mode().Perm())
	}
	for _, n := range []string{keyFileName, keyMarkerName} {
		fi, err := os.Lstat(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o400 || !fi.Mode().IsRegular() {
			t.Fatalf("%s mode %v", n, fi.Mode())
		}
		if st := fi.Sys().(*syscall.Stat_t); st.Nlink != 1 || int(st.Uid) != os.Geteuid() {
			t.Fatalf("%s nlink/uid %d/%d", n, st.Nlink, st.Uid)
		}
	}
	fi, _ := os.Stat(filepath.Join(dir, keyFileName))
	if fi.Size() != int64(keyFileSize) || keyFileSize != 82 {
		t.Fatalf("size %d", fi.Size())
	}
	if got := keyNames(t, dir); len(got) != 2 {
		t.Fatalf("leftover files: %v", got)
	}
	kid, key, err := p.KeyMaterial()
	if err != nil || kid != id || len(key) != HMACKeySize || DeriveKeyID(key) != id {
		t.Fatalf("material: %v %v", kid, err)
	}
	if ins := p.Inspect(); ins.State != KeyValid || ins.KeyID != id {
		t.Fatalf("inspect %+v", ins)
	}
}

func TestKey_ReopenAndRestartIdentityStable(t *testing.T) {
	dir := keyDir(t)
	id1, err := newKP(t, dir, nil).Generate()
	if err != nil {
		t.Fatal(err)
	}
	var firstKey []byte
	for i := 0; i < 3; i++ {
		p := newKP(t, dir, nil) // a "restart": brand-new provider
		kid, key, err := p.KeyMaterial()
		if err != nil || kid != id1 {
			t.Fatalf("restart %d: %v %v", i, kid, err)
		}
		if firstKey == nil {
			firstKey = key
		} else if !bytes.Equal(firstKey, key) {
			t.Fatal("key material changed across restarts")
		}
		if id, err := p.EnsureKey(); err != nil || id != id1 {
			t.Fatalf("EnsureKey: %v %v", id, err)
		}
	}
}

func TestKey_GenerateNeverReplacesExistingKey(t *testing.T) {
	dir := keyDir(t)
	p := newKP(t, dir, nil)
	id, _ := p.Generate()
	before, _ := os.ReadFile(filepath.Join(dir, keyFileName))
	cr := &countingReader{r: strings.NewReader("")}
	q := newKP(t, dir, func(d *keyDeps) { d.random = cr })
	_, err := q.Generate()
	mustKind(t, err, ErrKeyExists)
	var ke *KeyError
	if !errors.As(err, &ke) || ke.KeyID != id {
		t.Fatalf("want key id in error, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, keyFileName))
	if !bytes.Equal(before, after) || cr.reads != 0 {
		t.Fatal("existing key touched or randomness consumed")
	}
}

func TestKey_ConcurrentInit(t *testing.T) {
	const attempts, workers = 25, 16
	for a := 0; a < attempts; a++ {
		dir := filepath.Join(safeTemp(t), "ledger")
		ids := make([]string, workers)
		errs := make([]error, workers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p := newKP(t, dir, nil)
				<-start
				if i%4 == 3 {
					ids[i], errs[i] = p.Generate() // may lose to ErrKeyExists if late
				} else {
					ids[i], errs[i] = p.EnsureKey()
				}
			}()
		}
		close(start)
		wg.Wait()
		var want string
		for i := range ids {
			if errs[i] != nil {
				if i%4 == 3 && errors.Is(errs[i], ErrKeyExists) {
					continue
				}
				t.Fatalf("attempt %d worker %d: %v", a, i, errs[i])
			}
			if want == "" {
				want = ids[i]
			} else if ids[i] != want {
				t.Fatalf("attempt %d: ids differ %s vs %s", a, ids[i], want)
			}
		}
		if want == "" {
			t.Fatal("nobody created a key")
		}
		if got := keyNames(t, dir); len(got) != 2 {
			t.Fatalf("attempt %d: dir contents %v", a, got)
		}
		if kid, _, err := newKP(t, dir, nil).KeyMaterial(); err != nil || kid != want {
			t.Fatalf("final read: %v %v", kid, err)
		}
	}
}

func TestKey_RaceLoserAdoptsWinner(t *testing.T) {
	dir := keyDir(t)
	var winner string
	loser := newKP(t, dir, func(d *keyDeps) {
		real := d.renameNoReplace
		first := true
		d.renameNoReplace = func(fd int, o, n string) error {
			if first && n == keyFileName {
				first = false
				winner, _ = newKP(t, dir, nil).EnsureKey() // wins the race
			}
			return real(fd, o, n)
		}
	})
	id, err := loser.Generate()
	if err != nil || id != winner || winner == "" {
		t.Fatalf("loser got %q %v, winner %q", id, err, winner)
	}
	if got := keyNames(t, dir); len(got) != 2 {
		t.Fatalf("contents %v", got)
	}
}

func TestKey_RejectsBadFileModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o440, 0o444, 0o640, 0o644, 0o660, 0o604, 0o700, 0o200, 0o000} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			dir := keyDir(t)
			_, _, content := goodKeyFile(t)
			path := filepath.Join(dir, keyFileName)
			writeKeyFile(t, path, content, mode)
			p := newKP(t, dir, nil)
			if ins := p.Inspect(); ins.State != KeyUnsafe {
				t.Fatalf("inspect %+v", ins)
			}
			_, err := p.LoadKey()
			mustKind(t, err, ErrKeyUnsafe)
			_, err = p.EnsureKey()
			mustKind(t, err, ErrKeyUnsafe)
			_, err = p.Generate()
			mustKind(t, err, ErrKeyUnsafe)
			if got, _ := os.ReadFile(path); mode&0o400 != 0 && !bytes.Equal(got, content) {
				t.Fatal("file modified")
			}
			if len(keyNames(t, dir)) != 1 {
				t.Fatalf("extra files %v", keyNames(t, dir))
			}
		})
	}
}

func TestKey_Accepts0600(t *testing.T) {
	dir := keyDir(t)
	id, _, content := goodKeyFile(t)
	writeKeyFile(t, filepath.Join(dir, keyFileName), content, 0o600)
	if ins := newKP(t, dir, nil).Inspect(); ins.State != KeyValid || ins.KeyID != id {
		t.Fatalf("%+v", ins)
	}
}

func TestKey_RejectsUnsafeDirectory(t *testing.T) {
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777, 0o720, 0o702} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			dir := keyDir(t)
			_, _, content := goodKeyFile(t)
			writeKeyFile(t, filepath.Join(dir, keyFileName), content, 0o400)
			if err := os.Chmod(dir, mode); err != nil {
				t.Fatal(err)
			}
			p := newKP(t, dir, nil)
			if ins := p.Inspect(); ins.State != KeyUnsafe {
				t.Fatalf("inspect %+v", ins)
			}
			_, err := p.EnsureKey()
			mustKind(t, err, ErrKeyUnsafe)
			_, err = p.Generate()
			mustKind(t, err, ErrKeyUnsafe)
		})
	}
	t.Run("symlinked directory", func(t *testing.T) {
		real := keyDir(t)
		link := filepath.Join(safeTemp(t), "lnk")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		_, err := newKP(t, link, nil).Generate()
		mustKind(t, err, ErrKeyUnsafe)
		if len(keyNames(t, real)) != 0 {
			t.Fatal("wrote through a symlinked directory")
		}
	})
}

func TestKey_RejectsWrongSizeTruncatedAndCorrupt(t *testing.T) {
	id, key, good := goodKeyFile(t)
	other := bytes.Repeat([]byte{0x11}, HMACKeySize)
	cases := map[string][]byte{
		"empty":        {},
		"truncated":    good[:len(good)-5],
		"one short":    good[:len(good)-1],
		"extra byte":   append(append([]byte{}, good...), 'x'),
		"extra line":   append(append([]byte{}, good...), '\n'),
		"bad header":   bytes.Replace(good, []byte("key-v1"), []byte("key-v2"), 1),
		"wrong id":     encodeKeyFile(DeriveKeyID(other), key),
		"bad id shape": []byte(keyFileHeader + "\nK-0000000000000000\n" + base64.StdEncoding.EncodeToString(key) + "\n"),
		"bad base64":   []byte(keyFileHeader + "\n" + id + "\n" + strings.Repeat("!", 44) + "\n"),
		"crlf":         bytes.ReplaceAll(good, []byte("\n"), []byte("\r\n"))[:len(good)],
		"zero fill":    make([]byte, len(good)),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := keyDir(t)
			path := filepath.Join(dir, keyFileName)
			writeKeyFile(t, path, content, 0o400)
			p := newKP(t, dir, nil)
			if ins := p.Inspect(); ins.State != KeyMalformed {
				t.Fatalf("inspect %+v", ins)
			}
			for _, fn := range []func() (string, error){p.Generate, p.EnsureKey} {
				_, err := fn()
				mustKind(t, err, ErrKeyCorrupt)
			}
			if got, _ := os.ReadFile(path); !bytes.Equal(got, content) {
				t.Fatal("corrupt key file was modified")
			}
			if len(keyNames(t, dir)) != 1 {
				t.Fatalf("extra files %v", keyNames(t, dir))
			}
		})
	}
}

func TestKey_RejectsSymlinkHardlinkAndNonRegular(t *testing.T) {
	_, _, good := goodKeyFile(t)
	t.Run("symlink", func(t *testing.T) {
		dir := keyDir(t)
		target := filepath.Join(safeTemp(t), "elsewhere")
		writeKeyFile(t, target, good, 0o400)
		if err := os.Symlink(target, filepath.Join(dir, keyFileName)); err != nil {
			t.Fatal(err)
		}
		p := newKP(t, dir, nil)
		if p.Inspect().State != KeyUnsafe {
			t.Fatal("symlink accepted")
		}
		_, err := p.EnsureKey()
		mustKind(t, err, ErrKeyUnsafe)
		if fi, _ := os.Lstat(filepath.Join(dir, keyFileName)); fi.Mode()&os.ModeSymlink == 0 {
			t.Fatal("symlink was replaced")
		}
	})
	t.Run("dangling symlink", func(t *testing.T) {
		dir := keyDir(t)
		if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, keyFileName)); err != nil {
			t.Fatal(err)
		}
		_, err := newKP(t, dir, nil).EnsureKey()
		mustKind(t, err, ErrKeyUnsafe)
	})
	t.Run("hardlink", func(t *testing.T) {
		dir := keyDir(t)
		path := filepath.Join(dir, keyFileName)
		writeKeyFile(t, path, good, 0o400)
		if err := os.Link(path, filepath.Join(dir, "alias")); err != nil {
			t.Fatal(err)
		}
		p := newKP(t, dir, nil)
		if p.Inspect().State != KeyUnsafe {
			t.Fatal("hard link accepted")
		}
		_, err := p.Generate()
		mustKind(t, err, ErrKeyUnsafe)
	})
	t.Run("directory", func(t *testing.T) {
		dir := keyDir(t)
		if err := os.Mkdir(filepath.Join(dir, keyFileName), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := newKP(t, dir, nil).EnsureKey()
		mustKind(t, err, ErrKeyUnsafe)
	})
	t.Run("fifo", func(t *testing.T) {
		dir := keyDir(t)
		if err := unix.Mkfifo(filepath.Join(dir, keyFileName), 0o600); err != nil {
			t.Fatal(err)
		}
		p := newKP(t, dir, nil)
		if p.Inspect().State != KeyUnsafe { // must not block either
			t.Fatal("fifo accepted")
		}
		_, err := p.EnsureKey()
		mustKind(t, err, ErrKeyUnsafe)
	})
}

func TestKey_LostKeyIsNeverRegenerated(t *testing.T) {
	setups := map[string]func(t *testing.T, dir string){
		"ledger file": func(t *testing.T, dir string) {
			writeKeyFile(t, filepath.Join(dir, ledgerFileName), []byte("{}"), 0o600)
		},
		"preserved ledger": func(t *testing.T, dir string) {
			writeKeyFile(t, filepath.Join(dir, "ledger.json.keyloss-20260101"), []byte("{}"), 0o600)
		},
		"key-id marker": func(t *testing.T, dir string) {
			writeKeyFile(t, filepath.Join(dir, keyMarkerName), []byte("k-0123456789abcdef\n"), 0o400)
		},
		"preserved key": func(t *testing.T, dir string) {
			writeKeyFile(t, filepath.Join(dir, keyLossPrefix+"x"), []byte("x"), 0o400)
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			dir := keyDir(t)
			setup(t, dir)
			before := keyNames(t, dir)
			cr := &countingReader{r: strings.NewReader("")}
			p := newKP(t, dir, func(d *keyDeps) { d.random = cr })
			if ins := p.Inspect(); ins.State != KeyUnsafe || !strings.Contains(ins.Reason, "lost") {
				t.Fatalf("inspect %+v", ins)
			}
			for _, fn := range []func() (string, error){p.Generate, p.EnsureKey} {
				_, err := fn()
				mustKind(t, err, ErrKeyLost)
			}
			_, _, err := p.KeyMaterial()
			mustKind(t, err, ErrKeyLost)
			if cr.reads != 0 {
				t.Fatal("Generate drew randomness over a lost key")
			}
			if got := keyNames(t, dir); strings.Join(got, ",") != strings.Join(before, ",") {
				t.Fatalf("directory changed: %v -> %v", before, got)
			}
		})
	}
	t.Run("ledger lock and temp are not evidence", func(t *testing.T) {
		dir := keyDir(t)
		writeKeyFile(t, filepath.Join(dir, "ledger.lock"), nil, 0o600)
		writeKeyFile(t, filepath.Join(dir, "ledger.json.tmp-aa"), nil, 0o600)
		writeKeyFile(t, filepath.Join(dir, keyTmpPrefix+"aa"), nil, 0o400)
		if _, err := newKP(t, dir, nil).Generate(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestKey_MarkerMismatchAndRepair(t *testing.T) {
	dir := keyDir(t)
	p := newKP(t, dir, nil)
	id, _ := p.Generate()
	// Missing marker with a valid key: read works, EnsureKey repairs.
	if err := os.Remove(filepath.Join(dir, keyMarkerName)); err != nil {
		t.Fatal(err)
	}
	if kid, _, err := p.KeyMaterial(); err != nil || kid != id {
		t.Fatal(err)
	}
	if got, err := p.EnsureKey(); err != nil || got != id {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, keyMarkerName)); string(b) != id+"\n" {
		t.Fatal("marker not repaired")
	}
	// Mismatching marker fails closed.
	if err := os.Remove(filepath.Join(dir, keyMarkerName)); err != nil {
		t.Fatal(err)
	}
	writeKeyFile(t, filepath.Join(dir, keyMarkerName), []byte("k-ffffffffffffffff\n"), 0o400)
	_, err := p.LoadKey()
	mustKind(t, err, ErrKeyCorrupt)
	_, err = p.EnsureKey()
	mustKind(t, err, ErrKeyCorrupt)
}

func TestKey_RejectsRunDsoDirectory(t *testing.T) {
	for _, d := range []string{"/run/dso", "/run/dso/ledger", "/run/dso/../dso/x", "/var/run/dso", "/var/run/dso/ledger/"} {
		if _, err := NewFileKeyProvider(d); !errors.Is(err, ErrKeyDirectory) {
			t.Errorf("%s: err %v", d, err)
		}
	}
	for _, d := range []string{"", DefaultKeyDir, "/run/dsox/ledger", "/var/lib/dso/ledger"} {
		_, err := NewFileKeyProvider(d)
		if d == "" && !errors.Is(err, ErrKeyDirectory) {
			t.Errorf("empty accepted")
		}
		if d != "" && err != nil {
			t.Errorf("%s: %v", d, err)
		}
	}
	if DefaultKeyDir != "/var/lib/dso/ledger" {
		t.Fatal("default dir changed")
	}
	// A symlink into /run/dso is refused too.
	link := filepath.Join(safeTemp(t), "lnk")
	if err := os.Symlink("/run/dso", link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileKeyProvider(filepath.Join(link, "ledger")); !errors.Is(err, ErrKeyDirectory) {
		t.Errorf("symlink to /run/dso accepted: %v", err)
	}
	if _, err := os.Stat("/run/dso"); err == nil {
		t.Log("note: /run/dso exists on this host (not created by this test)")
	}
}

func TestKey_CSPRNGFailureLeavesNothing(t *testing.T) {
	for _, failAt := range []int{1, 2} { // 1: the key read, 2: the temp-name read
		t.Run(fmt.Sprintf("read%d", failAt), func(t *testing.T) {
			dir := keyDir(t)
			cr := &countingReader{fail: failAt, r: strings.NewReader(strings.Repeat("a", 4096))}
			p := newKP(t, dir, func(d *keyDeps) { d.random = cr })
			_, err := p.Generate()
			mustKind(t, err, ErrKeyCreate)
			if got := keyNames(t, dir); len(got) != 0 {
				t.Fatalf("partial files left: %v", got)
			}
			// A later healthy attempt succeeds (nothing poisoned).
			if _, err := newKP(t, dir, nil).Generate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("short randomness", func(t *testing.T) {
		dir := keyDir(t)
		p := newKP(t, dir, func(d *keyDeps) { d.random = strings.NewReader("short") })
		_, err := p.Generate()
		mustKind(t, err, ErrKeyCreate)
		if len(keyNames(t, dir)) != 0 {
			t.Fatal("files left")
		}
	})
}

func TestKey_OwnerCheckInjectable(t *testing.T) {
	dir := keyDir(t)
	if _, err := newKP(t, dir, nil).Generate(); err != nil {
		t.Fatal(err)
	}
	real := os.Geteuid()
	t.Run("file owned by someone else", func(t *testing.T) {
		p := newKP(t, dir, func(d *keyDeps) { d.euid = func() int { return real + 4242 } })
		_, err := p.LoadKey()
		mustKind(t, err, ErrKeyUnsafe)
	})
	t.Run("owner unknown", func(t *testing.T) {
		p := newKP(t, dir, func(d *keyDeps) { d.ownerUID = func(os.FileInfo) (uint32, bool) { return 0, false } })
		_, err := p.LoadKey()
		mustKind(t, err, ErrKeyUnsafe)
	})
	t.Run("root owner accepted", func(t *testing.T) {
		p := newKP(t, dir, func(d *keyDeps) {
			d.ownerUID = func(os.FileInfo) (uint32, bool) { return 0, true }
			d.euid = func() int { return real + 4242 }
		})
		if _, err := p.LoadKey(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("foreign owner rejected without regenerate", func(t *testing.T) {
		p := newKP(t, dir, func(d *keyDeps) {
			d.ownerUID = func(os.FileInfo) (uint32, bool) { return uint32(real + 1), true }
		})
		_, err := p.EnsureKey()
		mustKind(t, err, ErrKeyUnsafe)
		if len(keyNames(t, dir)) != 2 {
			t.Fatal("directory changed")
		}
	})
}

func TestKey_FailurePaths(t *testing.T) {
	t.Run("file fsync failure leaves nothing", func(t *testing.T) {
		dir := keyDir(t)
		p := newKP(t, dir, func(d *keyDeps) { d.fsyncFile = func(*os.File) error { return errors.New("injected") } })
		_, err := p.Generate()
		mustKind(t, err, ErrKeyCreate)
		if len(keyNames(t, dir)) != 0 {
			t.Fatalf("left %v", keyNames(t, dir))
		}
	})
	t.Run("rename failure leaves nothing", func(t *testing.T) {
		dir := keyDir(t)
		p := newKP(t, dir, func(d *keyDeps) { d.renameNoReplace = func(int, string, string) error { return unix.EINVAL } })
		_, err := p.Generate()
		mustKind(t, err, ErrKeyCreate)
		if len(keyNames(t, dir)) != 0 {
			t.Fatalf("left %v", keyNames(t, dir))
		}
	})
	t.Run("dir fsync failure reports error, keeps a valid key", func(t *testing.T) {
		dir := keyDir(t)
		p := newKP(t, dir, func(d *keyDeps) { d.fsyncDir = func(*os.File) error { return errors.New("injected") } })
		_, err := p.Generate()
		mustKind(t, err, ErrKeyCreate)
		id, err := newKP(t, dir, nil).EnsureKey() // retry finds the kept key and repairs the marker
		if err != nil || !ValidKeyID(id) {
			t.Fatalf("%v %v", id, err)
		}
	})
	t.Run("EINTR with completed rename", func(t *testing.T) {
		dir := keyDir(t)
		p := newKP(t, dir, func(d *keyDeps) {
			real := d.renameNoReplace
			d.renameNoReplace = func(fd int, o, n string) error {
				if err := real(fd, o, n); err != nil {
					return err
				}
				return unix.EINTR
			}
		})
		if _, err := p.Generate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("read-only directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory modes")
		}
		dir := keyDir(t)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		_, err := newKP(t, dir, nil).Generate()
		mustKind(t, err, ErrKeyCreate)
		if len(keyNames(t, dir)) != 0 {
			t.Fatal("files created in a read-only directory")
		}
	})
}

func TestKey_MaterialIsACopy(t *testing.T) {
	dir := keyDir(t)
	p := newKP(t, dir, nil)
	if _, err := p.Generate(); err != nil {
		t.Fatal(err)
	}
	_, k1, _ := p.KeyMaterial()
	orig := append([]byte(nil), k1...)
	for i := range k1 {
		k1[i] = 0
	}
	_, k2, _ := p.KeyMaterial()
	if !bytes.Equal(k2, orig) {
		t.Fatal("KeyMaterial did not return an independent copy")
	}
	hk, _ := p.LoadKey()
	b := hk.Bytes()
	b[0] ^= 0xff
	if bytes.Equal(hk.Bytes(), b) {
		t.Fatal("Bytes aliases internal storage")
	}
	// The key works with the existing identity encoder.
	id, key, _ := p.KeyMaterial()
	if _, err := NewHMACVersion(id, key, "p", "s", map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
}

func TestKey_NoMaterialInAnyFormattedOutput(t *testing.T) {
	dir := keyDir(t)
	p := newKP(t, dir, nil)
	if _, err := p.Generate(); err != nil {
		t.Fatal(err)
	}
	hk, err := p.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	key := hk.Bytes()
	raw, _ := os.ReadFile(filepath.Join(dir, keyFileName))
	forbidden := []string{
		hex.EncodeToString(key), strings.ToUpper(hex.EncodeToString(key)),
		base64.StdEncoding.EncodeToString(key), base64.RawURLEncoding.EncodeToString(key),
		base64.URLEncoding.EncodeToString(key), string(key), fmt.Sprintf("%v", key), fmt.Sprintf("%v", [HMACKeySize]byte(key)),
		strings.Join(strings.Fields(strings.Trim(fmt.Sprintf("%d", key), "[]")), ","),
	}
	check := func(label, s string) {
		for _, f := range forbidden {
			if len(f) > 6 && strings.Contains(s, f) {
				t.Errorf("%s leaks key material", label)
			}
		}
		if strings.Contains(s, string(raw[len(keyFileHeader)+20:len(raw)-1])) { // the base64 line
			t.Errorf("%s leaks the encoded key line", label)
		}
	}
	// Every exported type holding key material, under every verb.
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%T", "%10.3v"}
	subjects := map[string]any{"HMACKey": hk, "*HMACKey": &hk, "FileKeyProvider": p, "KeyInspection": p.Inspect()}
	for name, v := range subjects {
		for _, vb := range verbs {
			check(name+" "+vb, fmt.Sprintf(vb, v))
		}
		check(name+" Sprint", fmt.Sprint(v))
		check(name+" Sprintln", fmt.Sprintln(v))
		check(name+" nested", fmt.Sprintf("%+v", struct{ V any }{v}))
		check(name+" slice", fmt.Sprintf("%v", []any{v}))
	}
	if j, err := jsonMarshal(hk); err != nil {
		t.Fatal(err)
	} else {
		check("json", string(j))
	}
	if j, _ := jsonMarshal(map[string]any{"k": &hk}); true {
		check("json nested", string(j))
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("k", "key", hk)
	slog.New(slog.NewTextHandler(&buf, nil)).Info("k", "key", hk, "p", p)
	check("slog", buf.String())
	if hk.String() != hk.GoString() || !strings.Contains(hk.String(), "REDACTED") || !strings.Contains(hk.String(), hk.ID()) {
		t.Errorf("redaction text: %s", hk)
	}
	// Errors may include the key id but never material: provoke every kind with
	// files that contain the real key.
	badID := encodeKeyFile("k-0000000000000000", key)
	short := raw[:len(raw)-10]
	var errsOut []error
	mk := func(content []byte, mode os.FileMode, extra func(string)) {
		d := keyDir(t)
		writeKeyFile(t, filepath.Join(d, keyFileName), content, mode)
		if extra != nil {
			extra(d)
		}
		_, e := newKP(t, d, nil).LoadKey()
		_, e2 := newKP(t, d, nil).Generate()
		_, e3 := newKP(t, d, nil).EnsureKey()
		errsOut = append(errsOut, e, e2, e3, fmt.Errorf("wrapped: %w", e))
		ins := newKP(t, d, nil).Inspect()
		check("inspection reason", ins.Reason+ins.KeyID)
	}
	mk(badID, 0o400, nil)
	mk(short, 0o400, nil)
	mk(raw, 0o644, nil)
	mk(raw, 0o400, func(d string) {
		writeKeyFile(t, filepath.Join(d, keyMarkerName), []byte("k-ffffffffffffffff\n"), 0o400)
	})
	lost := keyDir(t)
	writeKeyFile(t, filepath.Join(lost, ledgerFileName), raw, 0o600)
	_, e := newKP(t, lost, nil).LoadKey()
	errsOut = append(errsOut, e)
	_, e = newKP(t, dir, func(d *keyDeps) { d.random = strings.NewReader("") }).Generate()
	errsOut = append(errsOut, e)
	for i, e := range errsOut {
		if e == nil {
			continue
		}
		for _, vb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			check(fmt.Sprintf("error %d %s", i, vb), fmt.Sprintf(vb, e))
		}
	}
}

func TestKey_StoreIntegration(t *testing.T) {
	dir := filepath.Join(safeTemp(t), "ledger")
	p := newKP(t, dir, nil)
	s, res, err := Open(dir, Config{Keys: p})
	if err != nil {
		t.Fatal(err)
	}
	kid, _, kerr := p.KeyMaterial()
	if kerr != nil || res.Mode != ModeDurable || res.KeyID != kid {
		t.Fatalf("first run: %+v kid=%s err=%v", res, kid, kerr)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Restart with the same key: durable, same id.
	s2, res2, err := Open(dir, Config{Keys: newKP(t, dir, nil)})
	if err != nil || res2.Mode != ModeDurable || res2.KeyID != kid {
		t.Fatalf("restart: %+v %v", res2, err)
	}
	_ = s2.Close()
	// Key file lost while a ledger exists: fail closed, nothing created or moved.
	if err := os.Remove(filepath.Join(dir, keyFileName)); err != nil {
		t.Fatal(err)
	}
	before := keyNames(t, dir)
	s3, res3, err := Open(dir, Config{Keys: newKP(t, dir, nil)})
	if err != nil {
		t.Fatal(err)
	}
	_ = s3.Close()
	if res3.Mode != ModeMemoryOnly || res3.Reason != ReasonKeyUnsafe {
		t.Fatalf("lost key: %+v", res3)
	}
	if _, err := os.Lstat(filepath.Join(dir, keyFileName)); err == nil {
		t.Fatal("a key was recreated")
	}
	for _, n := range before {
		if _, err := os.Lstat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s vanished: %v", n, err)
		}
	}
}
