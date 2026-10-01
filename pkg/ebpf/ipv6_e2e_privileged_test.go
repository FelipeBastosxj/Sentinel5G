//go:build linux && privileged

package ebpf

import (
	"encoding/binary"
	"net"
	"testing"
)

// XDP return codes (uapi/linux/bpf.h).
const (
	xdpDrop = 1
	xdpPass = 2
)

// gtpuTPDU builds a GTP-U T-PDU shaped like the real captures: flags 0x34 (E
// set), one 4-byte PDU Session Container extension header, then an inner
// payload -- the same framing scripts/loadtest/gen_packets.py feeds the
// benchmark, so this exercises the production parse path, extension-header
// walk included.
func gtpuTPDU(teid uint32, inner int) []byte {
	b := []byte{0x34, 0xFF}
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(4+4+inner))
	b = append(b, length...)
	t := make([]byte, 4)
	binary.BigEndian.PutUint32(t, teid)
	b = append(b, t...)
	b = append(b, 0x00, 0x00, 0x00, 0x85) // seq(2) + N-PDU(1) + next ext type
	b = append(b, 0x01, 0x00, 0x00, 0x00) // ext: 1*4 bytes, next type 0 (end)
	for i := 0; i < inner; i++ {
		b = append(b, 0xA5)
	}
	return b
}

// ipv6GTPUFrame builds a full Ethernet + IPv6 + UDP(2152) + GTP-U frame, the
// input BPF_PROG_TEST_RUN feeds the XDP program. src is the outer IPv6
// source (a gNB's N3 address), which is what the v6 tunnel key is built from.
func ipv6GTPUFrame(src net.IP, teid uint32) []byte {
	gtp := gtpuTPDU(teid, 64)
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:], 2152)               // src port (a gNB's)
	binary.BigEndian.PutUint16(udp[2:], 2152)               // dst port GTPU_PORT
	binary.BigEndian.PutUint16(udp[4:], uint16(8+len(gtp))) // length
	// checksum 0 (optional for the parser, which never validates it)
	payload := append(udp, gtp...)

	ip6 := make([]byte, 40)
	ip6[0] = 0x60 // version 6
	binary.BigEndian.PutUint16(ip6[4:], uint16(len(payload)))
	ip6[6] = 17 // next header: UDP
	ip6[7] = 64 // hop limit
	copy(ip6[8:24], src.To16())
	copy(ip6[24:40], net.ParseIP("2001:db8::9").To16()) // dst (the UPF)
	l3 := append(ip6, payload...)

	eth := make([]byte, 14)
	binary.BigEndian.PutUint16(eth[12:], 0x86DD) // EtherType IPv6
	return append(eth, l3...)
}

// The IPv6 path, run end to end for the first time (ROADMAP.md Phase 4). A
// real v6 GTP-U frame is fed to the real, loaded XDP program via
// BPF_PROG_TEST_RUN; the test asserts the program parses it (XDP_PASS),
// populates the v6 per-tunnel rate map from the outer v6 source, and then --
// once that (source, TEID) is blocked -- drops the identical frame
// (XDP_DROP). Every v6 map and the v6 tunnel key were asserted structurally
// before; this is the first time v6 packets actually traverse the program.
func TestIPv6DataPathEndToEnd(t *testing.T) {
	l := loadForBench(t)
	prog := l.collection.Programs[xdpProgramName]
	if prog == nil {
		t.Fatal("xdp program not found in collection")
	}

	src := net.ParseIP("2001:db8::1")
	const teid = uint32(0x4D84)
	frame := ipv6GTPUFrame(src, teid)

	// 1. Parse + emit: a clean v6 GTP-U packet passes, and the v6 rate map
	//    now holds its (source, TEID) -- proof the v6 parse and v6 tunnel
	//    tracking ran, not just that the program didn't crash.
	ret, _, err := prog.Test(frame)
	if err != nil {
		t.Fatalf("prog.Test (v6 GTP-U): %v", err)
	}
	if ret != xdpPass {
		t.Fatalf("a clean v6 GTP-U packet returned %d, want XDP_PASS (%d)", ret, xdpPass)
	}

	key := tunnelRateKeyV6{TEID: teid}
	copy(key.Saddr[:], src.To16())
	var entry signalRateEntry
	if err := l.tunnelRateV6.Lookup(&key, &entry); err != nil {
		t.Fatalf("tunnel_rate_v6 has no entry for the v6 (source, TEID) after a v6 T-PDU: %v -- "+
			"the v6 parse or v6 rate tracking did not run", err)
	}
	if entry.Count == 0 {
		t.Fatal("tunnel_rate_v6 entry exists but counted 0 packets")
	}

	// 2. Enforce: block that exact v6 tunnel, then the identical frame must
	//    now be dropped before anything else.
	if err := l.BlockTunnel(src, teid); err != nil {
		t.Fatalf("BlockTunnel (v6): %v", err)
	}
	ret, _, err = prog.Test(frame)
	if err != nil {
		t.Fatalf("prog.Test (v6 GTP-U, blocked): %v", err)
	}
	if ret != xdpDrop {
		t.Fatalf("a blocked v6 tunnel's packet returned %d, want XDP_DROP (%d)", ret, xdpDrop)
	}
}
