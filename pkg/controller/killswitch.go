package controller

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// killSwitchEngagedKey is the ConfigMap data key that engages the switch
// when set to "true". Any other value (or a missing key, or a missing
// ConfigMap) means disengaged.
const killSwitchEngagedKey = "engaged"

// defaultKillSwitchTTL is how long a read of the kill-switch ConfigMap is
// trusted before it is re-read. It bounds both the API load (at most one
// read per TTL, no matter the score rate) and the latency of a human
// flipping the switch -- "stop everything now" takes effect within one TTL.
// Two seconds is fast enough for a human-operated control and slow enough
// that a score storm cannot turn it into an API storm.
const defaultKillSwitchTTL = 2 * time.Second

// KillSwitch is the global "stop all mitigation now" control ROADMAP.md
// Phase 4 asks for: autoMitigate is per policy, and an operator watching a
// mitigation go wrong across many policies at once needs one lever, not a
// per-policy edit storm, and needs it to take effect without a rollout.
//
// It is a ConfigMap, deliberately, because that makes engaging it a single
// command against the API an on-call already has (`kubectl create configmap
// sentinel5g-killswitch --from-literal=engaged=true`), with no new tooling,
// no operator restart, and an audit trail in the API server. When engaged,
// detection continues -- scores are still consumed, threshold crossings
// still counted, policies still move to Alerting -- but no Block,
// BlockTunnel or Quarantine is taken. It is a global override toward
// detection-only, not an off switch for the whole operator.
//
// Read through the APIReader (direct, uncached) rather than the manager's
// cached client on purpose: caching it would mean watching every ConfigMap
// in scope just for this one, and the TTL already bounds the read rate. The
// last successful read is trusted through a transient API error (so a flap
// cannot silently drop an engaged switch); only a NotFound positively means
// disengaged.
type KillSwitch struct {
	Reader client.Reader
	Key    types.NamespacedName
	TTL    time.Duration
	// Now is overridable in tests; nil uses time.Now.
	Now func() time.Time

	mu          sync.Mutex
	lastChecked time.Time
	engaged     bool
	everRead    bool
}

func (k *KillSwitch) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

func (k *KillSwitch) ttl() time.Duration {
	if k.TTL > 0 {
		return k.TTL
	}
	return defaultKillSwitchTTL
}

// Engaged reports whether mitigation is currently suppressed. Nil receiver
// (no kill switch configured) is always false -- the same "nil disables it"
// convention the rest of this package uses. The result is cached for TTL.
func (k *KillSwitch) Engaged(ctx context.Context) bool {
	if k == nil || k.Reader == nil {
		return false
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if k.everRead && k.now().Sub(k.lastChecked) < k.ttl() {
		return k.engaged
	}

	var cm corev1.ConfigMap
	err := k.Reader.Get(ctx, k.Key, &cm)
	k.lastChecked = k.now()
	switch {
	case err == nil:
		k.engaged = cm.Data[killSwitchEngagedKey] == "true"
		k.everRead = true
	case apierrors.IsNotFound(err):
		// The positive "disengaged" answer: the ConfigMap is absent, which
		// is the normal, unarmed state.
		k.engaged = false
		k.everRead = true
	default:
		// A transient read error (API server briefly unreachable). Keep the
		// last known value rather than flipping the switch on a blip: a
		// deliberately engaged switch must survive a flap, and an
		// unarmed one must not silently disable protection either. If we
		// have never read it, default disengaged -- failing toward
		// "protection stays on" rather than toward a surprise global stop.
		if !k.everRead {
			k.engaged = false
		}
	}

	KillSwitchEngaged.Set(boolToFloat(k.engaged))
	return k.engaged
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
