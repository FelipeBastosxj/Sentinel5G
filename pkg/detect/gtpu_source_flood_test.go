package detect

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

func tunnelEvent(sourceIP string, teid uint32, tunnelRate float64) events.NormalizedEvent {
	return events.NormalizedEvent{
		EventID:             fmt.Sprintf("evt-%s-%d", sourceIP, teid),
		Namespace:           "telecom-core",
		PodName:             "upf-0",
		SourceIP:            sourceIP,
		DestPort:            2152,
		Protocol:            events.ProtocolGTPU,
		TEID:                teid,
		TunnelRatePerSecond: tunnelRate,
	}
}

func sourceFloodCfg(pps uint32, distinct int) GTPUSourceFloodConfig {
	return GTPUSourceFloodConfig{
		Enabled:          true,
		PacketsPerSecond: pps,
		DistinctTunnels:  distinct,
		Window:           time.Second,
		Cooldown:         30 * time.Second,
	}
}

// The reason this detector exists, demonstrated end to end rather than
// argued: ROADMAP.md Phase 4's one-line evasion. A flood spread across 200
// tunnels at 999 pkt/s each is ~199,800 pkt/s from one peer and crosses the
// shipped 1000 pkt/s PER-TUNNEL threshold exactly zero times.
//
// Both detectors are run over the identical event stream, which is the only
// way the comparison means anything.
func TestSourceFlood_CatchesTheEvasionThePerTunnelRuleMisses(t *testing.T) {
	const (
		tunnels      = 200
		perTunnelPPS = 999
	)
	start := time.Now()

	perTunnel := NewGTPUFloodDetector(GTPUFloodConfig{
		Enabled: true, PacketsPerSecond: 1000, Cooldown: 30 * time.Second,
	})
	perSource := NewGTPUSourceFloodDetector(sourceFloodCfg(20000, 0))

	var tunnelFirings, sourceFirings int
	var firstSourceScore events.ThreatScoreEvent

	// One event per packet, which is what pkg/ingestion.Publisher delivers.
	// Interleaved across tunnels rather than tunnel-by-tunnel, because that
	// is what a real distributed flood looks like on the wire and it is the
	// ordering most likely to break a per-source window.
	for pkt := 0; pkt < perTunnelPPS; pkt++ {
		for teid := uint32(1); teid <= tunnels; teid++ {
			evt := tunnelEvent("10.42.0.7", teid, float64(pkt+1))
			if _, fired := perTunnel.Evaluate(evt, start); fired {
				tunnelFirings++
			}
			if score, _, fired := perSource.Evaluate(evt, start); fired {
				if sourceFirings == 0 {
					firstSourceScore = score
				}
				sourceFirings++
			}
		}
	}

	if tunnelFirings != 0 {
		t.Fatalf("the per-tunnel rule fired %d times at %d pkt/s per tunnel; the evasion premise is wrong",
			tunnelFirings, perTunnelPPS)
	}
	if sourceFirings == 0 {
		t.Fatalf("the per-source rule never fired on %d tunnels x %d pkt/s = %d pkt/s aggregate",
			tunnels, perTunnelPPS, tunnels*perTunnelPPS)
	}
	// Exactly one: the cooldown is what keeps a 200k pkt/s flood from
	// becoming 200k ThreatScoreEvents.
	if sourceFirings != 1 {
		t.Errorf("fired %d times within one cooldown, want 1", sourceFirings)
	}
	if firstSourceScore.Model != ModelGTPUSourceFlood {
		t.Errorf("Model = %q, want %q", firstSourceScore.Model, ModelGTPUSourceFlood)
	}
}

// The false-positive side of the same coin, and the one that decides whether
// this is safe to ship on by default: a busy but ordinary gNB carrying many
// subscribers at ordinary rates must produce nothing. Four UEs at 25 pkt/s
// each is the shape of the committed multi-UE capture
// (docs/paper-data/real-dataset-v2/).
func TestSourceFlood_SilentOnOrdinaryMultiSubscriberTraffic(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(20000, 256))
	start := time.Now()

	for second := 0; second < 60; second++ {
		at := start.Add(time.Duration(second) * time.Second)
		for pkt := 0; pkt < 25; pkt++ {
			for teid := uint32(1); teid <= 4; teid++ {
				if _, _, fired := d.Evaluate(tunnelEvent("10.42.0.7", teid, 25), at); fired {
					t.Fatalf("fired on ordinary traffic: second %d, 4 tunnels x 25 pkt/s", second)
				}
			}
		}
	}
}

