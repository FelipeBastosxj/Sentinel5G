package controller

import (
	"sync"

	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/types"
)

// ReactorLimiter bounds how fast the reactor (ThreatScoreWatcher) issues
// *non-meaningful* status writes per policy -- the refreshes that carry only
// a new ObservedThreatScore, not a phase transition or a blocklist change.
//
// It is the bound ROADMAP.md Phase 4 asks for on the reactor. The detector
// side already has one (pkg/detect's per-tunnel cooldown), but an ML score
// is produced once per NormalizedEvent with no cooldown, and JetStream is
// at-least-once, so a single busy source can drive thousands of scores a
// second at one policy. Each one used to be a Status().Update() -- a write
// to the API server -- even when nothing about the decision changed. Under a
// storm that is apiserver pressure for no information.
//
// What it deliberately does NOT throttle is the meaningful writes: a phase
// transition, or a change to BlockedSourceIPs/BlockedTunnels. Those are
// bounded by the number of distinct threats, not by the score rate, and
// losing one would mean the status (which the finalizer and de-escalation
// read) no longer matches what was actually blocked. Only the informational
// refresh is rate-limited; the decision record is always written. See
// ThreatScoreWatcher.writeStatus.
//
// Per policy, not global: one noisy policy must not starve another's
// refreshes. Safe for concurrent use.
type ReactorLimiter struct {
	rate  rate.Limit
	burst int

	mu       sync.Mutex
	limiters map[types.NamespacedName]*rate.Limiter
}

// NewReactorLimiter returns a limiter allowing r informational writes per
// second per policy, with a burst of b. r <= 0 or b <= 0 returns nil, which
// callers treat as "no limit" (the nil-is-fine convention).
func NewReactorLimiter(r float64, b int) *ReactorLimiter {
	if r <= 0 || b <= 0 {
		return nil
	}
	return &ReactorLimiter{
		rate:     rate.Limit(r),
		burst:    b,
		limiters: make(map[types.NamespacedName]*rate.Limiter),
	}
}

// Allow reports whether a non-meaningful status write for key may proceed
// now. A nil limiter always allows (no limit configured).
func (l *ReactorLimiter) Allow(key types.NamespacedName) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	lim, ok := l.limiters[key]
	if !ok {
		lim = rate.NewLimiter(l.rate, l.burst)
		l.limiters[key] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// Forget drops a policy's limiter, called when the policy is deleted so the
// map doesn't grow without bound across a cluster's policy churn.
func (l *ReactorLimiter) Forget(key types.NamespacedName) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.limiters, key)
	l.mu.Unlock()
}
