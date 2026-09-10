package hubble

import (
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

func TestFromFlow_MapsGTPUFields(t *testing.T) {
	observedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	f := &flowpb.Flow{
		Time:     timestamppb.New(observedAt),
		NodeName: "worker-node-1",
		IP: &flowpb.IP{
			Source:      "10.42.0.7",
			Destination: "10.42.0.9",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_UDP{
				UDP: &flowpb.UDP{DestinationPort: 2152, SourcePort: 55123},
			},
		},
		Source: &flowpb.Endpoint{
			Namespace: "telecom-core",
			PodName:   "amf-0",
		},
	}

	got, ok := FromFlow(f)
	if !ok {
		t.Fatal("expected a GTP-U flow to match")
	}
	if got.EventID == "" {
		t.Fatal("expected a non-empty generated eventId")
	}
	if !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("expected observedAt %v, got %v", observedAt, got.ObservedAt)
	}
	if got.NodeName != "worker-node-1" {
		t.Fatalf("expected nodeName %q, got %q", "worker-node-1", got.NodeName)
	}
	if got.Namespace != "telecom-core" || got.PodName != "amf-0" {
		t.Fatalf("expected resolved namespace/podName, got %q/%q", got.Namespace, got.PodName)
	}
	if got.SourceIP != "10.42.0.7" || got.DestIP != "10.42.0.9" {
		t.Fatalf("expected source/dest IPs to round-trip, got %q/%q", got.SourceIP, got.DestIP)
	}
	if got.DestPort != 2152 {
		t.Fatalf("expected destPort 2152, got %d", got.DestPort)
	}
	if got.Protocol != events.ProtocolGTPU {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolGTPU, got.Protocol)
	}
}

func TestFromFlow_MapsSIPFields(t *testing.T) {
	f := &flowpb.Flow{
		IP: &flowpb.IP{Source: "10.42.0.7", Destination: "10.42.0.9"},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{DestinationPort: 5060}},
		},
	}

	got, ok := FromFlow(f)
	if !ok {
		t.Fatal("expected a SIP flow to match")
	}
	if got.Protocol != events.ProtocolSIP {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolSIP, got.Protocol)
	}
}

func TestFromFlow_NonSignalingUDPPortSkipped(t *testing.T) {
	f := &flowpb.Flow{
		IP: &flowpb.IP{Source: "10.42.0.7", Destination: "10.42.0.9"},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{DestinationPort: 4444}},
		},
	}

	if _, ok := FromFlow(f); ok {
		t.Fatal("expected a non-signaling UDP port to be skipped")
	}
}

func TestFromFlow_TCPOnSignalingPortSkipped(t *testing.T) {
	// bpf/packet_filter.c's is_signaling_port() only ever inspects UDP
	// traffic (IPPROTO_UDP is required before either port is even
	// checked) -- a TCP flow to port 5060 must be skipped the same way,
	// not accidentally matched just because the port number lines up.
	f := &flowpb.Flow{
		IP: &flowpb.IP{Source: "10.42.0.7", Destination: "10.42.0.9"},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 5060}},
		},
	}

	if _, ok := FromFlow(f); ok {
		t.Fatal("expected a TCP flow on a signaling port number to be skipped")
	}
}

func TestFromFlow_NoLayer4Skipped(t *testing.T) {
	f := &flowpb.Flow{IP: &flowpb.IP{Source: "10.42.0.7", Destination: "10.42.0.9"}}

	if _, ok := FromFlow(f); ok {
		t.Fatal("expected a flow with no L4 info to be skipped")
	}
}

func TestFromFlow_NilTimeDefaultsToNow(t *testing.T) {
	f := &flowpb.Flow{
		L4: &flowpb.Layer4{Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{DestinationPort: 2152}}},
	}

	before := time.Now().UTC()
	got, ok := FromFlow(f)
	after := time.Now().UTC()

	if !ok {
		t.Fatal("expected the flow to match")
	}
	if got.ObservedAt.Before(before) || got.ObservedAt.After(after) {
		t.Fatalf("expected observedAt to default to now() when Flow.Time is nil, got %v (window %v-%v)", got.ObservedAt, before, after)
	}
}

func TestFromFlow_MissingSourceEndpointStillProducesEvent(t *testing.T) {
	// Mirrors pkg/ingestion.FromSignalingEvent's and pkg/falco.Bridge.
	// FromAlert's identical tolerance: a flow with no resolved Source
	// endpoint identity (e.g. traffic from outside the cluster) must
	// still produce a NormalizedEvent, not be dropped.
	f := &flowpb.Flow{
		IP: &flowpb.IP{Source: "203.0.113.7", Destination: "10.42.0.9"},
		L4: &flowpb.Layer4{Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{DestinationPort: 5060}}},
	}

	got, ok := FromFlow(f)
	if !ok {
		t.Fatal("expected the flow to match")
	}
	if got.Namespace != "" || got.PodName != "" {
		t.Fatalf("expected empty namespace/podName for an unattributed source, got %q/%q", got.Namespace, got.PodName)
	}
	if got.SourceIP != "203.0.113.7" {
		t.Fatalf("expected sourceIp to still be populated, got %q", got.SourceIP)
	}
}

func TestFromFlow_ZeroValueFieldsWithNoHubbleEquivalent(t *testing.T) {
	f := &flowpb.Flow{
		L4: &flowpb.Layer4{Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{DestinationPort: 2152}}},
	}

	got, _ := FromFlow(f)
	if got.PayloadSize != 0 || got.RatePerSecond != 0 || got.Malformed || got.VLANID != 0 {
		t.Fatalf("expected fields with no Hubble flow equivalent to stay at zero value, got %+v", got)
	}
}
