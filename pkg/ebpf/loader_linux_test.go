//go:build linux

package ebpf

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

// Regression guard for a real bug found via live testing (bpftool map dump
// against a real kernel map + a real Lookup call), not by inspection:
// cilium/ebpf's key/value marshaling sums each Go struct field's own size
// (binary.Size semantics) rather than using Go's struct-level
// unsafe.Sizeof, so a value type missing explicit trailing padding silently
// fails every real Lookup against a BPF map whose C-side struct *is*
// naturally padded — see signalRateEntry's doc comment. binary.Size is
// exactly the computation that broke before the fix, so it's what this
// test asserts against, not unsafe.Sizeof (which would have passed even on
// the broken version, since Go's own struct layout already included the
// padding — the mismatch was between Go's layout and cilium/ebpf's
// reflection, not between Go's layout and C's).
func TestSignalRateEntryMatchesKernelValueSize(t *testing.T) {
	const kernelValueSize = 16 // bpf/packet_filter.c's struct signal_rate_entry, confirmed via `bpftool map list` (value 16B)
	if got := binary.Size(signalRateEntry{}); got != kernelValueSize {
		t.Fatalf("binary.Size(signalRateEntry{}) = %d, want %d (must match bpf/packet_filter.c's "+
			"struct signal_rate_entry's real, C-padded size, or every real Lookup fails with "+
			"\"doesn't consume all data\")", got, kernelValueSize)
	}
}

func TestScanRateKeyMatchesKernelKeySize(t *testing.T) {
	const kernelKeySize = 8 // bpf/packet_filter.c's struct scan_key, confirmed via `bpftool map list` (key 8B)
	if got := binary.Size(scanRateKey{}); got != kernelKeySize {
		t.Fatalf("binary.Size(scanRateKey{}) = %d, want %d (must match bpf/packet_filter.c's "+
			"struct scan_key exactly)", got, kernelKeySize)
	}
}

// Regression guard, same rationale as TestSignalRateEntryMatchesKernelValueSize:
// confirmed against a real `bpftool prog load` + `bpftool map list` of
// port_scan (key 4B, value 48B) — see portScanEntry's doc comment for why
// this type exists despite having no real reader yet.
func TestPortScanEntryMatchesKernelValueSize(t *testing.T) {
	const kernelValueSize = 48 // bpf/packet_filter.c's struct port_scan_entry, confirmed via `bpftool map list` (value 48B)
	if got := binary.Size(portScanEntry{}); got != kernelValueSize {
		t.Fatalf("binary.Size(portScanEntry{}) = %d, want %d (must match bpf/packet_filter.c's "+
			"struct port_scan_entry exactly)", got, kernelValueSize)
	}
}

// Regression guard, same rationale as TestSignalRateEntryMatchesKernelValueSize:
// rawSignalingEvent must stay byte-exact with bpf/packet_filter.c's struct
// signaling_event, including after VLAN support repurposed its trailing
// 2-byte pad into VlanID — the struct's total size (and therefore the
// ringbuf record size cilium/ebpf decodes) must not change.
func TestRawSignalingEventMatchesKernelSize(t *testing.T) {
	const kernelEventSize = 32 // bpf/packet_filter.c's struct signaling_event, packed+aligned(8).
	if got := binary.Size(rawSignalingEvent{}); got != kernelEventSize {
		t.Fatalf("binary.Size(rawSignalingEvent{}) = %d, want %d (must match bpf/packet_filter.c's "+
			"struct signaling_event exactly)", got, kernelEventSize)
	}
}

// Regression guard, same rationale as TestRawSignalingEventMatchesKernelSize:
// rawSignalingEventV6 must stay byte-exact with bpf/packet_filter.c's
// struct signaling_event_v6.
func TestRawSignalingEventV6MatchesKernelSize(t *testing.T) {
	const kernelEventSize = 56 // bpf/packet_filter.c's struct signaling_event_v6, packed+aligned(8).
	if got := binary.Size(rawSignalingEventV6{}); got != kernelEventSize {
		t.Fatalf("binary.Size(rawSignalingEventV6{}) = %d, want %d (must match bpf/packet_filter.c's "+
			"struct signaling_event_v6 exactly)", got, kernelEventSize)
	}
}

