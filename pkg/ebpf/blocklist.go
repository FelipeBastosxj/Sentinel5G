// Package ebpf loads and drives the XDP program in bpf/packet_filter.c,
// exposing a small, platform-independent interface (BlocklistUpdater) that
// pkg/controller uses to react to threat scores. The real cilium/ebpf-based
// implementation only builds on Linux (see loader_linux.go); a no-op stub
// (loader_other.go) keeps the module buildable on non-Linux dev machines.
package ebpf

import (
	"fmt"
	"net"
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
