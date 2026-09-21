package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
)

func TestReconciler_TransitionsPendingToMonitoring(t *testing.T) {
	scheme := newScheme()

	policy := &securityv1alpha1.TelecomSecurityPolicy{}
	policy.Namespace = "telecom-core"
	policy.Name = "protect-amf-core"
	policy.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	r := &Reconciler{
		Client: fakeClient,
		Log:    testr.New(t),
		Index:  NewPolicyIndex(),
	}

	req := ctrl.Request{NamespacedName: nnFor(policy)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}

	if got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("expected phase %q, got %q", securityv1alpha1.PolicyPhaseMonitoring, got.Status.Phase)
	}

	matches := r.Index.MatchingPolicies("telecom-core", map[string]string{"app": "amf-service"})
	if len(matches) != 1 {
		t.Fatalf("expected policy to be indexed after reconcile, got %d matches", len(matches))
	}

	if !controllerutil.ContainsFinalizer(&got, telecomSecurityPolicyFinalizer) {
		t.Fatalf("expected %q finalizer to be added on first reconcile", telecomSecurityPolicyFinalizer)
	}
}

// TestReconciler_DeletionReleasesQuarantineAndUnblocksIPs is the finalizer
// test: it simulates a policy that has already mitigated (quarantine
// applied, two attacker IPs blocked, as recorded in Status.BlockedSourceIPs
// by ThreatScoreWatcher.applyPolicy), then deletes it and asserts the
// operator actually releases/unblocks both before letting the object go --
// not just that a status field changes (see ROADMAP.md Phase 1,
// "Finalizer-based cleanup").
func TestReconciler_DeletionReleasesQuarantineAndUnblocksIPs(t *testing.T) {
	scheme := newScheme()

	policy := newTestPolicy(securityv1alpha1.SensitivityHigh, true, true, true)
	controllerutil.AddFinalizer(policy, telecomSecurityPolicyFinalizer)
	policy.Status.Phase = securityv1alpha1.PolicyPhaseMitigating
	policy.Status.BlockedSourceIPs = []string{"203.0.113.7", "198.51.100.9"}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	idx := NewPolicyIndex()
	idx.Put(policy)

	blocklist := &recordingBlocklist{}
	meshAdapter := &recordingMesh{}

	r := &Reconciler{
		Client:    fakeClient,
		Log:       testr.New(t),
		Index:     idx,
		Blocklist: blocklist,
		Mesh:      meshAdapter,
	}

	ctx := context.Background()
	if err := fakeClient.Delete(ctx, policy); err != nil {
		t.Fatalf("delete: %v", err)
	}

	req := ctrl.Request{NamespacedName: nnFor(policy)}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile (finalize) returned error: %v", err)
	}

	if len(meshAdapter.released) != 1 || meshAdapter.released[0] != policy.Namespace {
		t.Fatalf("expected mesh quarantine released for namespace %q, got %v", policy.Namespace, meshAdapter.released)
	}
	if len(blocklist.unblocked) != 2 {
		t.Fatalf("expected both blocked IPs to be unblocked, got %v", blocklist.unblocked)
	}
	for _, ip := range policy.Status.BlockedSourceIPs {
		found := false
		for _, u := range blocklist.unblocked {
			if u == ip {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %s to be unblocked, got %v", ip, blocklist.unblocked)
		}
	}

	// The finalizer must actually be gone -- and with it, per real API server
	// (and the fake client's) semantics, the object itself: a Delete only
	// removes an object once its finalizer list is empty.
	var afterDelete securityv1alpha1.TelecomSecurityPolicy
	err := fakeClient.Get(ctx, req.NamespacedName, &afterDelete)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected policy to be fully deleted after finalizer removal, got err=%v obj=%+v", err, afterDelete)
	}

	if matches := idx.MatchingPolicies("telecom-core", map[string]string{"app": "amf-service"}); len(matches) != 0 {
		t.Fatalf("expected index to drop the deleted policy, got %+v", matches)
	}
}

