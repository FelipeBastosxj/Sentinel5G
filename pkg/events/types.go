// Package events defines the wire schema shared between the eBPF/ingestion
// side, the Python AI engine (cmd/ai-engine), and the Go operator (pkg/controller).
//
// This is the canonical Go representation of the schema documented in
// docs/event-model.md. Any change here MUST be mirrored there and in
// cmd/ai-engine/sentinel_ai/features.py.
package events

import "time"

// Protocol identifies the telecom/transport protocol a NormalizedEvent was
// classified as by the capture layer.
type Protocol string

const (
	ProtocolGTPU     Protocol = "GTP-U"
	ProtocolSIP      Protocol = "SIP"
	ProtocolSMPP     Protocol = "SMPP"
	ProtocolHTTP2    Protocol = "HTTP2"
	ProtocolPortScan Protocol = "PORT_SCAN"
	ProtocolUnknown  Protocol = "UNKNOWN"
)

// NormalizedEvent is the common representation of a single capture-layer
// observation after normalization by pkg/ingestion (Layer 2), published on
// NATS_EVENTS_SUBJECT ("sentinel5g.events.normalized" by default).
//
// It intentionally carries a small, fixed set of fields: this is a
// high-volume telemetry event, not a full packet capture.
type NormalizedEvent struct {
	// EventID is a unique identifier assigned by the ingestion layer.
	EventID string `json:"eventId"`

	// ObservedAt is when the underlying packet/message was captured.
	ObservedAt time.Time `json:"observedAt"`

	// Namespace and PodName locate the workload the event was captured for.
	Namespace string `json:"namespace"`
	PodName   string `json:"podName"`
	NodeName  string `json:"nodeName"`

	SourceIP string `json:"sourceIp"`
	DestIP   string `json:"destIp"`
	DestPort uint16 `json:"destPort"`

	Protocol Protocol `json:"protocol"`

	// PayloadSize is the size in bytes of the signaling payload observed,
	// except for Protocol == ProtocolPortScan, where the eBPF layer
	// repurposes this field to carry the distinct-destination-port count
	// that triggered the scan detection instead of a byte size (see
	// bpf/packet_filter.c's port_scan/track_port_scan()).
	PayloadSize uint32 `json:"payloadSize"`

	// RatePerSecond is the eBPF-side rolling rate of events with the same
	// (SourceIP, Protocol) tuple, used as a signaling-storm signal.
	RatePerSecond float64 `json:"ratePerSecond"`

	// Malformed is true when the eBPF parser could not fully validate the
	// protocol framing (a strong anomaly signal on its own).
	Malformed bool `json:"malformed"`

	// VLANID is the 802.1Q VLAN ID the packet was tagged with, or 0 for an
	// untagged frame.
	VLANID uint16 `json:"vlanId"`

	// TEID is the GTP-U Tunnel Endpoint Identifier the packet belongs to,
	// or 0 when the capture path could not determine one. 0 is the explicit
	// "no tunnel identity" sentinel, and consumers MUST read it that way
	// rather than as tunnel number zero: the non-eBPF capture paths
	// (pkg/hubble, pkg/falco) can never produce a TEID at all, so they
	// always emit 0.
	TEID uint32 `json:"teid"`

	// TunnelRatePerSecond is the eBPF-side rolling rate for this
	// (SourceIP, TEID) tuple, as opposed to RatePerSecond's per-SourceIP
	// aggregate. The distinction is the point: on a real N3 interface every
	// subscriber's user-plane traffic arrives from the same source IP (the
	// peer gNB/UPF), so RatePerSecond alone cannot isolate one tunnel
	// flooding from ordinary combined load — the measured detection gap in
	// ROADMAP.md Phase 2.5. Always 0 when TEID is 0.
	TunnelRatePerSecond float64 `json:"tunnelRatePerSecond"`
}

// ThreatScoreEvent is produced by the AI engine after scoring one or more
// NormalizedEvents, published on NATS_THREATS_SUBJECT
// ("sentinel5g.threats.scored" by default) and consumed by pkg/controller.
type ThreatScoreEvent struct {
	// SourceEventID references the NormalizedEvent.EventID that produced
	// this score (or the last event of a scored window).
	SourceEventID string `json:"sourceEventId"`

	Namespace string `json:"namespace"`
	PodName   string `json:"podName"`
	SourceIP  string `json:"sourceIp"`

	// Score is the reconstruction-error-derived anomaly score, normalized
	// to [0.0, 1.0], where 1.0 is maximally anomalous.
	Score float64 `json:"score"`

	// Model identifies the scoring model/version (e.g. "autoencoder-v1").
	// A "rule:" prefix means the score came from a deterministic, non-ML
	// detector (pkg/detect) rather than the autoencoder — e.g.
	// "rule:gtpu-tunnel-flood". Consumers that need to tell the two apart
	// should match on that prefix; everything else about the event, and the
	// entire pkg/controller.ThreatScoreWatcher path it drives, is
	// deliberately identical for both, so a rule-sourced score is subject to
	// exactly the same policy/sensitivity/autoMitigate gating as an ML one.
	Model string `json:"model"`

	DetectedAt time.Time `json:"detectedAt"`
}
