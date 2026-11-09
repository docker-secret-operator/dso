package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const helperEnv = "DSO_VAULT_TEST_HELPER"

// initTestVault points HOME at a temp dir and initializes a real vault.
func initTestVault(t *testing.T) (dir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSO_MASTER_KEY", "")
	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault: %v", err)
	}
	return filepath.Join(home, ".dso")
}

func mustLoad(t *testing.T) *Vault {
	t.Helper()
	v, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	return v
}

func readVaultBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "vault.enc"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func leftoverTemps(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			out = append(out, e.Name())
		}
	}
	return out
}

// Regression for the lost-update defect: two handles loaded from the same
// on-disk state, each writing a different key, must BOTH be present afterwards.
// (Before the fix the second Save overwrote the first handle's write.)
func TestSet_NoLostUpdateBetweenStaleHandles(t *testing.T) {
	initTestVault(t)
	a, b := mustLoad(t), mustLoad(t)
	if err := a.Set("proj", "from-a", "A"); err != nil {
		t.Fatal(err)
	}
	if err := b.Set("proj", "from-b", "B"); err != nil { // b was loaded before a's write
		t.Fatal(err)
	}
	fresh := mustLoad(t)
	for k, want := range map[string]string{"from-a": "A", "from-b": "B"} {
		got, err := fresh.Get("proj", k)
		if err != nil || got.Value != want {
			t.Errorf("%s lost: %v %q", k, err, got.Value)
		}
	}
	// The stale handle also sees the other writer's key after its own write.
	if _, err := b.Get("proj", "from-a"); err != nil {
		t.Errorf("handle did not refresh from disk: %v", err)
	}
}

func TestSetBatch_NoLostUpdateBetweenStaleHandles(t *testing.T) {
	initTestVault(t)
	a, b := mustLoad(t), mustLoad(t)
	if err := a.SetBatch("proj", map[string]string{"a1": "1", "a2": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetBatch("proj", map[string]string{"b1": "3"}); err != nil {
		t.Fatal(err)
	}
	paths, _ := mustLoad(t).List("proj")
	if len(paths) != 3 {
		t.Fatalf("expected 3 keys, got %v", paths)
	}
}

func TestSetBatch_InvalidEntryAppliesNothing(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	before := readVaultBytes(t, dir)
	err := v.SetBatch("proj", map[string]string{"ok": "1", "../evil": "2"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !bytes.Equal(before, readVaultBytes(t, dir)) {
		t.Error("vault file changed despite a rejected batch")
	}
	if _, err := v.Get("proj", "ok"); err == nil {
		t.Error("partially applied batch left state in memory")
	}
}

// Concurrent writers on different keys through independent handles (each has
// its own lock file descriptor, exactly like separate processes).
func TestConcurrentWriters_DistinctKeys_AllPreserved(t *testing.T) {
	initTestVault(t)
	const writers, perWriter = 8, 3
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			v, err := LoadDefault()
			if err != nil {
				errs <- err
				return
			}
			for i := 0; i < perWriter; i++ {
				if err := v.Set("proj", fmt.Sprintf("w%d-k%d", w, i), fmt.Sprintf("v%d-%d", w, i)); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("writer failed: %v", err)
	}
	fresh := mustLoad(t)
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			sec, err := fresh.Get("proj", fmt.Sprintf("w%d-k%d", w, i))
			if err != nil || sec.Value != fmt.Sprintf("v%d-%d", w, i) {
				t.Errorf("key w%d-k%d lost or wrong: %v %q", w, i, err, sec.Value)
			}
		}
	}
}

// Real separate OS processes: the test binary re-executes itself.
func TestConcurrentWriters_SeparateProcesses(t *testing.T) {
	if os.Getenv(helperEnv) != "" {
		t.Skip("helper mode")
	}
	dir := initTestVault(t)
	home := filepath.Dir(dir)
	const procs, perProc = 4, 3
	var wg sync.WaitGroup
	outs := make([][]byte, procs)
	errs := make([]error, procs)
	for p := 0; p < procs; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestVaultHelperWriter$", "-test.v")
			cmd.Env = append(os.Environ(), helperEnv+"=1", "HOME="+home,
				fmt.Sprintf("DSO_HELPER_ID=%d", p), fmt.Sprintf("DSO_HELPER_N=%d", perProc))
			outs[p], errs[p] = cmd.CombinedOutput()
		}(p)
	}
	wg.Wait()
	for p := 0; p < procs; p++ {
		if errs[p] != nil {
			t.Fatalf("process %d failed: %v\n%s", p, errs[p], outs[p])
		}
	}
	fresh := mustLoad(t)
	for p := 0; p < procs; p++ {
		for i := 0; i < perProc; i++ {
			if _, err := fresh.Get("proj", fmt.Sprintf("p%d-k%d", p, i)); err != nil {
				t.Errorf("process %d key %d lost: %v", p, i, err)
			}
		}
	}
	if left := leftoverTemps(t, dir); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// TestVaultHelperWriter is the child half of the multi-process test.
func TestVaultHelperWriter(t *testing.T) {
	if os.Getenv(helperEnv) == "" {
		t.Skip("only runs as a child of TestConcurrentWriters_SeparateProcesses")
	}
	var id, n int
	_, _ = fmt.Sscan(os.Getenv("DSO_HELPER_ID"), &id)
	_, _ = fmt.Sscan(os.Getenv("DSO_HELPER_N"), &n)
	v, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := v.Set("proj", fmt.Sprintf("p%d-k%d", id, i), "value"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitDefault_ConcurrentInitsShareOneKeyAndVault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSO_MASTER_KEY", "")
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- InitDefault() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("InitDefault: %v", err)
		}
	}
	if _, err := LoadDefault(); err != nil {
		t.Fatalf("vault undecryptable after concurrent init (key/vault mismatch): %v", err)
	}
}

