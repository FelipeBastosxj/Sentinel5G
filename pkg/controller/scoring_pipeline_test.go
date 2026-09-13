package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
)

func newReconcilerFixture(t *testing.T, tracker *ScoringPipelineTracker, policy *securityv1alpha1.TelecomSecurityPolicy) *Reconciler {
	t.Helper()
	fakeClient := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	return &Reconciler{
		Client:  fakeClient,
		Log:     testr.New(t),
		Index:   NewPolicyIndex(),
		Scoring: tracker,
	}
}

func conditionOf(t *testing.T, r *Reconciler, policy *securityv1alpha1.TelecomSecurityPolicy) *metav1.Condition {
	t.Helper()
	var got securityv1alpha1.TelecomSecurityPolicy
	if err := r.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get policy: %v", err)
	}
	return meta.FindStatusCondition(got.Status.Conditions, ConditionScoringPipelineReady)
}

// Inside the grace period, "no score yet" is the expected state right after
// an install and must not be reported as a problem.
func TestScoringPipeline_WithinGraceReportsAwaiting(t *testing.T) {
	now := time.Now()
	tracker := &ScoringPipelineTracker{Grace: 10 * time.Minute, Now: func() time.Time { return now }}
	tracker.startedAt.Store(now.UnixNano())

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, false, false)
	r := newReconcilerFixture(t, tracker, policy)

	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	cond := conditionOf(t, r, policy)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != ReasonAwaitingFirstScore {
		t.Fatalf("expected False/%s inside the grace period, got %+v", ReasonAwaitingFirstScore, cond)
	}
	// It has to requeue, or the condition would only flip when something
	// unrelated happened to trigger the next reconcile.
	if result.RequeueAfter <= 0 || result.RequeueAfter > 10*time.Minute {
		t.Fatalf("expected a requeue within the remaining grace, got %v", result.RequeueAfter)
	}
}

// This is the case the whole mechanism exists for: the AI engine was never
// deployed (or has no model), so nothing ever scores and the policy would
// otherwise sit at Monitoring forever with no signal anywhere.
func TestScoringPipeline_AfterGraceWithoutScoresReportsNotReady(t *testing.T) {
	now := time.Now()
	tracker := &ScoringPipelineTracker{Grace: 10 * time.Minute, Now: func() time.Time { return now }}
	tracker.startedAt.Store(now.Add(-11 * time.Minute).UnixNano())

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, false, false)
	r := newReconcilerFixture(t, tracker, policy)

	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	cond := conditionOf(t, r, policy)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != ReasonNoThreatScoresReceived {
		t.Fatalf("expected False/%s after the grace period, got %+v", ReasonNoThreatScoresReceived, cond)
	}
	// Still requeues, at the slow not-ready cadence: nothing in the event
	// stream wakes a quiet policy when the engine finally comes up.
	if result.RequeueAfter != notReadyRecheck {
		t.Fatalf("expected a %v recheck while not ready, got %v", notReadyRecheck, result.RequeueAfter)
	}
	// The message has to name the actual cause; a bare "not ready" would be
	// no more actionable than the silent Monitoring it replaces.
	if !containsAll(cond.Message, "AI engine", "model", "docs/production-install.md") {
		t.Fatalf("expected an actionable message naming the AI engine and the model, got %q", cond.Message)
	}
}

// One observed score flips it, and that's a one-way door: quiet is the normal
// state of a network under no attack, so the condition must not decay back to
// False just because no threats have been seen lately.
func TestScoringPipeline_OneScoreFlipsItPermanently(t *testing.T) {
	now := time.Now()
	tracker := &ScoringPipelineTracker{Grace: 10 * time.Minute, Now: func() time.Time { return now }}
	tracker.startedAt.Store(now.Add(-11 * time.Minute).UnixNano())
	tracker.Observe()

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, false, false)
	r := newReconcilerFixture(t, tracker, policy)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if cond := conditionOf(t, r, policy); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected True after a score arrived, got %+v", cond)
	}

	// Much later, still no further scores.
	tracker.Now = func() time.Time { return now.Add(24 * time.Hour) }
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if cond := conditionOf(t, r, policy); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected the condition to stay True during a quiet period, got %+v", cond)
	}
}

// A nil tracker (every existing test's Reconciler, and any caller that hasn't
// wired it) must leave the condition off entirely rather than report a state
// it has no information about.
func TestScoringPipeline_NilTrackerWritesNoCondition(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, false, false)
	r := newReconcilerFixture(t, nil, policy)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if cond := conditionOf(t, r, policy); cond != nil {
		t.Fatalf("expected no condition without a tracker, got %+v", cond)
	}
}

// The condition is written on a status subresource, so an unchanged one must
// not produce an API write on every resync of every policy.
func TestScoringPipeline_UnchangedConditionDoesNotRewriteStatus(t *testing.T) {
	now := time.Now()
	tracker := &ScoringPipelineTracker{Grace: 10 * time.Minute, Now: func() time.Time { return now }}
	tracker.startedAt.Store(now.Add(-11 * time.Minute).UnixNano())

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, false, false)
	r := newReconcilerFixture(t, tracker, policy)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	var after securityv1alpha1.TelecomSecurityPolicy
	if err := r.Get(context.Background(), nnFor(policy), &after); err != nil {
		t.Fatalf("get: %v", err)
	}
	firstRV := after.ResourceVersion

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nnFor(policy)}); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if err := r.Get(context.Background(), nnFor(policy), &after); err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.ResourceVersion != firstRV {
		t.Fatalf("an unchanged condition still wrote status (resourceVersion %s -> %s)", firstRV, after.ResourceVersion)
	}
}

// Observe() runs on the NATS callback goroutine while Reconcile reads from
// controller worker goroutines; the race detector is what makes this test
// meaningful (`go test -race`).
func TestScoringPipelineTracker_ObserveIsConcurrencySafe(t *testing.T) {
	tracker := NewScoringPipelineTracker(time.Minute)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 200; j++ {
				tracker.Observe()
				_ = tracker.EverScored()
				_, _ = tracker.Status()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if !tracker.EverScored() {
		t.Fatal("expected EverScored after concurrent Observe calls")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Review finding, pinned: a standby replica that becomes leader after
// sitting idle longer than the grace must NOT immediately report
// NoThreatScoresReceived. The grace is anchored where the process can
// actually receive a score -- ThreatScoreWatcher.Start -- not at
// construction.
func TestScoringPipeline_GraceIsAnchoredAtWatcherStart(t *testing.T) {
	now := time.Now()
	tracker := &ScoringPipelineTracker{Grace: 10 * time.Minute, Now: func() time.Time { return now }}
	tracker.startedAt.Store(now.Add(-3 * time.Hour).UnixNano()) // constructed long ago, as a standby

	if reason, _ := tracker.Status(); reason != ReasonNoThreatScoresReceived {
		t.Fatalf("precondition: a stale anchor reads not-ready, got %s", reason)
	}

	tracker.MarkStarted() // what Start does on becoming leader
	reason, retry := tracker.Status()
	if reason != ReasonAwaitingFirstScore {
		t.Fatalf("after MarkStarted expected %s, got %s", ReasonAwaitingFirstScore, reason)
	}
	if retry <= 0 || retry > 10*time.Minute {
		t.Fatalf("expected a fresh grace window, got retry %v", retry)
	}
}
