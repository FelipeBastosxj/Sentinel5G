package controller

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// resetMetrics clears every collector between tests. The collectors are
// package-level (they have to be, to be registered once on controller-
// runtime's global registry), so without this a test's assertion would see
// counts left behind by whichever tests happened to run first.
func resetMetrics(t *testing.T) {
	t.Helper()
	reset := func() {
		ThreatScoresReceived.Reset()
		ThresholdCrossings.Reset()
		Mitigations.Reset()
		PolicyPhase.Reset()
	}
	reset()
	t.Cleanup(reset)
}

// The shadow-mode counter is the whole point of ROADMAP.md Phase 2.5's
// remaining sub-item: a detection-only pilot (autoMitigate: false) must
// produce a countable "this would have mitigated" signal, distinct from a
// crossing that actually did mitigate.
func TestThresholdCrossings_AlertingAndMitigatingAreDistinct(t *testing.T) {
	resetMetrics(t)

	alertOnly := newTestPolicy(securityv1alpha1.SensitivityMedium, false, true, true)
	pod := newTestPod(alertOnly.Namespace, "amf-0")
	w, _, _ := newWatcherFixture(t, alertOnly, pod)

	event := events.ThreatScoreEvent{
		Namespace: alertOnly.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     0.99,
	}
	if err := w.applyPolicy(context.Background(), alertOnly, event); err != nil {
		t.Fatalf("applyPolicy (alerting): %v", err)
	}

	got := testutil.ToFloat64(ThresholdCrossings.WithLabelValues(alertOnly.Namespace, alertOnly.Name, "alerting"))
	if got != 1 {
		t.Fatalf("expected 1 alerting crossing, got %v", got)
	}
	if got := testutil.ToFloat64(ThresholdCrossings.WithLabelValues(alertOnly.Namespace, alertOnly.Name, "mitigating")); got != 0 {
		t.Fatalf("expected 0 mitigating crossings for a detection-only policy, got %v", got)
	}

	// Same policy shape, but allowed to act.
	mitigating := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	pod2 := newTestPod(mitigating.Namespace, "amf-1")
	w2, _, _ := newWatcherFixture(t, mitigating, pod2)
	event.PodName = pod2.Name
	if err := w2.applyPolicy(context.Background(), mitigating, event); err != nil {
		t.Fatalf("applyPolicy (mitigating): %v", err)
	}

	if got := testutil.ToFloat64(ThresholdCrossings.WithLabelValues(mitigating.Namespace, mitigating.Name, "mitigating")); got != 1 {
		t.Fatalf("expected 1 mitigating crossing, got %v", got)
	}
	if got := testutil.ToFloat64(Mitigations.WithLabelValues("ebpf_block", "success")); got != 1 {
		t.Fatalf("expected 1 successful ebpf_block, got %v", got)
	}
	if got := testutil.ToFloat64(Mitigations.WithLabelValues("mesh_quarantine", "success")); got != 1 {
		t.Fatalf("expected 1 successful mesh_quarantine, got %v", got)
	}
}

// A score below the effective threshold is not a crossing at all — the
// shadow-mode rate would be meaningless if ordinary traffic incremented it.
func TestThresholdCrossings_BelowThresholdCountsNothing(t *testing.T) {
	resetMetrics(t)

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, _, _ := newWatcherFixture(t, policy, pod)

	err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "203.0.113.7", Score: 0.2,
	})
	if err != nil {
		t.Fatalf("applyPolicy: %v", err)
	}

	if got := testutil.CollectAndCount(ThresholdCrossings); got != 0 {
		t.Fatalf("expected no crossing series at all below threshold, got %d", got)
	}
}

