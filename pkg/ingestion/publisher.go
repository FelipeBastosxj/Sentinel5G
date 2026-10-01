package ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/controller"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/detect"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Publisher reads SignalingEvents from Source, converts each to a
// NormalizedEvent (via FromSignalingEvent), and publishes it on Bus/Subject
// — the running half of this package's Layer 1 -> Layer 2 bridge. Implements
// manager.Runnable so its lifecycle is tied to the controller-runtime
// manager, the same as pkg/controller.ThreatScoreWatcher's relationship to
// Layer 4.
type Publisher struct {
	Source   ebpf.EventSource
	PodIndex *controller.PodIPIndex
	Bus      *events.Connector
	Subject  string
	NodeName string
	Log      logr.Logger

	// TunnelFlood is the deterministic GTP-U tunnel-flood detector
	// (pkg/detect). It runs here, inline on the event this Publisher just
	// built, rather than as a second NATS consumer on Subject: a separate
	// consumer would compete with the AI engine's own durable queue group,
	// double the bus load, and gain nothing -- the decision needs only the
	// single event in hand.
	//
	// Running it inside Publisher also puts it on the right side of leader
	// election. Publisher is deliberately NOT leader-gated (see
	// NeedLeaderElection below) because each replica reads its own node's
	// ring buffer; a leader-gated detector would be blind to every
	// non-leader node's tunnels.
	//
	// Nil disables it, the same convention Blocklist/Mesh use elsewhere.
	TunnelFlood *detect.GTPUFloodDetector

	// SourceFlood is the per-PEER counterpart of TunnelFlood: the same
	// inline, non-leader-gated placement, catching the aggregate a
	// per-tunnel threshold structurally cannot see (ROADMAP.md Phase 4's
	// evasion item). Nil disables it.
	SourceFlood *detect.GTPUSourceFloodDetector

	// ThreatsSubject is where TunnelFlood's and SourceFlood's scores are
	// published -- the same
	// subject the AI engine publishes to, consumed by the same
	// pkg/controller.ThreatScoreWatcher, so a rule-sourced score is subject
	// to identical policy/sensitivity/autoMitigate gating.
	ThreatsSubject string

	// BatchSize coalesces up to this many NormalizedEvents into one NATS
	// message instead of publishing one per packet -- the bus is the
	// design's first throughput ceiling, not eBPF (ROADMAP.md Phase 4). 0 or
	// 1 publishes per event (the pre-batching behaviour). The inline
	// detectors below are NOT batched: they run per event the moment it
	// arrives, so batching the telemetry publish never delays detection.
	BatchSize int

	// FlushInterval bounds how long a partial batch waits before it is
	// published anyway, so a quiet period does not strand events in the
	// buffer. Ignored when BatchSize <= 1.
	FlushInterval time.Duration

	// Now returns the current time; nil uses time.Now. Overridden in tests.
	Now func() time.Time
}

func (p *Publisher) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Start implements manager.Runnable. The eBPF ring buffer is drained
// regardless of whether NATS is connected yet -- SignalingEvents starts
// immediately, not after Bus.Wait -- since a bounded kernel-side ring
// buffer left undrained during a NATS outage is worse than dropping events
// for that window (see Bus.Connected's doc comment).
func (p *Publisher) Start(ctx context.Context) error {
	stream, err := p.Source.SignalingEvents(ctx)
	if err != nil {
		return fmt.Errorf("start signaling event stream: %w", err)
	}

	// The flush timer only matters when batching; with batching off it is
	// created but never consulted (batch is flushed inline below).
	flushEvery := p.FlushInterval
	if flushEvery <= 0 {
		flushEvery = time.Second
	}
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	batch := make([]events.NormalizedEvent, 0, p.batchCap())

	for {
		select {
		case evt, ok := <-stream:
			if !ok {
				p.flush(&batch) // publish whatever is buffered before exiting
				return nil
			}
			bus, connected := p.Bus.Bus()
			if !connected {
				p.Log.V(1).Info("dropping normalized event: NATS not yet connected",
					"sourceIp", evt.SourceIP.String(), "destPort", evt.DestPort)
				continue
			}
			normalized := FromSignalingEvent(evt, p.PodIndex, p.Source.SignalRate, p.NodeName)

			// Detectors run per event, immediately, BEFORE any batching, so a
			// flood is acted on the instant its packet arrives rather than
			// when a telemetry batch happens to flush. Both see every event
			// and neither short-circuits the other: they answer different
			// questions (is THIS tunnel flooding / is this PEER abusive in
			// aggregate), and a flood that trips both produces one score of
			// each, scoped differently.
			now := p.now()
			p.evaluateTunnelFlood(bus, normalized, now)
			p.evaluateSourceFlood(bus, normalized, now)

			// Telemetry publish: batched (the bus ceiling), or per-event when
			// batching is off.
			if p.batchCap() <= 1 {
				if err := bus.PublishNormalizedEvent(p.Subject, normalized); err != nil {
					p.Log.Error(err, "failed to publish normalized event",
						"sourceIp", normalized.SourceIP, "destPort", normalized.DestPort)
				}
				continue
			}
			batch = append(batch, normalized)
			if len(batch) >= p.batchCap() {
				p.flush(&batch)
			}
		case <-ticker.C:
			p.flush(&batch)
		case <-ctx.Done():
			p.flush(&batch)
			return nil
		}
	}
}

