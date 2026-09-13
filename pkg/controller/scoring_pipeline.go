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

	// startedAt anchors the grace period. It is (re)set by MarkStarted when
	// ThreatScoreWatcher.Start runs -- i.e. when this process actually
	// becomes leader and subscribes -- NOT at construction. A standby
	// replica (replicaCount > 1, or daemonset.enabled) may sit idle for
	// hours before winning the lease; anchored at construction, the moment
	// it did it would report NoThreatScoresReceived on every policy and
	// emit a warning Event for each, then flip to True seconds later once
	// the durable delivered. Construction still sets it, so a tracker used
	// without a watcher (tests) has a sane anchor.
	startedAt atomic.Int64

	// Grace is how long to wait before reporting not-ready. Zero uses
	// DefaultScoringPipelineGrace.
	Grace time.Duration

	// Now returns the current time; nil uses time.Now. Overridden in tests.
	Now func() time.Time
}

// NewScoringPipelineTracker returns a tracker whose grace period starts now;
// MarkStarted re-anchors it when the watcher actually subscribes.
func NewScoringPipelineTracker(grace time.Duration) *ScoringPipelineTracker {
	t := &ScoringPipelineTracker{Grace: grace}
	t.MarkStarted()
	return t
}

// MarkStarted (re)anchors the grace period at now. Called by
// ThreatScoreWatcher.Start, once this process is leader and subscribed.
func (t *ScoringPipelineTracker) MarkStarted() {
	t.startedAt.Store(t.now().UnixNano())
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

// notReadyRecheck is how often a policy already reporting
// NoThreatScoresReceived is re-evaluated. Nothing in the event stream wakes
// a quiet policy's reconcile when the AI engine finally comes up -- the
// watcher's status write only touches the policies whose Pods received a
// score -- so without a requeue a policy with no traffic yet would keep its
// False condition and warning Event until controller-runtime's resync
// (10h by default). One reconcile a minute per not-ready policy is cheap;
// it also writes nothing unless the condition actually changes.
const notReadyRecheck = time.Minute

// Status returns the condition reason to report and how long until it
// should be re-evaluated (0 = no timer needed), so the caller can requeue
// and have the condition flip on its own rather than waiting for an
// unrelated event to trigger the next reconcile.
func (t *ScoringPipelineTracker) Status() (reason string, retryAfter time.Duration) {
	if t.EverScored() {
		return ReasonScoresReceived, 0
	}
	startedAt := time.Unix(0, t.startedAt.Load())
	if remaining := t.grace() - t.now().Sub(startedAt); remaining > 0 {
		return ReasonAwaitingFirstScore, remaining
	}
	return ReasonNoThreatScoresReceived, notReadyRecheck
}
