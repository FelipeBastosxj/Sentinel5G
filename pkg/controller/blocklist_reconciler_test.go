package controller

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
)

// fakeKernel stands in for an attached pkg/ebpf.Loader: it implements both
// halves of the contract BlocklistReconciler needs -- the writes
// (BlocklistUpdater) and the read-back (BlocklistInspector) -- over plain
// sets, so a test can set up a kernel that disagrees with policy status and
// assert exactly which corrections were made.
//
// Deliberately NOT the recordingBlocklist in threat_score_watcher_test.go:
// that one only records calls, and the whole point here is that the state
// read back has to reflect the writes.
type fakeKernel struct {
	// Mutex-guarded for the same reason recordingBlocklist is: the real
	// Loader is goroutine-safe (its mutations are map syscalls), and
	// TestBlocklistReconciler_StartSyncsBeforeTheFirstTick polls this from
	// the test goroutine while Start writes it from its own. A double that
	// isn't guarded would report its own race as the reconciler's.
	mu sync.Mutex

	ips     map[string]net.IP
	tunnels map[string]ebpf.BlockedTunnel

	pinPath  string
	pinReset bool

	capIPs, capTunnels uint32

	obsEntries, obsCapacity uint32
	obsErr                  error

	// blockErr, when set, fails every Block call -- the "map is full" shape.
	blockErr error

	// calls records every mutation in order, which is what the tests assert
	// on: the final state converging is necessary but not sufficient, since
	// a reconciler that blindly re-applied everything every tick would also
	// converge.
	calls []string
}

func newFakeKernel() *fakeKernel {
	return &fakeKernel{
		ips:         map[string]net.IP{},
		tunnels:     map[string]ebpf.BlockedTunnel{},
		pinPath:     ebpf.DefaultPinPath,
		capIPs:      65536,
		capTunnels:  16384,
		obsCapacity: 65536,
	}
}

func (k *fakeKernel) Block(ip net.IP) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.blockErr != nil {
		return k.blockErr
	}
	k.calls = append(k.calls, "block "+ip.String())
	k.ips[ip.String()] = ip
	return nil
}

func (k *fakeKernel) Unblock(ip net.IP) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, "unblock "+ip.String())
	delete(k.ips, ip.String())
	return nil
}

func (k *fakeKernel) BlockTunnel(ip net.IP, teid uint32) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.blockErr != nil {
		return k.blockErr
	}
	key := fmt.Sprintf("%s/%#x", ip, teid)
	k.calls = append(k.calls, "blockTunnel "+key)
	k.tunnels[key] = ebpf.BlockedTunnel{IP: ip, TEID: teid}
	return nil
}

func (k *fakeKernel) UnblockTunnel(ip net.IP, teid uint32) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	key := fmt.Sprintf("%s/%#x", ip, teid)
	k.calls = append(k.calls, "unblockTunnel "+key)
	delete(k.tunnels, key)
	return nil
}

func (k *fakeKernel) Close() error { return nil }

func (k *fakeKernel) BlockedIPs() ([]net.IP, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]net.IP, 0, len(k.ips))
	for _, ip := range k.ips {
		out = append(out, ip)
	}
	return out, nil
}

func (k *fakeKernel) BlockedTunnels() ([]ebpf.BlockedTunnel, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]ebpf.BlockedTunnel, 0, len(k.tunnels))
	for _, t := range k.tunnels {
		out = append(out, t)
	}
	return out, nil
}

func (k *fakeKernel) ObservationOccupancy() (uint32, uint32, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.obsEntries, k.obsCapacity, k.obsErr
}

func (k *fakeKernel) PinPath() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.pinPath
}
func (k *fakeKernel) PinReset() bool                { return k.pinReset }
func (k *fakeKernel) MapCapacity() (uint32, uint32) { return k.capIPs, k.capTunnels }
func (k *fakeKernel) sortedCalls() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := append([]string(nil), k.calls...)
	sort.Strings(out)
	return out
}

