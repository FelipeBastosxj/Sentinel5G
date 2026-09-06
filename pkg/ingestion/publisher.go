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
	Bus      *events.Bus
	Subject  string
	NodeName string
	Log      logr.Logger
}

// Start implements manager.Runnable.
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
			normalized := FromSignalingEvent(evt, p.PodIndex, p.Source.SignalRate, p.NodeName)
			if err := p.Bus.PublishNormalizedEvent(p.Subject, normalized); err != nil {
				p.Log.Error(err, "failed to publish normalized event",
					"sourceIp", normalized.SourceIP, "destPort", normalized.DestPort)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

var _ manager.Runnable = (*Publisher)(nil)
