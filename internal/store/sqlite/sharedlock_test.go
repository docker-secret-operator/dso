package sqlite

// Phase D: tests for AcquireSharedCentralLock, the new shared-lock entry
// point the recovery scanner uses to coordinate with other dso central
// processes without opening a Store or the active database.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedLock_AcquireAndRelease_Succeeds(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	l, err := AcquireSharedCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second Close must be a no-op: %v", err)
	}
}

func TestSharedLock_CreatesSidecarIfAbsent(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	sidecar := db + ".central.lock"
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("sidecar must not exist before acquisition")
	}
	l, err := AcquireSharedCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("acquiring the shared lock must create the sidecar: %v", err)
	}
}

func TestSharedLock_TwoSharedHolders_BothSucceed(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	l1, err := AcquireSharedCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()
	l2, err := AcquireSharedCentralLock(db, time.Second)
	if err != nil {
		t.Fatalf("a second shared holder must be compatible with the first: %v", err)
	}
	defer l2.Close()
}

func TestSharedLock_ConcurrentExclusiveHolder_TimesOutBusy(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	excl, err := AcquireExclusiveCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer excl.Close()

	_, err = AcquireSharedCentralLock(db, 150*time.Millisecond)
	if !errors.Is(err, ErrCentralLockBusy) {
		t.Fatalf("expected ErrCentralLockBusy while an exclusive holder is active, got %v", err)
	}
}

func TestSharedLock_LockFailure_NeverLeavesAPartialLock(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	excl, err := AcquireExclusiveCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = AcquireSharedCentralLock(db, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected the shared acquisition to fail while exclusive is held")
	}
	if err := excl.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := AcquireSharedCentralLock(db, time.Second)
	if err != nil {
		t.Fatalf("expected the shared lock to succeed once the exclusive holder released: %v", err)
	}
	l.Close()
}

func TestSharedLock_SidecarIsSymlink_IsRefused(t *testing.T) {
	dir := pvDir(t)
	db := filepath.Join(dir, "c.db")
	pvWrite(t, db, pvBytes(10))
	target := filepath.Join(dir, "elsewhere")
	pvWrite(t, target, pvBytes(1))
	if err := os.Symlink(target, db+".central.lock"); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	_, err := AcquireSharedCentralLock(db, time.Second)
	if err == nil {
		t.Fatal("expected a symlinked lock sidecar to be refused")
	}
}