// blockedCount and hasIP are the guarded reads the concurrent Start tests
// poll with; everything else runs on one goroutine.
func (k *fakeKernel) blockedCount() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.ips)
}

func (k *fakeKernel) hasIP(raw string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, ok := k.ips[raw]
	return ok
}
func (k *fakeKernel) seedIP(raw string) { ip := net.ParseIP(raw); k.ips[ip.String()] = ip }
func (k *fakeKernel) seedTunnel(raw string, t uint32) {
	ip := net.ParseIP(raw)
	k.tunnels[fmt.Sprintf("%s/%#x", ip, t)] = ebpf.BlockedTunnel{IP: ip, TEID: t}
}

var _ ebpf.BlocklistUpdater = (*fakeKernel)(nil)
var _ ebpf.BlocklistInspector = (*fakeKernel)(nil)

func mitigatingPolicy(name string, ips []string, tunnels []string) *securityv1alpha1.TelecomSecurityPolicy {
	return &securityv1alpha1.TelecomSecurityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "telecom"},
		Status: securityv1alpha1.TelecomSecurityPolicyStatus{
			Phase:            securityv1alpha1.PolicyPhaseMitigating,
			BlockedSourceIPs: ips,
			BlockedTunnels:   tunnels,
		},
	}
}

func newDriftReconciler(t *testing.T, kernel *fakeKernel, objs ...client.Object) *BlocklistReconciler {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{})
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	return &BlocklistReconciler{
		Client:    builder.Build(),
		Log:       testr.New(t),
		Blocklist: kernel,
		Inspector: kernel,
	}
}

// The restart this whole mechanism exists for: the operator comes back, the
// enforcement maps are empty (no pin, or a pin that could not be reused),
// and every policy's status still asserts drops that are not in force. The
// kernel must end up holding exactly what status claims.
func TestBlocklistReconciler_ReappliesWhatTheKernelLost(t *testing.T) {
	kernel := newFakeKernel()
	policy := mitigatingPolicy("gtpu-guard",
		[]string{"203.0.113.7", "2001:db8::1"},
		[]string{"10.0.0.1/0x4d84", "10.0.0.1/0x5fff"})
	b := newDriftReconciler(t, kernel, policy)

	report, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if report.MissingIPs != 2 || report.MissingTunnels != 2 {
		t.Fatalf("expected 2 missing IPs and 2 missing tunnels, got %+v", report)
	}
	if report.ExtraIPs != 0 || report.ExtraTunnels != 0 {
		t.Fatalf("nothing should have been removed, got %+v", report)
	}
	want := []string{
		"block 203.0.113.7",
		"block 2001:db8::1",
		"blockTunnel 10.0.0.1/0x4d84",
		"blockTunnel 10.0.0.1/0x5fff",
	}
	sort.Strings(want)
	got := kernel.sortedCalls()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("kernel calls:\n got %v\nwant %v", got, want)
	}
}

// The other direction, and the reason a pin is not simply "set and forget":
// a drop that survived the process must not outlive the policy that asked
// for it. Deleting the policy, or de-escalating it (which clears the status
// slices), has to take the kernel entry with it.
func TestBlocklistReconciler_RemovesDropsNoPolicyClaims(t *testing.T) {
	kernel := newFakeKernel()
	kernel.seedIP("198.51.100.4")
	kernel.seedTunnel("10.0.0.1", 0x4d84)
	b := newDriftReconciler(t, kernel, mitigatingPolicy("gtpu-guard", nil, nil))

	// Two passes: removal deliberately requires the entry to have been
	// unclaimed on the previous one as well. See the next test.
	first, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if first.PendingExtraIPs != 1 || first.PendingExtraTunnels != 1 {
		t.Fatalf("expected both to be pending after one pass, got %+v", first)
	}

	report, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if report.ExtraIPs != 1 || report.ExtraTunnels != 1 {
		t.Fatalf("expected one extra of each, got %+v", report)
	}
	if len(kernel.ips) != 0 || len(kernel.tunnels) != 0 {
		t.Fatalf("kernel still holds unclaimed drops: %v / %v", kernel.ips, kernel.tunnels)
	}
}

