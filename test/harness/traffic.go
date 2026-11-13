package harness

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Sample is one new-connection HTTP/1.0 request. It is the same measurement the
// in-process traffic tests (test/docker/traffic_rotation_test.go) take, factored
// here so the generator can live in the test process and OUTLIVE the agent under
// test. Clients open a NEW connection per request: they measure availability of
// new connections, not preservation of existing long-lived ones.
type Sample struct {
	Start, End time.Time
	OK         bool
	FailKind   string // connection_refused, dial_timeout, dial_error, write_error, read_timeout, closed_no_response, http_5xx, http_other
	Status     int
	Backend    string // body of the response: the serving instance's identity
	Latency    time.Duration
}

// Generator runs concurrent short-connection clients until stopped.
type Generator struct {
	addr    string
	mu      sync.Mutex
	samples []Sample
	stop    chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
}

// StartTraffic starts workers clients against addr (host:port).
func StartTraffic(addr string, workers int) *Generator {
	g := &Generator{addr: addr, stop: make(chan struct{})}
	for i := 0; i < workers; i++ {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			for {
				select {
				case <-g.stop:
					return
				default:
				}
				s := OneRequest(g.addr)
				g.mu.Lock()
				g.samples = append(g.samples, s)
				g.mu.Unlock()
				time.Sleep(3 * time.Millisecond)
			}
		}()
	}
	return g
}

// Snapshot returns a copy of the samples so far without stopping.
func (g *Generator) Snapshot() []Sample {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Sample(nil), g.samples...)
}

// Finish stops the generator and returns every sample.
func (g *Generator) Finish() []Sample {
	g.once.Do(func() { close(g.stop) })
	g.wg.Wait()
	return g.Snapshot()
}

// OneRequest performs a single request.
func OneRequest(addr string) Sample {
	s := Sample{Start: time.Now()}
	done := func() Sample { s.End = time.Now(); s.Latency = s.End.Sub(s.Start); return s }

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		switch ne, isNet := err.(net.Error); {
		case strings.Contains(err.Error(), "refused"):
			s.FailKind = "connection_refused"
		case isNet && ne.Timeout():
			s.FailKind = "dial_timeout"
		default:
			s.FailKind = "dial_error"
		}
		return done()
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("GET / HTTP/1.0\r\nHost: dso-it\r\n\r\n")); err != nil {
		s.FailKind = "write_error"
		return done()
	}
	raw, err := io.ReadAll(conn)
	if len(raw) == 0 {
		if ne, isNet := err.(net.Error); isNet && ne.Timeout() {
			s.FailKind = "read_timeout"
		} else {
			s.FailKind = "closed_no_response"
		}
		return done()
	}
	head, body, _ := strings.Cut(string(raw), "\r\n\r\n")
	if fields := strings.SplitN(head, " ", 3); len(fields) > 1 {
		_, _ = fmt.Sscanf(fields[1], "%d", &s.Status)
	}
	s.Backend = strings.TrimSpace(body)
	switch {
	case s.Status >= 500:
		s.FailKind = "http_5xx"
	case s.Status != http.StatusOK:
		s.FailKind = "http_other"
	default:
		s.OK = true
	}
	return done()
}
