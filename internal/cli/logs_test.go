package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestColorLevel(t *testing.T) {
	if colorLevel("INFO") == "" {
		t.Fatal("expected color")
	}
	if colorLevel("WARN") == "" {
		t.Fatal("expected color")
	}
	if colorLevel("ERROR") == "" {
		t.Fatal("expected color")
	}
}

func TestPrintHeader(t *testing.T) {
	printHeader()
}

func TestColorizeLine(t *testing.T) {
	line := colorizeLine("test [INFO] test")
	if len(line) == 0 {
		t.Fatal("expected colorized line")
	}

	line = colorizeLine("test level=error test")
	if len(line) == 0 {
		t.Fatal("expected colorized line")
	}
}

// TestFetchEvents_MalformedResponse is a regression test for QUAL-1:
// fetchEvents previously discarded the JSON unmarshal error, so a malformed
// agent API response looked identical to "no events" to the operator. This
// verifies fetchEvents still returns nil (callers' behavior is unchanged)
// but now also surfaces a diagnostic to stderr rather than staying silent.
func TestFetchEvents_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w

	events := fetchEvents(srv.URL)

	_ = w.Close()
	os.Stderr = origStderr
	out, _ := io.ReadAll(r)

	if events != nil {
		t.Errorf("expected nil events for malformed response, got: %v", events)
	}
	if !strings.Contains(string(out), "Malformed response from agent API") {
		t.Errorf("expected a diagnostic on stderr about the malformed response, got: %s", out)
	}
}

func TestPrintEvent(t *testing.T) {
	ev := map[string]interface{}{
		"Timestamp": time.Now().Format(time.RFC3339),
		"Level":     "INFO",
		"Message":   "msg",
		"Container": "container",
		"Secret":    "secret",
		"Status":    "success",
		"Error":     "",
	}
	printEvent(ev)
}