// The race this reconciler could otherwise lose to itself.
// ThreatScoreWatcher.applyPolicy writes the kernel BEFORE it writes the
// status that claims the write, and this reconciler reads that status
// through a cache that lags the API server again -- so a drop placed
// moments ago is briefly claimed by nothing it can see. Removing it on
// sight would silently revert a live mitigation, which is the exact failure
// the whole mechanism exists to prevent.
func TestBlocklistReconciler_DoesNotRemoveADropItSawForTheFirstTime(t *testing.T) {
	kernel := newFakeKernel()
	b := newDriftReconciler(t, kernel, mitigatingPolicy("gtpu-guard", nil, nil))

	// A mitigation lands between the status read and this pass.
	if err := kernel.Block(net.ParseIP("203.0.113.7")); err != nil {
		t.Fatal(err)
	}

	report, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if report.ExtraIPs != 0 {
		t.Fatalf("a drop seen unclaimed for the first time was removed: %+v", report)
	}
	if !kernel.hasIP("203.0.113.7") {
		t.Fatal("the reconciler reverted a mitigation that had not yet reached status")
	}

	// And once the status catches up, it must stop being a candidate at all.
	policy := &securityv1alpha1.TelecomSecurityPolicy{}
	if getErr := b.Get(context.Background(), client.ObjectKey{Namespace: "telecom", Name: "gtpu-guard"}, policy); getErr != nil {
		t.Fatal(getErr)
	}
	policy.Status.BlockedSourceIPs = []string{"203.0.113.7"}
	if updErr := b.Status().Update(context.Background(), policy); updErr != nil {
		t.Fatal(updErr)
	}

	report, err = b.Sync(context.Background())
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if report.Drifted() {
		t.Fatalf("expected convergence once status caught up, got %+v", report)
	}
	if !kernel.hasIP("203.0.113.7") {
		t.Fatal("the drop was removed on the second pass despite status now claiming it")
	}
}

// The steady state, which is the one that runs every minute forever: when
// the two sides already agree, nothing is written to the kernel at all.
func TestBlocklistReconciler_ConvergedStateWritesNothing(t *testing.T) {
	kernel := newFakeKernel()
	kernel.seedIP("203.0.113.7")
	kernel.seedTunnel("10.0.0.1", 0x4d84)
	b := newDriftReconciler(t, kernel,
		mitigatingPolicy("gtpu-guard", []string{"203.0.113.7"}, []string{"10.0.0.1/0x4d84"}))

	report, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if report.Drifted() {
		t.Fatalf("expected no drift, got %+v", report)
	}
	if len(kernel.calls) != 0 {
		t.Fatalf("expected no kernel writes, got %v", kernel.calls)
	}
	if report.DesiredIPs != 1 || report.KernelIPs != 1 || report.DesiredTunnels != 1 || report.KernelTunnels != 1 {
		t.Fatalf("set sizes misreported: %+v", report)
	}
}

// A policy mid-deletion is the finalizer's business, and it is actively
// unblocking everything the status still lists. Re-applying from here would
// put back what the finalizer just removed, with the two racing each other
// until one of them happened to run last.
func TestBlocklistReconciler_IgnoresPoliciesBeingDeleted(t *testing.T) {
	kernel := newFakeKernel()
	deleting := mitigatingPolicy("going-away", []string{"203.0.113.7"}, []string{"10.0.0.1/0x4d84"})
	now := metav1.NewTime(time.Now())
	deleting.DeletionTimestamp = &now
	deleting.Finalizers = []string{telecomSecurityPolicyFinalizer}

	b := newDriftReconciler(t, kernel, deleting)
	report, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if report.Drifted() {
		t.Fatalf("a deleting policy should contribute nothing, got %+v", report)
	}
	if len(kernel.calls) != 0 {
		t.Fatalf("expected no kernel writes, got %v", kernel.calls)
	}
}

