package cli

import (
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
