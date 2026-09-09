//go:build linux

package ebpf

import (
	"encoding/binary"
	"strings"
	"testing"
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
	const kernelEventSize = 24 // bpf/packet_filter.c's struct signaling_event, packed+aligned(8).
	if got := binary.Size(rawSignalingEvent{}); got != kernelEventSize {
		t.Fatalf("binary.Size(rawSignalingEvent{}) = %d, want %d (must match bpf/packet_filter.c's "+
			"struct signaling_event exactly)", got, kernelEventSize)
	}
}

// Regression guard, same rationale as TestRawSignalingEventMatchesKernelSize:
// rawSignalingEventV6 must stay byte-exact with bpf/packet_filter.c's
// struct signaling_event_v6.
func TestRawSignalingEventV6MatchesKernelSize(t *testing.T) {
	const kernelEventSize = 48 // bpf/packet_filter.c's struct signaling_event_v6, packed+aligned(8).
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