// Regression guard, same rationale as TestScanRateKeyMatchesKernelKeySize:
// confirmed against a real `bpftool map list` of scan_rate_v6 (key 24B).
func TestScanRateKeyV6MatchesKernelKeySize(t *testing.T) {
	const kernelKeySize = 24 // bpf/packet_filter.c's struct scan_key_v6, confirmed via `bpftool map list` (key 24B)
	if got := binary.Size(scanRateKeyV6{}); got != kernelKeySize {
		t.Fatalf("binary.Size(scanRateKeyV6{}) = %d, want %d (must match bpf/packet_filter.c's "+
			"struct scan_key_v6 exactly)", got, kernelKeySize)
	}
}

// Regression guard for ClassifyAttachError's fs.ErrNotExist branch (see
// errors.go): asserts against a real Attach() failure, not a synthetic one,
// since what actually matters is whether cilium/ebpf's real error chain for
// a missing object file still satisfies errors.Is(err, fs.ErrNotExist) after
// its own wrapping -- errors_test.go covers the classification logic itself
// with synthetic errors, this covers the assumption underneath it.
func TestAttachMissingObjectClassifiesAsNotFound(t *testing.T) {
	_, err := Attach("/nonexistent/path/does/not/exist/packet_filter.o", "lo")
	if err == nil {
		t.Fatal("Attach with a nonexistent object path unexpectedly succeeded")
	}
	if got := ClassifyAttachError(err); !strings.Contains(got, "was not found") {
		t.Fatalf("ClassifyAttachError(%v) = %q, want it to classify as a missing object file", err, got)
	}
}

// Same hand-synced-constant guard as the others, for the per-TEID rate map's
// key. Confirmed against a real `bpftool map show` on Linux 6.14 (key 8B).
func TestTunnelRateKeyMatchesKernelKeySize(t *testing.T) {
	const kernelKeySize = 8 // bpf/packet_filter.c's struct tunnel_key.

	if got := binary.Size(tunnelRateKey{}); got != kernelKeySize {
		t.Fatalf("binary.Size(tunnelRateKey{}) = %d, want %d (must match bpf/packet_filter.c's struct tunnel_key exactly)", got, kernelKeySize)
	}
}

// Confirmed via `bpftool map show` (key 24B) — unlike scan_key_v6, the C
// struct declares its padding explicitly, so this needs only one pad field.
func TestTunnelRateKeyV6MatchesKernelKeySize(t *testing.T) {
	const kernelKeySize = 24 // bpf/packet_filter.c's struct tunnel_key_v6.

	if got := binary.Size(tunnelRateKeyV6{}); got != kernelKeySize {
		t.Fatalf("binary.Size(tunnelRateKeyV6{}) = %d, want %d (must match bpf/packet_filter.c's struct tunnel_key_v6 exactly)", got, kernelKeySize)
	}
}

