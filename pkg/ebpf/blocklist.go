// Package ebpf loads and drives the XDP program in bpf/packet_filter.c,
// exposing a small, platform-independent interface (BlocklistUpdater) that
// pkg/controller uses to react to threat scores. The real cilium/ebpf-based
// implementation only builds on Linux (see loader_linux.go); a no-op stub
// (loader_other.go) keeps the module buildable on non-Linux dev machines.
package ebpf

import (
	"context"
	"fmt"
	"net"
	"time"
)

// BlocklistUpdater pushes/removes IPv4 or IPv6 addresses from the
// "blocklist"/"blocklist_v6" BPF maps that bpf/packet_filter.c consults
// before making its XDP verdict — the implementation picks the map matching
// ip's address family (see ipv4Key/ipv6Key).
type BlocklistUpdater interface {
	// Block causes packets sourced from ip to be dropped at the XDP layer.
	Block(ip net.IP) error
	// Unblock removes a previously blocked ip, restoring normal delivery.
	Unblock(ip net.IP) error
	// Close detaches the XDP program and releases the underlying resources.
	Close() error
}

// SignalProtocol identifies which signaling protocol a SignalingEvent was
// classified as, mirroring bpf/headers/common.h's SIGNAL_PROTO_* constants.
type SignalProtocol uint8

const (
	SignalProtoUnknown  SignalProtocol = 0
	SignalProtoGTPU     SignalProtocol = 1
	SignalProtoSIP      SignalProtocol = 2
	SignalProtoPortScan SignalProtocol = 3
)

// gtpuPort/sipPort mirror bpf/headers/common.h's GTPU_PORT/SIP_PORT —
// same hand-kept-in-sync convention as SignalProtocol above. Used by
// Loader.SignalRate (loader_linux.go) to pick signal_rate vs scan_rate.
const (
	gtpuPort uint16 = 2152
	sipPort  uint16 = 5060
)

// SignalingEvent is a single GTP-U/SIP packet observation read from the
// kernel's signaling_events ring buffer (bpf/packet_filter.c). It is the
// platform-independent counterpart of bpf's `struct signaling_event` —
// pkg/ingestion turns this into an events.NormalizedEvent.
type SignalingEvent struct {
	// ObservedAt is wall-clock time, already converted from the kernel's
	// monotonic bpf_ktime_get_ns() reading (see loader_linux.go) — never a
	// raw reading of it.
	ObservedAt time.Time
	SourceIP   net.IP
	DestIP     net.IP
	DestPort   uint16
	// PayloadSize is the UDP payload size in bytes for every Protocol except
	// SignalProtoPortScan, where bpf/packet_filter.c repurposes this field
	// to carry the number of distinct destination ports that triggered the
	// scan detection (always MULTIPORT_SCAN_THRESHOLD, by definition of the
	// edge-triggered emit) instead of a byte size.
	PayloadSize uint16
	Protocol    SignalProtocol
	Malformed   bool
	// VLANID is the 802.1Q VLAN ID the packet was tagged with, or 0 for an
	// untagged frame (see bpf/packet_filter.c's VLAN-unwrap comment).
	VLANID uint16
}

// EventSource is implemented by BlocklistUpdaters that can also stream
// per-packet signaling observations for Layer 2 ingestion (pkg/ingestion).
// Only the real Linux Loader does — callers should type-assert for it and
// treat a missing implementation as "no event stream available" rather than
// an error, the same way EbpfBlock actions already degrade gracefully when
// eBPF isn't attached (see cmd/operator/main.go's attachBlocklist).
type EventSource interface {
	// SignalingEvents starts the background ring-buffer reader and returns
	// a channel of decoded events; the channel closes when ctx is done or
	// the underlying reader errors. Intended to be called at most once per
	// Loader.
	SignalingEvents(ctx context.Context) (<-chan SignalingEvent, error)
	// SignalRate returns the current window's packet count for (ip,
	// destPort), and false if there's no entry this window. Backed by one
	// of two kernel maps depending on destPort: signal_rate (GTP-U/SIP,
	// keyed by source IP only — see track_signal_rate()) or scan_rate
	// (everything else, keyed by source IP *and* port — see
	// track_scan_rate()'s comment in packet_filter.c for why those two
	// need different keying). Callers holding a SignalingEvent should pass
	// its own DestPort, not a guess.
	SignalRate(ip net.IP, destPort uint16) (count uint32, ok bool)
}

// isIPv4 reports whether ip should be treated as an IPv4 address for the
// purpose of choosing between the IPv4 and IPv6 map families (blocklist vs
// blocklist_v6, signal_rate vs signal_rate_v6, scan_rate vs scan_rate_v6) —
// the single, shared classifier for that routing decision, used by both
// Loader.blocklistMapAndKey and Loader.SignalRate in loader_linux.go so the
// two can't independently drift on how a v4-mapped IPv6 address
// (::ffff:a.b.c.d, which net.IP.To4() already treats as IPv4) is handled;
// see ipv6Key's doc comment below for why that specific case is routed to
// IPv4.
func isIPv4(ip net.IP) bool {
	return ip.To4() != nil
}

// ipv4Key returns the raw 4-byte representation of ip, passed to the BPF map
// as an opaque byte key. This matches the byte layout the kernel side reads
// directly out of `struct iphdr.saddr` / `.daddr` (network byte order, no
// host-endian conversion applied): both sides treat the key as 4 opaque
// bytes, never as an integer to do arithmetic on, so byte order never has to
// agree with the host's native endianness.
func ipv4Key(ip net.IP) ([]byte, error) {
	v4 := ip.To4()
	if v4 == nil {
		return nil, fmt.Errorf("blocklist only supports IPv4 addresses, got %q", ip.String())
	}
	return []byte(v4), nil
}

// ipv6Key returns the raw 16-byte representation of ip, passed to the
// blocklist_v6/signal_rate_v6/scan_rate_v6 BPF maps as an opaque byte key —
// the same "opaque bytes, not an integer" treatment ipv4Key documents.
//
// ip.To4() != nil (true for both real IPv4 addresses and v4-mapped IPv6
// addresses like ::ffff:10.0.0.1, since Go's net.IP treats those as
// equivalent) is rejected here deliberately: callers route those into the
// IPv4 maps instead (see Loader.blocklistMapAndKey/SignalRate in
// loader_linux.go). A v4-mapped address really is an IPv4 endpoint on the
// wire — bpf/packet_filter.c's IPv4 path is what will actually observe its
// traffic, so that's the map whose state should reflect it.
func ipv6Key(ip net.IP) ([]byte, error) {
	if isIPv4(ip) {
		return nil, fmt.Errorf("blocklist_v6 only supports IPv6 addresses, got %q", ip.String())
	}
	v6 := ip.To16()
	if v6 == nil {
		return nil, fmt.Errorf("blocklist_v6 only supports IPv6 addresses, got %q", ip.String())
	}
	return []byte(v6), nil
}
