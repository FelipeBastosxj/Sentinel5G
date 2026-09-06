package ingestion

import (
	"net"
	"testing"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/controller"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

func TestFromSignalingEvent_ResolvesKnownPod(t *testing.T) {
	podIndex := controller.NewPodIPIndex()
	podIndex.Put("10.42.0.7", controller.PodRef{Namespace: "telecom-core", Name: "amf-0"})

	observedAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	raw := ebpf.SignalingEvent{
		ObservedAt:  observedAt,
		SourceIP:    net.ParseIP("10.42.0.7"),
		DestIP:      net.ParseIP("10.42.0.9"),
		DestPort:    2152,
		PayloadSize: 128,
		Protocol:    ebpf.SignalProtoGTPU,
		Malformed:   false,
	}
	rate := func(net.IP) (uint32, bool) { return 42, true }

	got := FromSignalingEvent(raw, podIndex, rate, "node-1")

	if got.Namespace != "telecom-core" || got.PodName != "amf-0" {
		t.Fatalf("expected resolved namespace/podName, got %q/%q", got.Namespace, got.PodName)
	}
	if got.NodeName != "node-1" {
		t.Fatalf("expected nodeName %q, got %q", "node-1", got.NodeName)
	}
	if got.SourceIP != "10.42.0.7" || got.DestIP != "10.42.0.9" {
		t.Fatalf("expected source/dest IPs to round-trip, got %q/%q", got.SourceIP, got.DestIP)
	}
	if got.Protocol != events.ProtocolGTPU {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolGTPU, got.Protocol)
	}
	if got.RatePerSecond != 42 {
		t.Fatalf("expected ratePerSecond 42, got %v", got.RatePerSecond)
	}
	if !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("expected observedAt %v, got %v", observedAt, got.ObservedAt)
	}
	if got.EventID == "" {
		t.Fatal("expected a non-empty generated eventId")
	}
}

func TestFromSignalingEvent_UnknownSourceStillProducesEvent(t *testing.T) {
	podIndex := controller.NewPodIPIndex() // empty: nothing registered

	raw := ebpf.SignalingEvent{
		ObservedAt: time.Now().UTC(),
		SourceIP:   net.ParseIP("203.0.113.7"), // external attacker, not a cluster Pod
		DestIP:     net.ParseIP("10.42.0.9"),
		DestPort:   5060,
		Protocol:   ebpf.SignalProtoSIP,
	}
	rate := func(net.IP) (uint32, bool) { return 0, false }

	got := FromSignalingEvent(raw, podIndex, rate, "node-1")

	if got.Namespace != "" || got.PodName != "" {
		t.Fatalf("expected empty namespace/podName for an unattributed source, got %q/%q", got.Namespace, got.PodName)
	}
	if got.Protocol != events.ProtocolSIP {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolSIP, got.Protocol)
	}
	if got.RatePerSecond != 0 {
		t.Fatalf("expected ratePerSecond 0 when rateLookup reports no entry, got %v", got.RatePerSecond)
	}
}

func TestFromSignalingEvent_MalformedAndUnknownProtocol(t *testing.T) {
	podIndex := controller.NewPodIPIndex()
	raw := ebpf.SignalingEvent{
		ObservedAt: time.Now().UTC(),
		SourceIP:   net.ParseIP("10.42.0.7"),
		DestIP:     net.ParseIP("10.42.0.9"),
		DestPort:   0,
		Protocol:   ebpf.SignalProtoUnknown,
		Malformed:  true,
	}
	rate := func(net.IP) (uint32, bool) { return 0, false }

	got := FromSignalingEvent(raw, podIndex, rate, "node-1")

	if !got.Malformed {
		t.Fatal("expected Malformed to propagate through")
	}
	if got.Protocol != events.ProtocolUnknown {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolUnknown, got.Protocol)
	}
}