// The size assertions above prove the Go mirrors are the right *length*;
// nothing proved the fields were in the right *order* until this. That
// mattered enough to write once TEID and TunnelRate existed: they are two
// same-typed uint32s at the tail of the struct, so transposing them would be
// size-clean, compile cleanly, and produce a wrong tunnel rate for every
// event — exactly the kind of silent cross-language mismatch this package's
// other tests exist to prevent. Every field gets a distinct sentinel so a
// swap of any pair fails.
func TestDecodeSignalingEventFieldOrder(t *testing.T) {
	sample := make([]byte, 32)
	binary.LittleEndian.PutUint64(sample[0:], 1_000_000_000) // timestamp_ns: 1s since boot
	binary.LittleEndian.PutUint32(sample[8:], 0x04030201)    // saddr
	binary.LittleEndian.PutUint32(sample[12:], 0x08070605)   // daddr
	binary.LittleEndian.PutUint16(sample[16:], 2152)         // dest_port
	binary.LittleEndian.PutUint16(sample[18:], 300)          // payload_size
	sample[20] = 1                                           // protocol: SignalProtoGTPU
	sample[21] = 0                                           // malformed
	binary.LittleEndian.PutUint16(sample[22:], 42)           // vlan_id
	binary.LittleEndian.PutUint32(sample[24:], 0x00004d84)   // teid
	binary.LittleEndian.PutUint32(sample[28:], 31)           // tunnel_rate

	boot := time.Unix(1700000000, 0).UTC()
	l := &Loader{bootTime: boot}

	evt, err := l.decodeSignalingEvent(sample)
	if err != nil {
		t.Fatalf("decodeSignalingEvent: %v", err)
	}

	if want := boot.Add(time.Second); !evt.ObservedAt.Equal(want) {
		t.Errorf("ObservedAt = %v, want %v", evt.ObservedAt, want)
	}
	if got, want := evt.SourceIP.String(), "1.2.3.4"; got != want {
		t.Errorf("SourceIP = %s, want %s", got, want)
	}
	if got, want := evt.DestIP.String(), "5.6.7.8"; got != want {
		t.Errorf("DestIP = %s, want %s", got, want)
	}
	if evt.DestPort != 2152 {
		t.Errorf("DestPort = %d, want 2152", evt.DestPort)
	}
	if evt.PayloadSize != 300 {
		t.Errorf("PayloadSize = %d, want 300", evt.PayloadSize)
	}
	if evt.Protocol != SignalProtoGTPU {
		t.Errorf("Protocol = %d, want %d", evt.Protocol, SignalProtoGTPU)
	}
	if evt.Malformed {
		t.Error("Malformed = true, want false")
	}
	if evt.VLANID != 42 {
		t.Errorf("VLANID = %d, want 42", evt.VLANID)
	}
	// The pair this test exists for.
	if evt.TEID != 0x00004d84 {
		t.Errorf("TEID = %#x, want %#x", evt.TEID, 0x00004d84)
	}
	if evt.TunnelRate != 31 {
		t.Errorf("TunnelRate = %d, want 31", evt.TunnelRate)
	}
}

// The IPv6 counterpart, where the two tail uint32s sit at offsets 48 and 52.
func TestDecodeSignalingEventV6FieldOrder(t *testing.T) {
	sample := make([]byte, 56)
	binary.LittleEndian.PutUint64(sample[0:], 2_000_000_000)
	copy(sample[8:], net.ParseIP("2001:db8::1").To16())
	copy(sample[24:], net.ParseIP("2001:db8::2").To16())
	binary.LittleEndian.PutUint16(sample[40:], 5060)
	binary.LittleEndian.PutUint16(sample[42:], 128)
	sample[44] = 2 // SignalProtoSIP
	sample[45] = 1 // malformed
	binary.LittleEndian.PutUint16(sample[46:], 7)
	binary.LittleEndian.PutUint32(sample[48:], 0xdeadbeef)
	binary.LittleEndian.PutUint32(sample[52:], 4096)

	boot := time.Unix(1700000000, 0).UTC()
	l := &Loader{bootTime: boot}

	evt, err := l.decodeSignalingEventV6(sample)
	if err != nil {
		t.Fatalf("decodeSignalingEventV6: %v", err)
	}

	if got, want := evt.SourceIP.String(), "2001:db8::1"; got != want {
		t.Errorf("SourceIP = %s, want %s", got, want)
	}
	if got, want := evt.DestIP.String(), "2001:db8::2"; got != want {
		t.Errorf("DestIP = %s, want %s", got, want)
	}
	if evt.DestPort != 5060 || evt.PayloadSize != 128 {
		t.Errorf("DestPort/PayloadSize = %d/%d, want 5060/128", evt.DestPort, evt.PayloadSize)
	}
	if evt.Protocol != SignalProtoSIP || !evt.Malformed || evt.VLANID != 7 {
		t.Errorf("Protocol/Malformed/VLANID = %d/%v/%d, want %d/true/7", evt.Protocol, evt.Malformed, evt.VLANID, SignalProtoSIP)
	}
	if evt.TEID != 0xdeadbeef {
		t.Errorf("TEID = %#x, want %#x", evt.TEID, 0xdeadbeef)
	}
	if evt.TunnelRate != 4096 {
		t.Errorf("TunnelRate = %d, want 4096", evt.TunnelRate)
	}
}