// ---- failure injection ------------------------------------------------------

func withHooks(t *testing.T) {
	t.Helper()
	r, s, d := renameFile, syncFile, syncDir
	t.Cleanup(func() { renameFile, syncFile, syncDir = r, s, d })
}

func TestSet_RenameFailure_LeavesVaultIntactReleasesLockAndCleansTemp(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	if err := v.Set("proj", "existing", "keep"); err != nil {
		t.Fatal(err)
	}
	before := readVaultBytes(t, dir)

	withHooks(t)
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	err := v.Set("proj", "new", "x")
	if err == nil || !strings.Contains(err.Error(), "injected rename failure") {
		t.Fatalf("Set must surface the rename error, got %v", err)
	}
	if !bytes.Equal(before, readVaultBytes(t, dir)) {
		t.Error("vault file modified by a failed save")
	}
	if left := leftoverTemps(t, dir); len(left) != 0 {
		t.Errorf("temp files not cleaned: %v", left)
	}
	if _, gerr := v.Get("proj", "new"); gerr == nil {
		t.Error("failed write left the new key in memory (a later Save would persist it)")
	}

	// Lock released: another writer can take it immediately.
	release, lerr := acquireFileLock(filepath.Join(dir, "vault.enc.lock"), 500*time.Millisecond)
	if lerr != nil {
		t.Fatalf("lock still held after failed Set: %v", lerr)
	}
	_ = release()

	// And the same handle works once the fault clears.
	renameFile = os.Rename
	if err := v.Set("proj", "new", "x"); err != nil {
		t.Fatalf("Set after recovery: %v", err)
	}
	got := mustLoad(t)
	if s, err := got.Get("proj", "new"); err != nil || s.Value != "x" {
		t.Errorf("post-recovery value: %v %q", err, s.Value)
	}
	if s, err := got.Get("proj", "existing"); err != nil || s.Value != "keep" {
		t.Errorf("existing value lost: %v %q", err, s.Value)
	}
}

func TestSet_FileSyncFailure_IsReportedAndNothingCommitted(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	before := readVaultBytes(t, dir)
	withHooks(t)
	syncFile = func(*os.File) error { return errors.New("injected fsync failure") }
	if err := v.Set("proj", "k", "v"); err == nil || !strings.Contains(err.Error(), "fsync") {
		t.Fatalf("fsync failure must not be reported as success: %v", err)
	}
	if !bytes.Equal(before, readVaultBytes(t, dir)) {
		t.Error("vault changed even though the data was never flushed")
	}
	if left := leftoverTemps(t, dir); len(left) != 0 {
		t.Errorf("temp files not cleaned: %v", left)
	}
}

