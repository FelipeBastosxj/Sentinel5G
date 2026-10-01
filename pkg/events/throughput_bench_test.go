package events

import (
	"fmt"
	"testing"
	"time"
)

// Measures real NATS JetStream publish throughput on this host, for
// ROADMAP.md Phase 4's "the bus is the first ceiling" item: the point is to
// confirm the single-event ceiling and the multiplier batching buys, against
// a live server rather than a quoted number. Skips without NATS.
//
//	go test ./pkg/events/ -bench Throughput -run x -benchtime 3s
func benchBus(b *testing.B) (*Bus, Config) {
	cfg := DefaultConfig()
	cfg.StreamName = "SENTINEL5G_BENCH"
	cfg.EventsSubject = "sentinel5g.bench.events"
	cfg.ThreatsSubject = "sentinel5g.bench.threats"
	cfg.ConnectTimeout = 2 * time.Second
	bus, err := Connect(cfg)
	if err != nil {
		b.Skipf("no NATS: %v", err)
	}
	return bus, cfg
}

func sampleEvent() NormalizedEvent {
	return NormalizedEvent{
		EventID: "evt", ObservedAt: time.Now().UTC(), NodeName: "node-1",
		SourceIP: "10.0.0.1", DestIP: "10.0.0.9", DestPort: 2152,
		Protocol: ProtocolGTPU, PayloadSize: 120, TEID: 0x4D84, TunnelRatePerSecond: 25,
	}
}

func BenchmarkThroughput_SingleEventPerMessage(b *testing.B) {
	bus, cfg := benchBus(b)
	defer bus.Close()
	evt := sampleEvent()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bus.PublishNormalizedEvent(cfg.EventsSubject, evt); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msg/s")
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkThroughput_BatchedEvents(b *testing.B) {
	for _, batchSize := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			bus, cfg := benchBus(b)
			defer bus.Close()
			batch := make([]NormalizedEvent, batchSize)
			for i := range batch {
				batch[i] = sampleEvent()
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := bus.PublishNormalizedEventBatch(cfg.EventsSubject, batch); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			msgs := float64(b.N) / b.Elapsed().Seconds()
			b.ReportMetric(msgs, "msg/s")
			b.ReportMetric(msgs*float64(batchSize), "events/s")
		})
	}
}