// TestReconciler_DeletionWithoutMitigationIsANoOp covers the common case: a
// policy deleted before it ever mitigated anything still needs its
// finalizer removed so the object can go away, but Release/Unblock have
// nothing real to do. Both are documented idempotent, so calling them
// unconditionally (see Reconciler.finalize) must not error even here.
func TestReconciler_DeletionWithoutMitigationIsANoOp(t *testing.T) {
	scheme := newScheme()

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	controllerutil.AddFinalizer(policy, telecomSecurityPolicyFinalizer)
	policy.Status.Phase = securityv1alpha1.PolicyPhaseMonitoring

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	idx := NewPolicyIndex()
	idx.Put(policy)

	r := &Reconciler{
		Client:    fakeClient,
		Log:       testr.New(t),
		Index:     idx,
		Blocklist: &recordingBlocklist{},
		Mesh:      &recordingMesh{},
	}

	ctx := context.Background()
	if err := fakeClient.Delete(ctx, policy); err != nil {
		t.Fatalf("delete: %v", err)
	}

	req := ctrl.Request{NamespacedName: nnFor(policy)}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile (finalize) returned error: %v", err)
	}

	var afterDelete securityv1alpha1.TelecomSecurityPolicy
	err := fakeClient.Get(ctx, req.NamespacedName, &afterDelete)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected policy to be fully deleted after finalizer removal, got err=%v obj=%+v", err, afterDelete)
	}
}

func TestReconciler_DeletedPolicyIsRemovedFromIndex(t *testing.T) {
	scheme := newScheme()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	idx := NewPolicyIndex()
	ghost := &securityv1alpha1.TelecomSecurityPolicy{}
	ghost.Namespace = "telecom-core"
	ghost.Name = "already-deleted"
	ghost.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	idx.Put(ghost)

	r := &Reconciler{Client: fakeClient, Log: testr.New(t), Index: idx}

	req := ctrl.Request{NamespacedName: nnFor(ghost)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile returned error for a not-found object: %v", err)
	}

	if matches := idx.MatchingPolicies("telecom-core", map[string]string{"app": "amf-service"}); len(matches) != 0 {
		t.Fatalf("expected index to drop the deleted policy, got %+v", matches)
	}
}

// deEscalationFixture builds a Mitigating policy with two blocked IPs and a
// quarantine already recorded (mirroring what ThreatScoreWatcher.applyPolicy
// leaves behind), lastMitigated ago, wired to a fake clock fixed at "now".
func deEscalationFixture(t *testing.T, lastMitigated time.Duration, dwell time.Duration) (*Reconciler, *securityv1alpha1.TelecomSecurityPolicy, *recordingBlocklist, *recordingMesh) {
	t.Helper()
	scheme := newScheme()

	policy := newTestPolicy(securityv1alpha1.SensitivityHigh, true, true, true)
	controllerutil.AddFinalizer(policy, telecomSecurityPolicyFinalizer)
	policy.Status.Phase = securityv1alpha1.PolicyPhaseMitigating
	policy.Status.BlockedSourceIPs = []string{"203.0.113.7", "198.51.100.9"}

	now := time.Now()
	mitigatedAt := metav1.NewTime(now.Add(-lastMitigated))
	policy.Status.LastMitigationTime = &mitigatedAt

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	idx := NewPolicyIndex()
	idx.Put(policy)

	blocklist := &recordingBlocklist{}
	meshAdapter := &recordingMesh{}

	r := &Reconciler{
		Client:            fakeClient,
		Log:               testr.New(t),
		Index:             idx,
		Blocklist:         blocklist,
		Mesh:              meshAdapter,
		DeEscalationDwell: dwell,
		Now:               func() time.Time { return now },
	}
	return r, policy, blocklist, meshAdapter
}

// TestReconciler_DeEscalatesAfterQuietDwellPeriod is the de-escalation
// happy path (ROADMAP.md Phase 1, "Automatic de-escalation"): a policy that
// mitigated well outside the dwell window gets its blocklist entries
// unblocked and its mesh quarantine released automatically, with no manual
// kubectl intervention.
func TestReconciler_DeEscalatesAfterQuietDwellPeriod(t *testing.T) {
	r, policy, blocklist, meshAdapter := deEscalationFixture(t, 10*time.Minute, 5*time.Minute)

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: nnFor(policy)}
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue once de-escalated, got RequeueAfter=%v", result.RequeueAfter)
	}

	if len(blocklist.unblocked) != 2 {
		t.Fatalf("expected both blocked IPs to be unblocked, got %v", blocklist.unblocked)
	}
	if len(meshAdapter.released) != 1 || meshAdapter.released[0] != policy.Namespace {
		t.Fatalf("expected mesh quarantine released for namespace %q, got %v", policy.Namespace, meshAdapter.released)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := r.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("expected phase %q after de-escalation, got %q", securityv1alpha1.PolicyPhaseMonitoring, got.Status.Phase)
	}
	if len(got.Status.BlockedSourceIPs) != 0 {
		t.Fatalf("expected BlockedSourceIPs cleared after de-escalation, got %v", got.Status.BlockedSourceIPs)
	}
}

