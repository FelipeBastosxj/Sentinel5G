//go:build linux

package ebpf

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/cilium/ebpf"
)

// PinPath implements BlocklistInspector: the bpffs directory this Loader's
// enforcement maps are pinned into, or "" when they are not pinned and
// therefore do not survive the process.
func (l *Loader) PinPath() string { return l.pinPath }

// PinReset implements BlocklistInspector.
func (l *Loader) PinReset() bool { return l.pinReset }

// BlockedIPs implements BlocklistInspector: every source address currently
// in the blocklist/blocklist_v6 maps, which is the ACTUAL enforcement state
// — as opposed to TelecomSecurityPolicy.status.blockedSourceIPs, which is
// the desired one. pkg/controller.BlocklistReconciler compares the two.
//
// Iteration over a BPF hash map is not a snapshot: the kernel may hand back
// a key that a concurrent Block/Unblock has already changed. That is fine
// for this caller — the reconcile runs on a timer and converges — and it is
// why this returns what it saw rather than claiming to be consistent.
func (l *Loader) BlockedIPs() ([]net.IP, error) {
	out := make([]net.IP, 0)

	var key4 [4]byte
	var value uint8
	it := l.blocklist.Iterate()
	for it.Next(&key4, &value) {
		ip := make(net.IP, net.IPv4len)
		copy(ip, key4[:])
		out = append(out, ip)
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s map: %w", blocklistMapName, err)
	}

	var key6 [16]byte
	it = l.blocklistV6.Iterate()
	for it.Next(&key6, &value) {
		ip := make(net.IP, net.IPv6len)
		copy(ip, key6[:])
		out = append(out, ip)
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s map: %w", blocklistV6MapName, err)
	}

	return out, nil
}

// BlockedTunnels implements BlocklistInspector: every (source, TEID) pair
// currently in the tunnel_blocklist/tunnel_blocklist_v6 maps. Same
// desired-versus-actual role as BlockedIPs, and the same non-snapshot
// caveat.
func (l *Loader) BlockedTunnels() ([]BlockedTunnel, error) {
	out := make([]BlockedTunnel, 0)

	var value uint8

	var key tunnelRateKey
	it := l.tunnelBlocklist.Iterate()
	for it.Next(&key, &value) {
		// Mirrors tunnelBlocklistMapAndKey's encoding exactly: Saddr is the
		// 4 address bytes read back through LittleEndian (an opaque byte
		// key carried in a uint32 field, not an integer), TEID is already
		// host order.
		addr := make([]byte, net.IPv4len)
		binary.LittleEndian.PutUint32(addr, key.Saddr)
		out = append(out, BlockedTunnel{IP: net.IP(addr), TEID: key.TEID})
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s map: %w", tunnelBlocklistMapName, err)
	}

	var keyV6 tunnelRateKeyV6
	it = l.tunnelBlocklistV6.Iterate()
	for it.Next(&keyV6, &value) {
		ip := make(net.IP, net.IPv6len)
		copy(ip, keyV6.Saddr[:])
		out = append(out, BlockedTunnel{IP: ip, TEID: keyV6.TEID})
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s map: %w", tunnelBlocklistV6MapName, err)
	}

	return out, nil
}

// MapCapacity reports max_entries for the two enforcement maps, so
// pkg/controller can export how close the blocklists are to the point where
// bpf/packet_filter.c's plain-HASH choice turns a new mitigation into an
// error (ROADMAP.md Phase 4's "a full tunnel_blocklist is a denial of
// service against the mitigation path").
func (l *Loader) MapCapacity() (ips, tunnels uint32) {
	return l.blocklist.MaxEntries(), l.tunnelBlocklist.MaxEntries()
}

var _ BlocklistInspector = (*Loader)(nil)

// verifyEnforcementKeySizes checks that the Go key types this file iterates
// with are the size the kernel actually declared for each enforcement map.
// Called from Attach, before the program is attached.
//
// Without it, a change to `struct tunnel_key` in bpf/packet_filter.c that
// was not mirrored in tunnelRateKey would not fail anywhere: Map.Put and
// Map.Iterate both marshal to whatever the map says, so the mismatch would
// surface as drops landing on the wrong tunnel and a reconcile that reads
// back addresses nobody blocked. The ring-buffer record size already has
// this guarantee by test (loader_linux_test.go's kernelEventSize); the map
// keys did not.
func verifyEnforcementKeySizes(maps map[string]*ebpf.Map) error {
	want := map[string]int{
		blocklistMapName:         4,  // __u32 saddr
		blocklistV6MapName:       16, // struct in6_key
		tunnelBlocklistMapName:   binary.Size(tunnelRateKey{}),
		tunnelBlocklistV6MapName: binary.Size(tunnelRateKeyV6{}),
	}
	for name, size := range want {
		m, ok := maps[name]
		if !ok {
			continue // Attach reports a missing map itself, with a better message.
		}
		if got := int(m.KeySize()); got != size {
			return fmt.Errorf("map %q has key size %d, this build expects %d: "+
				"bpf/packet_filter.c and pkg/ebpf are out of sync", name, got, size)
		}
	}
	return nil
}
