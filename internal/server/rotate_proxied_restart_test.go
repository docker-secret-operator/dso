package server

import (
	"testing"
	"time"
)

// A proxied restart rotation (the original carries dso.host_ports, the proxy owns the host
// port) starts the replacement BEFORE stopping the original and then hands traffic over
// with a proxy swap. If that swap did not happen -- here the replacement has no network
// IP, so there is nothing to route to -- the original is the only backend and the only
// workload that can serve. The old code logged "proxy swap failed, continuing", waited
// the drain window, then stopped and DELETED the original and journalled the rotation
// completed: no backend, no original, reported as success.
func TestManualRotate_ProxiedRestart_SwapDidNotHappen_NeverRemovesTheOriginal(t *testing.T) {
	_, ts, calls := rotationFixtureOpts(t, "ok", rotationOpts{hostPorts: "18080:80"})
	postRotate(t, ts)

	// The failed handoff is rolled back: the replacement is discarded, the original gets
	// its name back and is running.
	waitFor(t, "the replacement to be discarded", func() bool { return calls.countReq("DELETE", "/containers/new-1") > 0 })
	waitFor(t, "the original's name to be restored", func() bool { return calls.countEnds("POST", "/containers/orig-1/rename?name=myapp") > 0 })
	waitFor(t, "the original to be running again", func() bool { return calls.countReq("POST", "/containers/orig-1/start") > 0 })

	// Give a (wrongly) continuing rotation the whole drain window plus margin to reach
	// its destructive steps; it must not.
	time.Sleep(6 * time.Second)
	if n := calls.countReq("DELETE", "/containers/orig-1"); n != 0 {
		t.Fatalf("the original was deleted %d time(s) although the proxy swap never happened", n)
	}
	if n := calls.count("/containers/orig-1/stop"); n != 0 {
		t.Fatalf("the original was stopped %d time(s) although the proxy swap never happened", n)
	}
}

// Unchanged behaviour: when the swap DOES happen the original is drained, stopped and
// retired as before, and the replacement is kept.
func TestManualRotate_ProxiedRestart_SwapSucceeded_RetiresTheOriginalAsBefore(t *testing.T) {
	_, ts, calls := rotationFixtureOpts(t, "ok", rotationOpts{hostPorts: "18081:80", replacementIP: "172.17.0.9"})
	postRotate(t, ts)

	// The drain window (5s) precedes the stop; allow it.
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) && calls.countReq("DELETE", "/containers/orig-1") == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if calls.countReq("DELETE", "/containers/orig-1") == 0 {
		t.Fatal("after a successful proxy swap the original must still be retired")
	}
	if calls.count("/containers/orig-1/stop") == 0 {
		t.Fatal("the original must be stopped before it is removed")
	}
	if n := calls.countReq("DELETE", "/containers/new-1"); n != 0 {
		t.Fatalf("the verified replacement must be kept; it was deleted %d time(s)", n)
	}
}
