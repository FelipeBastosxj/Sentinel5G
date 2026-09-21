// Package detect holds deterministic, non-ML threat detectors that run
// alongside the AI engine rather than inside it.
//
// They exist for the class of anomaly the autoencoder provably cannot catch.
// ROADMAP.md Phase 2.5's in-tunnel-flood gap is the motivating case, and it
// is worth stating precisely because it is not a tuning problem: measured
// against real GTP-U captured from a live Open5GS/UERANSIM core, an in-tunnel
// flood scored 0.0679 — BELOW the 0.0811 held-out normal baseline — for a
// recall of 0/3,348. An anomaly that reconstructs *better* than normal
// traffic cannot be separated by any threshold on reconstruction error. A
// rule can separate it trivially. See
// docs/paper-data/02-ai-training-inference.md §2.4.
//
// What a detector here is NOT allowed to do is bypass policy. Its output is
// an ordinary events.ThreatScoreEvent on the ordinary subject, so it goes
// through exactly the same pkg/controller.ThreatScoreWatcher gating as an ML
// score: policy matching, sensitivity, and autoMitigate. A detection-only
// pilot stays detection-only.
package detect

import (
	"sync"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// ModelGTPUTunnelFlood is what this detector puts in
// ThreatScoreEvent.Model. The "rule:" prefix is the documented marker for a
// deterministic, non-ML score (see pkg/events.ThreatScoreEvent.Model and
// docs/event-model.md).
const ModelGTPUTunnelFlood = "rule:gtpu-tunnel-flood"

// DefaultCooldown bounds how often one tunnel can produce an event. See
// GTPUFloodDetector's doc comment for why this is a cost control rather than
// a correctness one.
const DefaultCooldown = 30 * time.Second

// defaultTrackedTunnels caps how many (source, TEID) pairs the cooldown map
// holds. Fixed capacity with FIFO eviction, the same discipline CLAUDE.md
// requires of the in-kernel maps and for the same reason: the key is
// partly attacker-chosen (a flood can rotate TEIDs), so an unbounded map is
// a memory-growth primitive. Evicting early only costs a duplicate event.
const defaultTrackedTunnels = 4096

// GTPUFloodConfig configures GTPUFloodDetector.
type GTPUFloodConfig struct {
	// Enabled turns the detector off entirely when false.
	Enabled bool

	// PacketsPerSecond is the per-TUNNEL rate at or above which a GTP-U
	// event is treated as a flood. Per tunnel, not per source: on a real N3
	// interface a per-source threshold would have to be set above the
	// combined load of every subscriber on the gNB, which is exactly the
	// aggregation that hides a single-tunnel flood.
	PacketsPerSecond uint32

	// Cooldown is the minimum interval between events for one
	// (source IP, TEID). Zero uses DefaultCooldown.
	Cooldown time.Duration

	// MaxTrackedTunnels caps the cooldown map. Zero uses
	// defaultTrackedTunnels.
	MaxTrackedTunnels int
}

// tunnelID identifies one GTP-U tunnel for cooldown purposes. Source IP is
// part of the key because a TEID is only unique per GTP-U peer, never
// globally — see struct tunnel_key in bpf/packet_filter.c.
type tunnelID struct {
	sourceIP string
	teid     uint32
}

// GTPUFloodDetector reports a GTP-U tunnel whose own packet rate crosses a
// threshold, independent of how much traffic the source IP carries in
// aggregate.
//
// The cooldown is the part worth explaining. Without it, a 3,000 pkt/s flood
// produces 3,000 ThreatScoreEvents per second, each of which drives a
// Status().Update() in ThreatScoreWatcher — a conflict storm against the API
// server and a NATS flood, for no additional information after the first
// event. Note what this is NOT: it is not what makes the pipeline safe under
// duplicate delivery. JetStream is at-least-once and Block/Quarantine are
// idempotent by design (see pkg/controller.ThreatScoreWatcher's doc
// comment); that guarantee is unchanged and this does not lean on it. This
// is purely a cost control layered on top.
//
// Safe for concurrent use: pkg/ingestion.Publisher runs one per node, but a
// future caller with several readers shouldn't have to discover a data race.
type GTPUFloodDetector struct {
	cfg GTPUFloodConfig

	mu        sync.Mutex
	lastFired map[tunnelID]time.Time
	// order is a ring buffer of keys in insertion order, so eviction is O(1)
	// rather than a scan for the oldest entry on every insert once full.
	order []tunnelID
	next  int
}

// NewGTPUFloodDetector returns a detector for cfg.
func NewGTPUFloodDetector(cfg GTPUFloodConfig) *GTPUFloodDetector {
	capacity := cfg.MaxTrackedTunnels
	if capacity <= 0 {
		capacity = defaultTrackedTunnels
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = DefaultCooldown
	}

	return &GTPUFloodDetector{
		cfg:       cfg,
		lastFired: make(map[tunnelID]time.Time, capacity),
		order:     make([]tunnelID, capacity),
	}
}

// Evaluate returns a ThreatScoreEvent and true when evt is a GTP-U tunnel
// flood that hasn't already been reported for this (source IP, TEID) within
// the cooldown.
//
// All four conditions are required, and the TEID one carries weight beyond
// the obvious: requiring a non-zero TEID is what makes it structurally
// impossible for a capture path that cannot see tunnels — pkg/hubble reads
// flow summaries, pkg/falco traces syscalls, and both always emit 0 — to
// trip this detector. That is a guarantee, not a coincidence, and both
// packages have tests pinning it.
func (d *GTPUFloodDetector) Evaluate(evt events.NormalizedEvent, now time.Time) (events.ThreatScoreEvent, bool) {
	if !d.cfg.Enabled || d.cfg.PacketsPerSecond == 0 {
		return events.ThreatScoreEvent{}, false
	}
	if evt.Protocol != events.ProtocolGTPU || evt.TEID == 0 {
		return events.ThreatScoreEvent{}, false
	}
	if evt.TunnelRatePerSecond < float64(d.cfg.PacketsPerSecond) {
		return events.ThreatScoreEvent{}, false
	}
	if !d.claim(tunnelID{sourceIP: evt.SourceIP, teid: evt.TEID}, now) {
		return events.ThreatScoreEvent{}, false
	}

	return events.ThreatScoreEvent{
		SourceEventID: evt.EventID,
		Namespace:     evt.Namespace,
		PodName:       evt.PodName,
		SourceIP:      evt.SourceIP,
		// The whole point of this detector is that it knows WHICH tunnel;
		// carrying it through is what lets the mitigation be equally
		// precise (Actions.EbpfBlockTunnel).
		TEID: evt.TEID,
		// 1.0, not the configured threshold scaled to something: a
		// deterministic rule has no uncertainty to express. It also has to
		// clear the most conservative policy tier, where the effective
		// threshold clamps to exactly 1.00 and the comparison is a strict
		// `score < threshold` (pkg/controller.ThreatScoreWatcher.applyPolicy)
		// -- anything below 1.0 would silently never fire at low sensitivity.
		Score:      1.0,
		Model:      ModelGTPUTunnelFlood,
		DetectedAt: now.UTC(),
	}, true
}

// claim records that id fired at now, returning false if it is still within
// the cooldown from its last event.
func (d *GTPUFloodDetector) claim(id tunnelID, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if last, seen := d.lastFired[id]; seen {
		if now.Sub(last) < d.cfg.Cooldown {
			return false
		}
		d.lastFired[id] = now
		return true
	}

	if len(d.lastFired) >= len(d.order) {
		delete(d.lastFired, d.order[d.next])
	}
	d.order[d.next] = id
	d.next = (d.next + 1) % len(d.order)
	d.lastFired[id] = now
	return true
}
