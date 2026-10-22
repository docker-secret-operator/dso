//go:build unix

package config

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
	"testing"
)

// The group of the config file survives the atomic replace. Needs a
// supplementary group the test user belongs to (any machine with one); skipped
// otherwise because the assertion cannot be made, not because it can fail.
func TestEnsureOwnerID_PreservesGroup(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	gids, _ := u.GroupIds()
	alt := -1
	for _, g := range gids {
		if g != u.Gid {
			alt, _ = strconv.Atoi(g)
			break
		}
	}
	if alt < 0 {
		t.Skip("the test user has no supplementary group to chgrp the config to")
	}
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)
	if err := os.Chown("dso.yaml", -1, alt); err != nil {
		t.Skipf("cannot chgrp the fixture: %v", err)
	}
	gidOf := func() int { st, _ := os.Stat("dso.yaml"); return int(st.Sys().(*syscall.Stat_t).Gid) }
	if before := gidOf(); before != alt {
		t.Fatalf("fixture group %d, want %d", before, alt)
	}
	if err := EnsureOwnerID(&Config{}, "dso.yaml"); err != nil {
		t.Fatal(err)
	}
	if after := gidOf(); after != alt {
		t.Errorf("group changed from %d to %d", alt, after)
	}
	if st, _ := os.Stat("dso.yaml"); st.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o", st.Mode().Perm())
	}
}