// evaluateTunnelFlood runs the per-tunnel rule and publishes its score.
func (p *Publisher) evaluateTunnelFlood(bus *events.Bus, normalized events.NormalizedEvent, now time.Time) {
	if p.TunnelFlood == nil {
		return
	}
	score, fired := p.TunnelFlood.Evaluate(normalized, now)
	if !fired {
		return
	}
	p.Log.Info("gtpu tunnel flood detected",
		"sourceIp", normalized.SourceIP, "teid", normalized.TEID,
		"tunnelRatePerSecond", normalized.TunnelRatePerSecond)
	if err := bus.PublishThreatScore(p.ThreatsSubject, score); err != nil {
		p.Log.Error(err, "failed to publish tunnel flood threat score",
			"sourceIp", normalized.SourceIP, "teid", normalized.TEID)
	}
}

// evaluateSourceFlood runs the per-peer rule and publishes its score. The
// log line carries both counts because the verdict is source-wide and the
// action an operator takes on it drops every subscriber behind that peer --
// "which half fired, and by how much" is the minimum needed to justify
// that.
func (p *Publisher) evaluateSourceFlood(bus *events.Bus, normalized events.NormalizedEvent, now time.Time) {
	if p.SourceFlood == nil {
		return
	}
	score, observed, fired := p.SourceFlood.Evaluate(normalized, now)
	if !fired {
		return
	}
	p.Log.Info("gtpu source flood detected (aggregate across tunnels)",
		"sourceIp", normalized.SourceIP,
		"packetsInWindow", observed.PacketsInWindow,
		"distinctTunnels", observed.DistinctTunnels)
	if err := bus.PublishThreatScore(p.ThreatsSubject, score); err != nil {
		p.Log.Error(err, "failed to publish source flood threat score",
			"sourceIp", normalized.SourceIP)
	}
}

// batchCap is the effective batch size (0/1 both mean "no batching").
func (p *Publisher) batchCap() int {
	if p.BatchSize < 1 {
		return 1
	}
	return p.BatchSize
}

// flush publishes the buffered events as one batch message and resets the
// buffer. A publish failure is logged, not retried: the events are telemetry
// for the ML path, the inline detectors already acted on anything urgent, and
// a bounded kernel ring buffer left undrained waiting on a retry is worse
// than dropping a telemetry batch (the same trade PublishNormalizedEvent's
// error handling already makes).
func (p *Publisher) flush(batch *[]events.NormalizedEvent) {
	if len(*batch) == 0 {
		return
	}
	bus, connected := p.Bus.Bus()
	if connected {
		if err := bus.PublishNormalizedEventBatch(p.Subject, *batch); err != nil {
			p.Log.Error(err, "failed to publish normalized event batch", "events", len(*batch))
		}
	} else {
		p.Log.V(1).Info("dropping normalized event batch: NATS not connected", "events", len(*batch))
	}
	*batch = (*batch)[:0]
}

// NeedLeaderElection implements manager.LeaderElectionRunnable. Publisher
// reads a per-node eBPF ring buffer (bpf/packet_filter.c's signaling_events,
// attached to whatever node this specific pod landed on) — it has to run on
// every replica, not just the leader, or every non-leader node's real
// signaling traffic goes silently unpublished. Without this override,
// controller-runtime's default for a plain manager.Runnable is to already
// be leader-gated (see runnable_group.go's Add()), which would silently
// drop every non-leader node's events with no error, no log, no metric —
// found by reading controller-runtime's source directly, not by symptom.
func (p *Publisher) NeedLeaderElection() bool { return false }

var _ manager.Runnable = (*Publisher)(nil)
var _ manager.LeaderElectionRunnable = (*Publisher)(nil)
