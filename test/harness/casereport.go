package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Incremental per-case evidence. The matrix report (Report.Markdown/JSON) is only written
// when the whole matrix finishes, so a go test timeout used to erase everything the run had
// established. WriteCaseReport persists one case's report the moment it is final, atomically
// (temp file + rename), so whatever completed before a timeout or crash survives.

// CaseFileName is the stable file name of a case's report: path separators and the "#" of
// the Nth-hit suffix are replaced so the ID is a single safe path element.
func CaseFileName(id string) string {
	return strings.NewReplacer("/", "_", "#", "_", " ", "_").Replace(id) + ".json"
}

// CaseStatus summarises a report for the index: PASS, FAIL or NOT-EXECUTED (an unreached
// point is never counted as a pass).
func CaseStatus(c CaseReport) string {
	switch {
	case c.NotExecuted != "":
		return "NOT-EXECUTED"
	case c.Passed():
		return "PASS"
	default:
		return "FAIL"
	}
}

// WriteCaseReport writes <dir>/<CaseFileName(c.ID)> and appends one line to <dir>/index.tsv.
// dir is created if needed. Errors are returned for the caller to log; evidence collection
// must never change a test's outcome.
func WriteCaseReport(dir string, c CaseReport) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(dir, CaseFileName(c.ID))
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "index.tsv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- harness-owned evidence directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "%s\t%s\tcrash_journal=%s\tfinal=%s/%s\tviolations=%d\tduration=%s\n",
		c.ID, CaseStatus(c), c.CrashJournal, c.FinalStatus, c.FinalOutcome, len(c.Violations), c.Duration.Round(1e9))
	return err
}

// LayoutOf renders the containers of one snapshot that belong to the service (the real name
// holder and its rotation artifacts: _dso_new_, _dso_backup_, _old_<ts>) as stable, sorted
// "name id12 running|stopped" lines, so a case report shows WHICH container held which name
// at each stage. Unrelated and foreign containers are omitted.
func LayoutOf(s Snapshot, service string) []string {
	var out []string
	for _, c := range s.Containers {
		base, isArtifact := ArtifactBase(c.Name)
		if c.Name != service && (!isArtifact || base != service) {
			continue
		}
		state := "stopped"
		if c.Running {
			state = "running"
		}
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		out = append(out, fmt.Sprintf("%s %s %s", c.Name, id, state))
	}
	sort.Strings(out)
	return out
}

// CaseLogPrefix marks a one-line JSON case report in a test's output so scripts can pull
// the reports out of the `go test -json` stream (scripts/integration/summarize-results.sh).
const CaseLogPrefix = "CASE-REPORT "

// CaseLogLine renders c as CaseLogPrefix followed by compact JSON on ONE line. Logged by every
// matrix case, it puts the full per-case evidence (crash and final journal, container layouts,
// second-pass verdict, violations) into the CI job log, which is kept whatever the outcome; the
// artifact upload only runs on failure, so a passing run would otherwise keep none of it.
func CaseLogLine(c CaseReport) string {
	b, err := json.Marshal(c)
	if err != nil {
		return CaseLogPrefix + fmt.Sprintf(`{"ID":%q,"error":%q}`, c.ID, err.Error())
	}
	return CaseLogPrefix + string(b)
}
