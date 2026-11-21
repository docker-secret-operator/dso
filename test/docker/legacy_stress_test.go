//go:build dockerint

package docker

// Guarded migration of TestStress_ConcurrentRotations from
// test/integration/stress_test.go (M0-11). The four Docker-free stress tests of
// that file moved to ordinary unit tests (see docs/testing/LEGACY-DOCKER-TESTS.md).
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Original assertion: all 5 simultaneous rotations succeed. Deviation: one
// RollingStrategy per rotation (the original shared one, which holds per-Execute
// state). Expanded: every name is held by a NEW running container with its own
// ROTATION_NUM, and no original id survives.
func TestStress_ConcurrentRotations(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	const n = 5
	ids, names := make([]string, n), make([]string, n)
	for i := 0; i < n; i++ {
		ids[i], names[i] = lgRun(t, fmt.Sprintf("stress%d", i), runLabel)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var successCount, failureCount int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rotCtx, c := context.WithTimeout(ctx, 120*time.Second)
			defer c()
			err := lgRotator(t, cli).Execute(rotCtx, ids[idx], map[string]string{
				"ROTATION_NUM": fmt.Sprintf("%d", idx),
				"TIMESTAMP":    time.Now().Format(time.RFC3339),
			}, nil, nil, 5*time.Second)
			if err != nil {
				atomic.AddInt32(&failureCount, 1)
				t.Logf("Container %d rotation failed: %v", idx, err)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}
	wg.Wait()

	s, f := atomic.LoadInt32(&successCount), atomic.LoadInt32(&failureCount)
	t.Logf("Concurrent rotations: %d succeeded, %d failed", s, f)
	if f > 0 {
		t.Errorf("Expected all rotations to succeed, but %d failed", f)
	}
	for i := 0; i < n; i++ {
		in := lgRunningByName(t, cli, names[i])
		if in.ID == ids[i] {
			t.Errorf("container %d was not replaced", i)
		}
		if got := lgEnv(in)["ROTATION_NUM"]; got != fmt.Sprintf("%d", i) {
			t.Errorf("container %d carries the wrong ROTATION_NUM %q", i, got)
		}
		assertGone(t, cli, ids[i])
	}
}
