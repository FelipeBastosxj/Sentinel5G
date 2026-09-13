package detect

import (
	"testing"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Evaluate sits on the per-packet path of every node. Arbitrary events must
// never panic, the cooldown map must never exceed its capacity, and the two
// structural gates (GTP-U only, non-zero TEID) must hold for any input.
func FuzzEvaluate(f *testing.F) {
	f.Add("GTP-U", uint32(0x4d84), 3001.0, "10.0.0.1", int64(0))
	f.Add("GTP-U", uint32(0), 100000.0, "10.0.0.1", int64(0))
	f.Add("SIP", uint32(7), 100000.0, "", int64(-1))
	f.Add("", uint32(1), -5.0, "::1", int64(1<<40))

	d := NewGTPUFloodDetector(GTPUFloodConfig{
		Enabled: true, PacketsPerSecond: 1000, Cooldown: time.Second, MaxTrackedTunnels: 64,
	})
	base := time.Unix(1_700_000_000, 0)

	f.Fuzz(func(t *testing.T, proto string, teid uint32, rate float64, src string, offsetNs int64) {
		evt := events.NormalizedEvent{
			Protocol: events.Protocol(proto), TEID: teid, TunnelRatePerSecond: rate, SourceIP: src,
		}
		score, fired := d.Evaluate(evt, base.Add(time.Duration(offsetNs)))

		if fired && (evt.Protocol != events.ProtocolGTPU || teid == 0 || rate < 1000) {
			t.Fatalf("fired on an event that fails a structural gate: %+v", evt)
		}
		if fired && (score.Score != 1.0 || score.Model != ModelGTPUTunnelFlood) {
			t.Fatalf("fired with a malformed score: %+v", score)
		}
		d.mu.Lock()
		n := len(d.lastFired)
		d.mu.Unlock()
		if n > 64 {
			t.Fatalf("cooldown map grew to %d, cap 64", n)
		}
	})
}

func BenchmarkEvaluate_NormalTraffic(b *testing.B) {
	d := NewGTPUFloodDetector(GTPUFloodConfig{Enabled: true, PacketsPerSecond: 1000})
	evt := gtpuEvent(10)
	now := time.Now()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d.Evaluate(evt, now)
	}
}

// The worst case for the cooldown map: every packet is a fresh tunnel over
// threshold, so every call inserts and (once full) evicts.
func BenchmarkEvaluate_RotatingTEIDFlood(b *testing.B) {
	d := NewGTPUFloodDetector(GTPUFloodConfig{Enabled: true, PacketsPerSecond: 1000, Cooldown: time.Hour})
	evt := gtpuEvent(3000)
	now := time.Now()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		evt.TEID = uint32(i) + 1
		d.Evaluate(evt, now)
	}
}
