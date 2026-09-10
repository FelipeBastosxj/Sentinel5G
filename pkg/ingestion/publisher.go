package ingestion

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/controller"
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
