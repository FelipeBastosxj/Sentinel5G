//go:build linux

package ebpf

import (
	"encoding/binary"
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
