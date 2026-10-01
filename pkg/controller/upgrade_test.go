package controller

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/runtime"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
)

// recordingEventRecorder captures the reasons passed to Eventf, so a test
// can assert an operator-facing Event was emitted. Implements
// k8s.io/client-go/tools/events.EventRecorder's one method.
type recordingEventRecorder struct {
	mu      sync.Mutex
	reasons []string
}

func (r *recordingEventRecorder) Eventf(_ runtime.Object, _ runtime.Object, _, reason, _, _ string, _ ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reasons = append(r.reasons, reason)
}

func (r *recordingEventRecorder) has(reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.reasons {
		if x == reason {
			return true
		}
	}
	return false
}

// An upgrade that changes a BPF map's shape (a new max_entries, say) cannot
// reuse the pinned maps, so pkg/ebpf discards them and recreates empty --
// PinReset. ROADMAP.md Phase 4's upgrade item asks that this not be a SILENT
// unblock: it must be loud (an EBPFPinsReset Event) and recovered (the
// reconciler re-applies from status). This proves both, against the earlier
// behaviour where an upgrade simply lost every active block.
func TestUpgrade_PinResetIsLoudAndRecovered(t *testing.T) {
	EnforcementPinned.Set(-1) // a value neither branch would leave, to prove it is set

	kernel := newFakeKernel()
	kernel.pinReset = true // the upgrade discarded incompatible pins

	policy := mitigatingPolicy("gtpu-guard", []string{"203.0.113.7"}, []string{"10.0.0.1/0x4d84"})
	recorder := &recordingEventRecorder{}
	b := newDriftReconciler(t, kernel, policy)
	b.Recorder = recorder
	b.PodRef = &securityv1alpha1.TelecomSecurityPolicy{} // any runtime.Object stands in for the operator Pod

	// Loud: the Event an on-call sees.
	b.reportPinState()
	if !recorder.has("EBPFPinsReset") {
		t.Fatalf("a pin reset did not emit an EBPFPinsReset Event; got %v", recorder.reasons)
	}
	if testutil.ToFloat64(EnforcementPinned) != 1 {
		t.Fatal("EnforcementPinned was not set for a pinned (reset) loader")
	}

	// Recovered: the reconcile re-applies what status still claims, so the
	// post-upgrade kernel ends up holding the blocks again.
	if _, err := b.Sync(context.Background()); err != nil {
		t.Fatalf("Sync after pin reset: %v", err)
	}
	if len(kernel.ips) != 1 || len(kernel.tunnels) != 1 {
		t.Fatalf("the upgrade did not recover the active blocks: ips=%v tunnels=%v", kernel.ips, kernel.tunnels)
	}
}

// Rollback compatibility: the ONLY mitigation state that has to survive a
// version change is Status.BlockedSourceIPs / Status.BlockedTunnels, both
// plain []string. A policy written by a NEWER operator (tunnels populated)
// must be undoable by any version, and a policy from an OLDER one (no
// tunnels field set) must not break. parseTunnel is the one format that
// crosses versions, so its stability is the compatibility contract.
func TestUpgrade_StatusFormatCrossesVersions(t *testing.T) {
	// A status an older operator produced: source IPs only, no tunnels.
	oldStyle := mitigatingPolicy("old", []string{"203.0.113.7"}, nil)
	if statusDecisionChanged(oldStyle, oldStyle.DeepCopy()) {
		t.Fatal("an older-style status did not compare equal to itself")
	}

	// A status a newer operator produced, round-tripped through the wire
	// format the finalizer and de-escalation parse.
	for _, entry := range []string{"10.0.0.1/0x4d84", "2001:db8::1/0x1"} {
		ip, teid, err := parseTunnel(entry)
		if err != nil {
			t.Fatalf("a newer operator's tunnel entry %q did not parse: %v", entry, err)
		}
		if got := formatTunnel(ip.String(), teid); got != entry {
			t.Fatalf("round trip changed %q -> %q; an upgrade would mis-parse active blocks", entry, got)
		}
	}

	// And an unparseable entry (a hypothetical future format an older binary
	// can't read) must be SKIPPED, not fatal -- it must never wedge the
	// deletion or de-escalation of every other block behind it.
	if _, _, err := parseTunnel("some-future-format::v2::garbage"); err == nil {
		t.Fatal("expected an unknown format to be rejected so callers skip it")
	}
}

// desiredEnforcement is what the reconciler builds from across all policies;
// a mix of old-style (IP-only) and new-style (tunnels) policies must union
// cleanly, which is the state a cluster is in DURING a rolling upgrade.
func TestUpgrade_MixedPolicyVersionsUnionCleanly(t *testing.T) {
	policies := []securityv1alpha1.TelecomSecurityPolicy{
		*mitigatingPolicy("old-style", []string{"203.0.113.7"}, nil),
		*mitigatingPolicy("new-style", []string{"203.0.113.8"}, []string{"10.0.0.1/0x4d84"}),
	}
	ips, tunnels := desiredEnforcement(policies)
	if len(ips) != 2 {
		t.Fatalf("source IPs across versions = %d, want 2", len(ips))
	}
	if len(tunnels) != 1 {
		t.Fatalf("tunnels across versions = %d, want 1", len(tunnels))
	}
	if !strings.Contains(strings.Join(keysOf(tunnels), ","), "0x4d84") {
		t.Fatal("the new-style tunnel was dropped in the union")
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
