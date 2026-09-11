package controller

import (
	"sync/atomic"
	"time"
)

// ConditionScoringPipelineReady is set on every TelecomSecurityPolicy to
// report whether the scoring half of the system -- the AI engine
// (cmd/ai-engine), which is deployed separately from this operator -- has
// ever actually published a ThreatScoreEvent.
//
// This exists because of a specific, silent failure mode ROADMAP.md Phase
// 2.5 describes: the AI engine needs a trained model delivered to it, and if
// that step is skipped nothing ever publishes a score. Every policy then sits
// at Phase: Monitoring forever, which is indistinguishable from "everything
// is fine and no threats have been seen" -- there was no signal anywhere that
// half the system was never deployed. A False condition here is that signal,
// visible in `kubectl describe tsp` without having to be scraping Prometheus.
const ConditionScoringPipelineReady = "ScoringPipelineReady"

// Condition reasons for ConditionScoringPipelineReady.
const (
	// ReasonScoresReceived: at least one ThreatScoreEvent has arrived.
	ReasonScoresReceived = "ScoresReceived"
	// ReasonAwaitingFirstScore: no score yet, but still inside the startup
	// grace period -- expected right after an install, not a problem.
	ReasonAwaitingFirstScore = "AwaitingFirstScore"
	// ReasonNoThreatScoresReceived: the grace period elapsed with no score.
	ReasonNoThreatScoresReceived = "NoThreatScoresReceived"
)

// DefaultScoringPipelineGrace is how long after startup a policy waits before
// reporting the pipeline as not ready. Long enough to cover an ordinary
// rollout where the operator happens to start before the AI engine (helm
// installs them in no guaranteed order, and the AI engine pulls a model on
// first start), short enough that a genuinely missing AI engine is visible
// well inside a first pilot. Reasoned, not empirically tuned -- the same
// caveat defaultDeEscalationDwell carries.
const DefaultScoringPipelineGrace = 10 * time.Minute

// ScoringPipelineTracker records whether any ThreatScoreEvent has ever been
// seen. ThreatScoreWatcher observes; Reconciler reads. One shared instance is
// wired into both by cmd/operator/main.go.
//
// Deliberately "ever", not "recently": a rate-based liveness check would need
// a threshold for what counts as too quiet, and quiet is the normal, healthy
// state of a network under no attack -- there is no rate that distinguishes
// "no threats today" from "the AI engine is gone". The transition from never
// to once, by contrast, is unambiguous. Continuous liveness is what the
// sentinel5g_threat_scores_received_total metric is for (see metrics.go).
type ScoringPipelineTracker struct {
	// firstScoreUnixNano is 0 until the first score arrives. atomic because
	// ThreatScoreWatcher writes it from the NATS subscription's callback
	// while Reconciler reads it from the controller's worker goroutines.
	firstScoreUnixNano atomic.Int64

	// startedAt anchors the grace period. Set at construction rather than at
	// manager start, so an operator that takes a while to become leader
	// doesn't get a grace period that starts late.
	startedAt time.Time

	// Grace is how long to wait before reporting not-ready. Zero uses
	// DefaultScoringPipelineGrace.
	Grace time.Duration

	// Now returns the current time; nil uses time.Now. Overridden in tests.
	Now func() time.Time
}

// NewScoringPipelineTracker returns a tracker whose grace period starts now.
func NewScoringPipelineTracker(grace time.Duration) *ScoringPipelineTracker {
	t := &ScoringPipelineTracker{Grace: grace}
	t.startedAt = t.now()
	return t
}

func (t *ScoringPipelineTracker) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *ScoringPipelineTracker) grace() time.Duration {
	if t.Grace > 0 {
		return t.Grace
	}
	return DefaultScoringPipelineGrace
}

// Observe records that a score arrived. Only the first one matters, so
// repeated calls on the hot path settle into a single atomic load.
func (t *ScoringPipelineTracker) Observe() {
	if t.firstScoreUnixNano.Load() != 0 {
		return
	}
	t.firstScoreUnixNano.CompareAndSwap(0, t.now().UnixNano())
}

// EverScored reports whether any ThreatScoreEvent has been seen.
func (t *ScoringPipelineTracker) EverScored() bool {
	return t.firstScoreUnixNano.Load() != 0
}

// Status returns the condition reason to report and, while still inside the
// grace period, how long remains -- so the caller can requeue and have the
// condition flip on its own rather than waiting for an unrelated event to
// trigger the next reconcile.
func (t *ScoringPipelineTracker) Status() (reason string, retryAfter time.Duration) {
	if t.EverScored() {
		return ReasonScoresReceived, 0
	}
	if remaining := t.grace() - t.now().Sub(t.startedAt); remaining > 0 {
		return ReasonAwaitingFirstScore, remaining
	}
	return ReasonNoThreatScoresReceived, 0
}
