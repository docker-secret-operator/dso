package proxy

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestParseHostPorts_AddressForms(t *testing.T) {
	got := ParseHostPorts("3306:3306,127.0.0.1:5432:5432,[::1]:8443:443,0.0.0.0:8080:80")
	want := []PortMap{
		{"", 3306, 3306}, {"127.0.0.1", 5432, 5432}, {"::1", 8443, 443}, {"0.0.0.0", 8080, 80},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestParseHostPorts_RejectsMalformedAndAmbiguousEntries(t *testing.T) {
	for _, bad := range []string{
		"localhost:8080:80",    // hostname
		"[::1:8080:80",         // unterminated bracket
		"[127.0.0.1]:8080:80",  // v4 in brackets
		"::1:8080:80",          // unbracketed v6
		"fe80::1%eth0:8080:80", // zone
		"[fe80::1%eth0]:8080:80",
		"1.2.3.4:80:80",      // privileged host port
		"1.2.3.4:8080:0",     // bad container port
		"1:2:3:4",            // too many parts
		"127.0.0.1:99999:80", // out of range
		"::ffff:1.2.3.4:8080:80",
	} {
		if got := ParseHostPorts(bad); len(got) != 0 {
			t.Errorf("%q must be rejected, got %+v", bad, got)
		}
	}
	// One host port with two addresses is ambiguous: only the first entry survives,
	// the second is never widened or merged.
	got := ParseHostPorts("127.0.0.1:8080:80,[::1]:8080:80,0.0.0.0:8080:80")
	if len(got) != 1 || got[0].HostIP != "127.0.0.1" {
		t.Fatalf("ambiguous duplicates must not widen: %+v", got)
	}
}

// nonLoopbackIPv4 returns an address of this host that is not 127.0.0.0/8, or "".
func nonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			return ipn.IP.String()
		}
	}
	return ""
}

func dialable(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// Real sockets: a loopback-only mapping must be reachable on loopback and NOT on
// any other local address (the S100 exposure regression).
func TestServerBind_LoopbackIsNotReachableOnOtherInterfaces(t *testing.T) {
	other := nonLoopbackIPv4(t)
	if other == "" {
		t.Skip("no non-loopback IPv4 address on this host to use as the second vantage point")
	}
	srv := NewServer(NewRouter(NewRegistry()), zap.NewNop())
	defer srv.Close()
	port := freePort(t)
	if err := srv.Bind(PortBinding{ListenIP: "127.0.0.1", ListenPort: port}); err != nil {
		t.Fatal(err)
	}
	if !dialable(fmt.Sprintf("127.0.0.1:%d", port)) {
		t.Fatal("loopback must be reachable")
	}
	if dialable(fmt.Sprintf("%s:%d", other, port)) {
		t.Fatalf("loopback-only binding is reachable on %s: exposure widened", other)
	}
	bs := srv.Bindings()
	if len(bs) != 1 || bs[0].ListenIP != "127.0.0.1" {
		t.Fatalf("binding lost its address: %+v", bs)
	}
}

func TestServerBind_WildcardAndExplicitInterface(t *testing.T) {
	other := nonLoopbackIPv4(t)
	if other == "" {
		t.Skip("no non-loopback IPv4 address")
	}
	srv := NewServer(NewRouter(NewRegistry()), zap.NewNop())
	defer srv.Close()
	pw, pi := freePort(t), freePort(t)
	if err := srv.Bind(PortBinding{ListenPort: pw}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Bind(PortBinding{ListenIP: other, ListenPort: pi}); err != nil {
		t.Fatal(err)
	}
	if !dialable(fmt.Sprintf("%s:%d", other, pw)) || !dialable(fmt.Sprintf("127.0.0.1:%d", pw)) {
		t.Fatal("wildcard must be reachable on every interface")
	}
	if !dialable(fmt.Sprintf("%s:%d", other, pi)) {
		t.Fatal("explicit-interface binding must be reachable on that interface")
	}
	if dialable(fmt.Sprintf("127.0.0.1:%d", pi)) {
		t.Fatal("explicit-interface binding must NOT be reachable on loopback")
	}
}

func TestServerBind_IPv6LoopbackOnlyWhenAvailable(t *testing.T) {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable")
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	srv := NewServer(NewRouter(NewRegistry()), zap.NewNop())
	defer srv.Close()
	if err := srv.Bind(PortBinding{ListenIP: "::1", ListenPort: port}); err != nil {
		t.Fatal(err)
	}
	if !dialable(fmt.Sprintf("[::1]:%d", port)) {
		t.Fatal("::1 must be reachable")
	}
	if dialable(fmt.Sprintf("127.0.0.1:%d", port)) {
		t.Fatal("an IPv6-loopback binding must not answer on IPv4 loopback")
	}
}

func TestServerBind_RefusesToReuseAPortWithADifferentAddress(t *testing.T) {
	srv := NewServer(NewRouter(NewRegistry()), zap.NewNop())
	defer srv.Close()
	port := freePort(t)
	if err := srv.Bind(PortBinding{ListenIP: "127.0.0.1", ListenPort: port}); err != nil {
		t.Fatal(err)
	}
	// Same address again: idempotent.
	if err := srv.Bind(PortBinding{ListenIP: "127.0.0.1", ListenPort: port}); err != nil {
		t.Fatal(err)
	}
	// Wildcard request for a port already owned on loopback must NOT silently succeed.
	err := srv.Bind(PortBinding{ListenPort: port})
	if err == nil || !strings.Contains(err.Error(), "refusing to reuse") {
		t.Fatalf("want a refusal, got %v", err)
	}
}

func TestServerBind_RejectsInvalidAddresses(t *testing.T) {
	srv := NewServer(NewRouter(NewRegistry()), zap.NewNop())
	defer srv.Close()
	for _, ip := range []string{"localhost", "fe80::1%eth0", "300.1.1.1", "[::1]", " 127.0.0.1"} {
		if err := srv.Bind(PortBinding{ListenIP: ip, ListenPort: freePort(t)}); err == nil {
			t.Errorf("bind address %q must be rejected", ip)
		}
	}
}