// The failure mode that would turn this safety net into the outage it is
// meant to prevent: a failed List looks exactly like "no policy wants
// anything blocked". Acting on that would flush every active mitigation out
// of the kernel because the API server was briefly unreachable.
func TestBlocklistReconciler_ListFailureNeverFlushesTheKernel(t *testing.T) {
	kernel := newFakeKernel()
	kernel.seedIP("203.0.113.7")
	kernel.seedTunnel("10.0.0.1", 0x4d84)

	failing := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return fmt.Errorf("apiserver unavailable")
			},
		}).
		Build()

	b := &BlocklistReconciler{Client: failing, Log: testr.New(t), Blocklist: kernel, Inspector: kernel}
	if _, err := b.Sync(context.Background()); err == nil {
		t.Fatal("expected Sync to fail when the policy list cannot be read")
	}
	if len(kernel.calls) != 0 {
		t.Fatalf("a failed list must not touch the kernel, got %v", kernel.calls)
	}
	if len(kernel.ips) != 1 || len(kernel.tunnels) != 1 {
		t.Fatal("active drops were removed because the API server was unreachable")
	}
}

// Status is written by ThreatScoreWatcher from whatever string the event
// carried; the kernel hands back 4 raw bytes. "::ffff:10.0.0.1" and
// "10.0.0.1" are the same endpoint and pkg/ebpf routes both into the IPv4
// map, so comparing the raw strings would report permanent drift and
// re-block an already-blocked address on every single tick.
func TestBlocklistReconciler_CanonicalizesV4MappedAddresses(t *testing.T) {
	kernel := newFakeKernel()
	kernel.seedIP("10.0.0.1")
	b := newDriftReconciler(t, kernel, mitigatingPolicy("gtpu-guard", []string{"::ffff:10.0.0.1"}, nil))

	report, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if report.Drifted() {
		t.Fatalf("v4-mapped status entry should match the IPv4 map entry, got %+v", report)
	}
}

// TEID 0 is the "no tunnel identity" sentinel and BlockTunnel refuses it by
// design. A status entry carrying it (a hand-edited status, an older
// operator) must be skipped, not retried every tick forever.
func TestBlocklistReconciler_SkipsUnblockableStatusEntries(t *testing.T) {
	kernel := newFakeKernel()
	b := newDriftReconciler(t, kernel,
		mitigatingPolicy("gtpu-guard", []string{"not-an-ip"}, []string{"10.0.0.1/0x0", "garbage"}))

	report, err := b.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if report.Drifted() {
		t.Fatalf("unusable status entries should contribute nothing, got %+v", report)
	}
}

// One failing map write must not abandon the rest: each correction is
// independent, and every one left undone is a drop that is wrong in the
// kernel. The error is still reported.
func TestBlocklistReconciler_AppliesEveryCorrectionDespiteOneFailure(t *testing.T) {
	kernel := newFakeKernel()
	kernel.blockErr = fmt.Errorf("map is full")
	kernel.seedIP("198.51.100.4") // unclaimed: must still be removed
	b := newDriftReconciler(t, kernel,
		mitigatingPolicy("gtpu-guard", []string{"203.0.113.7"}, nil))

	if _, err := b.Sync(context.Background()); err == nil {
		t.Fatal("expected Sync to report the failed insert")
	}
	report, err := b.Sync(context.Background()) // second pass: the removal is now eligible
	if err == nil {
		t.Fatal("expected Sync to report the failed insert")
	}
	if report.MissingIPs != 1 || report.ExtraIPs != 1 {
		t.Fatalf("both corrections should have been attempted, got %+v", report)
	}
	if _, stillThere := kernel.ips["198.51.100.4"]; stillThere {
		t.Fatal("the unclaimed drop was abandoned because an unrelated insert failed")
	}
}

