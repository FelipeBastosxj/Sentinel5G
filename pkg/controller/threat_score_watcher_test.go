package controller

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Regression guard: ThreatScoreWatcher must stay leader-gated (exactly one
// active instance cluster-wide) — see NeedLeaderElection's doc comment for
// why this is defensive hardening, not a bug fix.
func TestThreatScoreWatcher_IsLeaderGated(t *testing.T) {
	w := &ThreatScoreWatcher{}
	if !w.NeedLeaderElection() {
		t.Fatal("expected ThreatScoreWatcher.NeedLeaderElection() = true (must run as a single active instance)")
	}
}

// recordingBlocklist and recordingMesh are in-memory test doubles standing
// in for pkg/ebpf.BlocklistUpdater and pkg/mesh.Adapter, so applyPolicy's
// decisions can be asserted without a real kernel or Istio control plane.
// Mutex-guarded because the real implementations are goroutine-safe (a map
// syscall, a Kubernetes client) and stress_test.go drives them from many
// goroutines at once; a double that isn't would report its own race as the
// watcher's.
type recordingBlocklist struct {
	mu                 sync.Mutex
	blocked, unblocked []string
}

func (r *recordingBlocklist) Block(ip net.IP) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocked = append(r.blocked, ip.String())
	return nil
}
func (r *recordingBlocklist) Unblock(ip net.IP) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unblocked = append(r.unblocked, ip.String())
	return nil
}
func (r *recordingBlocklist) Close() error { return nil }

type recordingMesh struct {
	mu                    sync.Mutex
	quarantined, released []string
}

func (r *recordingMesh) Quarantine(_ context.Context, namespace string, _ map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.quarantined = append(r.quarantined, namespace)
	return nil
}
func (r *recordingMesh) Release(_ context.Context, namespace string, _ map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = append(r.released, namespace)
	return nil
}

func newWatcherFixture(t *testing.T, policy *securityv1alpha1.TelecomSecurityPolicy, pod *corev1.Pod) (*ThreatScoreWatcher, *recordingBlocklist, *recordingMesh) {
	t.Helper()
	scheme := newScheme()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy, pod).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	idx := NewPolicyIndex()
	idx.Put(policy)

	blocklist := &recordingBlocklist{}
	meshAdapter := &recordingMesh{}

	w := &ThreatScoreWatcher{
		Client:        fakeClient,
		Log:           testr.New(t),
		Index:         idx,
		Blocklist:     blocklist,
		Mesh:          meshAdapter,
		BaseThreshold: 0.85,
	}
	return w, blocklist, meshAdapter
}

func newTestPolicy(sensitivity securityv1alpha1.Sensitivity, autoMitigate, ebpfBlock, isolatePod bool) *securityv1alpha1.TelecomSecurityPolicy {
	p := &securityv1alpha1.TelecomSecurityPolicy{}
	p.Namespace = "telecom-core"
	p.Name = "protect-amf-core"
	p.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	p.Spec.ThreatDetection = securityv1alpha1.ThreatDetectionSpec{Sensitivity: sensitivity, AutoMitigate: autoMitigate}
	p.Spec.Actions = securityv1alpha1.ActionsSpec{EbpfBlock: ebpfBlock, IsolatePod: isolatePod}
	return p
}

func newTestPod(namespace, name string) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Namespace = namespace
	pod.Name = name
	pod.Labels = map[string]string{"app": "amf-service"}
	return pod
}

