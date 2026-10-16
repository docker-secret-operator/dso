package crypto

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

func mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := mustKey(t)
	for _, pt := range [][]byte{[]byte("x"), []byte("hunter2"), bytes.Repeat([]byte{0, 1, 2}, 1000), make([]byte, MaxPlaintextSize)} {
		n, ct, err := Seal(key, pt, []byte("aad"))
		if err != nil {
			t.Fatal(err)
		}
		if len(n) != NonceSize {
			t.Fatalf("nonce size %d", len(n))
		}
		if len(pt) > 8 && bytes.Contains(ct, pt[:8]) && !bytes.Equal(pt[:8], make([]byte, 8)) {
			t.Fatal("ciphertext contains plaintext")
		}
		got, err := Open(key, n, ct, []byte("aad"))
		if err != nil || !bytes.Equal(got, pt) {
			t.Fatalf("round trip failed: %v", err)
		}
	}
}

func TestSealRejectsOversizeAndBadKey(t *testing.T) {
	if _, _, err := Seal(mustKey(t), make([]byte, MaxPlaintextSize+1), nil); !errors.Is(err, ErrPlaintextTooLarge) {
		t.Fatalf("got %v", err)
	}
	for _, k := range [][]byte{nil, make([]byte, 16), make([]byte, 33)} {
		if _, _, err := Seal(k, []byte("x"), nil); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("len %d: %v", len(k), err)
		}
		if _, err := Open(k, make([]byte, NonceSize), make([]byte, 32), nil); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("open len %d: %v", len(k), err)
		}
	}
}

func TestOpenFailsClosedOnAnyTamper(t *testing.T) {
	key := mustKey(t)
	n, ct, _ := Seal(key, []byte("super-secret-value"), []byte("ctx"))
	// flip every single bit of ciphertext (including the tag) and of the nonce
	for i := range ct {
		for b := 0; b < 8; b++ {
			c := append([]byte(nil), ct...)
			c[i] ^= 1 << b
			if _, err := Open(key, n, c, []byte("ctx")); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("tampered byte %d bit %d accepted", i, b)
			}
		}
	}
	for i := range n {
		nn := append([]byte(nil), n...)
		nn[i] ^= 1
		if _, err := Open(key, nn, ct, []byte("ctx")); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("tampered nonce byte %d accepted", i)
		}
	}
	// truncated tag / ciphertext, short nonce
	for _, c := range [][]byte{nil, ct[:len(ct)-1], ct[:5]} {
		if _, err := Open(key, n, c, []byte("ctx")); !errors.Is(err, ErrDecrypt) {
			t.Fatal("truncated ciphertext accepted")
		}
	}
	if _, err := Open(key, n[:5], ct, []byte("ctx")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("short nonce accepted")
	}
	// wrong aad, wrong key
	if _, err := Open(key, n, ct, []byte("other")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("wrong aad accepted")
	}
	if _, err := Open(mustKey(t), n, ct, []byte("ctx")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("wrong key accepted")
	}
}

func TestDecryptErrorsDoNotLeakMaterial(t *testing.T) {
	key := mustKey(t)
	n, ct, _ := Seal(key, []byte("PLAINTEXT-MARKER"), nil)
	_, err := Open(mustKey(t), n, ct, nil)
	for _, leak := range []string{"PLAINTEXT-MARKER", string(key), string(ct)} {
		if strings.Contains(err.Error(), leak) {
			t.Fatal("error leaks material")
		}
	}
}