// handle() counts before policy matching, so an event for a Pod that no
// longer exists still proves the AI engine is publishing — that's the whole
// reason this counter exists (ROADMAP.md Phase 2.5's "no signal anywhere that
// the scoring half of the system was never deployed").
func TestThreatScoresReceived_CountsEvenWhenNoPolicyMatches(t *testing.T) {
	resetMetrics(t)

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, _, _ := newWatcherFixture(t, policy, pod)

	err := w.handle(context.Background(), events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: "a-pod-that-does-not-exist", SourceIP: "203.0.113.7", Score: 0.99,
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	if got := testutil.ToFloat64(ThreatScoresReceived.WithLabelValues("unknown")); got != 1 {
		t.Fatalf("expected the dropped event to still be counted, got %v", got)
	}
}

// Model is attacker-supplied whenever the bus is unauthenticated (see
// docs/integrations.md), so it must never reach Prometheus as a raw label.
func TestScoreSourceLabel_IsAClosedSet(t *testing.T) {
	cases := map[string]string{
		"":                        "unknown",
		"autoencoder-v1":          "model",
		"rule:gtpu-tunnel-flood":  "rule",
		strings.Repeat("x", 4096): "model",
		"rule:":                   "rule",
	}
	for in, want := range cases {
		if got := scoreSourceLabel(in); got != want {
			t.Fatalf("scoreSourceLabel(%.20q) = %q, want %q", in, got, want)
		}
	}
}

// Exactly one phase series per policy may be 1, or `sum by (phase)` counts
// the same policy in two phases at once.
func TestSetPolicyPhase_ZeroesEveryOtherPhase(t *testing.T) {
	resetMetrics(t)

	SetPolicyPhase("telecom-core", "protect-amf-core", securityv1alpha1.PolicyPhaseMonitoring)
	SetPolicyPhase("telecom-core", "protect-amf-core", securityv1alpha1.PolicyPhaseMitigating)

	var active []string
	for _, phase := range allPolicyPhases {
		if testutil.ToFloat64(PolicyPhase.WithLabelValues("telecom-core", "protect-amf-core", string(phase))) == 1 {
			active = append(active, string(phase))
		}
	}
	if len(active) != 1 || active[0] != string(securityv1alpha1.PolicyPhaseMitigating) {
		t.Fatalf("expected only Mitigating active, got %v", active)
	}
}

// A deleted policy's series must go away: Prometheus cannot tell "policy
// gone" from "policy idle", so a lingering gauge reads as a live policy stuck
// in its last phase forever.
func TestForgetPolicyMetrics_DropsEverySeriesForThatPolicy(t *testing.T) {
	resetMetrics(t)

	SetPolicyPhase("telecom-core", "doomed", securityv1alpha1.PolicyPhaseMitigating)
	SetPolicyPhase("telecom-core", "survivor", securityv1alpha1.PolicyPhaseMonitoring)
	ThresholdCrossings.WithLabelValues("telecom-core", "doomed", "alerting").Inc()
	ThresholdCrossings.WithLabelValues("telecom-core", "survivor", "alerting").Inc()

	ForgetPolicyMetrics("telecom-core", "doomed")

	if got := testutil.CollectAndCount(PolicyPhase); got != len(allPolicyPhases) {
		t.Fatalf("expected only the survivor's phase series to remain, got %d", got)
	}
	if got := testutil.ToFloat64(ThresholdCrossings.WithLabelValues("telecom-core", "survivor", "alerting")); got != 1 {
		t.Fatalf("expected the survivor's crossing counter untouched, got %v", got)
	}
	if got := testutil.CollectAndCount(ThresholdCrossings); got != 1 {
		t.Fatalf("expected 1 remaining crossing series, got %d", got)
	}
}

// allPolicyPhases has to list every api/v1alpha1.PolicyPhase constant, and
// nothing enforces that at compile time: Go has no exhaustive-switch check
// for a string-typed enum, and a forgotten phase fails silently (that phase's
// gauge just never reports 1). Parsing the API source is deliberate — it
// catches a constant added there without being added here, which is exactly
// the drift a hand-written list in a different package invites. This is the
// same convention the eBPF side already uses for its own hand-synced
// cross-language constants (pkg/ebpf/loader_linux_test.go's struct-size
// assertions).
func TestAllPolicyPhases_CoversTheAPI(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "api", "v1alpha1", "telecomsecuritypolicy_types.go"))
	if err != nil {
		t.Fatalf("read the API types: %v", err)
	}

	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`PolicyPhase\s*=\s*"([A-Za-z]+)"`).FindAllStringSubmatch(string(src), -1) {
		declared[m[1]] = true
	}
	if len(declared) == 0 {
		t.Fatal("found no PolicyPhase constants in the API source; this test's regex has gone stale")
	}

	covered := map[string]bool{}
	for _, p := range allPolicyPhases {
		covered[string(p)] = true
	}

	for phase := range declared {
		if !covered[phase] {
			t.Errorf("api/v1alpha1 declares PolicyPhase %q, but allPolicyPhases in metrics.go doesn't list it", phase)
		}
	}
	for phase := range covered {
		if !declared[phase] {
			t.Errorf("allPolicyPhases lists %q, which api/v1alpha1 no longer declares", phase)
		}
	}
}
