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

// loadPinnedForTest attaches with pinning into a throwaway bpffs directory,
// returning the Loader and the pin path. The directory is NOT removed on
// cleanup of the first attach -- surviving the Loader is the behaviour under
// test -- so each test that uses it calls RemovePins itself at the end.
func loadPinnedForTest(tb testing.TB, pinPath string) *Loader {
	tb.Helper()
	obj := os.Getenv(benchObjEnv)
	if obj == "" {
		tb.Skipf("set %s to bpf/packet_filter.o to run this", benchObjEnv)
	}
	if os.Geteuid() != 0 {
		tb.Skip("needs root to load a BPF object")
	}
	if err := ensurePinDir(pinPath); err != nil {
		tb.Skipf("no usable bpffs at %s: %v", pinPath, err)
	}
	l, err := AttachWithOptions(obj, "lo", Options{PinPath: pinPath})
	if err != nil {
		tb.Fatalf("attach %s pinned at %s: %v", obj, pinPath, err)
	}
	return l
}

// The measurement ROADMAP.md Phase 4's first item asks for, made rather than
// argued: a drop applied by one Loader must still be in force after that
// Loader is closed and a new one attaches -- which is exactly what an
// operator rollout, an OOM kill or a crash does to the process.
//
// Before pinning, the collection died with the link and this test's second
// half read back an empty map while every policy's status still asserted
// the block.
func TestPinnedEnforcementSurvivesALoaderRestart(t *testing.T) {
	pinPath := DefaultPinPath + "-restart-test"
	t.Cleanup(func() { _ = RemovePins(pinPath); _ = os.Remove(pinPath) })

	ip := net.ParseIP("203.0.113.9")
	const teid = uint32(0x4D84)
	tunnelIP := net.ParseIP("10.0.0.1")

	first := loadPinnedForTest(t, pinPath)
	if err := first.Block(ip); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if err := first.BlockTunnel(tunnelIP, teid); err != nil {
		t.Fatalf("BlockTunnel: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := loadPinnedForTest(t, pinPath)
	defer second.Close()

	ips, err := second.BlockedIPs()
	if err != nil {
		t.Fatalf("BlockedIPs: %v", err)
	}
	if !containsIP(ips, ip) {
		t.Fatalf("%s was not still blocked after a restart; got %v", ip, ips)
	}

	tunnels, err := second.BlockedTunnels()
	if err != nil {
		t.Fatalf("BlockedTunnels: %v", err)
	}
	found := false
	for _, tn := range tunnels {
		if tn.TEID == teid && tn.IP.Equal(tunnelIP) {
			found = true
		}
	}
	if !found {
		t.Fatalf("tunnel %s/%#x was not still blocked after a restart; got %+v", tunnelIP, teid, tunnels)
	}

	if second.PinPath() != pinPath {
		t.Fatalf("PinPath() = %q, want %q", second.PinPath(), pinPath)
	}
}

// The control case, stated so the two are not confused: without a pin path
// the maps really do die with the Loader. This is the documented
// BPF_PIN_PATH="" behaviour, not a bug, and pkg/controller's reconcile is
// what repairs it.
func TestUnpinnedEnforcementDoesNotSurviveALoaderRestart(t *testing.T) {
	first := loadForBench(t)
	ip := net.ParseIP("203.0.113.10")
	if err := first.Block(ip); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := loadForBench(t)
	ips, err := second.BlockedIPs()
	if err != nil {
		t.Fatalf("BlockedIPs: %v", err)
	}
	if containsIP(ips, ip) {
		t.Fatal("an unpinned blocklist survived a restart; the pinned/unpinned distinction is not what it claims")
	}
}

// De-escalation and finalization both walk a policy's status and unblock
// every entry. After a restart that lost the maps, or after a reconcile
// that already pruned them, those entries are phantoms -- and an Unblock
// that errored on the first one used to abort the loop and leave every real
// entry behind it untouched.
func TestUnblockOnAMissingEntryIsNotAnError(t *testing.T) {
	l := loadForBench(t)
	if err := l.Unblock(net.ParseIP("203.0.113.254")); err != nil {
		t.Fatalf("Unblock on an address that was never blocked: %v", err)
	}
	if err := l.UnblockTunnel(net.ParseIP("10.0.0.254"), 0x1234); err != nil {
		t.Fatalf("UnblockTunnel on a tunnel that was never blocked: %v", err)
	}
}

// BlockedIPs/BlockedTunnels are the "actual state" half of the drift
// comparison, so a key encoded one way by Block and read back another way
// would make the reconciler re-apply an already-applied drop on every tick
// forever. Covers both address families.
func TestInspectorRoundTripsBothAddressFamilies(t *testing.T) {
	l := loadForBench(t)

	v4, v6 := net.ParseIP("203.0.113.11"), net.ParseIP("2001:db8::25")
	if err := l.Block(v4); err != nil {
		t.Fatalf("Block v4: %v", err)
	}
	if err := l.Block(v6); err != nil {
		t.Fatalf("Block v6: %v", err)
	}
	if err := l.BlockTunnel(v4, 0x1111); err != nil {
		t.Fatalf("BlockTunnel v4: %v", err)
	}
	if err := l.BlockTunnel(v6, 0x2222); err != nil {
		t.Fatalf("BlockTunnel v6: %v", err)
	}

	ips, err := l.BlockedIPs()
	if err != nil {
		t.Fatalf("BlockedIPs: %v", err)
	}
	if !containsIP(ips, v4) || !containsIP(ips, v6) {
		t.Fatalf("BlockedIPs lost an address family: %v", ips)
	}

	tunnels, err := l.BlockedTunnels()
	if err != nil {
		t.Fatalf("BlockedTunnels: %v", err)
	}
	var sawV4, sawV6 bool
	for _, tn := range tunnels {
		if tn.IP.Equal(v4) && tn.TEID == 0x1111 {
			sawV4 = true
		}
		if tn.IP.Equal(v6) && tn.TEID == 0x2222 {
			sawV6 = true
		}
	}
	if !sawV4 || !sawV6 {
		t.Fatalf("BlockedTunnels lost an address family: %+v", tunnels)
	}
}

func containsIP(ips []net.IP, want net.IP) bool {
	for _, ip := range ips {
		if ip.Equal(want) {
			return true
		}
	}
	return false
}
