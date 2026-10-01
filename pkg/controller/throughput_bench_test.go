package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Benchmarks for ROADMAP.md Phase 4's "ThreatScoreWatcher is single-active,
// and the tradeoff has never been measured" item. One process consumes every
// score for the cluster, which is correct for idempotency; these put a number
// on what that one process sustains, and on the two in-memory indexes it
// leans on at cluster scale.
//
// Run: go test ./pkg/controller/ -bench 'Watcher|Index' -run x -benchmem

// BenchmarkWatcherHandle_SubThresholdSteadyState is the dominant production
// case: a Mitigating/Monitoring policy seeing scores that don't change the
// decision. With the reactor rate limit these are throttled rather than
// written, so this measures how fast the single consumer drains scores that
// need no API write -- the rate that actually matters, since meaningful
// writes are bounded by distinct threats, not by the score rate.
func BenchmarkWatcherHandle_SubThresholdSteadyState(b *testing.B) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, false)
	pod := newTestPod(policy.Namespace, "amf-0")
	w := benchWatcher(b, policy, pod)
	w.ActionLimiter = NewReactorLimiter(10, 20) // production defaults

	ctx := context.Background()
	score := events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "203.0.113.7",
		Score: 0.10, Model: "autoencoder-v1", // below threshold: no decision
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.handle(ctx, score)
	}
}

// BenchmarkPolicyIndex_MatchingPolicies is the per-score matching cost at
// cluster scale: the watcher runs it on every event, against every policy in
// the namespace. Measured with a realistic fan-out of policies.
func BenchmarkPolicyIndex_MatchingPolicies(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("policies=%d", n), func(b *testing.B) {
			idx := NewPolicyIndex()
			for i := 0; i < n; i++ {
				p := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, false)
				p.Name = fmt.Sprintf("policy-%d", i)
				p.Namespace = "telecom-core"
				idx.Put(p)
			}
			labels := map[string]string{"app": "amf-service"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = idx.MatchingPolicies("telecom-core", labels)
			}
		})
	}
}

// BenchmarkPodIPIndex_Lookup is the per-score pod resolution cost. PodIPIndex
// watches every Pod in the cluster; this measures a lookup against a large
// populated index -- the "never exercised at cluster scale" concern.
func BenchmarkPodIPIndex_Lookup(b *testing.B) {
	for _, n := range []int{1000, 50000} {
		b.Run(fmt.Sprintf("pods=%d", n), func(b *testing.B) {
			idx := NewPodIPIndex()
			for i := 0; i < n; i++ {
				idx.Put(fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256),
					PodRef{Namespace: "telecom-core", Name: fmt.Sprintf("pod-%d", i)})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = idx.Lookup("10.0.128.128")
			}
		})
	}
}

// benchWatcher builds a watcher backed by a fake client with the policy and
// pod, and a warmed index -- the steady state the benchmark measures.
func benchWatcher(b *testing.B, policy *securityv1alpha1.TelecomSecurityPolicy, pod *corev1.Pod) *ThreatScoreWatcher {
	b.Helper()
	// Seed the policy as already Mitigating, so a sub-threshold score is a
	// no-decision refresh rather than a transition.
	policy.Status.Phase = securityv1alpha1.PolicyPhaseMitigating
	policy.Status.LastMitigationTime = &metav1.Time{}
	c := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy, pod).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()
	idx := NewPolicyIndex()
	idx.Put(policy)
	return &ThreatScoreWatcher{
		Client: c, Log: logr.Discard(), Index: idx,
		Blocklist: &recordingBlocklist{}, Mesh: &recordingMesh{}, BaseThreshold: 0.85,
	}
}
