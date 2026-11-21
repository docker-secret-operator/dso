package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/api"
)

func TestStatus_LedgerDisabledIsVisibleWithCauseAndRemediation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startFakeAgent(t, api.StatusResponse{Ledger: api.LedgerStatusInfo{
		State: api.LedgerDisabled, Code: "unsafe_permissions",
		Message:     "/var/lib/dso: directory is writable by group or others (actual uid 0 gid 0, mode 0770, expected root:dso, mode 0750)",
		Remediation: "re-apply the DSO directory permissions with `sudo docker dso bootstrap agent`",
		Degraded:    "durable version tracking is unavailable: no mass rotation",
	}})
	st, code, _ := statusJSON(t)
	if code != 0 {
		t.Fatalf("a disabled ledger must not change the exit code, got %d", code)
	}
	if st.Ledger.State != "disabled" || st.Ledger.Code != "unsafe_permissions" || st.Ledger.Remediation == "" || st.Ledger.Degraded == "" {
		t.Fatalf("ledger status %+v", st.Ledger)
	}

	var out, errOut bytes.Buffer
	if c := Run([]string{"status"}, &out, &errOut); c != 0 {
		t.Fatalf("exit %d: %s", c, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"LEDGER", "disabled (unsafe_permissions)", "Cause:", "Fix:", "bootstrap agent"} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal status lacks %q:\n%s", want, text)
		}
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "…") {
			t.Errorf("a ledger line was truncated: %q", l)
		}
	}
}

func TestStatus_LedgerStates(t *testing.T) {
	for name, tc := range map[string]struct {
		in   api.LedgerStatusInfo
		want string
	}{
		"active":         {api.LedgerStatusInfo{State: api.LedgerActive, Code: "active"}, "active"},
		"not configured": {api.LedgerStatusInfo{State: api.LedgerNotConfigured, Code: "not_configured"}, "not_configured"},
		"older agent":    {api.LedgerStatusInfo{}, "unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			startFakeAgent(t, api.StatusResponse{Ledger: tc.in})
			st, code, _ := statusJSON(t)
			if code != 0 || st.Ledger.State != tc.want {
				t.Fatalf("code=%d ledger=%+v, want state %q", code, st.Ledger, tc.want)
			}
		})
	}
}
