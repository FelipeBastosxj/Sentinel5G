// Package hubble is the Cilium-native alternative to pkg/ebpf+pkg/ingestion:
// instead of attaching bpf/packet_filter.c's own XDP program to a node
// interface, it consumes Cilium's already-running dataplane visibility via
// Hubble's gRPC Observer API (typically Hubble Relay, which aggregates the
// per-node Hubble agents' flow streams into one) and turns matching flows
// into events.NormalizedEvent, published on the same NATS subject the eBPF
// path uses. See docs/integrations.md's "Capture layer" section for when to
// choose this over standalone XDP: two independent XDP programs can't both
// own the same attachment point, so a cluster already running Cilium as its
// CNI should use this instead of also attaching bpf/packet_filter.c.
//
// Unlike pkg/falco (whose Alert type mirrors an HTTP JSON payload) and
// pkg/ebpf (whose structs mirror a hand-maintained wire-format C struct),
// this package's input is Cilium's own official generated protobuf/gRPC
// client (github.com/cilium/cilium/api/v1/{flow,observer}) -- there is no
// "unstructured" analog for a gRPC streaming API the way pkg/mesh avoids a
// vendored CRD client for Istio/Cilium/Linkerd, and hand-rolling protobuf
// wire decoding for Hubble's flow.Flow message would trade a well-known,
// versioned client for a much larger and riskier hand-maintenance surface.
//
// This package was NOT verified against a live Hubble/Cilium deployment --
// see docs/integrations.md for why (this project's real WSL2 test cluster
// runs flannel, not Cilium, and installing Cilium as its CNI would risk
// breaking the Istio/NATS/Open5GS environment already relied on for other
// testing) and what was verified instead: an in-process gRPC server
// implementing the real observer.ObserverServer interface, exercised
// end-to-end over a real (bufconn) gRPC connection with hand-built but
// schema-accurate flow.Flow messages.
package hubble

import (
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/google/uuid"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Signaling ports this package watches for, matching
// bpf/headers/common.h's GTPU_PORT/SIP_PORT exactly -- same telecom
// signaling scope as the eBPF path, not a general-purpose flow exporter.
// bpf/packet_filter.c's is_signaling_port() only ever inspects UDP traffic
// (IPPROTO_UDP is required before either port is even checked), so this
// mirrors that and ignores TCP flows on these same port numbers.
const (
	gtpuPort = 2152
	sipPort  = 5060
)

// FromFlow converts a single Hubble flow.Flow into an events.NormalizedEvent.
// ok is false for any flow that isn't a UDP packet to gtpuPort/sipPort --
// Hubble reports on every flow in the cluster, not just telecom signaling
// traffic, and publishing all of them would flood NATS with traffic this
// project has no model for; callers must skip publishing when ok is false.
//
// Namespace/PodName are read directly from Flow.Source (Cilium already
// resolves the source endpoint's identity), unlike pkg/ingestion.
// FromSignalingEvent, which has to look the source IP up in a separately
// maintained PodIPIndex because bpf/packet_filter.c's ring buffer records
// carry only a raw IP address, not endpoint identity.
//
// PayloadSize, RatePerSecond, Malformed, and VLANID are left at their zero
// value: Hubble's flow summaries don't carry the underlying packet's
// payload byte count, a windowed rate (that's this project's own
// scan_rate/signal_rate accounting, which has no Hubble equivalent),
// protocol-framing validation, or an exposed VLAN tag.
func FromFlow(f *flowpb.Flow) (events.NormalizedEvent, bool) {
	udp := f.GetL4().GetUDP()
	if udp == nil {
		return events.NormalizedEvent{}, false
	}

	// classifyPort compares in the uint32 domain udp.GetDestinationPort()
	// is already in (Hubble's protobuf schema doesn't itself bound this
	// field to a real 16-bit port range) and, on a match, returns one of
	// the two known-safe uint16 constants below directly -- never a
	// narrowing conversion of the unbounded raw value itself.
	destPort, protocol, ok := classifyPort(udp.GetDestinationPort())
	if !ok {
		return events.NormalizedEvent{}, false
	}

	observedAt := time.Now().UTC()
	if ts := f.GetTime(); ts != nil {
		observedAt = ts.AsTime()
	}

	return events.NormalizedEvent{
		EventID:    uuid.NewString(),
		ObservedAt: observedAt,
		Namespace:  f.GetSource().GetNamespace(),
		PodName:    f.GetSource().GetPodName(),
		NodeName:   f.GetNodeName(),
		SourceIP:   f.GetIP().GetSource(),
		DestIP:     f.GetIP().GetDestination(),
		DestPort:   destPort,
		Protocol:   protocol,
	}, true
}

func classifyPort(rawPort uint32) (uint16, events.Protocol, bool) {
	switch rawPort {
	case gtpuPort:
		return gtpuPort, events.ProtocolGTPU, true
	case sipPort:
		return sipPort, events.ProtocolSIP, true
	default:
		return 0, events.ProtocolUnknown, false
	}
}