// The cardinality half, which is the one that sees TEID rotation at a rate
// no aggregate threshold would catch: 300 distinct TEIDs, one packet each,
// is 300 pkt/s -- utterly unremarkable -- and is not a shape any real gNB
// produces.
func TestSourceFlood_CardinalityFiresWhereRateDoesNot(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(20000, 256))
	now := time.Now()

	var fired bool
	var observed SourceFloodObservation
	for teid := uint32(1); teid <= 300; teid++ {
		_, obs, f := d.Evaluate(tunnelEvent("10.42.0.7", teid, 1), now)
		if f {
			fired, observed = true, obs
			break
		}
	}
	if !fired {
		t.Fatal("300 distinct TEIDs in one window did not trip the cardinality threshold")
	}
	if observed.DistinctTunnels != 256 {
		t.Errorf("fired at %d distinct tunnels, want exactly the configured 256", observed.DistinctTunnels)
	}
	if observed.PacketsInWindow >= 20000 {
		t.Errorf("the aggregate threshold was also crossed (%d pkt); this test no longer isolates cardinality",
			observed.PacketsInWindow)
	}
}

// The blast-radius contract. A per-peer verdict names no tunnel, because
// there is no single tunnel whose removal fixes it -- and emitting one
// would make actions.ebpfBlockTunnel look applicable when it is not. A
// policy configured only for the per-tunnel action therefore takes no
// action and counts a no_teid no-op, which is the honest outcome.
func TestSourceFlood_ScoreCarriesNoTEID(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(10, 0))
	now := time.Now()

	for i := 0; i < 10; i++ {
		score, _, fired := d.Evaluate(tunnelEvent("10.42.0.7", uint32(i+1), 1), now)
		if !fired {
			continue
		}
		if score.TEID != 0 {
			t.Fatalf("score carries TEID %#x; a source-wide finding must not name one tunnel", score.TEID)
		}
		if score.SourceIP != "10.42.0.7" {
			t.Fatalf("SourceIP = %q", score.SourceIP)
		}
		if score.Score != 1.0 {
			t.Fatalf("Score = %v, want 1.0 (a deterministic rule has no uncertainty, and 1.0 is what clears the most conservative sensitivity tier)", score.Score)
		}
		return
	}
	t.Fatal("never fired")
}

// Counts are per window, not cumulative. Without the reset, any source left
// running long enough eventually crosses any threshold -- the detector
// would report "flood" for traffic that was never fast.
func TestSourceFlood_CountsResetBetweenWindows(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(100, 0))
	start := time.Now()

	for second := 0; second < 20; second++ {
		at := start.Add(time.Duration(second) * time.Second)
		for pkt := 0; pkt < 99; pkt++ { // one below the threshold, forever
			if _, _, fired := d.Evaluate(tunnelEvent("10.42.0.7", 1, 99), at); fired {
				t.Fatalf("fired at 99 pkt/s sustained; counts are accumulating across windows (second %d)", second)
			}
		}
	}
}

// The cooldown deliberately spans windows. Resetting it along with the
// counts would mean a sustained flood produced one event per window
// forever, which is the storm the cooldown exists to prevent.
func TestSourceFlood_CooldownSurvivesAWindowReset(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(10, 0))
	start := time.Now()

	fire := func(at time.Time) int {
		n := 0
		for pkt := 0; pkt < 50; pkt++ {
			if _, _, fired := d.Evaluate(tunnelEvent("10.42.0.7", 1, 50), at); fired {
				n++
			}
		}
		return n
	}

	if got := fire(start); got != 1 {
		t.Fatalf("first window fired %d times, want 1", got)
	}
	if got := fire(start.Add(2 * time.Second)); got != 0 {
		t.Fatalf("fired %d times in a new window still inside the 30s cooldown, want 0", got)
	}
	if got := fire(start.Add(31 * time.Second)); got != 1 {
		t.Fatalf("fired %d times after the cooldown elapsed, want 1", got)
	}
}

// Two peers are two independent findings. Sharing a window between them
// would let a busy legitimate gNB push an innocent one over the threshold.
func TestSourceFlood_SourcesAreIndependent(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(100, 0))
	now := time.Now()

	for pkt := 0; pkt < 99; pkt++ {
		if _, _, fired := d.Evaluate(tunnelEvent("10.42.0.7", 1, 99), now); fired {
			t.Fatal("the loud peer fired below its own threshold")
		}
		if _, _, fired := d.Evaluate(tunnelEvent("10.42.0.9", 1, 99), now); fired {
			t.Fatal("the second peer fired below its own threshold")
		}
	}
}

// Structurally inert for every capture path that cannot see a tunnel:
// pkg/hubble reads flow summaries and pkg/falco traces syscalls, and both
// always emit TEID 0. Same guarantee the per-tunnel detector carries.
func TestSourceFlood_IgnoresEverythingWithoutATunnelIdentity(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(1, 1))
	now := time.Now()

	cases := []events.NormalizedEvent{
		// GTP-U port, no valid header: belongs to the per-source rate
		// feature the model already has, not here.
		{SourceIP: "10.42.0.7", Protocol: events.ProtocolGTPU, TEID: 0, DestPort: 2152},
		// Another protocol entirely.
		{SourceIP: "10.42.0.7", Protocol: events.ProtocolSIP, TEID: 0, DestPort: 5060},
		// A tunnel identity with no source to attribute it to.
		{SourceIP: "", Protocol: events.ProtocolGTPU, TEID: 7},
	}
	for i, evt := range cases {
		if _, _, fired := d.Evaluate(evt, now); fired {
			t.Errorf("case %d fired: %+v", i, evt)
		}
	}
}