func TestSet_DirectorySyncFailure_IsReported(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	withHooks(t)
	syncDir = func(string) error { return errors.New("injected dir sync failure") }
	err := v.Set("proj", "k", "v")
	if err == nil || !strings.Contains(err.Error(), "directory sync failed") {
		t.Fatalf("directory sync failure must be reported: %v", err)
	}
	// The rename already happened, so the file is complete and readable; the
	// error honestly says durability of the directory entry is unconfirmed.
	syncDir = syncDirectory
	if s, gerr := mustLoad(t).Get("proj", "k"); gerr != nil || s.Value != "v" {
		t.Errorf("vault unreadable after dir-sync failure: %v", gerr)
	}
	if left := leftoverTemps(t, dir); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

// Simulates a process killed mid-save: it leaves a partial temp file (and the
// legacy fixed-name temp) but never touched vault.enc.
func TestInterruptedSave_StaleTempFilesDoNotAffectVault(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	if err := v.Set("proj", "k1", "v1"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".vault.enc.tmp-123456", "vault.enc.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("partial garbage"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if s, err := mustLoad(t).Get("proj", "k1"); err != nil || s.Value != "v1" {
		t.Fatalf("stale temp files broke loading: %v", err)
	}
	if err := v.Set("proj", "k2", "v2"); err != nil {
		t.Fatalf("stale temp files broke saving: %v", err)
	}
	fresh := mustLoad(t)
	for k, want := range map[string]string{"k1": "v1", "k2": "v2"} {
		if s, err := fresh.Get("proj", k); err != nil || s.Value != want {
			t.Errorf("%s: %v %q", k, err, s.Value)
		}
	}
}

func TestSet_RefusesToOverwriteUnreadableVault(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	if err := os.WriteFile(filepath.Join(dir, "vault.enc"), []byte("corrupted by something else, not a vault"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readVaultBytes(t, dir)
	if err := v.Set("proj", "k", "v"); err == nil {
		t.Fatal("Set must fail rather than overwrite a vault it cannot decrypt")
	}
	if !bytes.Equal(before, readVaultBytes(t, dir)) {
		t.Error("unreadable vault was overwritten")
	}
}

func TestSet_WrongMasterKeyDoesNotDestroyVault(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	if err := v.Set("proj", "k", "v"); err != nil {
		t.Fatal(err)
	}
	before := readVaultBytes(t, dir)
	wrong := &Vault{store: &VaultStore{Projects: map[string]map[string]Secret{}}, vaultPath: filepath.Join(dir, "vault.enc"), masterKey: "not-the-key"}
	if err := wrong.Set("proj", "x", "y"); err == nil {
		t.Fatal("expected decrypt failure")
	}
	if !bytes.Equal(before, readVaultBytes(t, dir)) {
		t.Error("vault overwritten using the wrong key")
	}
}

func TestLock_TimeoutIsReportedAndReleasedLockUnblocks(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	lockPath := filepath.Join(dir, "vault.enc.lock")
	release, err := acquireFileLock(lockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	old := lockTimeout
	lockTimeout = 150 * time.Millisecond
	t.Cleanup(func() { lockTimeout = old })

	start := time.Now()
	err = v.Set("proj", "k", "v")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a lock timeout error, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("lock wait did not honor the timeout")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("proj", "k", "v"); err != nil {
		t.Fatalf("Set after lock release: %v", err)
	}
}

// ---- format / permission compatibility -------------------------------------

func TestPermissions_VaultAndLockFileAre0600(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	if err := v.Set("proj", "k", "v"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"vault.enc", "vault.enc.lock", "master.key"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, fi.Mode().Perm())
		}
	}
}

// A vault blob produced exactly the way the pre-hardening code produced it
// (same JSON layout, same checksum rule, same Encrypt) must still load, and a
// write must preserve its contents.
func TestFormatCompatibility_LegacyVaultStillLoadsAndSurvivesWrites(t *testing.T) {
	dir := initTestVault(t)
	keyBytes, _ := os.ReadFile(filepath.Join(dir, "master.key"))
	key := strings.TrimSpace(string(keyBytes))

	store := VaultStore{
		Version:  "1.0",
		Metadata: map[string]string{"created_at": "2024-01-01T00:00:00Z"},
		Projects: map[string]map[string]Secret{"legacy": {"db/password": {Value: "old-secret", Meta: map[string]string{"updated_at": "2024-01-01T00:00:00Z"}}}},
	}
	h, _ := json.Marshal(store)
	store.Metadata["checksum"] = fmt.Sprintf("sha256:%x", sha256.Sum256(h))
	plain, _ := json.Marshal(store)
	ct, err := Encrypt(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault.enc"), ct, 0o600); err != nil {
		t.Fatal(err)
	}

	v := mustLoad(t)
	if s, err := v.Get("legacy", "db/password"); err != nil || s.Value != "old-secret" {
		t.Fatalf("legacy vault unreadable: %v %q", err, s.Value)
	}
	if err := v.Set("legacy", "new", "n"); err != nil {
		t.Fatal(err)
	}
	fresh := mustLoad(t)
	if s, err := fresh.Get("legacy", "db/password"); err != nil || s.Value != "old-secret" {
		t.Errorf("legacy secret lost after write: %v", err)
	}
	if s, err := fresh.Get("legacy", "new"); err != nil || s.Value != "n" {
		t.Errorf("new secret missing: %v", err)
	}
}

// ---- post-rename failure semantics (review finding L2) ----------------------

var errInjectedDirSync = errors.New("injected dir sync failure")

func TestSet_PostRenameFailure_IsTypedCommittedAndStateStaysConsistent(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	if err := v.Set("proj", "before", "1"); err != nil {
		t.Fatal(err)
	}

	withHooks(t)
	syncDir = func(string) error { return errInjectedDirSync }
	err := v.Set("proj", "during", "2")
	if err == nil {
		t.Fatal("expected an error: durability could not be confirmed")
	}
	if !IsCommitted(err) {
		t.Fatalf("a failure after the rename must be reported as committed: %v", err)
	}
	var de *DurabilityError
	if !errors.As(err, &de) || !errors.Is(err, errInjectedDirSync) {
		t.Errorf("error does not expose DurabilityError / underlying cause: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "committed") || !strings.Contains(msg, "directory sync failed") || strings.Contains(strings.ToLower(msg), "rolled back") {
		t.Errorf("message must say committed + durability unconfirmed, never rolled back: %q", msg)
	}

	// Disk and memory agree: the new value is on disk AND in the handle.
	if s, gerr := v.Get("proj", "during"); gerr != nil || s.Value != "2" {
		t.Errorf("in-memory state was rolled back although the file was committed: %v %q", gerr, s.Value)
	}
	onDisk := mustLoad(t)
	for k, want := range map[string]string{"before": "1", "during": "2"} {
		if s, gerr := onDisk.Get("proj", k); gerr != nil || s.Value != want {
			t.Errorf("disk %s: %v %q", k, gerr, s.Value)
		}
	}
	if left := leftoverTemps(t, dir); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}

	// The handle remains fully usable and consistent afterwards.
	syncDir = syncDirectory
	if err := v.Set("proj", "after", "3"); err != nil {
		t.Fatalf("Set after durability failure: %v", err)
	}
	final := mustLoad(t)
	for _, k := range []string{"before", "during", "after"} {
		if _, gerr := final.Get("proj", k); gerr != nil {
			t.Errorf("%s missing: %v", k, gerr)
		}
	}
}

func TestSetBatch_And_Save_PostRenameFailureAreTypedToo(t *testing.T) {
	initTestVault(t)
	v := mustLoad(t)
	withHooks(t)
	syncDir = func(string) error { return errInjectedDirSync }
	if err := v.SetBatch("proj", map[string]string{"a": "1", "b": "2"}); !IsCommitted(err) {
		t.Errorf("SetBatch: %v", err)
	}
	if paths, _ := v.List("proj"); len(paths) != 2 {
		t.Errorf("SetBatch in-memory state = %v, want both keys", paths)
	}
	if err := v.Save(); !IsCommitted(err) {
		t.Errorf("Save: %v", err)
	}
}

func TestSet_PreCommitFailuresAreNotReportedAsCommitted(t *testing.T) {
	initTestVault(t)
	v := mustLoad(t)
	withHooks(t)

	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	if err := v.Set("proj", "k", "v"); err == nil || IsCommitted(err) {
		t.Errorf("rename failure: err=%v committed=%v", err, IsCommitted(err))
	}
	renameFile = os.Rename

	syncFile = func(*os.File) error { return errors.New("injected fsync failure") }
	if err := v.Set("proj", "k", "v"); err == nil || IsCommitted(err) {
		t.Errorf("fsync failure: err=%v committed=%v", err, IsCommitted(err))
	}
	if _, err := v.Get("proj", "k"); err == nil {
		t.Error("pre-commit failure must leave no trace in memory")
	}
	if IsCommitted(nil) || IsCommitted(errors.New("plain")) {
		t.Error("IsCommitted must be false for nil and unrelated errors")
	}
}

// ---- master key validity and atomic installation (review finding L5) --------

func keyPathOf(dir string) string { return filepath.Join(dir, "master.key") }

func TestMasterKey_EmptyExistingKeyIsRejected_AndNoVaultIsCreated(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "newline only": "\n", "whitespace": "  \n\t \n"} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("DSO_MASTER_KEY", "")
		dir := filepath.Join(home, ".dso")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyPathOf(dir), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		err := InitDefault()
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Errorf("%s: InitDefault must reject the blank key clearly, got %v", name, err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "vault.enc")); statErr == nil {
			t.Errorf("%s: a vault was created under an empty key", name)
		}
		if _, err := LoadDefault(); err == nil {
			t.Errorf("%s: LoadDefault accepted a blank key", name)
		}
	}
}