func TestNoncesAreUniquePerSeal(t *testing.T) {
	key := mustKey(t)
	seen := map[string]bool{}
	for i := 0; i < 20000; i++ {
		n, _, err := Seal(key, []byte("same plaintext"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if seen[string(n)] {
			t.Fatalf("nonce reuse after %d seals", i)
		}
		seen[string(n)] = true
	}
}

func TestSameInputGivesDifferentCiphertexts(t *testing.T) {
	key := mustKey(t)
	_, a, _ := Seal(key, []byte("v"), nil)
	_, b, _ := Seal(key, []byte("v"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("deterministic encryption")
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("entropy exhausted") }

func TestEntropyFailureFailsClosed(t *testing.T) {
	old := randReader
	randReader = failReader{}
	defer func() { randReader = old }()
	if _, err := GenerateKey(); err == nil {
		t.Fatal("GenerateKey succeeded without entropy")
	}
	if _, _, err := Seal(bytes.Repeat([]byte{1}, KeySize), []byte("x"), nil); err == nil {
		t.Fatal("Seal succeeded without entropy")
	}
}

func TestGeneratedKeysAreDistinctAndFullLength(t *testing.T) {
	a, b := mustKey(t), mustKey(t)
	if len(a) != KeySize || bytes.Equal(a, b) || bytes.Equal(a, make([]byte, KeySize)) {
		t.Fatal("bad key generation")
	}
}

func TestAADEncodingIsUnambiguous(t *testing.T) {
	if bytes.Equal(aad("d", "ab", "c"), aad("d", "a", "bc")) {
		t.Fatal("field boundary collision")
	}
	base := SecretAAD("p", "e", "s", "n", 1, 1)
	for name, other := range map[string][]byte{
		"project": SecretAAD("p2", "e", "s", "n", 1, 1), "env": SecretAAD("p", "e2", "s", "n", 1, 1),
		"secret": SecretAAD("p", "e", "s2", "n", 1, 1), "name": SecretAAD("p", "e", "s", "n2", 1, 1),
		"version": SecretAAD("p", "e", "s", "n", 2, 1), "dek": SecretAAD("p", "e", "s", "n", 1, 2),
	} {
		if bytes.Equal(base, other) {
			t.Fatalf("AAD does not bind %s", name)
		}
	}
	if bytes.Equal(DEKAAD("p", "k", 1), SecretAAD("p", "k", "", "", 1, 1)) {
		t.Fatal("DEK and secret AAD domains collide")
	}
}

// ---- providers ------------------------------------------------------------

func TestFileProviderLifecycle(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "keys", "root.key")
	id, err := InstallKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o400 {
		t.Fatalf("key mode %04o, want 0400", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("created key dir mode %04o, want 0700", di.Mode().Perm())
	}
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 1 {
		t.Fatalf("temp files left behind: %v", ents)
	}
	kp, err := NewFileKeyProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	if kp.ID() != id {
		t.Fatal("key id mismatch")
	}
	dek := mustKey(t)
	w, err := kp.Wrap(context.Background(), dek, []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(w, dek) {
		t.Fatal("wrapped key contains DEK")
	}
	got, err := kp.Unwrap(context.Background(), w, []byte("a"))
	if err != nil || !bytes.Equal(got, dek) {
		t.Fatal("unwrap failed")
	}
	if _, err := kp.Unwrap(context.Background(), w, []byte("b")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("unwrap with wrong aad succeeded")
	}
	w[len(w)-1] ^= 1
	if _, err := kp.Unwrap(context.Background(), w, []byte("a")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("tampered wrapped key accepted")
	}
	if _, err := kp.Unwrap(context.Background(), []byte("short"), nil); !errors.Is(err, ErrDecrypt) {
		t.Fatal("short wrapped key accepted")
	}
	if _, err := kp.Wrap(context.Background(), []byte("not 32"), nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatal("wrapped a non-32-byte key")
	}
}

func TestInstallKeyFileNeverOverwrites(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "root.key")
	if _, err := InstallKeyFile(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := InstallKeyFile(path); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected refusal, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing key was modified")
	}
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 1 {
		t.Fatalf("temp files left behind after refusal: %d entries", len(ents))
	}
}

func TestInstallKeyFileConcurrentExactlyOneWins(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "root.key")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := InstallKeyFile(path); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d installers succeeded, want exactly 1", wins)
	}
	if _, err := NewFileKeyProvider(path); err != nil {
		t.Fatalf("winning key unreadable: %v", err)
	}
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 1 {
		t.Fatalf("temp files left behind: %d", len(ents))
	}
}

func TestFileProviderRejectsBadKeyFiles(t *testing.T) {
	dir := securefstest.TempDir(t)
	write := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := base64Key()
	cases := map[string]string{
		"missing":      filepath.Join(dir, "nope"),
		"empty":        write("empty", "", 0o400),
		"garbage":      write("garbage", "hello world", 0o400),
		"short":        write("short", fileKeyPrefix+"AAAA", 0o400),
		"not-base64":   write("b64", fileKeyPrefix+"!!!!", 0o400),
		"group-read":   write("grp", good, 0o440),
		"world-read":   write("wld", good, 0o644),
		"world-write":  write("ww", good, 0o602),
		"is-directory": dir,
	}
	for name, p := range cases {
		kp, err := NewFileKeyProvider(p)
		if err == nil || kp != nil {
			t.Fatalf("%s: accepted", name)
		}
		if !errors.Is(err, ErrKeyMaterial) {
			t.Fatalf("%s: wrong error type: %v", name, err)
		}
	}
	// symlink to a good key must not be followed
	target := write("target", good, 0o400)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := NewFileKeyProvider(link); err == nil {
		t.Fatal("symlinked key file accepted")
	}
	if kp, err := NewFileKeyProvider(target); err != nil || kp == nil {
		t.Fatalf("good key rejected: %v", err)
	}
}

func base64Key() string {
	return fileKeyPrefix + "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" + "\n"
}

func TestKeyErrorsDoNotContainKeyMaterial(t *testing.T) {
	p := filepath.Join(securefstest.TempDir(t), "k")
	secret := "SECRETKEYMATERIAL1234567890abcdefghijk"
	os.WriteFile(p, []byte(fileKeyPrefix+secret), 0o644)
	_, err := NewFileKeyProvider(p)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks key material or is nil: %v", err)
	}
}

func TestPassphraseProvider(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "root.pass")
	pass := []byte("correct horse battery staple")
	id, err := InstallPassphraseKeyFile(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, pass) {
		t.Fatal("passphrase stored in key file")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o400 {
		t.Fatalf("mode %04o", fi.Mode().Perm())
	}
	kp, err := NewPassphraseKeyProvider(path, pass)
	if err != nil || kp.ID() != id {
		t.Fatalf("unseal failed: %v", err)
	}
	dek := mustKey(t)
	w, _ := kp.Wrap(context.Background(), dek, nil)
	kp2, _ := NewPassphraseKeyProvider(path, pass)
	if got, err := kp2.Unwrap(context.Background(), w, nil); err != nil || !bytes.Equal(got, dek) {
		t.Fatal("re-derived key cannot unwrap")
	}
	for _, bad := range [][]byte{[]byte("correct horse battery stapl"), []byte("x"), nil} {
		if _, err := NewPassphraseKeyProvider(path, bad); !errors.Is(err, ErrWrongPassphrase) {
			t.Fatalf("wrong passphrase %q: %v", bad, err)
		}
	}
	if _, err := InstallPassphraseKeyFile(path, pass); err == nil {
		t.Fatal("overwrote existing passphrase key file")
	}
	if _, err := InstallPassphraseKeyFile(filepath.Join(securefstest.TempDir(t), "x"), []byte("short")); err == nil {
		t.Fatal("accepted a short passphrase")
	}
}

func TestPassphraseSaltsDiffer(t *testing.T) {
	d := securefstest.TempDir(t)
	pass := []byte("same passphrase twice!")
	a, _ := InstallPassphraseKeyFile(filepath.Join(d, "a"), pass)
	b, _ := InstallPassphraseKeyFile(filepath.Join(d, "b"), pass)
	if a == b {
		t.Fatal("same passphrase produced same key (salt reused)")
	}
}

func TestPassphraseParameterTamperingIsRejected(t *testing.T) {
	d := securefstest.TempDir(t)
	pass := []byte("a long enough passphrase")
	src := filepath.Join(d, "ok")
	if _, err := InstallPassphraseKeyFile(src, pass); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(src)
	mutate := func(name, from, to string) {
		p := filepath.Join(d, name)
		s := strings.Replace(string(orig), from, to, 1)
		if s == string(orig) {
			t.Fatalf("%s: mutation did not apply", name)
		}
		os.WriteFile(p, []byte(s), 0o400)
		if _, err := NewPassphraseKeyProvider(p, pass); err == nil || errors.Is(err, ErrWrongPassphrase) {
			t.Fatalf("%s: expected parameter rejection, got %v", name, err)
		}
	}
	mutate("downgrade-time", `"time": 3`, `"time": 1`)
	mutate("downgrade-mem", `"memory_kib": 65536`, `"memory_kib": 8`)
	mutate("huge-mem", `"memory_kib": 65536`, `"memory_kib": 4000000`)
	mutate("huge-time", `"time": 3`, `"time": 5000`)
	mutate("kdf", `"argon2id"`, `"md5"`)
	mutate("format", `dso-passphrase-key-v1`, `v0`)
	// a changed salt derives a different key: wrong-passphrase, never silent success
	p := filepath.Join(d, "salt")
	os.WriteFile(p, []byte(strings.Replace(string(orig), `"salt": "`, `"salt": "AAAA`, 1)), 0o400)
	if _, err := NewPassphraseKeyProvider(p, pass); err == nil {
		t.Fatal("tampered salt accepted")
	}
}
