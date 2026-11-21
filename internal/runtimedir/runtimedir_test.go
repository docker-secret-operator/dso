package runtimedir

import (
	"errors"
	"testing"
)

func TestCanonicalConstants(t *testing.T) {
	if CanonicalDir != "/run/dso" {
		t.Fatalf("CanonicalDir = %q", CanonicalDir)
	}
	if DirMode != 0o750 {
		t.Fatalf("DirMode = %#o, the invariant is exactly 0750", DirMode)
	}
	if DirMode&0o022 != 0 {
		t.Fatalf("DirMode %#o grants group or other write", DirMode)
	}
	if FileMode&0o077 != 0 {
		t.Fatalf("FileMode %#o grants group or other access", FileMode)
	}
}

func TestCanonicalPolicyFor_RootEstablishesTheInvariant(t *testing.T) {
	p := CanonicalPolicyFor(0)
	if !p.Enforce || !p.Create {
		t.Fatalf("a root agent must create and enforce: %+v", p)
	}
	if p.OwnerUID != 0 {
		t.Fatalf("owner must be root, got uid %d", p.OwnerUID)
	}
	if p.GroupName != "dso" || p.Mode != 0o750 {
		t.Fatalf("want group dso mode 0750, got %q %#o", p.GroupName, p.Mode)
	}
	if !p.RequireNoGroupOtherWrite {
		t.Fatal("group/other write must be forbidden")
	}
}

func TestCanonicalPolicyFor_NonRootNeverModifiesTheCanonicalDirectory(t *testing.T) {
	p := CanonicalPolicyFor(1234)
	if p.Enforce {
		t.Fatal("a non-root process must not enforce (chown/chmod) the canonical directory")
	}
	if p.Create {
		t.Fatal("a non-root process must not create the canonical directory")
	}
	if p.OwnerUID != 1234 {
		t.Fatalf("a non-root process accepts only a directory it owns, got uid %d", p.OwnerUID)
	}
	if !p.RequireNoGroupOtherWrite {
		t.Fatal("group/other write must still be forbidden")
	}
	if fp := CanonicalFilePolicyFor(1234); fp.OwnerUID != 1234 || fp.MaxMode != 0o600 {
		t.Fatalf("file policy = %+v", fp)
	}
}

func TestUnsafeError_ChainAndClassification(t *testing.T) {
	inner := errors.New("errno")
	var err error = &UnsafeError{Path: "/p", Reason: "is a symbolic link", Err: inner}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatal("errors.Is(err, ErrUnsafe) must hold for an *UnsafeError")
	}
	if !errors.Is(err, inner) {
		t.Fatal("the underlying error must stay in the chain")
	}
	var ue *UnsafeError
	if !errors.As(err, &ue) || ue.Path != "/p" {
		t.Fatal("errors.As must recover the *UnsafeError")
	}
	wrapped := errors.Join(errors.New("ctx"), err)
	if !errors.Is(wrapped, ErrUnsafe) {
		t.Fatal("classification must survive wrapping")
	}
}

func TestResolveGID(t *testing.T) {
	cases := []struct {
		name string
		p    Policy
		want int
	}{
		{"no group name means root", Policy{}, 0},
		{"injected lookup finds it", Policy{GroupName: "dso", LookupGroup: func(string) (int, bool) { return 4242, true }}, 4242},
		{"injected lookup misses -> root", Policy{GroupName: "dso", LookupGroup: func(string) (int, bool) { return 0, false }}, 0},
	}
	for _, c := range cases {
		if got := c.p.resolveGID(); got != c.want {
			t.Errorf("%s: gid %d, want %d", c.name, got, c.want)
		}
	}
	// the real lookup of a group that cannot exist falls back to root
	if got := (Policy{GroupName: "dso-no-such-group-for-tests"}).resolveGID(); got != 0 {
		t.Errorf("unknown group resolved to gid %d, want 0", got)
	}
}

func TestEnforceRequiresAPlainMode(t *testing.T) {
	// validated before anything is opened, on every platform that can run it
	_, err := Ensure("/nonexistent/for/tests", Policy{Enforce: true})
	if err == nil {
		t.Fatal("Enforce without a Mode must be rejected")
	}
}
