//go:build unix

package sqlite

// B10: can a "ghost" connection (one that still has the OLD, displaced database
// open) delete the REPLACEMENT database's -wal when it closes?
//
// SQLite removes a database's -wal and -shm BY PATH when the last connection to
// the database closes. A connection that holds an inode which was renamed aside
// believes it is the last connection to that database, so on close it may unlink
// whatever now sits at "<path>-wal". This test makes that situation and RECORDS
// what happens. It does not assert the hazard either way: the observation is the
// result, and the Restore design documents the residual risk accordingly. The
// "displacement" here is plain os.Rename in the test, not production code.

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

// TestHelperProcessGhostConnection is not a real test: it runs only when
// DSO_TEST_GHOST_DB is set. It opens the database in WAL mode, commits a row,
// prints READY, waits for a line on stdin, closes the connection and prints
// CLOSED.
func TestHelperProcessGhostConnection(t *testing.T) {
	path := os.Getenv("DSO_TEST_GHOST_DB")
	if path == "" {
		t.Skip("helper process")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	db.SetMaxOpenConns(1)
	for _, s := range []string{`CREATE TABLE g(a)`, `INSERT INTO g VALUES(1)`} {
		if _, err := db.Exec(s); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	fmt.Println("READY")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	err = db.Close()
	fmt.Printf("CLOSED err=%v\n", err)
	os.Exit(0)
}

func TestCharacterize_GhostConnection_WALCloseByPath(t *testing.T) {
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "c.db")
	writeFile0600(t, path, nil)

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessGhostConnection$") // #nosec G204 -- this test binary
	cmd.Env = append(os.Environ(), "DSO_TEST_GHOST_DB="+path)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	killer := time.AfterFunc(60*time.Second, func() { _ = cmd.Process.Kill() })
	defer killer.Stop()
	defer func() { _ = cmd.Process.Kill() }()

	sc := bufio.NewScanner(stdout)
	waitFor := func(prefix string) string {
		t.Helper()
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), prefix) {
				return sc.Text()
			}
		}
		t.Fatalf("the ghost helper never printed %q", prefix)
		return ""
	}
	waitFor("READY")

	// Displace the old database, its -wal and its -shm into a sibling directory.
	aside := filepath.Join(dir, "aside")
	if err := os.Mkdir(aside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, sfx := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(path+sfx, filepath.Join(aside, "c.db"+sfx)); err != nil {
			t.Fatalf("displacing %s: %v", sfx, err)
		}
	}

	// A fresh database at the same path, with its own non-empty -wal, held open.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)"
	fresh, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	fresh.SetMaxOpenConns(1)
	defer fresh.Close()
	if _, err := fresh.Exec(`CREATE TABLE n(a)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := fresh.Exec(`INSERT INTO n VALUES(?)`, i); err != nil {
			t.Fatal(err)
		}
	}
	walBefore, err := os.Stat(path + "-wal")
	if err != nil || walBefore.Size() == 0 {
		t.Fatalf("precondition: the replacement must have a non-empty -wal (%v)", err)
	}
	hashBefore := fileHash(t, path+"-wal")

	// The ghost closes.
	if _, err := fmt.Fprintln(stdin, "CLOSE"); err != nil {
		t.Fatal(err)
	}
	closed := waitFor("CLOSED")
	_ = cmd.Wait()

	walAfter, statErr := os.Stat(path + "-wal")
	shmAfter, shmErr := os.Stat(path + "-shm")
	walExists := statErr == nil
	sameInode := walExists && os.SameFile(walBefore, walAfter)
	identical := walExists && fileHash(t, path+"-wal") == hashBefore

	var viaOpen int
	openErr := fresh.QueryRow(`SELECT count(*) FROM n`).Scan(&viaOpen)

	second, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	second.SetMaxOpenConns(1)
	defer second.Close()
	var viaSecond int
	secondErr := second.QueryRow(`SELECT count(*) FROM n`).Scan(&viaSecond)

	banner := "RESULT: the ghost close left the replacement's -wal intact"
	if !walExists || !identical {
		banner = "RESULT (HAZARD OBSERVED): the ghost close removed or altered the replacement's -wal"
	}
	t.Logf("%s | ghost: %s | replacement -wal exists=%v sameInode=%v identical=%v (size before %d) | -shm exists=%v | rows via the still-open connection=%d (err %v), via a NEW connection=%d (err %v)",
		banner, closed, walExists, sameInode, identical, walBefore.Size(), shmErr == nil && shmAfter != nil, viaOpen, openErr, viaSecond, secondErr)
}
