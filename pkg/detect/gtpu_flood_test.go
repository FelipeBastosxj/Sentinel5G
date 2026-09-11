package detect

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

func gtpuEvent(rate float64) events.NormalizedEvent {
	return events.NormalizedEvent{
		EventID:             "evt-1",
		Namespace:           "telecom-core",
		PodName:             "upf-0",
		SourceIP:            "10.42.0.7",
		DestPort:            2152,
		Protocol:            events.ProtocolGTPU,
		TEID:                0x00004d84,
		TunnelRatePerSecond: rate,
	}
}

func enabled(pps uint32) GTPUFloodConfig {
	return GTPUFloodConfig{Enabled: true, PacketsPerSecond: pps, Cooldown: 30 * time.Second}
}

// At the threshold, not one above it: an operator setting 1000 means "1000 is
// already too much", and an off-by-one here would be invisible in production.
func TestEvaluate_FiresAtExactlyTheThreshold(t *testing.T) {
	now := time.Now()

	d := NewGTPUFloodDetector(enabled(1000))
	if _, fired := d.Evaluate(gtpuEvent(999), now); fired {
		t.Fatal("fired one packet below the threshold")
	}

	d = NewGTPUFloodDetector(enabled(1000))
	score, fired := d.Evaluate(gtpuEvent(1000), now)
	if !fired {
		t.Fatal("did not fire at exactly the threshold")
	}
	if score.Score != 1.0 {
		t.Errorf("Score = %v, want 1.0", score.Score)
	}
	if score.Model != ModelGTPUTunnelFlood {
		t.Errorf("Model = %q, want %q", score.Model, ModelGTPUTunnelFlood)
	}
	if score.SourceIP != "10.42.0.7" || score.Namespace != "telecom-core" || score.PodName != "upf-0" {
		t.Errorf("attribution not carried through: %+v", score)
	}
	if score.SourceEventID != "evt-1" {
		t.Errorf("SourceEventID = %q, want the triggering event's id", score.SourceEventID)
	}
}

// Score must be exactly 1.0 and not merely "high". pkg/controller's low
// sensitivity tier clamps the effective threshold to exactly 1.00 and
// compares with a strict `score < threshold`, so anything below 1.0 would
// silently never fire for policies at that tier.
func TestEvaluate_ScoreClearsTheMostConservativeTier(t *testing.T) {
	d := NewGTPUFloodDetector(enabled(10))
	score, fired := d.Evaluate(gtpuEvent(50), time.Now())
	if !fired {
		t.Fatal("expected a fire")
	}
	const lowSensitivityThreshold = 1.0
	if score.Score < lowSensitivityThreshold {
		t.Fatalf("Score = %v would be withheld at low sensitivity (threshold %v, strict <)", score.Score, lowSensitivityThreshold)
	}
}

// The TEID gate is what makes it structurally impossible for Hubble- and
// Falco-sourced events -- which always carry TEID 0 because neither can see a
// tunnel header -- to trip this detector.
func TestEvaluate_NeverFiresWithoutATunnelIdentity(t *testing.T) {
	d := NewGTPUFloodDetector(enabled(10))

	evt := gtpuEvent(100_000)
	evt.TEID = 0 // What pkg/hubble and pkg/falco always produce.

	if _, fired := d.Evaluate(evt, time.Now()); fired {
		t.Fatal("fired on an event with no tunnel identity")
	}
}

// Only GTP-U has tunnels; every other protocol must be untouched at any rate.
func TestEvaluate_IgnoresNonGTPUProtocols(t *testing.T) {
	for _, proto := range []events.Protocol{
		events.ProtocolSIP, events.ProtocolSMPP, events.ProtocolHTTP2,
		events.ProtocolPortScan, events.ProtocolUnknown,
	} {
		t.Run(string(proto), func(t *testing.T) {
			d := NewGTPUFloodDetector(enabled(10))
			evt := gtpuEvent(100_000)
			evt.Protocol = proto
			if _, fired := d.Evaluate(evt, time.Now()); fired {
				t.Fatalf("fired for protocol %q", proto)
			}
		})
	}
}

func TestEvaluate_DisabledNeverFires(t *testing.T) {
	d := NewGTPUFloodDetector(GTPUFloodConfig{Enabled: false, PacketsPerSecond: 10})
	if _, fired := d.Evaluate(gtpuEvent(100_000), time.Now()); fired {
		t.Fatal("fired while disabled")
	}
}

