package events

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

// TestConnector_UnconnectedReportsNotConnected doesn't need a NATS server:
// a fresh Connector must report "not connected" and never block a
// non-blocking caller (pkg/ingestion.Publisher, pkg/hubble.Observer) before
// Run has ever succeeded.
func TestConnector_UnconnectedReportsNotConnected(t *testing.T) {
	c := NewConnector(DefaultConfig(), logr.Discard())

	if c.Connected() {
		t.Fatal("Connected() = true before Run ever succeeded, want false")
	}
	if bus, ok := c.Bus(); ok || bus != nil {
		t.Fatalf("Bus() = (%v, %v), want (nil, false) before Run ever succeeded", bus, ok)
	}
}

// TestConnector_WaitReturnsContextErrorWhenNeverConnected exercises the
// path cmd/operator/main.go relies on for graceful shutdown:
// ThreatScoreWatcher.Start/Publisher.Start/Observer.Start call Wait and
// must return promptly (not hang) when ctx is cancelled before NATS ever
// connects -- the "ordinary shutdown, not a Start failure" case documented
// on Wait.
func TestConnector_WaitReturnsContextErrorWhenNeverConnected(t *testing.T) {
	// An address nothing listens on, with a short connect timeout, so Run's
	// retry loop below has time to attempt (and fail) at least once before
	// the test's own timeout fires -- this test needs no real NATS server.
	cfg := DefaultConfig()
	cfg.URL = "nats://127.0.0.1:1"
	cfg.ConnectTimeout = 200 * time.Millisecond

	c := NewConnector(cfg, logr.Discard())
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go c.Run(runCtx)

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()

	bus, err := c.Wait(waitCtx)
	if err == nil {
		t.Fatalf("Wait() = (%v, nil), want a context error against an unreachable NATS URL", bus)
	}
	if bus != nil {
		t.Fatalf("Wait() returned a non-nil Bus (%v) alongside an error", bus)
	}
	if c.Connected() {
		t.Fatal("Connected() = true against an unreachable NATS URL, want false")
	}
}

// TestConnector_RunConnectsSuccessfully is a real integration test against a
// live NATS JetStream server, matching nats_test.go's own convention: skips
// (rather than failing unrelated `go test ./...` runs) if none is reachable.
func TestConnector_RunConnectsSuccessfully(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StreamName = "SENTINEL5G_TEST_" + t.Name()
	cfg.EventsSubject = "sentinel5g.test.events." + t.Name()
	cfg.ThreatsSubject = "sentinel5g.test.threats." + t.Name()
	cfg.ConnectTimeout = 2 * time.Second

	// Probe reachability the same way Connect's own callers already do
	// elsewhere in this package, so a genuinely unreachable NATS skips
	// instead of this test hanging until its own deadline.
	probe, err := Connect(cfg)
	if err != nil {
		t.Skipf("no reachable NATS server at %s, skipping integration test: %v", cfg.URL, err)
	}
	probe.Close()

	c := NewConnector(cfg, logr.Discard())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Run did not return after connecting")
	}

	if !c.Connected() {
		t.Fatal("Connected() = false after Run returned against a reachable NATS server")
	}
	bus, ok := c.Bus()
	if !ok || bus == nil {
		t.Fatalf("Bus() = (%v, %v), want a non-nil Bus and true", bus, ok)
	}
	defer bus.Close()

	waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	if waited, err := c.Wait(waitCtx); err != nil || waited != bus {
		t.Fatalf("Wait() = (%v, %v), want (%v, nil) once already connected", waited, err, bus)
	}
}

func TestNewConnectedConnector_ReportsConnectedImmediately(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StreamName = "SENTINEL5G_TEST_" + t.Name()
	cfg.EventsSubject = "sentinel5g.test.events." + t.Name()
	cfg.ThreatsSubject = "sentinel5g.test.threats." + t.Name()
	cfg.ConnectTimeout = 2 * time.Second

	bus, err := Connect(cfg)
	if err != nil {
		t.Skipf("no reachable NATS server at %s, skipping integration test: %v", cfg.URL, err)
	}
	defer bus.Close()

	c := NewConnectedConnector(bus)
	if !c.Connected() {
		t.Fatal("Connected() = false immediately after NewConnectedConnector, want true")
	}
	if got, ok := c.Bus(); !ok || got != bus {
		t.Fatalf("Bus() = (%v, %v), want (%v, true)", got, ok, bus)
	}
}