// TestReconciler_DoesNotDeEscalateBeforeQuietDwellPeriod is the anti-evasion
// half: a policy mitigated recently must NOT be unblocked yet, and the
// Reconciler must self-schedule a requeue for exactly when it becomes
// eligible rather than relying on another event to wake it up (a blocked
// source produces no further ThreatScoreEvents — see tryDeEscalate's doc
// comment — so nothing else would).
func TestReconciler_DoesNotDeEscalateBeforeQuietDwellPeriod(t *testing.T) {
	r, policy, blocklist, meshAdapter := deEscalationFixture(t, 1*time.Minute, 5*time.Minute)

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: nnFor(policy)}
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if len(blocklist.unblocked) != 0 || len(meshAdapter.released) != 0 {
		t.Fatalf("expected no de-escalation before the dwell period elapses, got unblocked=%v released=%v",
			blocklist.unblocked, meshAdapter.released)
	}

	wantRequeue := 4 * time.Minute // 5m dwell - 1m elapsed
	if result.RequeueAfter <= 0 || result.RequeueAfter > wantRequeue {
		t.Fatalf("expected a requeue around %v (remaining dwell), got %v", wantRequeue, result.RequeueAfter)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := r.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMitigating {
		t.Fatalf("expected phase to stay %q before the dwell period elapses, got %q", securityv1alpha1.PolicyPhaseMitigating, got.Status.Phase)
	}
	if len(got.Status.BlockedSourceIPs) != 2 {
		t.Fatalf("expected BlockedSourceIPs untouched before de-escalation, got %v", got.Status.BlockedSourceIPs)
	}
}

// Blocked tunnels need the same reversal plumbing blocked IPs have: without
// it a per-tunnel drop is permanent, which is worse than the source-wide
// one it replaces.
func TestTryDeEscalate_UnblocksTunnelsAfterTheQuietPeriod(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	policy.Status.Phase = securityv1alpha1.PolicyPhaseMitigating
	policy.Status.BlockedTunnels = []string{"10.0.0.1/0x4d84", "10.0.0.1/0xc9c7"}
	long := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	policy.Status.LastMitigationTime = &long

	r, blocklist, _ := newReconcilerFixtureWithDoubles(t, policy)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if len(blocklist.unblockedTunnels) != 2 {
		t.Fatalf("expected both tunnels unblocked, got %v", blocklist.unblockedTunnels)
	}
	var got securityv1alpha1.TelecomSecurityPolicy
	if err := r.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Status.BlockedTunnels) != 0 || got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("after de-escalation: phase=%s tunnels=%v", got.Status.Phase, got.Status.BlockedTunnels)
	}
}

// A status entry that can't be parsed must not wedge de-escalation for the
// tunnels behind it, or a single bad write strands every other drop.
func TestTryDeEscalate_SkipsAnUnparseableTunnelEntry(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	policy.Status.Phase = securityv1alpha1.PolicyPhaseMitigating
	policy.Status.BlockedTunnels = []string{"garbage", "10.0.0.1/0x4d84"}
	long := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	policy.Status.LastMitigationTime = &long

	r, blocklist, _ := newReconcilerFixtureWithDoubles(t, policy)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(blocklist.unblockedTunnels) != 1 || blocklist.unblockedTunnels[0] != "10.0.0.1/0x4d84" {
		t.Fatalf("the good entry should still be unblocked, got %v", blocklist.unblockedTunnels)
	}
}

func TestFinalize_UnblocksTunnels(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	policy.Finalizers = []string{telecomSecurityPolicyFinalizer}
	now := metav1.Now()
	policy.DeletionTimestamp = &now
	policy.Status.BlockedTunnels = []string{"10.0.0.1/0x4d84"}

	r, blocklist, _ := newReconcilerFixtureWithDoubles(t, policy)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(blocklist.unblockedTunnels) != 1 {
		t.Fatalf("deleting a policy must release its tunnel drops, got %v", blocklist.unblockedTunnels)
	}
}

// newReconcilerFixtureWithDoubles builds a Reconciler over a fake client
// preloaded with policy, returning the recording doubles so a test can
// assert what the reconciler actually asked the kernel and the mesh to do.
func newReconcilerFixtureWithDoubles(t *testing.T, policy *securityv1alpha1.TelecomSecurityPolicy) (*Reconciler, *recordingBlocklist, *recordingMesh) {
	t.Helper()
	fakeClient := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	blocklist := &recordingBlocklist{}
	meshAdapter := &recordingMesh{}
	return &Reconciler{
		Client:    fakeClient,
		Log:       testr.New(t),
		Index:     NewPolicyIndex(),
		Blocklist: blocklist,
		Mesh:      meshAdapter,
	}, blocklist, meshAdapter
}