// Drift is a per-node fact about a node-local kernel object, so every
// replica reconciles its own -- the same reasoning as
// pkg/ingestion.Publisher, and the opposite of ThreatScoreWatcher.
func TestBlocklistReconciler_IsNotLeaderGated(t *testing.T) {
	b := &BlocklistReconciler{}
	if b.NeedLeaderElection() {
		t.Fatal("expected BlocklistReconciler.NeedLeaderElection() = false (must run on every replica)")
	}
}

// Without a real attach there is no kernel state to compare against, so
// Start must return rather than "correcting" a permanently empty answer
// against every policy on every tick.
func TestBlocklistReconciler_StartsDisabledWithoutAnInspector(t *testing.T) {
	b := &BlocklistReconciler{Log: testr.New(t)}
	done := make(chan error, 1)
	go func() { done <- b.Start(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start blocked with no blocklist wired; it should have returned immediately")
	}
}

// Start's first act is the restart replay, before any tick elapses -- a
// reconciler that only corrected on its interval would leave a minute-wide
// window after every rollout in which the drops are not in force.
func TestBlocklistReconciler_StartSyncsBeforeTheFirstTick(t *testing.T) {
	kernel := newFakeKernel()
	b := newDriftReconciler(t, kernel, mitigatingPolicy("gtpu-guard", []string{"203.0.113.7"}, nil))
	b.Interval = time.Hour // far longer than this test waits

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = b.Start(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for kernel.blockedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if !kernel.hasIP("203.0.113.7") {
		t.Fatal("Start did not re-apply the policy's blocks before its first tick")
	}
}

func TestBlocklistReconciler_ExportsDriftAndPinMetrics(t *testing.T) {
	BlocklistDrift.Reset()
	BlocklistEntries.Reset()
	BlocklistCapacity.Reset()

	kernel := newFakeKernel()
	kernel.seedIP("198.51.100.4")
	b := newDriftReconciler(t, kernel, mitigatingPolicy("gtpu-guard", []string{"203.0.113.7"}, nil))
	for i := 0; i < 2; i++ { // the removal needs two passes; see the two-pass rule
		if _, err := b.Sync(context.Background()); err != nil {
			t.Fatalf("Sync: %v", err)
		}
	}

	// One per pass for the re-application (the fake kernel's Block is
	// recorded, so the second pass sees it present -- but the policy's
	// desired entry is applied on the first pass only).
	if got := testutil.ToFloat64(BlocklistDrift.WithLabelValues("ip", "missing")); got != 1 {
		t.Fatalf("drift{ip,missing} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(BlocklistDrift.WithLabelValues("ip", "extra")); got != 1 {
		t.Fatalf("drift{ip,extra} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(BlocklistEntries.WithLabelValues("ip", "desired")); got != 1 {
		t.Fatalf("entries{ip,desired} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(BlocklistCapacity.WithLabelValues("tunnel")); got != 16384 {
		t.Fatalf("capacity{tunnel} = %v, want 16384", got)
	}
	if got := testutil.ToFloat64(ObservationMapCapacity.WithLabelValues("tunnel_rate")); got != 65536 {
		t.Fatalf("observation capacity = %v, want 65536", got)
	}

	b.reportPinState()
	if got := testutil.ToFloat64(EnforcementPinned); got != 1 {
		t.Fatalf("enforcement_pinned = %v, want 1 for a pinned loader", got)
	}
	kernel.pinPath = ""
	b.reportPinState()
	if got := testutil.ToFloat64(EnforcementPinned); got != 0 {
		t.Fatalf("enforcement_pinned = %v, want 0 for an unpinned loader", got)
	}
}
