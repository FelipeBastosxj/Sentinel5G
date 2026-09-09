// Package ingestion implements Layer 2 (see docs/architecture.md): it turns
// raw kernel-observed signaling events (pkg/ebpf.SignalingEvent, read from
// bpf/packet_filter.c's signaling_events ring buffer) into the wire-schema
// events.NormalizedEvent and publishes them on NATS_EVENTS_SUBJECT for the
// AI engine (cmd/ai-engine) to score.
//
// Before this package existed, nothing in the repository actually produced
// a NormalizedEvent from real traffic: bpf/packet_filter.c exported only
// the blocklist and signal_rate maps, and every scoring test exercised the
// AI engine with a synthetic or hand-published event instead. This is the
// bridge that was missing.
package ingestion

import (
	"net"

	"github.com/google/uuid"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/controller"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// SignalRateLookup returns the current rate (packets in the active window)
// for (sourceIP, destPort), mirroring ebpf.Loader.SignalRate — destPort
// selects signal_rate vs scan_rate kernel-side, see that method's doc.
// Accepting this as a function (rather than a *ebpf.Loader) keeps this
// package testable without a real kernel attachment.
type SignalRateLookup func(sourceIP net.IP, destPort uint16) (count uint32, ok bool)

// FromSignalingEvent converts a raw kernel observation into a
// events.NormalizedEvent (see docs/event-model.md), resolving the source IP
// to its owning Pod via podIndex when known, and its current signaling rate
// via rateLookup.
//
// A source IP with no PodIPIndex entry (traffic from outside the cluster,
// or a Pod PodIPIndexer hasn't observed yet) still produces an event, with
// Namespace/PodName left empty — an external probe against a telecom-facing
// Service is exactly the kind of traffic this project exists to catch, and
// it will never have a cluster-local Pod to attribute to. Callers that only
// want to act on cluster-local traffic should filter on those fields being
// non-empty themselves; this package doesn't drop unattributed events.
func FromSignalingEvent(evt ebpf.SignalingEvent, podIndex *controller.PodIPIndex, rateLookup SignalRateLookup, nodeName string) events.NormalizedEvent {
	ref, _ := podIndex.Lookup(evt.SourceIP.String())

	var ratePerSecond float64
	if count, ok := rateLookup(evt.SourceIP, evt.DestPort); ok {
		// signal_rate's window is fixed at SIGNALING_RATE_WINDOW_NS (1s, see
		// bpf/headers/common.h), so the window's packet count already is a
		// per-second rate; no scaling needed unless that window ever
		// becomes configurable.
		ratePerSecond = float64(count)
	}

	return events.NormalizedEvent{
		EventID:       uuid.NewString(),
		ObservedAt:    evt.ObservedAt,
		Namespace:     ref.Namespace,
		PodName:       ref.Name,
		NodeName:      nodeName,
		SourceIP:      evt.SourceIP.String(),
		DestIP:        evt.DestIP.String(),
		DestPort:      evt.DestPort,
		Protocol:      protocolFromSignal(evt.Protocol),
		PayloadSize:   uint32(evt.PayloadSize),
		RatePerSecond: ratePerSecond,
		Malformed:     evt.Malformed,
		VLANID:        evt.VLANID,
	}
}

func protocolFromSignal(p ebpf.SignalProtocol) events.Protocol {
	switch p {
	case ebpf.SignalProtoGTPU:
		return events.ProtocolGTPU
	case ebpf.SignalProtoSIP:
		return events.ProtocolSIP
	case ebpf.SignalProtoPortScan:
		return events.ProtocolPortScan
	default:
		return events.ProtocolUnknown
	}
}