// A zero threshold would fire on every single GTP-U packet. Treat it as
// "not configured" rather than "maximally sensitive" -- pkg/config rejects it
// outright, this is the defence in depth for a caller constructing the
// detector directly.
func TestEvaluate_ZeroThresholdNeverFires(t *testing.T) {
	d := NewGTPUFloodDetector(GTPUFloodConfig{Enabled: true, PacketsPerSecond: 0})
	if _, fired := d.Evaluate(gtpuEvent(1), time.Now()); fired {
		t.Fatal("a zero threshold fired on ordinary traffic")
	}
}

// Without the cooldown a 3000 pkt/s flood would produce 3000 events/s, each
// driving a Status().Update() in the watcher.
func TestEvaluate_CooldownSuppressesRepeatsForTheSameTunnel(t *testing.T) {
	now := time.Now()
	d := NewGTPUFloodDetector(GTPUFloodConfig{Enabled: true, PacketsPerSecond: 10, Cooldown: 30 * time.Second})

	if _, fired := d.Evaluate(gtpuEvent(3000), now); !fired {
		t.Fatal("first event did not fire")
	}
	if _, fired := d.Evaluate(gtpuEvent(3000), now.Add(29*time.Second)); fired {
		t.Fatal("fired again inside the cooldown")
	}
	if _, fired := d.Evaluate(gtpuEvent(3000), now.Add(30*time.Second)); !fired {
		t.Fatal("did not fire once the cooldown elapsed")
	}
}

// The cooldown is per tunnel, so one flooding subscriber must not mask
// another starting up -- which is the entire point of per-TEID tracking.
func TestEvaluate_CooldownIsPerTunnelNotPerSource(t *testing.T) {
	now := time.Now()
	d := NewGTPUFloodDetector(GTPUFloodConfig{Enabled: true, PacketsPerSecond: 10, Cooldown: 30 * time.Second})

	first := gtpuEvent(3000)
	second := gtpuEvent(3000)
	second.TEID = 0x00004d85 // Same source IP, different PDU session.

	if _, fired := d.Evaluate(first, now); !fired {
		t.Fatal("first tunnel did not fire")
	}
	if _, fired := d.Evaluate(second, now); !fired {
		t.Fatal("a second tunnel from the same source was suppressed by the first's cooldown")
	}
}

// The cooldown map's key is partly attacker-chosen: a flood can rotate its
// TEID every packet. The map must stay bounded regardless.
func TestEvaluate_TrackedTunnelsStayBounded(t *testing.T) {
	const capacity = 64
	now := time.Now()
	d := NewGTPUFloodDetector(GTPUFloodConfig{
		Enabled: true, PacketsPerSecond: 10, Cooldown: time.Hour, MaxTrackedTunnels: capacity,
	})

	for i := 0; i < 100_000; i++ {
		evt := gtpuEvent(3000)
		evt.TEID = uint32(i) + 1
		if _, fired := d.Evaluate(evt, now); !fired {
			t.Fatalf("a never-before-seen tunnel (%d) was suppressed", i)
		}
	}

	d.mu.Lock()
	size := len(d.lastFired)
	d.mu.Unlock()
	if size > capacity {
		t.Fatalf("cooldown map grew to %d entries, cap is %d", size, capacity)
	}
}

// Eviction costs at most a duplicate event, never a missed one: a tunnel
// pushed out of the map simply gets reported again.
func TestEvaluate_EvictionOnlyCostsADuplicate(t *testing.T) {
	now := time.Now()
	d := NewGTPUFloodDetector(GTPUFloodConfig{
		Enabled: true, PacketsPerSecond: 10, Cooldown: time.Hour, MaxTrackedTunnels: 2,
	})

	victim := gtpuEvent(3000)
	if _, fired := d.Evaluate(victim, now); !fired {
		t.Fatal("first fire failed")
	}
	if _, fired := d.Evaluate(victim, now); fired {
		t.Fatal("cooldown not applied before eviction")
	}

	for _, teid := range []uint32{0xAAAA, 0xBBBB} {
		evt := gtpuEvent(3000)
		evt.TEID = teid
		d.Evaluate(evt, now)
	}

	if _, fired := d.Evaluate(victim, now); !fired {
		t.Fatal("an evicted tunnel should be reportable again, not permanently suppressed")
	}
}

// Run with -race. One Publisher per node today, but a caller with several
// readers shouldn't have to discover a data race in production.
func TestEvaluate_IsConcurrencySafe(t *testing.T) {
	now := time.Now()
	d := NewGTPUFloodDetector(GTPUFloodConfig{Enabled: true, PacketsPerSecond: 10, Cooldown: time.Millisecond})

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				evt := gtpuEvent(3000)
				evt.TEID = uint32(i%37) + 1
				evt.SourceIP = fmt.Sprintf("10.42.0.%d", worker)
				d.Evaluate(evt, now.Add(time.Duration(i)*time.Millisecond))
			}
		}(worker)
	}
	wg.Wait()
}
