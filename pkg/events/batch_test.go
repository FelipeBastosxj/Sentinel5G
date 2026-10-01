package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// The wire contract the AI-engine consumer relies on: a batch is a JSON
// ARRAY (leading '['), a single event is an OBJECT (leading '{'). The Python
// worker distinguishes them by that first byte, so if this ever changed the
// consumer would silently mis-parse every batch. A pure marshaling test, no
// NATS needed.
func TestPublishNormalizedEventBatch_IsAJSONArray(t *testing.T) {
	batch := []NormalizedEvent{
		{EventID: "a", SourceIP: "203.0.113.7", Protocol: ProtocolGTPU, DestPort: 2152, TEID: 1},
		{EventID: "b", SourceIP: "203.0.113.8", Protocol: ProtocolGTPU, DestPort: 2152, TEID: 2},
	}
	payload, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if payload[0] != '[' {
		t.Fatalf("a batch did not marshal to a JSON array (first byte %q); the consumer detects batches by the leading '['", payload[0])
	}

	var back []NormalizedEvent
	if err := json.Unmarshal(payload, &back); err != nil {
		t.Fatalf("a batch did not round-trip: %v", err)
	}
	if len(back) != 2 || back[0].EventID != "a" || back[1].TEID != 2 {
		t.Fatalf("round-trip mismatch: %+v", back)
	}

	// A single event stays an object, so the two are distinguishable.
	single, _ := json.Marshal(batch[0])
	if single[0] != '{' {
		t.Fatalf("a single event is not a JSON object (first byte %q)", single[0])
	}
}

// Against a live JetStream server, a batch publishes as one message whose raw
// bytes are the array the consumer will parse. Skips without a server, like
// the other pkg/events integration tests.
func TestPublishNormalizedEventBatch_RoundTripsThroughJetStream(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StreamName = "SENTINEL5G_TEST_" + t.Name()
	cfg.EventsSubject = "sentinel5g.test.events." + t.Name()
	cfg.ThreatsSubject = "sentinel5g.test.threats." + t.Name()
	cfg.ConnectTimeout = 2 * time.Second

	bus, err := Connect(cfg)
	if err != nil {
		t.Skipf("no reachable NATS server at %s: %v", cfg.URL, err)
	}
	defer bus.Close()

	// Raw core-NATS subscription on the events subject to capture the bytes
	// exactly as published (bus.js is reachable from this in-package test).
	raw := make(chan []byte, 1)
	sub, err := bus.js.Subscribe(cfg.EventsSubject, func(m *nats.Msg) {
		raw <- append([]byte(nil), m.Data...)
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	batch := []NormalizedEvent{
		{EventID: "a", SourceIP: "203.0.113.7", Protocol: ProtocolGTPU, DestPort: 2152, TEID: 1},
		{EventID: "b", SourceIP: "203.0.113.8", Protocol: ProtocolGTPU, DestPort: 2152, TEID: 2},
		{EventID: "c", SourceIP: "203.0.113.9", Protocol: ProtocolGTPU, DestPort: 2152, TEID: 3},
	}
	if err := bus.PublishNormalizedEventBatch(cfg.EventsSubject, batch); err != nil {
		t.Fatalf("publish batch: %v", err)
	}

	select {
	case data := <-raw:
		var back []NormalizedEvent
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("the published batch was not a JSON array: %v (%s)", err, data)
		}
		if len(back) != 3 {
			t.Fatalf("received %d events in the batch, want 3", len(back))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the batch message")
	}

	// An empty batch publishes nothing.
	if err := bus.PublishNormalizedEventBatch(cfg.EventsSubject, nil); err != nil {
		t.Fatalf("empty batch should be a no-op, got %v", err)
	}
}