// Both halves off leaves a detector that is "enabled" and can never fire --
// a configuration that looks protective and is not. pkg/config refuses it;
// Configured() is what makes that refusable.
func TestSourceFlood_ConfiguredReportsAnInertRule(t *testing.T) {
	if NewGTPUSourceFloodDetector(sourceFloodCfg(0, 0)).Configured() {
		t.Error("a detector with both thresholds at zero reports itself configured")
	}
	if NewGTPUSourceFloodDetector(GTPUSourceFloodConfig{Enabled: false, PacketsPerSecond: 10}).Configured() {
		t.Error("a disabled detector reports itself configured")
	}
	if !NewGTPUSourceFloodDetector(sourceFloodCfg(0, 256)).Configured() {
		t.Error("cardinality alone should be a usable configuration")
	}
	var nilDetector *GTPUSourceFloodDetector
	if nilDetector.Configured() {
		t.Error("a nil detector reports itself configured")
	}
}

// A detector that cannot fire must also not ACCUMULATE: an inert rule still
// seeing every packet would hold per-source state for a verdict it can
// never reach, which is memory spent on nothing.
func TestSourceFlood_InertRuleEvaluatesToNothing(t *testing.T) {
	now := time.Now()
	for _, d := range []*GTPUSourceFloodDetector{
		NewGTPUSourceFloodDetector(sourceFloodCfg(0, 0)),
		NewGTPUSourceFloodDetector(GTPUSourceFloodConfig{Enabled: false, PacketsPerSecond: 1}),
	} {
		for i := 0; i < 100; i++ {
			if _, _, fired := d.Evaluate(tunnelEvent("10.42.0.7", uint32(i+1), 1), now); fired {
				t.Fatal("an unconfigured detector fired")
			}
		}
		d.mu.Lock()
		tracked := len(d.sources)
		d.mu.Unlock()
		if tracked != 0 {
			t.Errorf("an unconfigured detector tracked %d sources", tracked)
		}
	}
}

// The source address is attacker-influenced on any interface that is not
// strictly a single known peer, so the map has to be bounded -- the same
// fixed-capacity discipline CLAUDE.md requires of the in-kernel maps, and
// the same reasoning as the per-tunnel detector's cooldown map.
func TestSourceFlood_SourceMapIsBounded(t *testing.T) {
	cfg := sourceFloodCfg(1000000, 0) // unreachable threshold: only the map matters here
	cfg.MaxTrackedSources = 8
	d := NewGTPUSourceFloodDetector(cfg)
	now := time.Now()

	for i := 0; i < 10000; i++ {
		d.Evaluate(tunnelEvent(fmt.Sprintf("10.0.%d.%d", i/256%256, i%256), 1, 1), now)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.sources) > 8 {
		t.Fatalf("tracked %d sources with MaxTrackedSources=8", len(d.sources))
	}
}

// The per-source TEID set is bounded for the same reason, and reaching the
// cap must not stop the rule working -- the cap sits far above any
// cardinality threshold worth setting, so a source that fills it has long
// since been reported.
func TestSourceFlood_TunnelSetIsBoundedWithoutBlindingTheRule(t *testing.T) {
	cfg := sourceFloodCfg(0, 16)
	cfg.MaxTunnelsPerSource = 32
	d := NewGTPUSourceFloodDetector(cfg)
	now := time.Now()

	fired := false
	for teid := uint32(1); teid <= 10000; teid++ {
		if _, _, f := d.Evaluate(tunnelEvent("10.42.0.7", teid, 1), now); f {
			fired = true
		}
	}
	if !fired {
		t.Fatal("never fired despite 10,000 distinct TEIDs against a threshold of 16")
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if got := len(d.sources["10.42.0.7"].teids); got > 32 {
		t.Fatalf("held %d TEIDs with MaxTunnelsPerSource=32", got)
	}
}

// pkg/ingestion.Publisher runs one per node today, but a future caller with
// several readers should not have to discover a data race -- the same
// posture the per-tunnel detector takes.
func TestSourceFlood_ConcurrentEvaluateIsSafe(t *testing.T) {
	d := NewGTPUSourceFloodDetector(sourceFloodCfg(50, 16))
	now := time.Now()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				d.Evaluate(tunnelEvent(fmt.Sprintf("10.42.0.%d", g%3), uint32(i%64)+1, 1), now)
			}
		}(g)
	}
	wg.Wait()
}
