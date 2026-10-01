package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Leader failover with in-flight scores (ROADMAP.md Phase 4). Delivery is
// at-least-once and the mitigation path is written to be idempotent; these
// exercise that idempotency THROUGH the two things a real failover does:
// redelivering a score the dead leader may have already acted on, and a new
// leader rebuilding its in-memory index from the API before processing.

// A score redelivered after the previous leader already acted on it (the
// at-least-once case, and what JetStream does on resubscribe) must not
// double-apply or corrupt state: the block stays a single entry, the status
// stays one tunnel, the phase stays Mitigating.
func TestFailover_RedeliveredScoreIsIdempotent(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	pod := newTestPod(policy.Namespace, "upf-0")
	w, blocklist, _ := newWatcherFixture(t, policy, pod)

	score := events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		TEID: 0x4D84, Score: 1.0, Model: "rule:x",
	}

	// Delivered three times -- once by the dead leader (conceptually), twice
	// more on redelivery to the new one.
	for i := 0; i < 3; i++ {
		cur := policyFromIndex(t, w, policy)
		if err := w.applyPolicy(context.Background(), cur, score); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Status.BlockedTunnels) != 1 {
		t.Fatalf("redelivery grew the blocked set to %v; status is not a set under redelivery", got.Status.BlockedTunnels)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMitigating {
		t.Fatalf("phase = %q after redelivery, want Mitigating", got.Status.Phase)
	}
	// The kernel key is the same each time -- a map overwrite, not a leak.
	// (The recording double counts calls; the real map holds one key.) What
	// matters is the DESIRED state stayed singular.
	if len(blocklist.blockedTunnels) < 1 {
		t.Fatal("the block was never applied")
	}
}

// A new leader must rebuild its index from the API before it can match
// redelivered scores -- otherwise the buffered scores JetStream replays on
// resubscribe match nothing, get acked, and are lost. warmIndex is what a
// freshly-elected leader runs; this proves a score for a policy the new
// leader never saw created still matches after it.
func TestFailover_NewLeaderWarmsIndexAndMatches(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	pod := newTestPod(policy.Namespace, "upf-0")

	apiClient := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy, pod).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	// The NEW leader: a fresh watcher with an EMPTY index, as it would be
	// right after election. No Index.Put(policy) -- that is the point.
	blocklist := &recordingBlocklist{}
	w := &ThreatScoreWatcher{
		Client: apiClient, Log: testr.New(t), Index: NewPolicyIndex(),
		Blocklist: blocklist, Mesh: &recordingMesh{}, BaseThreshold: 0.85,
	}

	// Before warming, the new leader's index is empty: a redelivered score
	// would match no policy and be lost.
	if got := w.Index.MatchingPolicies(policy.Namespace, pod.Labels); len(got) != 0 {
		t.Fatalf("index unexpectedly already populated with %d policies", len(got))
	}

	// warmIndex is what Start runs before subscribing.
	if err := w.warmIndex(context.Background()); err != nil {
		t.Fatalf("warmIndex: %v", err)
	}

	if err := w.handle(context.Background(), events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		TEID: 0x4D84, Score: 1.0, Model: "rule:x",
	}); err != nil {
		t.Fatalf("handle after warmIndex: %v", err)
	}
	if len(blocklist.blockedTunnels) != 1 {
		t.Fatal("the new leader did not act on a redelivered score after warming its index")
	}
	if testutil.ToFloat64(ThreatScoresReceived.WithLabelValues("rule")) < 1 {
		t.Fatal("the redelivered score was not counted as received")
	}
}

// warmIndex must not resurrect a policy that is mid-deletion -- the
// finalizer is removing it, and re-indexing it would let a redelivered score
// re-mitigate a policy on its way out.
func TestFailover_WarmIndexSkipsDeletingPolicies(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	now := metav1.Now()
	policy.DeletionTimestamp = &now
	policy.Finalizers = []string{telecomSecurityPolicyFinalizer}

	apiClient := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()
	w := &ThreatScoreWatcher{Client: apiClient, Log: testr.New(t), Index: NewPolicyIndex(), BaseThreshold: 0.85}

	if err := w.warmIndex(context.Background()); err != nil {
		t.Fatalf("warmIndex: %v", err)
	}
	if got := w.Index.MatchingPolicies(policy.Namespace, map[string]string{"app": "amf-service"}); len(got) != 0 {
		t.Fatal("warmIndex resurrected a policy that is being deleted")
	}
}
