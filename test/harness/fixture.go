package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

// Root is the per-test directory layout of one agent deployment. Everything the
// agent writes goes under Dir, so a leak scan of Dir covers all persisted agent
// state, and nothing outside it (and nothing under /var/lib/dso or /run/dso) is
// touched.
type Root struct {
	Dir    string // owns everything below; scanned for secrets
	Cfg    string // the agent's working directory; holds dso.yaml
	State  string // journal (rotations.json)
	Lock   string
	Ledger string // applied-version ledger (key + records); the agent's default is /var/lib/dso/ledger
	Logs   string
	Home   string
	Sock   string // IPC and driver sockets (kept short: unix socket paths are limited to ~104 bytes)
}

// maxSockPath leaves headroom under the 104/108-byte sockaddr_un limit.
const maxSockPath = 90

// NewRoot creates the layout under a fresh directory inside base.
func NewRoot(base string) (Root, error) {
	dir, err := os.MkdirTemp(base, "dsoct")
	if err != nil {
		return Root{}, err
	}
	r := Root{
		Dir: dir, Cfg: filepath.Join(dir, "cfg"), State: filepath.Join(dir, "state"), Lock: filepath.Join(dir, "lock"), Ledger: filepath.Join(dir, "ledger"),
		Logs: filepath.Join(dir, "logs"), Home: filepath.Join(dir, "home"), Sock: filepath.Join(dir, "s"),
	}
	for _, d := range []string{r.Cfg, r.State, r.Lock, r.Ledger, r.Logs, r.Home, r.Sock} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Root{}, err
		}
	}
	if n := len(filepath.Join(r.Sock, "driver.sock")); n > maxSockPath {
		return Root{}, fmt.Errorf("socket path is %d bytes (limit %d): choose a shorter base directory (set TMPDIR)", n, maxSockPath)
	}
	return r, nil
}

// WriteConfig writes dso.yaml (0600) into the agent's working directory.
func (r Root) WriteConfig(c AgentConfig) error {
	return os.WriteFile(filepath.Join(r.Cfg, "dso.yaml"), []byte(c.YAML()), 0o600)
}

// AgentArgs are the command-line arguments of `dso agent` for this layout.
// apiAddr and metricsAddr must be loopback host:port.
func (r Root) AgentArgs(apiAddr, metricsAddr string) []string {
	return []string{
		"--config", "dso.yaml", "agent",
		"--socket", filepath.Join(r.Sock, "dso.sock"),
		"--driver-socket", filepath.Join(r.Sock, "driver.sock"),
		"--api-addr", apiAddr, "--metrics-addr", metricsAddr,
	}
}

// AgentLog is the log file of the n-th agent run (1-based).
func (r Root) AgentLog(n int) string { return filepath.Join(r.Logs, fmt.Sprintf("agent-%d.log", n)) }

// MarkerPath is the fault marker path of the n-th agent run.
func (r Root) MarkerPath(n int) string {
	return filepath.Join(r.Logs, fmt.Sprintf("fault-%d.marker", n))
}
