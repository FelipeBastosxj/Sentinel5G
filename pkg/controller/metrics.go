package controller

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
)

// Metrics for Layer 4's decision path, registered on controller-runtime's
// own registry so they appear on the manager's existing metrics endpoint
// (config.MetricsBindAddress, ":8080" by default) rather than needing a
// second HTTP server -- the Service and the optional ServiceMonitor the Helm
// chart already renders point at that same port, so nothing on the
// deployment side has to change for these to be scrapeable.
//
// The AI engine has had its own metrics since ROADMAP.md Phase 2
// (cmd/ai-engine/sentinel_ai/metrics.py); the operator had none at all, which
// is what ROADMAP.md Phase 2.5 records as still open inside the otherwise
// closed "detection-only pilot" item: "A Prometheus counter for shadow-mode
// false-positive-rate measurement is still open, not yet added."
// ThresholdCrossings' outcome="alerting" below is exactly that counter -- see
// its comment.
//
// A note on label cardinality, because it is a real exposure here and not a
// style preference: namespace/policy labels are bounded by objects that exist
// in the cluster's own API server, so they're safe. Anything read off the
// NATS wire is NOT -- docs/integrations.md is explicit that whoever can reach
// an unauthenticated NATS_URL can forge a ThreatScoreEvent, so using an
// attacker-supplied string (ThreatScoreEvent.Model) directly as a label value
// would turn that into unbounded series growth in Prometheus. Every wire-
// derived label below therefore goes through scoreSourceLabel(), which maps
// into a fixed, closed set.
var (
	// ThreatScoresReceived counts every ThreatScoreEvent consumed off the
	// bus, before any policy matching -- it is the liveness proof for the
	// scoring half of the system. A deployment that never installed the AI
	// engine (the failure ROADMAP.md Phase 2.5's chart/model item describes,
	// where every policy sits at Phase: Monitoring forever with no signal
	// anywhere) leaves this counter flat at zero.
	ThreatScoresReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sentinel5g_threat_scores_received_total",
			Help: "Total ThreatScoreEvents consumed from the message bus, by scoring source.",
		},
		[]string{"source"},
	)

	// ThreatScore observes the raw score distribution. Deliberately
	// unlabelled: the only naturally interesting label (which model produced
	// it) comes off the wire, and a histogram multiplies whatever cardinality
	// it carries by its bucket count. The distribution across all sources is
	// what threshold calibration actually needs; ThreatScoresReceived above
	// carries the per-source split.
	ThreatScore = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "sentinel5g_threat_score",
			Help:    "Distribution of threat scores received, in [0,1].",
			Buckets: prometheus.LinearBuckets(0, 0.05, 21),
		},
	)

	// ThresholdCrossings counts every time a score crossed a policy's
	// effective threshold, split by what the policy allowed to happen next.
	//
	// outcome="alerting" is the shadow-mode false-positive counter: it is
	// incremented exactly when the threshold was crossed but
	// spec.threatDetection.autoMitigate is false, i.e. the detection-only
	// pilot would have acted. Running a pilot that way and watching this
	// counter's rate is how an operator measures the false-positive rate
	// they'd be signing up for BEFORE enabling automated mitigation on real
	// traffic -- which is the precondition ROADMAP.md Phase 2.5's preamble
	// sets out. outcome="mitigating" is the same crossing with autoMitigate
	// on, so the two together are the full crossing rate.
	ThresholdCrossings = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sentinel5g_threshold_crossings_total",
			Help: "Threat score threshold crossings, by what the matching policy allowed next.",
		},
		[]string{"namespace", "policy", "outcome"},
	)

	// Mitigations counts individual mitigation actions attempted. Split by
	// result because a failing mesh/eBPF action is what moves a policy to
	// Degraded, and "mitigations are firing but all of them error" is not
	// visible from the crossing counter alone.
	Mitigations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sentinel5g_mitigations_total",
			Help: "Automated mitigation actions attempted, by action and result.",
		},
		[]string{"action", "result"},
	)

	// PolicyPhase exposes each policy's current phase as a 0/1 gauge per
	// possible phase (the kube-state-metrics convention), so a dashboard can
	// sum by phase without re-deriving it from logs. SetPolicyPhase keeps
	// exactly one series per policy at 1.
	PolicyPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sentinel5g_policy_phase",
			Help: "Current phase of each TelecomSecurityPolicy (1 for the active phase, 0 otherwise).",
		},
		[]string{"namespace", "policy", "phase"},
	)
)

// allPolicyPhases is every value api/v1alpha1.PolicyPhase can take. Kept
// here (not exported from the API package) because it exists purely so
// SetPolicyPhase can zero the phases a policy is NOT in. A phase added to the
// API and forgotten here degrades silently -- that phase would simply never
// report 1, with nothing failing -- so TestAllPolicyPhases_CoversTheAPI in
// metrics_test.go parses the API source and fails on the drift.
var allPolicyPhases = []securityv1alpha1.PolicyPhase{
	securityv1alpha1.PolicyPhasePending,
	securityv1alpha1.PolicyPhaseMonitoring,
	securityv1alpha1.PolicyPhaseAlerting,
	securityv1alpha1.PolicyPhaseMitigating,
	securityv1alpha1.PolicyPhaseDegraded,
}

func init() {
	metrics.Registry.MustRegister(
		ThreatScoresReceived,
		ThreatScore,
		ThresholdCrossings,
		Mitigations,
		PolicyPhase,
	)
}

// scoreSourceLabel maps a ThreatScoreEvent.Model -- an arbitrary string off
// the wire -- onto a closed set of label values, so a forged or simply
// unexpected model name can't grow the series count. "rule" covers the
// explicit non-ML detectors (model "rule:<name>"), "model" covers ML scoring,
// "unknown" covers an event that named nothing at all.
func scoreSourceLabel(model string) string {
	switch {
	case model == "":
		return "unknown"
	case strings.HasPrefix(model, "rule:"):
		return "rule"
	default:
		return "model"
	}
}

// SetPolicyPhase records phase as this policy's active one and zeroes every
// other phase for it, so `sum by (phase) (sentinel5g_policy_phase)` counts
// policies rather than double-counting ones that have changed phase.
func SetPolicyPhase(namespace, name string, phase securityv1alpha1.PolicyPhase) {
	for _, p := range allPolicyPhases {
		value := 0.0
		if p == phase {
			value = 1.0
		}
		PolicyPhase.WithLabelValues(namespace, name, string(p)).Set(value)
	}
}

// ForgetPolicyMetrics drops every series belonging to one policy. Called when
// a policy is actually deleted (Reconciler.finalize): without it, a deleted
// policy's gauges and counters would linger at their last value forever,
// since Prometheus can't distinguish "policy gone" from "policy idle".
func ForgetPolicyMetrics(namespace, name string) {
	labels := prometheus.Labels{"namespace": namespace, "policy": name}
	PolicyPhase.DeletePartialMatch(labels)
	ThresholdCrossings.DeletePartialMatch(labels)
}
