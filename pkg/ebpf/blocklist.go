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

// BlocklistUpdater pushes/removes IPv4 addresses from the "blocklist" BPF map
// that bpf/packet_filter.c consults before making its XDP verdict.
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
	SignalProtoUnknown SignalProtocol = 0
	SignalProtoGTPU    SignalProtocol = 1
	SignalProtoSIP     SignalProtocol = 2
)

// SignalingEvent is a single GTP-U/SIP packet observation read from the
// kernel's signaling_events ring buffer (bpf/packet_filter.c). It is the
// platform-independent counterpart of bpf's `struct signaling_event` —
// pkg/ingestion turns this into an events.NormalizedEvent.
type SignalingEvent struct {
	// ObservedAt is wall-clock time, already converted from the kernel's
	// monotonic bpf_ktime_get_ns() reading (see loader_linux.go) — never a
	// raw reading of it.
	ObservedAt  time.Time
	SourceIP    net.IP
	DestIP      net.IP
	DestPort    uint16
	PayloadSize uint16
	Protocol    SignalProtocol
	Malformed   bool
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
	// SignalRate returns the current window's packet count for ip from the
	// kernel's signal_rate map, and false if ip has no entry this window
	// (see track_signal_rate() in packet_filter.c).
	SignalRate(ip net.IP) (count uint32, ok bool)
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