// The hand-written offsets in decodeSignalingEvent are the layout of
// rawSignalingEvent. Round-tripping a fully populated mirror struct through
// binary.Write and then the decoder is what keeps them equal: change one
// without the other and a field lands in the wrong place here.
func TestDecodeSignalingEvent_AgreesWithTheMirrorStruct(t *testing.T) {
	raw := rawSignalingEvent{
		TimestampNs: 123456789, Saddr: 0x0100007f, Daddr: 0x0700007f,
		DestPort: 2152, PayloadSize: 100, Protocol: 1, Malformed: 1, VlanID: 99,
		TEID: 0xdeadbeef, TunnelRate: 3001,
	}
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, raw); err != nil {
		t.Fatal(err)
	}
	l := &Loader{bootTime: time.Unix(0, 0)}
	evt, err := l.decodeSignalingEvent(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if evt.ObservedAt.UnixNano() != 123456789 || evt.SourceIP.String() != "127.0.0.1" ||
		evt.DestIP.String() != "127.0.0.7" || evt.DestPort != 2152 || evt.PayloadSize != 100 ||
		evt.Protocol != SignalProtoGTPU || !evt.Malformed || evt.VLANID != 99 ||
		evt.TEID != 0xdeadbeef || evt.TunnelRate != 3001 {
		t.Fatalf("decoder disagrees with the mirror struct: %+v", evt)
	}

	raw6 := rawSignalingEventV6{
		TimestampNs: 42, DestPort: 5060, PayloadSize: 7, Protocol: 2, VlanID: 3,
		TEID: 0x11, TunnelRate: 0x22,
	}
	copy(raw6.Saddr[:], net.ParseIP("2001:db8::1").To16())
	copy(raw6.Daddr[:], net.ParseIP("2001:db8::2").To16())
	buf.Reset()
	if err = binary.Write(&buf, binary.LittleEndian, raw6); err != nil {
		t.Fatal(err)
	}
	evt6, err := l.decodeSignalingEventV6(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if evt6.ObservedAt.UnixNano() != 42 || evt6.SourceIP.String() != "2001:db8::1" ||
		evt6.DestIP.String() != "2001:db8::2" || evt6.DestPort != 5060 || evt6.PayloadSize != 7 ||
		evt6.Protocol != SignalProtoSIP || evt6.VLANID != 3 || evt6.TEID != 0x11 || evt6.TunnelRate != 0x22 {
		t.Fatalf("v6 decoder disagrees with the mirror struct: %+v", evt6)
	}
}

// The v6 decoder must COPY the addresses out of the sample: the ring buffer
// reuses that memory, so a slice into it would silently change under a
// consumer holding the event.
func TestDecodeSignalingEventV6_CopiesAddressesOutOfTheSample(t *testing.T) {
	sample := make([]byte, 56)
	copy(sample[8:], net.ParseIP("2001:db8::1").To16())
	l := &Loader{bootTime: time.Unix(0, 0)}
	evt, err := l.decodeSignalingEventV6(sample)
	if err != nil {
		t.Fatal(err)
	}
	sample[8+15] = 0xff // The ring buffer moves on.
	if evt.SourceIP.String() != "2001:db8::1" {
		t.Fatalf("SourceIP aliased the ring buffer memory: now %s", evt.SourceIP)
	}
}
