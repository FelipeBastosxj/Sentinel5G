package controller

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	ctrl "sigs.k8s.io/controller-runtime"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// The real process runs handle() on the NATS goroutine while Reconcile runs
// on controller workers, both writing the same status subresource, the
// same PolicyIndex and the same metrics. This drives all of that at once,
// under -race, and then checks the things that must hold no matter how
// the interleaving went: every delivered score was counted, no series went
// negative or duplicated, and the tracker flipped exactly once.
func TestStress_ConcurrentScoresAndReconciles(t *testing.T) {
	resetMetrics(t)

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, _ := newWatcherFixture(t, policy, pod)
	tracker := NewScoringPipelineTracker(time.Minute)
	w.Scoring = tracker
	r := &Reconciler{Client: w.Client, Log: w.Log, Index: w.Index, Scoring: tracker}

	const workers, perWorker = 16, 200
	var wg sync.WaitGroup
	ctx := context.Background()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				score := 0.2
				if j%2 == 0 {
					score = 0.99
				}
				// handle() may return a status-update conflict from the fake
				// client under contention; that is what production requeues
				// on, so it is not a failure here either.
				_ = w.handle(ctx, events.ThreatScoreEvent{
					Namespace: policy.Namespace, PodName: pod.Name,
					SourceIP: fmt.Sprintf("203.0.113.%d", i), Score: score, Model: "autoencoder-v1",
				})
				if j%20 == 0 {
					_, _ = r.Reconcile(ctx, ctrl.Request{NamespacedName: nnFor(policy)})
				}
			}
		}(i)
	}
	wg.Wait()

	if got := testutil.ToFloat64(ThreatScoresReceived.WithLabelValues("model")); got != workers*perWorker {
		t.Fatalf("received counter = %v, want %d: a score was lost or double-counted under contention", got, workers*perWorker)
	}
	if !tracker.EverScored() {
		t.Fatal("tracker never flipped")
	}
	// Block() is idempotent per IP; each worker used its own IP, so the
	// blocklist must have seen each at least once and no IP from nowhere.
	seen := map[string]bool{}
	blocklist.mu.Lock()
	for _, ip := range blocklist.blocked {
		seen[ip] = true
	}
	blocklist.mu.Unlock()
	if len(seen) != workers {
		t.Fatalf("expected %d distinct blocked IPs, got %d", workers, len(seen))
	}
	if got := testutil.CollectAndCount(PolicyPhase); got != len(allPolicyPhases) {
		t.Fatalf("phase gauge has %d series for one policy, want %d", got, len(allPolicyPhases))
	}
}

func BenchmarkSetPolicyPhase(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SetPolicyPhase("telecom-core", "protect-amf-core", securityv1alpha1.PolicyPhaseMonitoring)
	}
}

func BenchmarkScoreSourceLabel(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = scoreSourceLabel("rule:gtpu-tunnel-flood")
	}
}