func TestMasterKey_BlankKeyDoesNotDecryptExistingVault(t *testing.T) {
	dir := initTestVault(t)
	v := mustLoad(t)
	if err := v.Set("proj", "k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPathOf(dir), []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadDefault()
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected a clear empty-key error, got %v", err)
	}
}

func TestMasterKey_BlankEnvironmentKeyIsRejected(t *testing.T) {
	initTestVault(t)
	t.Setenv("DSO_MASTER_KEY", "   ")
	if _, err := getMasterKey(); err == nil || !strings.Contains(err.Error(), "blank") {
		t.Errorf("blank DSO_MASTER_KEY: %v", err)
	}
}

func TestInitDefault_InterruptedKeyWrite_LeavesNoKeyNoVaultNoTemp_AndRecovers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSO_MASTER_KEY", "")
	dir := filepath.Join(home, ".dso")

	withHooks(t)
	syncFile = func(*os.File) error { return errors.New("injected crash during key write") }
	if err := InitDefault(); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(keyPathOf(dir)); err == nil {
		t.Error("a master.key exists after an interrupted install (it could be partial/empty)")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.enc")); err == nil {
		t.Error("vault created without a key")
	}
	if left := leftoverTemps(t, dir); len(left) != 0 {
		t.Errorf("temp key files left behind: %v", left)
	}

	syncFile = func(f *os.File) error { return f.Sync() }
	if err := InitDefault(); err != nil {
		t.Fatalf("re-running init after the interruption: %v", err)
	}
	if _, err := LoadDefault(); err != nil {
		t.Fatalf("vault unusable after recovery: %v", err)
	}
}

