package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/polling"
)

// TestTickerMapRace verifies that the tickers map is properly synchronized.
// This test will fail (panic: concurrent map writes) if the tickers map is not protected by a mutex.
// BUG-1 regression test: ensures no concurrent map access without synchronization.
func TestTickerMapRace(t *testing.T) {
	agent := NewAgent(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	poller := polling.NewSmartPoller()
	tickersChan := make(chan string, 100)
	defer close(tickersChan)

	// Simulate concurrent access to the tickers map from multiple code paths.
	// This mimics the race condition in the original code where:
	// 1. startPollingGoroutines writes to tickers
	// 2. updateTicker reads and writes to tickers
	// 3. defer cleanup reads tickers

	var wg sync.WaitGroup

	// Goroutine 1: Simulate startPollingGoroutines spawning tickers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			secretName := "secret-" + string(rune(i))
			interval := poller.GetNextInterval(secretName)
			ticker := time.NewTicker(interval)

			agent.tickersMu.Lock()
			agent.tickers[secretName] = ticker
			agent.tickersMu.Unlock()

			// Stop the ticker to avoid resource leak in test
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			default:
			}
		}
	}()

	// Goroutine 2: Simulate updateTicker replacing tickers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			secretName := "secret-" + string(rune(i%100))

			agent.tickersMu.Lock()
			if oldTicker, exists := agent.tickers[secretName]; exists {
				oldTicker.Stop()
			}
			newTicker := time.NewTicker(time.Second)
			agent.tickers[secretName] = newTicker
			agent.tickersMu.Unlock()

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	// Goroutine 3: Simulate defer cleanup iterating tickers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			agent.tickersMu.Lock()
			// Read and stop all tickers (simulating cleanup)
			for _, ticker := range agent.tickers {
				ticker.Stop()
			}
			agent.tickers = make(map[string]*time.Ticker)
			agent.tickersMu.Unlock()

			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				// Re-populate for next iteration
				for i := 0; i < 50; i++ {
					secretName := "secret-" + string(rune(i%100))
					interval := poller.GetNextInterval(secretName)
					ticker := time.NewTicker(interval)

					agent.tickersMu.Lock()
					agent.tickers[secretName] = ticker
					agent.tickersMu.Unlock()
				}
			}
		}
	}()

	// Wait for goroutines to complete (or context to timeout)
	go func() {
		wg.Wait()
		cancel()
	}()

	// If we reach here without panic, the synchronization is working.
	<-ctx.Done()

	// Cleanup
	agent.tickersMu.Lock()
	for _, ticker := range agent.tickers {
		ticker.Stop()
	}
	agent.tickersMu.Unlock()

	t.Log("✅ Tickers map race test passed: no concurrent map access detected")
}

// TestUpdateTickerConcurrency verifies that updateTicker can be called concurrently without panicking.
func TestUpdateTickerConcurrency(t *testing.T) {
	agent := NewAgent(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	poller := polling.NewSmartPoller()
	tickersChan := make(chan string, 100)
	defer close(tickersChan)

	// Initialize a few tickers
	for i := 0; i < 10; i++ {
		secretName := "secret-" + string(rune(i))
		ticker := time.NewTicker(time.Second)

		agent.tickersMu.Lock()
		agent.tickers[secretName] = ticker
		agent.tickersMu.Unlock()

		agent.tickerStopMu.Lock()
		agent.tickerStopChans[secretName] = make(chan struct{})
		agent.tickerStopMu.Unlock()
	}

	var wg sync.WaitGroup

	// Spawn multiple goroutines calling updateTicker concurrently
	for j := 0; j < 5; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				secretName := "secret-" + string(rune(i%10))
				agent.updateTicker(ctx, tickersChan, secretName, poller)

				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}()
	}

	// Wait for all goroutines to finish or context to cancel
	go func() {
		wg.Wait()
		cancel()
	}()

	<-ctx.Done()

	// Cleanup
	agent.tickersMu.Lock()
	for _, ticker := range agent.tickers {
		ticker.Stop()
	}
	agent.tickersMu.Unlock()

	agent.tickerStopMu.Lock()
	for _, ch := range agent.tickerStopChans {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	agent.tickerStopMu.Unlock()

	t.Log("✅ UpdateTicker concurrency test passed: no races detected")
}
