package core

import (
	"strings"
	"testing"
)

func svcWithPorts(ports ...interface{}) map[string]interface{} {
	return map[string]interface{}{"image": "x", "ports": ports}
}

func labelOf(t *testing.T, svc map[string]interface{}) string {
	t.Helper()
	l, _ := svc["labels"].(map[string]interface{})
	v, _ := l["dso.host_ports"].(string)
	return v
}

// M0-02: exposure must never widen; unsupported mappings leave `ports:` untouched.
func TestStripAndLabelHostPorts_PreservesBindAddress(t *testing.T) {
	cases := []struct {
		name  string
		ports []interface{}
		want  string
	}{
		{"ipv4 loopback", []interface{}{"127.0.0.1:5432:5432"}, "127.0.0.1:5432:5432"},
		{"wildcard stays address-less", []interface{}{"8080:80"}, "8080:80"},
		{"explicit 0.0.0.0 stays ipv4-any", []interface{}{"0.0.0.0:8080:80"}, "0.0.0.0:8080:80"},
		{"ipv6 loopback", []interface{}{"[::1]:8443:443"}, "[::1]:8443:443"},
		{"explicit interface", []interface{}{"192.168.1.5:9000:9000/tcp"}, "192.168.1.5:9000:9000"},
		{"multiple mappings keep their own addresses", []interface{}{"127.0.0.1:5432:5432", "0.0.0.0:8080:80", "[::1]:8443:443"}, "127.0.0.1:5432:5432,0.0.0.0:8080:80,[::1]:8443:443"},
		{"long syntax host_ip", []interface{}{map[string]interface{}{"target": 5432, "published": "5432", "host_ip": "127.0.0.1", "protocol": "tcp"}}, "127.0.0.1:5432:5432"},
		{"long syntax ipv6 host_ip", []interface{}{map[string]interface{}{"target": 443, "published": 8443, "host_ip": "::1"}}, "[::1]:8443:443"},
		{"long syntax without host_ip is address-less", []interface{}{map[string]interface{}{"target": 80, "published": 8080, "mode": "host"}}, "8080:80"},
		{"ipv6 is canonicalised", []interface{}{"[0:0:0:0:0:0:0:1]:8443:443"}, "[::1]:8443:443"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, notes := stripAndLabelHostPorts(svcWithPorts(c.ports...))
			if len(notes) != 0 {
				t.Fatalf("unexpected notes: %v", notes)
			}
			if _, still := svc["ports"]; still {
				t.Fatal("proxied service must not keep direct ports")
			}
			if got := labelOf(t, svc); got != c.want {
				t.Fatalf("label %q, want %q", got, c.want)
			}
		})
	}
}

func TestStripAndLabelHostPorts_UnsupportedMappingsLeavePortsUntouched(t *testing.T) {
	cases := []struct {
		name  string
		ports []interface{}
		why   string
	}{
		{"udp is never turned into tcp", []interface{}{"127.0.0.1:5353:53/udp"}, "udp"},
		{"udp mixed with tcp", []interface{}{"127.0.0.1:5432:5432", "5353:53/udp"}, "udp"},
		{"sctp", []interface{}{"9000:9000/sctp"}, "sctp"},
		{"port range", []interface{}{"8000-8010:8000-8010"}, "ranges"},
		{"auto-assigned host port with address", []interface{}{"127.0.0.1::80"}, "auto-assigned"},
		{"privileged host port", []interface{}{"127.0.0.1:80:80"}, "below"},
		{"privileged mixed with proxiable", []interface{}{"8080:80", "443:443"}, "below"},
		{"hostname instead of ip", []interface{}{"localhost:8080:80"}, "literal"},
		{"too many colons", []interface{}{"1:2:3:4"}, "unrecognised"},
		{"long syntax udp", []interface{}{map[string]interface{}{"target": 53, "published": 5353, "protocol": "udp"}}, "udp"},
		{"long syntax ingress mode", []interface{}{map[string]interface{}{"target": 80, "published": 8080, "mode": "ingress"}}, "mode"},
		{"long syntax unknown option", []interface{}{map[string]interface{}{"target": 80, "published": 8080, "weird": 1}}, "unsupported"},
		{"same port two addresses", []interface{}{"127.0.0.1:8080:80", "[::1]:8080:80"}, "two different addresses"},
		{"bad ipv6", []interface{}{"[::1:8443:443"}, "IPv6"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			orig := svcWithPorts(c.ports...)
			svc, notes := stripAndLabelHostPorts(orig)
			if len(notes) == 0 || !strings.Contains(strings.Join(notes, " "), c.why) {
				t.Fatalf("expected a note mentioning %q, got %v", c.why, notes)
			}
			if _, ok := svc["ports"]; !ok {
				t.Fatal("ports must be left untouched (never silently dropped)")
			}
			if got := len(svc["ports"].([]interface{})); got != len(c.ports) {
				t.Fatalf("ports changed: %d entries, want %d", got, len(c.ports))
			}
			if _, ok := svc["labels"]; ok {
				t.Fatal("no dso.host_ports label may be added for an unproxied service")
			}
			if _, ok := svc["expose"]; ok {
				t.Fatal("expose must not be added for an unproxied service")
			}
		})
	}
}

func TestStripAndLabelHostPorts_ContainerOnlyPortsAndNoPorts(t *testing.T) {
	svc, notes := stripAndLabelHostPorts(map[string]interface{}{"image": "x"})
	if len(notes) != 0 || svc["image"] != "x" {
		t.Fatal("service without ports must be untouched")
	}
	svc, notes = stripAndLabelHostPorts(svcWithPorts("3306", 8080))
	if len(notes) != 0 {
		t.Fatalf("container-only ports are not a problem: %v", notes)
	}
	if _, ok := svc["ports"]; !ok {
		t.Fatal("service with no host publication must keep its ports entry")
	}
}

func TestParseShortPort(t *testing.T) {
	cases := []struct {
		in        string
		publishes bool
		ip, host  string
		cont      string
		proto     string
		proxiable bool
	}{
		{"3306", false, "", "", "3306", "tcp", false},
		{"3306/tcp", false, "", "", "3306", "tcp", false},
		{"8080:80", true, "", "8080", "80", "tcp", true},
		{"8080:80/udp", true, "", "8080", "80", "udp", false},
		{"0.0.0.0:8080:80", true, "0.0.0.0", "8080", "80", "tcp", true},
		{"127.0.0.1:5432:5432/tcp", true, "127.0.0.1", "5432", "5432", "tcp", true},
		{"[::1]:8443:443", true, "::1", "8443", "443", "tcp", true},
		{"127.0.0.1:5353:53/udp", true, "127.0.0.1", "5353", "53", "udp", false},
	}
	for _, c := range cases {
		pp, publishes := parseShortPort(c.in)
		if publishes != c.publishes || pp.hostIP != c.ip || pp.host != c.host || pp.container != c.cont || pp.proto != c.proto || pp.proxiable != c.proxiable {
			t.Errorf("%q => %+v publishes=%v", c.in, pp, publishes)
		}
	}
}