func TestInitDefault_CrashResidueTempKeyIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSO_MASTER_KEY", "")
	dir := filepath.Join(home, ".dso")
	_ = os.MkdirAll(dir, 0o700)
	// What a process killed mid-install leaves behind: a partial/empty temp file.
	for _, name := range []string{".master.key.tmp-crashed1", ".master.key.tmp-crashed2"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault with crash residue: %v", err)
	}
	raw, err := os.ReadFile(keyPathOf(dir))
	if err != nil {
		t.Fatal(err)
	}
	if k := strings.TrimSpace(string(raw)); len(k) != 64 {
		t.Errorf("installed key = %d chars, want 64 hex", len(k))
	}
	if fi, _ := os.Stat(keyPathOf(dir)); fi.Mode().Perm() != 0o600 {
		t.Errorf("master.key mode %o", fi.Mode().Perm())
	}
	if _, err := LoadDefault(); err != nil {
		t.Fatal(err)
	}
}

func TestInitDefault_KeyAppearingDuringInstallIsNeverReplaced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSO_MASTER_KEY", "")
	dir := filepath.Join(home, ".dso")
	const winner = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	withHooks(t)
	linkFile = func(oldname, newname string) error {
		// Another initializer installs its key between our Stat and our install.
		if err := os.WriteFile(newname, []byte(winner+"\n"), 0o600); err != nil {
			return err
		}
		return fs.ErrExist
	}
	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault: %v", err)
	}
	raw, _ := os.ReadFile(keyPathOf(dir))
	if strings.TrimSpace(string(raw)) != winner {
		t.Error("an existing key was replaced")
	}
	if left := leftoverTemps(t, dir); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	if _, err := LoadDefault(); err != nil {
		t.Errorf("vault must be created under the winning key: %v", err)
	}
}

func TestInitDefault_FilesystemWithoutHardLinksFallsBackToRename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSO_MASTER_KEY", "")
	withHooks(t)
	linkFile = func(string, string) error { return syscall.ENOTSUP }
	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault without hard links: %v", err)
	}
	if _, err := LoadDefault(); err != nil {
		t.Fatal(err)
	}
	if left := leftoverTemps(t, filepath.Join(home, ".dso")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}
