package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/docker-secret-operator/dso/pkg/api"
)

// fakeAgent is a synthetic stand-in for the DSO agent RPC endpoint.
type fakeAgent struct{ resp api.StatusResponse }

func (f *fakeAgent) GetStatus(_ *api.StatusRequest, out *api.StatusResponse) error {
	*out = f.resp
	return nil
}

func startFakeAgent(t *testing.T, resp api.StatusResponse) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "a.sock")
	srv := rpc.NewServer()
	if err := srv.RegisterName("Agent", &fakeAgent{resp: resp}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.ServeConn(c)
		}
	}()
	t.Setenv("DSO_SOCKET_PATH", sock)
}

func writeRuntimeState(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".dso", "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func statusJSON(t *testing.T) (SystemStatus, int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run([]string{"status", "--json"}, &out, &errOut)
	var st SystemStatus
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("status --json not valid JSON: %v\n%s", err, out.String())
	}
	return st, code, errOut.String()
}

func TestStatus_NoFabricatedVersionOrUptime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DSO_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	old := Version
	defer func() { Version = old }()
	Version = ""

	st := (&Status{}).gatherStatus()
	if st.Runtime.Version != "unknown" {
		t.Errorf("missing build metadata must read 'unknown', got %q", st.Runtime.Version)
	}
	if st.Runtime.Uptime != "" || st.Runtime.StartTime != nil {
		t.Errorf("no uptime may be reported without a reachable agent, got %q / %v", st.Runtime.Uptime, st.Runtime.StartTime)
	}
	Version = "v9.9.9-test"
	if got := (&Status{}).gatherStatus().Runtime.Version; got != "v9.9.9-test" {
		t.Errorf("version should come from build metadata, got %q", got)
	}
}

func TestStatus_UnreachableAgentFailsNonZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSO_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	// A stale state file must NOT make the agent look healthy or give uptime.
	writeRuntimeState(t, home, `{"start_time":"`+time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339)+`"}`)

	st, code, errOut := statusJSON(t)
	if code == 0 {
		t.Fatal("unreachable agent must exit non-zero")
	}
	if st.State != StateUnavailable || st.AgentReached {
		t.Errorf("state = %q reached=%v, want unavailable/false", st.State, st.AgentReached)
	}
	if st.Runtime.Uptime != "" {
		t.Errorf("stale state file must not yield uptime, got %q", st.Runtime.Uptime)
	}
	if !strings.Contains(errOut, "unavailable") {
		t.Errorf("diagnostic belongs on stderr, got %q", errOut)
	}
}

func TestStatus_HealthyAgentExitsZeroWithVerifiedUptime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	startFakeAgent(t, api.StatusResponse{
		CacheEntries: 3,
		Providers:    []api.ProviderStatusInfo{{Name: "synthetic", Known: true, Healthy: true}},
	})
	writeRuntimeState(t, home, `{"start_time":"`+time.Now().Add(-90*time.Minute).UTC().Format(time.RFC3339)+`"}`)

	st, code, errOut := statusJSON(t)
	if code != 0 {
		t.Fatalf("healthy agent exit = %d (%s)", code, errOut)
	}
	if st.State != StateHealthy || !st.AgentReached || st.Cache.Entries != 3 {
		t.Errorf("unexpected status %+v", st)
	}
	if st.Runtime.Uptime != "1h 30m" && st.Runtime.Uptime != "1h 29m" {
		t.Errorf("uptime = %q, want ~1h 30m from the recorded start time", st.Runtime.Uptime)
	}
}

func TestStatus_HealthyAgentWithoutStateFileHasNoUptime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startFakeAgent(t, api.StatusResponse{})
	st, code, _ := statusJSON(t)
	if code != 0 || st.State != StateHealthy {
		t.Fatalf("code=%d state=%q", code, st.State)
	}
	if st.Runtime.Uptime != "" {
		t.Errorf("absent state file: uptime must be omitted, got %q", st.Runtime.Uptime)
	}
}

func TestStatus_MalformedOrFutureStateIgnored(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":     `{not json`,
		"bad time":    `{"start_time":"yesterday"}`,
		"future time": `{"start_time":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `"}`,
		"empty":       ``,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			startFakeAgent(t, api.StatusResponse{})
			writeRuntimeState(t, home, body)
			st, code, _ := statusJSON(t)
			if code != 0 || st.Runtime.Uptime != "" || st.Runtime.StartTime != nil {
				t.Errorf("code=%d uptime=%q start=%v", code, st.Runtime.Uptime, st.Runtime.StartTime)
			}
		})
	}
}

func TestStatus_UnhealthyProviderIsDegradedAndNonZero(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startFakeAgent(t, api.StatusResponse{
		Providers: []api.ProviderStatusInfo{
			{Name: "ok", Known: true, Healthy: true},
			{Name: "bad", Known: true, Healthy: false, Message: "synthetic failure"},
		},
	})
	st, code, _ := statusJSON(t)
	if code == 0 || st.State != StateDegraded {
		t.Fatalf("code=%d state=%q, want non-zero/degraded", code, st.State)
	}
}

func TestStatus_DegradedServicesAreDegraded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startFakeAgent(t, api.StatusResponse{DegradedServices: []api.DegradedServiceInfo{{Service: "web", Reason: "x"}}})
	st, code, _ := statusJSON(t)
	if code == 0 || st.State != StateDegraded || st.DegradedSvcs != 1 {
		t.Fatalf("code=%d state=%q degraded=%d", code, st.State, st.DegradedSvcs)
	}
}

func TestStatus_TextBoxIsAligned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startFakeAgent(t, api.StatusResponse{
		Providers: []api.ProviderStatusInfo{{Name: "synthetic", Known: true, Healthy: false, Message: strings.Repeat("long message ", 10)}},
	})
	var out bytes.Buffer
	Run([]string{"status"}, &out, &bytes.Buffer{})
	var width int
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		w := utf8.RuneCountInString(l)
		if width == 0 {
			width = w
		}
		if w != width {
			t.Errorf("misaligned line (%d != %d): %q", w, width, l)
		}
	}
	if strings.Contains(out.String(), "v1.0.0") || strings.Contains(out.String(), "2h 0m") {
		t.Error("fabricated version/uptime still present")
	}
}

func TestStatus_TextHealthyAndUnavailableLabels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DSO_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	var out bytes.Buffer
	if code := Run([]string{"status"}, &out, &bytes.Buffer{}); code == 0 {
		t.Error("unreachable agent must exit non-zero in text mode too")
	}
	if !strings.Contains(out.String(), "Uptime:   unavailable") || !strings.Contains(out.String(), "NOT REACHABLE") {
		t.Errorf("unexpected text output:\n%s", out.String())
	}
}
