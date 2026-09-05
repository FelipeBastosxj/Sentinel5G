package events

import (
	"testing"
	"time"
)

// TestBus_PublishAndSubscribeThreatScores is a real integration test against
// a live NATS JetStream server (NATS_URL, default nats://localhost:4222) —
// pkg/events has no mockable seam by design, since its entire job is to be a
// thin, faithful wrapper around the wire protocol both cmd/operator and
// cmd/ai-engine depend on. If no server is reachable, the test skips rather
// than failing unrelated `go test ./...` runs; see .github/workflows/ci.yml
// for how CI guarantees one is actually running so this never just skips
// silently in the pipeline.
func TestBus_PublishAndSubscribeThreatScores(t *testing.T) {
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

	received := make(chan ThreatScoreEvent, 1)
	unsubscribe, err := bus.SubscribeThreatScores(cfg.ThreatsSubject, "test-consumer-"+t.Name(), func(event ThreatScoreEvent) error {
		received <- event
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = unsubscribe() }()

	want := ThreatScoreEvent{
		SourceEventID: "evt-1",
		Namespace:     "telecom-core",
		PodName:       "amf-0",
		SourceIP:      "203.0.113.7",
		Score:         0.93,
		Model:         "autoencoder-v1",
		DetectedAt:    time.Now().UTC().Truncate(time.Second),
	}

	if err := bus.PublishThreatScore(cfg.ThreatsSubject, want); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if got.SourceEventID != want.SourceEventID || got.SourceIP != want.SourceIP || got.Score != want.Score {
			t.Fatalf("round-tripped event mismatch: got %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for published ThreatScoreEvent to be delivered")
	}
}

// TestBus_PublishNormalizedEvent only exercises the publish path (no
// consumer helper exists for this subject on the Go side by design — only
// cmd/ai-engine's NATS worker mode consumes it), confirming it round-trips
// through JetStream without error against a live server.
func TestBus_PublishNormalizedEvent(t *testing.T) {
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

	event := NormalizedEvent{
		EventID:       "evt-1",
		ObservedAt:    time.Now().UTC(),
		Namespace:     "telecom-core",
		PodName:       "amf-0",
		NodeName:      "node-1",
		SourceIP:      "203.0.113.7",
		DestIP:        "10.0.0.5",
		DestPort:      5060,
		Protocol:      ProtocolSIP,
		PayloadSize:   256,
		RatePerSecond: 3000,
		Malformed:     false,
	}

	if err := bus.PublishNormalizedEvent(cfg.EventsSubject, event); err != nil {
		t.Fatalf("publish: %v", err)
	}
}
