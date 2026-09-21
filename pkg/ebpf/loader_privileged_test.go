//go:build linux && privileged

// Measurements that need a real kernel object loaded, so they are behind a
// build tag rather than skipped at runtime: `go test -tags privileged` is
// an explicit choice, and CI's unprivileged job never compiles them.
//
// Run via scripts/loadtest/mitigation_latency.sh, which sets up the object
// and reports the numbers alongside the end-to-end ones.

package ebpf

import (
	"net"
	"os"
	"testing"
)

const benchObjEnv = "SENTINEL5G_BPF_OBJECT"

func loadForBench(tb testing.TB) *Loader {
	tb.Helper()
	obj := os.Getenv(benchObjEnv)
	if obj == "" {
		tb.Skipf("set %s to bpf/packet_filter.o to run this", benchObjEnv)
	}
	if os.Geteuid() != 0 {
		tb.Skip("needs root to load a BPF object")
	}
	// "lo" always exists and attaching there affects nothing real: the
	// measurement is of the map write, not of packet handling.
	l, err := Attach(obj, "lo")
	if err != nil {
		tb.Fatalf("attach %s: %v", obj, err)
	}
	tb.Cleanup(func() { _ = l.Close() })
	return l
}

// The kernel half of docs/observability.md's "closed-loop mitigation
// latency (score -> action)" SLO: once the operator has decided, how long
// until the drop is actually in force? This is the map update itself --
// every eBPF map write is visible to the XDP program on the next packet,
// so there is no propagation delay to add to it.
func BenchmarkBlock(b *testing.B) {
	l := loadForBench(b)
	ip := net.ParseIP("203.0.113.7")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := l.Block(ip); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBlockTunnel(b *testing.B) {
	l := loadForBench(b)
	ip := net.ParseIP("10.0.0.1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := l.BlockTunnel(ip, 0x4D84); err != nil {
			b.Fatal(err)
		}
	}
}

// Distinct keys, which is the real shape of a mitigation storm: many
// subscribers blocked in quick succession, each one a new hash insert
// rather than an overwrite of the same bucket.
func BenchmarkBlockTunnel_DistinctTEIDs(b *testing.B) {
	l := loadForBench(b)
	ip := net.ParseIP("10.0.0.1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Wraps well inside MAX_TUNNEL_BLOCKLIST_ENTRIES so the map never
		// fills -- capacity behaviour is xdp_bench.sh's question, not this
		// one's.
		if err := l.BlockTunnel(ip, uint32(i%8192)+1); err != nil {
			b.Fatal(err)
		}
	}
}

// ROADMAP.md Phase 3 asked whether `blocklist` needs LRU semantics. It is a
// plain HASH, so filling it is an error rather than a silent eviction --
// this proves which, and at what point.
func TestBlocklistIsBoundedAndFailsLoudlyWhenFull(t *testing.T) {
	l := loadForBench(t)

	const capacity = 16384 // MAX_TUNNEL_BLOCKLIST_ENTRIES, bpf/headers/common.h
	ip := net.ParseIP("10.0.0.1")
	for i := 1; i <= capacity; i++ {
		if err := l.BlockTunnel(ip, uint32(i)); err != nil {
			t.Fatalf("filling the tunnel blocklist failed early at %d: %v", i, err)
		}
	}
	if err := l.BlockTunnel(ip, capacity+1); err == nil {
		t.Fatal("a full tunnel blocklist accepted another entry: it is evicting, not refusing")
	} else {
		t.Logf("full at %d entries, next insert refused: %v", capacity, err)
	}
}
