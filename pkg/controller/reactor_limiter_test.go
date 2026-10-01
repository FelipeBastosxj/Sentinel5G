package controller

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/types"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

func TestReactorLimiter_NilAlwaysAllows(t *testing.T) {
	var l *ReactorLimiter
	for i := 0; i < 1000; i++ {
		if !l.Allow(types.NamespacedName{Name: "p"}) {
			t.Fatal("a nil limiter denied a write; nil must mean no limit")
		}
	}
	if NewReactorLimiter(0, 10) != nil || NewReactorLimiter(10, 0) != nil {
		t.Fatal("a zero rate or burst should produce a nil (disabled) limiter")
	}
}

func TestReactorLimiter_BurstThenDeny(t *testing.T) {
	l := NewReactorLimiter(1, 5) // 1/s, burst 5
	key := types.NamespacedName{Namespace: "telecom", Name: "p"}

	allowed := 0
	for i := 0; i < 100; i++ {
		if l.Allow(key) {
			allowed++
		}
	}
	// The burst is spent, then at 1/s essentially nothing more within this
	// tight loop. Exactly the burst, maybe one more if a token refilled.
	if allowed < 5 || allowed > 6 {
		t.Fatalf("allowed %d in a burst of 5; the limiter is not bounding the rate", allowed)
	}
}

func TestReactorLimiter_IsPerPolicy(t *testing.T) {
	l := NewReactorLimiter(1, 3)
	a := types.NamespacedName{Name: "a"}
	b := types.NamespacedName{Name: "b"}

	// Drain a's bucket.
	for i := 0; i < 3; i++ {
		l.Allow(a)
	}
	if l.Allow(a) {
		t.Fatal("policy a's bucket should be drained")
	}
	// b must be unaffected -- one noisy policy must not starve another.
	if !l.Allow(b) {
		t.Fatal("policy b was denied because policy a was noisy; the limit is not per-policy")
	}
}

func TestReactorLimiter_Forget(t *testing.T) {
	l := NewReactorLimiter(1, 2)
	key := types.NamespacedName{Name: "p"}
	l.Allow(key)
	l.Allow(key)
	if l.Allow(key) {
		t.Fatal("bucket should be drained")
	}
	l.Forget(key) // a fresh bucket next time
	if !l.Allow(key) {
		t.Fatal("Forget did not reset the policy's bucket")
	}
}

// statusDecisionChanged is the gate that decides what may be throttled. A
// phase change or a blocklist delta must count as a decision (never
// throttled); an ObservedThreatScore-only refresh must not.
func TestStatusDecisionChanged(t *testing.T) {
	base := &securityv1alpha1.TelecomSecurityPolicy{}
	base.Status.Phase = securityv1alpha1.PolicyPhaseMonitoring

	same := base.DeepCopy()
	same.Status.ObservedThreatScore = "0.9999" // only the informational field
	if statusDecisionChanged(base, same) {
		t.Error("an ObservedThreatScore-only change was treated as a decision; it would never be throttled")
	}

	phase := base.DeepCopy()
	phase.Status.Phase = securityv1alpha1.PolicyPhaseMitigating
	if !statusDecisionChanged(base, phase) {
		t.Error("a phase transition was not treated as a decision; it could be throttled and lost")
	}

	blocked := base.DeepCopy()
	blocked.Status.BlockedTunnels = []string{"10.0.0.1/0x1"}
	if !statusDecisionChanged(base, blocked) {
		t.Error("a new blocked tunnel was not treated as a decision; the finalizer would have nothing to undo")
	}

	// Set equality is order-independent.
	a := base.DeepCopy()
	a.Status.BlockedSourceIPs = []string{"1.1.1.1", "2.2.2.2"}
	b := base.DeepCopy()
	b.Status.BlockedSourceIPs = []string{"2.2.2.2", "1.1.1.1"}
	if statusDecisionChanged(a, b) {
		t.Error("the same set in a different order was treated as a change")
	}
}

// The integration that matters: under a storm of identical scores for an
// already-blocked tunnel, the FIRST write (the blocklist delta) goes
// through, and the rest (pure refreshes) are throttled rather than each
// becoming an API write.
func TestApplyPolicy_ThrottlesRefreshStormButNotTheDecision(t *testing.T) {
	ReactorThrottled.Reset()

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	pod := newTestPod(policy.Namespace, "upf-0")
	w, blocklist, _ := newWatcherFixture(t, policy, pod)
	w.ActionLimiter = NewReactorLimiter(1, 1) // burst 1: only the first refresh is allowed

	score := events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		TEID: 0x4D84, Score: 1.0, Model: "rule:gtpu-tunnel-flood",
	}
	// First score: a real decision (phase -> Mitigating, tunnel blocked).
	if err := w.applyPolicy(context.Background(), policyFromIndex(t, w, policy), score); err != nil {
		t.Fatalf("applyPolicy #1: %v", err)
	}
	// Many identical follow-ups: same tunnel, already blocked -> no decision
	// change, so each is a throttle candidate.
	for i := 0; i < 50; i++ {
		if err := w.applyPolicy(context.Background(), policyFromIndex(t, w, policy), score); err != nil {
			t.Fatalf("applyPolicy follow-up: %v", err)
		}
	}

	// The kernel BlockTunnel is idempotent and cheap (a map write for the
	// same key), so it is deliberately NOT throttled -- the item is about
	// apiserver pressure, which is the STATUS write. The recording double
	// counts every call; the real kernel would just overwrite the one key.
	if len(blocklist.blockedTunnels) < 1 {
		t.Fatal("the tunnel was never blocked")
	}
	// Most of the 50 status refreshes must have been throttled (burst 1 in a
	// tight loop allows only the first through).
	throttled := testutil.ToFloat64(ReactorThrottled.WithLabelValues(policy.Namespace, policy.Name))
	if throttled < 45 {
		t.Fatalf("only %v refreshes throttled of ~50; the reactor is not being bounded", throttled)
	}
}

// policyFromIndex returns the current cached copy, so each follow-up
// applyPolicy sees the status the previous one wrote (Mitigating, tunnel
// already recorded) -- which is what makes the follow-ups non-decisions.
func policyFromIndex(t *testing.T, w *ThreatScoreWatcher, p *securityv1alpha1.TelecomSecurityPolicy) *securityv1alpha1.TelecomSecurityPolicy {
	t.Helper()
	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(p), &got); err != nil {
		t.Fatal(err)
	}
	return &got
}