func TestApplyPolicy_BelowThresholdOnlyUpdatesScore(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	event := events.ThreatScoreEvent{
		Namespace: policy.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     0.2,
	}

	if err := w.applyPolicy(context.Background(), policy, event); err != nil {
		t.Fatalf("applyPolicy returned error: %v", err)
	}

	if len(blocklist.blocked) != 0 || len(meshAdapter.quarantined) != 0 {
		t.Fatalf("expected no mitigation below threshold, got blocked=%v quarantined=%v", blocklist.blocked, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get after applyPolicy: %v", err)
	}
	if got.Status.ObservedThreatScore != "0.2000" {
		t.Fatalf("expected observed score 0.2000, got %q", got.Status.ObservedThreatScore)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("expected phase Monitoring, got %q", got.Status.Phase)
	}
}

func TestApplyPolicy_AboveThresholdWithoutAutoMitigateOnlyAlerts(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	event := events.ThreatScoreEvent{
		Namespace: policy.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     0.99,
	}

	if err := w.applyPolicy(context.Background(), policy, event); err != nil {
		t.Fatalf("applyPolicy returned error: %v", err)
	}

	if len(blocklist.blocked) != 0 || len(meshAdapter.quarantined) != 0 {
		t.Fatalf("expected no mitigation when AutoMitigate is false, got blocked=%v quarantined=%v", blocklist.blocked, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get after applyPolicy: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseAlerting {
		t.Fatalf("expected phase Alerting, got %q", got.Status.Phase)
	}
}

func TestApplyPolicy_AboveThresholdWithAutoMitigateBlocksAndQuarantines(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityHigh, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	event := events.ThreatScoreEvent{
		Namespace: policy.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     0.9,
	}

	if err := w.applyPolicy(context.Background(), policy, event); err != nil {
		t.Fatalf("applyPolicy returned error: %v", err)
	}

	if len(blocklist.blocked) != 1 || blocklist.blocked[0] != "203.0.113.7" {
		t.Fatalf("expected 203.0.113.7 to be blocked, got %v", blocklist.blocked)
	}
	if len(meshAdapter.quarantined) != 1 || meshAdapter.quarantined[0] != policy.Namespace {
		t.Fatalf("expected namespace %q to be quarantined, got %v", policy.Namespace, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get after applyPolicy: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMitigating {
		t.Fatalf("expected phase Mitigating, got %q", got.Status.Phase)
	}
	if got.Status.LastMitigationTime == nil {
		t.Fatalf("expected LastMitigationTime to be set")
	}
	if len(got.Status.BlockedSourceIPs) != 1 || got.Status.BlockedSourceIPs[0] != "203.0.113.7" {
		t.Fatalf("expected BlockedSourceIPs to record 203.0.113.7, got %v", got.Status.BlockedSourceIPs)
	}

	// A duplicate delivery of the same event (NATS is at-least-once) must not
	// grow BlockedSourceIPs -- it has to stay a set for the finalizer to
	// Unblock each real attacker IP exactly once, not repeat entries. Pass
	// the just-persisted `got` (not the original, now-stale `policy`) as the
	// base, mirroring how production's handle() re-reads the current policy
	// from the Index (kept fresh by updateStatus's Index.Put) before every
	// applyPolicy call.
	if err := w.applyPolicy(context.Background(), &got, event); err != nil {
		t.Fatalf("applyPolicy (redelivery) returned error: %v", err)
	}
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get after redelivered applyPolicy: %v", err)
	}
	if len(got.Status.BlockedSourceIPs) != 1 {
		t.Fatalf("expected BlockedSourceIPs to stay deduplicated after redelivery, got %v", got.Status.BlockedSourceIPs)
	}
}

// A rule-sourced score (pkg/detect) must travel the exact same path as an ML
// one -- no bypass of policy matching, sensitivity, or autoMitigate. This
// test is what keeps that a property rather than an assumption, since the
// watcher has no code branch for it at all.
//
// It also pins the strict-comparison detail the detector depends on: at LOW
// sensitivity the effective threshold clamps to exactly 1.00 and applyPolicy
// compares `score < threshold`, so a rule score of 1.0 fires and anything
// below it silently would not.
func TestApplyPolicy_RuleSourcedScoreFiresEvenAtLowSensitivity(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityLow, true, true, true)
	pod := newTestPod(policy.Namespace, "upf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	event := events.ThreatScoreEvent{
		Namespace: policy.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     1.0,
		Model:     "rule:gtpu-tunnel-flood",
	}

	if err := w.applyPolicy(context.Background(), policy, event); err != nil {
		t.Fatalf("applyPolicy: %v", err)
	}

	if len(blocklist.blocked) != 1 || len(meshAdapter.quarantined) != 1 {
		t.Fatalf("expected the rule score to mitigate at low sensitivity, got blocked=%v quarantined=%v", blocklist.blocked, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMitigating {
		t.Fatalf("expected Mitigating, got %q", got.Status.Phase)
	}
}

// The counterpart, and the one that matters for a pilot: a deterministic
// detector must NOT override autoMitigate: false.
func TestApplyPolicy_RuleSourcedScoreStillRespectsDetectionOnly(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, true, true)
	pod := newTestPod(policy.Namespace, "upf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "203.0.113.7",
		Score: 1.0, Model: "rule:gtpu-tunnel-flood",
	})
	if err != nil {
		t.Fatalf("applyPolicy: %v", err)
	}

	if len(blocklist.blocked) != 0 || len(meshAdapter.quarantined) != 0 {
		t.Fatalf("a rule score bypassed autoMitigate: false: blocked=%v quarantined=%v", blocklist.blocked, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseAlerting {
		t.Fatalf("expected Alerting, got %q", got.Status.Phase)
	}
}

// An operator restart must not lose the scores JetStream buffered while it
// was down. The durable re-delivers them the instant Start subscribes, and
// Reconciler populates the index only as it visits each policy -- so Start
// has to seed the index from the cache first or every buffered score is
// matched against nothing, acked, and gone. Found on a real cluster.
func TestThreatScoreWatcher_WarmsIndexFromExistingPolicies(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, _ := newWatcherFixture(t, policy, pod)

	// Simulate the fresh-process state: the cluster has the policy, the
	// in-memory index does not.
	w.Index = NewPolicyIndex()
	if got := w.Index.MatchingPolicies(policy.Namespace, pod.Labels); len(got) != 0 {
		t.Fatalf("precondition: expected an empty index, got %d", len(got))
	}

	if err := w.warmIndex(context.Background()); err != nil {
		t.Fatalf("warmIndex: %v", err)
	}

	err := w.handle(context.Background(), events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "203.0.113.7", Score: 0.99,
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(blocklist.blocked) != 1 {
		t.Fatalf("a score arriving right after start was dropped: blocked=%v", blocklist.blocked)
	}
}

func TestThreatScoreWatcher_WarmIndexSkipsPoliciesBeingDeleted(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	now := metav1.Now()
	policy.DeletionTimestamp = &now
	policy.Finalizers = []string{telecomSecurityPolicyFinalizer}
	pod := newTestPod(policy.Namespace, "amf-0")
	w, _, _ := newWatcherFixture(t, policy, pod)
	w.Index = NewPolicyIndex()

	if err := w.warmIndex(context.Background()); err != nil {
		t.Fatalf("warmIndex: %v", err)
	}
	if got := w.Index.MatchingPolicies(policy.Namespace, pod.Labels); len(got) != 0 {
		t.Fatalf("a policy mid-deletion was resurrected into the index")
	}
}

// Review finding, pinned: a sub-threshold score arriving while a policy is
// Mitigating must not move it to Monitoring, because only tryDeEscalate
// (which runs solely in Mitigating) ever unblocks. With the AI engine
// scoring every event from every source on the Pod, that benign score
// arrives within milliseconds of the block -- and used to orphan it.
func TestApplyPolicy_BenignScoreDoesNotEndAMitigation(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, _ := newWatcherFixture(t, policy, pod)

	attack := events.ThreatScoreEvent{Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "203.0.113.7", Score: 0.99}
	if err := w.applyPolicy(context.Background(), policy, attack); err != nil {
		t.Fatalf("applyPolicy (attack): %v", err)
	}
	var mitigating securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &mitigating); err != nil {
		t.Fatal(err)
	}
	if mitigating.Status.Phase != securityv1alpha1.PolicyPhaseMitigating || len(blocklist.blocked) != 1 {
		t.Fatalf("precondition: expected Mitigating with one block, got %s / %v", mitigating.Status.Phase, blocklist.blocked)
	}

	benign := events.ThreatScoreEvent{Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "198.51.100.9", Score: 0.05}
	if err := w.applyPolicy(context.Background(), &mitigating, benign); err != nil {
		t.Fatalf("applyPolicy (benign): %v", err)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMitigating {
		t.Fatalf("a benign score ended the mitigation: phase is %s, and nothing will ever unblock %v", got.Status.Phase, got.Status.BlockedSourceIPs)
	}
	if got.Status.ObservedThreatScore != "0.0500" {
		t.Fatalf("the benign score should still be recorded, got %q", got.Status.ObservedThreatScore)
	}
	if len(got.Status.BlockedSourceIPs) != 1 {
		t.Fatalf("BlockedSourceIPs = %v, want the original block retained for tryDeEscalate", got.Status.BlockedSourceIPs)
	}
}

// ...but Alerting has no side effects to reverse, so it does return to
// Monitoring on a benign score, as before.
func TestApplyPolicy_BenignScoreDoesEndAlerting(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, _, _ := newWatcherFixture(t, policy, pod)

	if err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "203.0.113.7", Score: 0.99}); err != nil {
		t.Fatal(err)
	}
	var alerting securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &alerting); err != nil {
		t.Fatal(err)
	}
	if err := w.applyPolicy(context.Background(), &alerting, events.ThreatScoreEvent{Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "203.0.113.7", Score: 0.05}); err != nil {
		t.Fatal(err)
	}
	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("expected Alerting -> Monitoring on a benign score, got %s", got.Status.Phase)
	}
}
