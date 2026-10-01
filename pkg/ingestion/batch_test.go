package ingestion

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/nats-io/nats.go"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/controller"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// fakeSource emits n SignalingEvents then closes the channel, standing in
// for the eBPF ring-buffer reader so the Publisher's batching can be driven
// deterministically.
type fakeSource struct{ n int }

func (f *fakeSource) SignalingEvents(ctx context.Context) (<-chan ebpf.SignalingEvent, error) {
	ch := make(chan ebpf.SignalingEvent)
	go func() {
		defer close(ch)
		for i := 0; i < f.n; i++ {
			select {
			case <-ctx.Done():
				return
			case ch <- ebpf.SignalingEvent{
				ObservedAt: time.Now(), SourceIP: net.ParseIP("10.0.0.1"),
				DestIP: net.ParseIP("10.0.0.9"), DestPort: 2152,
				Protocol: ebpf.SignalProtoGTPU, TEID: uint32(i + 1), TunnelRate: 1,
			}:
			}
		}
	}()
	return ch, nil
}

func (f *fakeSource) SignalRate(net.IP, uint16) (uint32, bool) { return 1, true }
func (f *fakeSource) TunnelRate(net.IP, uint32) (uint32, bool) { return 1, true }

// Batching reduces the number of NATS messages from one-per-event to
// ceil(n/batch) -- the whole point of ROADMAP.md Phase 4's bus-ceiling item.
// Proven end to end against a live JetStream server.
func TestPublisher_BatchingReducesMessageCount(t *testing.T) {
	cfg := events.DefaultConfig()
	cfg.StreamName = "SENTINEL5G_TEST_" + t.Name()
	cfg.EventsSubject = "sentinel5g.test.events." + t.Name()
	cfg.ThreatsSubject = "sentinel5g.test.threats." + t.Name()
	cfg.ConnectTimeout = 2 * time.Second

	bus, err := events.Connect(cfg)
	if err != nil {
		t.Skipf("no reachable NATS server at %s: %v", cfg.URL, err)
	}
	defer bus.Close()

	// A plain core-NATS connection to read the raw messages as published,
	// independent of the Bus under test.
	reader, err := nats.Connect(cfg.URL)
	if err != nil {
		t.Fatalf("reader connect: %v", err)
	}
	defer reader.Close()

	var messages, eventsSeen int64
	sub, err := reader.Subscribe(cfg.EventsSubject, func(m *nats.Msg) {
		atomic.AddInt64(&messages, 1)
		var batch []events.NormalizedEvent
		if json.Unmarshal(m.Data, &batch) == nil {
			atomic.AddInt64(&eventsSeen, int64(len(batch)))
		}
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	_ = reader.Flush()

	const n, batchSize = 100, 10
	p := &Publisher{
		Source:        &fakeSource{n: n},
		PodIndex:      controller.NewPodIPIndex(),
		Bus:           events.NewConnectedConnector(bus),
		Subject:       cfg.EventsSubject,
		NodeName:      "node-1",
		Log:           testr.New(t),
		BatchSize:     batchSize,
		FlushInterval: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&eventsSeen) < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	gotEvents := atomic.LoadInt64(&eventsSeen)
	gotMessages := atomic.LoadInt64(&messages)
	if gotEvents != n {
		t.Fatalf("received %d events across batches, want %d", gotEvents, n)
	}
	if gotMessages > n/batchSize+2 {
		t.Fatalf("batching did not reduce the message count: %d messages for %d events (batch %d)",
			gotMessages, n, batchSize)
	}
	t.Logf("delivered %d events in %d messages (batch size %d)", gotEvents, gotMessages, batchSize)
}

// With batching off (the default), it is one message per event -- the
// behaviour older consumers and the pre-batching Publisher relied on.
func TestPublisher_NoBatchingIsOneMessagePerEvent(t *testing.T) {
	cfg := events.DefaultConfig()
	cfg.StreamName = "SENTINEL5G_TEST_" + t.Name()
	cfg.EventsSubject = "sentinel5g.test.events." + t.Name()
	cfg.ThreatsSubject = "sentinel5g.test.threats." + t.Name()
	cfg.ConnectTimeout = 2 * time.Second

	bus, err := events.Connect(cfg)
	if err != nil {
		t.Skipf("no reachable NATS server at %s: %v", cfg.URL, err)
	}
	defer bus.Close()

	reader, err := nats.Connect(cfg.URL)
	if err != nil {
		t.Fatalf("reader connect: %v", err)
	}
	defer reader.Close()

	var messages int64
	sub, err := reader.Subscribe(cfg.EventsSubject, func(m *nats.Msg) {
		// A single event is a JSON object, never an array.
		if len(m.Data) > 0 && m.Data[0] == '{' {
			atomic.AddInt64(&messages, 1)
		}
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	_ = reader.Flush()

	const n = 20
	p := &Publisher{
		Source:   &fakeSource{n: n},
		PodIndex: controller.NewPodIPIndex(),
		Bus:      events.NewConnectedConnector(bus),
		Subject:  cfg.EventsSubject,
		NodeName: "node-1",
		Log:      testr.New(t),
		// BatchSize unset == 0 == off
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&messages) < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&messages); got != n {
		t.Fatalf("expected one object message per event: got %d, want %d", got, n)
	}
}
