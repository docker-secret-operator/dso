package harness

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

func ok(ms int, backend string) Sample {
	return Sample{Start: at(ms), End: at(ms + 1), OK: true, Status: 200, Backend: backend}
}
func fail(ms int, kind string) Sample { return Sample{Start: at(ms), End: at(ms + 1), FailKind: kind} }

func TestSummarizeCountsAndLongestOutage(t *testing.T) {
	s := []Sample{ok(0, "a"), ok(10, "a"), fail(20, "connection_refused"), fail(30, "connection_refused"), fail(40, "dial_timeout"), ok(55, "b"), fail(70, "http_5xx"), ok(80, "b")}
	w := Summarize(s, at(0), at(100))
	if w.Total != 8 || w.OK != 4 || w.Failed() != 4 {
		t.Fatalf("%+v", w)
	}
	if w.LongestOutage != 35*time.Millisecond { // 20 -> 55
		t.Fatalf("longest outage %v", w.LongestOutage)
	}
	if w.FailKinds["connection_refused"] != 2 || w.FailKinds["dial_timeout"] != 1 || w.FailKinds["http_5xx"] != 1 {
		t.Fatalf("%v", w.FailKinds)
	}
	if w.Backends["a"] != 2 || w.Backends["b"] != 2 {
		t.Fatalf("%v", w.Backends)
	}
	if !w.FirstFailure.Equal(at(20)) || !w.LastFailure.Equal(at(70)) {
		t.Fatalf("%v %v", w.FirstFailure, w.LastFailure)
	}
	// samples are classified by the instant they STARTED
	if w2 := Summarize(s, at(50), at(100)); w2.Total != 3 {
		t.Fatalf("window boundaries: %d", w2.Total)
	}
}

func TestAnalyzeSeparatesDeathFromRecovery(t *testing.T) {
	var s []Sample
	for i := 0; i < 20; i++ { // healthy before death
		s = append(s, ok(i*10, "a"))
	}
	for i := 0; i < 50; i++ { // dead from 200ms; agent restarted at 700ms
		s = append(s, fail(200+i*10, "connection_refused"))
	}
	for i := 0; i < 10; i++ { // restarted but not serving yet
		s = append(s, fail(700+i*10, "connection_refused"))
	}
	for i := 0; i < 60; i++ { // serving from 800ms
		s = append(s, ok(800+i*10, "a"))
	}
	m := Marks{AgentDeath: at(200), RestartBegin: at(700), RotationStart: at(0), TrafficEnd: at(1500)}
	a := Analyze(s, m, at(200))
	if !a.ServedAfterRestart {
		t.Fatal("service never resumed")
	}
	if a.DeathToServing != 600*time.Millisecond {
		t.Fatalf("death->serving %v", a.DeathToServing)
	}
	if a.RestartToFirstSuccess != 100*time.Millisecond {
		t.Fatalf("restart->first success %v", a.RestartToFirstSuccess)
	}
	if a.RestartToStable != 100*time.Millisecond {
		t.Fatalf("restart->stable %v", a.RestartToStable)
	}
	if a.Death.Failed() != 60 || !a.ListenerDiedWithAgent() {
		t.Fatalf("death window: %+v", a.Death)
	}
	if a.Rotation.Failed() != 0 || a.Rotation.Total == 0 {
		t.Fatalf("ordinary window must be reported separately and be clean here: %+v", a.Rotation)
	}
	if !strings.Contains(a.String(), "(b) unexpected agent death") || !strings.Contains(a.String(), "(a) ordinary rotation") || !strings.Contains(a.String(), "(c) recovery") {
		t.Fatal(a.String())
	}
}

func TestAnalyzeNeverClaimsZeroDowntimeWhenTheListenerDied(t *testing.T) {
	// agent died but nobody refused: the architecture assumption is violated and the
	// helper must say so rather than let a zero-failure window pass as "survived".
	s := []Sample{ok(0, "a"), ok(100, "a"), ok(300, "a"), ok(900, "a")}
	a := Analyze(s, Marks{AgentDeath: at(50), RestartBegin: at(800)}, time.Time{})
	if a.ListenerDiedWithAgent() {
		t.Fatal("no connection was refused, so the listener-died claim must be false")
	}
}

func TestAnalyzeNeverServedAfterRestart(t *testing.T) {
	s := []Sample{ok(0, "a"), fail(100, "connection_refused"), fail(200, "connection_refused")}
	a := Analyze(s, Marks{AgentDeath: at(90), RestartBegin: at(150), TrafficEnd: at(300)}, time.Time{})
	if a.ServedAfterRestart || a.DeathToServing != 0 || a.Death.Failed() != 2 {
		t.Fatalf("%+v", a)
	}
}

// The generator really measures a live listener and a dead one.
func TestGeneratorAgainstLiveAndClosedListener(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ident-1")) }))
	addr := strings.TrimPrefix(srv.URL, "http://")
	g := StartTraffic(addr, 2)
	time.Sleep(150 * time.Millisecond)
	live := g.Snapshot()
	srv.Close()
	time.Sleep(150 * time.Millisecond)
	all := g.Finish()
	if len(live) == 0 || !live[0].OK || live[0].Backend != "ident-1" {
		t.Fatalf("live sample: %+v", live)
	}
	refused := 0
	for _, s := range all {
		if s.FailKind == "connection_refused" {
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("no connection_refused recorded after the listener closed")
	}
	if _, err := net.Dial("tcp", addr); err == nil {
		t.Fatal("listener still open")
	}
}
