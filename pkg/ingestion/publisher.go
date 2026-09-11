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

	// ThreatsSubject is where TunnelFlood's scores are published -- the same
	// subject the AI engine publishes to, consumed by the same
	// pkg/controller.ThreatScoreWatcher, so a rule-sourced score is subject
	// to identical policy/sensitivity/autoMitigate gating.
	ThreatsSubject string

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

	for {
		select {
		case evt, ok := <-stream:
			if !ok {
				return nil
			}
			bus, connected := p.Bus.Bus()
			if !connected {
				p.Log.V(1).Info("dropping normalized event: NATS not yet connected",
					"sourceIp", evt.SourceIP.String(), "destPort", evt.DestPort)
				continue
			}
			normalized := FromSignalingEvent(evt, p.PodIndex, p.Source.SignalRate, p.NodeName)
			if err := bus.PublishNormalizedEvent(p.Subject, normalized); err != nil {
				p.Log.Error(err, "failed to publish normalized event",
					"sourceIp", normalized.SourceIP, "destPort", normalized.DestPort)
			}

			// Published regardless of whether the NormalizedEvent above
			// succeeded: the two are independent statements, and losing a
			// real mitigation signal because a telemetry publish failed would
			// be the wrong trade.
			if p.TunnelFlood == nil {
				continue
			}
			score, fired := p.TunnelFlood.Evaluate(normalized, p.now())
			if !fired {
				continue
			}
			p.Log.Info("gtpu tunnel flood detected",
				"sourceIp", normalized.SourceIP, "teid", normalized.TEID,
				"tunnelRatePerSecond", normalized.TunnelRatePerSecond)
			if err := bus.PublishThreatScore(p.ThreatsSubject, score); err != nil {
				p.Log.Error(err, "failed to publish tunnel flood threat score",
					"sourceIp", normalized.SourceIP, "teid", normalized.TEID)
			}
		case <-ctx.Done():
			return nil
		}
	}
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
