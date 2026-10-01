package detect

import (
	"sync"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// ModelGTPUSourceFlood is what GTPUSourceFloodDetector puts in
// ThreatScoreEvent.Model, following the same "rule:" convention as
// ModelGTPUTunnelFlood.
const ModelGTPUSourceFlood = "rule:gtpu-source-flood"

// DefaultSourceFloodWindow is the measurement window. One second, to match
// the kernel's own rate window (bpf/packet_filter.c) so the two thresholds
// are expressed in the same units and an operator can reason about them
// together.
const DefaultSourceFloodWindow = time.Second

const (
	defaultTrackedSources     = 1024
	defaultTunnelsPerSource   = 4096
	defaultSourceFloodPPS     = 20000
	defaultDistinctTunnelsCap = 256
)

// GTPUSourceFloodConfig configures GTPUSourceFloodDetector.
type GTPUSourceFloodConfig struct {
	// Enabled turns the detector off entirely when false.
	Enabled bool

	// PacketsPerSecond is the AGGREGATE GTP-U packet rate from one source
	// address, across every tunnel it carries, at or above which the source
	// is reported. Zero disables this half of the rule (leaving the
	// cardinality half).
	PacketsPerSecond uint32

	// DistinctTunnels is the number of distinct TEIDs one source may use
	// within Window before it is reported, regardless of rate. Zero
	// disables this half.
	DistinctTunnels int

	// Window is the measurement window. Zero uses
	// DefaultSourceFloodWindow.
	Window time.Duration

	// Cooldown is the minimum interval between events for one source. Zero
	// uses DefaultCooldown.
	Cooldown time.Duration

	// MaxTrackedSources caps the number of source addresses held at once.
	// Zero uses defaultTrackedSources.
	MaxTrackedSources int

	// MaxTunnelsPerSource caps the distinct-TEID set held per source. Zero
	// uses defaultTunnelsPerSource. Reaching it is itself conclusive: the
	// cap is far above any DistinctTunnels threshold worth setting, so a
	// source that fills it has already been reported.
	MaxTunnelsPerSource int
}

// GTPUSourceFloodDetector reports a GTP-U peer whose traffic is abusive in
// aggregate even though no single tunnel it carries is.
//
// It exists because of a one-line evasion of GTPUFloodDetector, recorded as
// ROADMAP.md Phase 4's second item. That detector is per tunnel by design —
// the whole point of the per-TEID work was that a per-source threshold on
// an N3 interface has to sit above the combined load of every subscriber
// behind the gNB, which is exactly the aggregation that hides one flooding
// subscriber. But the inverse holds too: 200 tunnels at 999 pkt/s each is
// roughly 200,000 pkt/s from one peer and crosses no per-tunnel threshold
// at all. Two detectors, two scopes, deliberately: neither threshold can be
// set to do the other's job.
//
// Two signals, either of which fires:
//
//   - Aggregate rate. The Publisher receives one event per signaling
//     packet, so counting events per source over Window IS that source's
//     packet rate — measured here directly rather than inferred by summing
//     the kernel's per-tunnel counters, which would double-count and would
//     be sensitive to the two windows not being aligned. It under-reads if
//     the kernel ring buffer drops events under saturation, which is the
//     safe direction: a missed detection, never a false one.
//   - Distinct-TEID cardinality. A legitimate gNB's TEID set is bounded by
//     its active bearers and turns over slowly; an attacker rotating or
//     spoofing TEIDs produces cardinality a real peer does not. This is the
//     half that catches TEID rotation even when the aggregate rate is
//     modest. It does NOT catch an attacker forging a TEID that genuinely
//     belongs to another subscriber on the same peer — that is
//     indistinguishable from the victim's own traffic without UPF session
//     state this component does not have, and it is stated here rather
//     than left to be discovered.
//
// What this reports is a statement about the PEER, not about a subscriber,
// and the blast radius follows from that: the matching action is
// actions.ebpfBlock (source-wide), which on N3 drops every subscriber
// behind that gNB. Scores are therefore emitted with TEID 0, so a policy
// configured only for actions.ebpfBlockTunnel takes no action and counts a
// no_teid no-op rather than silently picking one tunnel out of the many
// that contributed. That is the honest shape of the finding: if one peer is
// sending 200,000 pkt/s across 200 tunnels, there is no single tunnel whose
// removal fixes it.
//
// Note what is deliberately NOT done: this signal is not given to the
// autoencoder as a feature. A per-source quantity is identical for every
// subscriber behind the same gNB, so feeding one to a per-packet model
// cross-attributes one subscriber's flood to all of its neighbours —
// measured, at 150 of 192 bystander packets scored above the mitigation
// threshold, in docs/paper-data/02-ai-training-inference.md §2.6.5. A rule
// can hold a per-source signal safely because its verdict is itself
// per-source and the operator acts on it with a per-source action; a
// per-packet model cannot.
//
// Safe for concurrent use.
type GTPUSourceFloodDetector struct {
	cfg GTPUSourceFloodConfig

	mu      sync.Mutex
	sources map[string]*sourceWindow
	// order/next are the same bounded FIFO the per-tunnel detector uses,
	// and for the same reason: the source address is attacker-influenced,
	// so an unbounded map is a memory-growth primitive.
	order []string
	next  int
}

// sourceWindow is one source's state for the current window.
type sourceWindow struct {
	start     time.Time
	packets   uint32
	teids     map[uint32]struct{}
	lastFired time.Time
}

// NewGTPUSourceFloodDetector returns a detector for cfg.
func NewGTPUSourceFloodDetector(cfg GTPUSourceFloodConfig) *GTPUSourceFloodDetector {
	if cfg.Window <= 0 {
		cfg.Window = DefaultSourceFloodWindow
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = DefaultCooldown
	}
	if cfg.MaxTrackedSources <= 0 {
		cfg.MaxTrackedSources = defaultTrackedSources
	}
	if cfg.MaxTunnelsPerSource <= 0 {
		cfg.MaxTunnelsPerSource = defaultTunnelsPerSource
	}

	return &GTPUSourceFloodDetector{
		cfg:     cfg,
		sources: make(map[string]*sourceWindow, cfg.MaxTrackedSources),
		order:   make([]string, cfg.MaxTrackedSources),
	}
}

// Configured reports whether the detector can ever fire. Both thresholds
// being zero leaves it enabled but inert, which is worth being able to
// detect at startup rather than wondering later.
func (d *GTPUSourceFloodDetector) Configured() bool {
	return d != nil && d.cfg.Enabled && (d.cfg.PacketsPerSecond > 0 || d.cfg.DistinctTunnels > 0)
}

// SourceFloodObservation is what the source's window looked like when it
// crossed a threshold. Returned alongside the score purely so the log line
// and any future metric can say WHICH half of the rule fired and by how
// much — "this peer is abusive" is not actionable without the number
// behind it.
type SourceFloodObservation struct {
	// PacketsInWindow is the aggregate GTP-U packet count for this source
	// across every tunnel, in the window that triggered.
	PacketsInWindow uint32
	// DistinctTunnels is how many distinct TEIDs that window saw, capped at
	// GTPUSourceFloodConfig.MaxTunnelsPerSource.
	DistinctTunnels int
}

// Evaluate records evt against its source's current window and returns a
// ThreatScoreEvent when that source has crossed either threshold and is not
// still within its cooldown.
func (d *GTPUSourceFloodDetector) Evaluate(evt events.NormalizedEvent, now time.Time) (events.ThreatScoreEvent, SourceFloodObservation, bool) {
	if !d.Configured() {
		return events.ThreatScoreEvent{}, SourceFloodObservation{}, false
	}
	// Tunneled traffic only. A packet merely addressed to port 2152 with no
	// valid GTP-U header carries no TEID and belongs to the per-source rate
	// signal the model already has (features.py's rate_norm), not here.
	if evt.Protocol != events.ProtocolGTPU || evt.TEID == 0 || evt.SourceIP == "" {
		return events.ThreatScoreEvent{}, SourceFloodObservation{}, false
	}

	packets, distinct, fired := d.observe(evt.SourceIP, evt.TEID, now)
	observed := SourceFloodObservation{PacketsInWindow: packets, DistinctTunnels: distinct}
	if !fired {
		return events.ThreatScoreEvent{}, observed, false
	}

	return events.ThreatScoreEvent{
		SourceEventID: evt.EventID,
		Namespace:     evt.Namespace,
		PodName:       evt.PodName,
		SourceIP:      evt.SourceIP,
		// Deliberately 0: see the type comment. The finding is about the
		// peer, and naming one of the contributing tunnels would make a
		// per-tunnel mitigation look applicable when it is not.
		TEID: 0,
		// 1.0 for the same reason ModelGTPUTunnelFlood uses it: a
		// deterministic rule has no uncertainty to express, and the most
		// conservative sensitivity tier clamps the effective threshold to
		// exactly 1.00 with a strict `score < threshold` comparison.
		Score:      1.0,
		Model:      ModelGTPUSourceFlood,
		DetectedAt: now.UTC(),
	}, observed, true
}

// report decides whether the observed counts cross either threshold.
func (d *GTPUSourceFloodDetector) report(packets uint32, distinct int) bool {
	if d.cfg.PacketsPerSecond > 0 && packets >= d.cfg.PacketsPerSecond {
		return true
	}
	return d.cfg.DistinctTunnels > 0 && distinct >= d.cfg.DistinctTunnels
}

// observe folds one packet into its source's window and reports the
// post-update counts, plus whether an event should be emitted now (both
// thresholds crossed AND the cooldown elapsed).
func (d *GTPUSourceFloodDetector) observe(sourceIP string, teid uint32, now time.Time) (packets uint32, distinct int, emit bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	w, ok := d.sources[sourceIP]
	if !ok {
		if len(d.sources) >= len(d.order) {
			delete(d.sources, d.order[d.next])
		}
		d.order[d.next] = sourceIP
		d.next = (d.next + 1) % len(d.order)
		w = &sourceWindow{start: now, teids: make(map[uint32]struct{})}
		d.sources[sourceIP] = w
	}

	if now.Sub(w.start) >= d.cfg.Window {
		// Counts reset; lastFired deliberately does not, so the cooldown
		// spans windows rather than being reset by them.
		w.start = now
		w.packets = 0
		w.teids = make(map[uint32]struct{})
	}

	w.packets++
	// Bounded: a TEID set that reaches the cap stops growing, which costs
	// nothing real because the cap is far above any threshold worth
	// configuring -- by the time it is reached the source has already been
	// reported many windows over.
	if len(w.teids) < d.cfg.MaxTunnelsPerSource {
		w.teids[teid] = struct{}{}
	}

	packets, distinct = w.packets, len(w.teids)
	if !d.report(packets, distinct) {
		return packets, distinct, false
	}
	if !w.lastFired.IsZero() && now.Sub(w.lastFired) < d.cfg.Cooldown {
		return packets, distinct, false
	}
	w.lastFired = now
	return packets, distinct, true
}
