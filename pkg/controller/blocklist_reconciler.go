package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoevents "k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
)

// defaultBlocklistReconcileInterval is how often BlocklistReconciler
// compares the kernel against the policies' status when
// BlocklistReconciler.Interval is zero.
//
// A minute, not a second: the sync at startup is the one that matters (it
// is the restart-replay), and the periodic ones only exist to catch drift
// that nothing reported — a dropped map write, a pin discarded by an
// upgrade, an entry evicted by something outside this operator. Each sync
// lists every policy and iterates both enforcement map families, so running
// it per second would be real, continuous cost against a failure mode that
// is rare by construction.
const defaultBlocklistReconcileInterval = time.Minute

// BlocklistReconciler keeps the eBPF enforcement maps equal to what the
// TelecomSecurityPolicy status subresources say should be enforced.
//
// It exists because of the failure ROADMAP.md Phase 4 opens with. Up to
// Phase 3 the enforcement maps lived and died with the operator process:
// Loader.Close() detached the XDP program and the collection went with it,
// so a rollout, an OOM kill or a crash un-blocked every active mitigation
// while every policy's status went on asserting them, the phase stayed
// Mitigating, and de-escalation later tried to undo drops that no longer
// existed. A control that reports itself as on while being off is worse
// than one that is plainly off, and that is what this closes, together with
// the bpffs pinning in pkg/ebpf (see pin_linux.go). The two are
// complementary, not alternatives: pinning means the drops are already back
// in force the instant the XDP program re-attaches, before any of this
// runs; this is what makes them *correct* afterwards, and what notices when
// they are not.
//
// Direction matters and is deliberate: status is the DESIRED state, the map
// is the ACTUAL one. Status is what survives in etcd, what the finalizer
// and the de-escalation timer read, and what an operator sees; the map is a
// kernel object this process can lose. So an entry in status and not in the
// kernel is re-applied, and an entry in the kernel claimed by no policy is
// removed — the second half is what keeps a stale pin from enforcing a drop
// forever after the policy that asked for it was deleted.
//
// One consequence worth stating rather than discovering: in DaemonSet mode
// every node runs this against the SAME cluster-wide policy status, so a
// source blocked because one node observed it ends up blocked on every
// node. That is a change from "only the node that saw the traffic drops
// it", and it is the reading this type is built on — status.blockedSourceIPs
// is a cluster-wide record of what should not be allowed, not a note about
// where it was seen.
type BlocklistReconciler struct {
	client.Client
	Log logr.Logger

	// Blocklist applies the corrections. Nil disables the reconciler
	// entirely — the same convention Reconciler already uses.
	Blocklist ebpf.BlocklistUpdater
	// Inspector reads the actual kernel state. Separate from Blocklist
	// because only a real attached Loader can answer it: the no-op
	// fallback cmd/operator/main.go installs when eBPF is unavailable
	// enforces nothing, so comparing against it would "correct" a kernel
	// that is not there. Nil disables the reconciler.
	Inspector ebpf.BlocklistInspector

	// Interval between syncs; zero uses defaultBlocklistReconcileInterval.
	// Negative disables the periodic sync, leaving only the one at startup.
	Interval time.Duration

	// Recorder reports a discarded pin set (see ebpf.BlocklistInspector's
	// PinReset) as a warning Event against the operator's own Pod, the same
	// way EBPFAttachFailed is. Nil skips the Event.
	Recorder clientgoevents.EventRecorder

	// unclaimed carries the keys that the PREVIOUS pass found in the kernel
	// with no policy claiming them, and it is what makes removal safe. See
	// Sync's comment on the two-pass rule. Only touched from Sync, which
	// Start calls from one goroutine.
	unclaimed map[string]bool
	// PodRef is the object that Event is recorded against; nil skips it.
	// See cmd/operator/main.go's operatorPodRef.
	PodRef runtime.Object
}

// DriftReport is one sync's outcome: how many entries each side was
// missing. Returned by Sync for tests and logging; the same numbers go to
// the BlocklistDrift counter.
type DriftReport struct {
	// MissingIPs/MissingTunnels were claimed by a policy's status and were
	// not in the kernel — the restart case, and the one that means traffic
	// an operator believes is being dropped was flowing.
	MissingIPs     int
	MissingTunnels int
	// ExtraIPs/ExtraTunnels were in the kernel and claimed by no policy —
	// a drop still in force after whatever asked for it went away — and
	// were removed, having been unclaimed on the previous pass too.
	ExtraIPs     int
	ExtraTunnels int
	// PendingExtraIPs/PendingExtraTunnels were unclaimed for the FIRST
	// time this pass and so were left alone; see Sync's two-pass comment.
	// A mitigation placed moments ago lands here, which is the point.
	PendingExtraIPs     int
	PendingExtraTunnels int
	// DesiredIPs/KernelIPs (and the tunnel pair) are the set sizes the
	// comparison was made over, before any correction.
	DesiredIPs     int
	DesiredTunnels int
	KernelIPs      int
	KernelTunnels  int
}

// Drifted reports whether this sync had to change anything.
func (d DriftReport) Drifted() bool {
	return d.MissingIPs+d.MissingTunnels+d.ExtraIPs+d.ExtraTunnels > 0
}

// Start implements manager.Runnable: syncs once immediately, then on
// Interval until ctx is done.
//
// The immediate sync is the restart replay and is the reason this is a
// Runnable rather than something folded into Reconcile. Reconcile is
// per-policy and only ever sees the policies the API server wakes it for;
// re-applying kernel state needs the union of every policy at once, and
// needs to run even on a cluster where nothing happens to change.
func (b *BlocklistReconciler) Start(ctx context.Context) error {
	if b.Blocklist == nil || b.Inspector == nil {
		b.Log.Info("eBPF enforcement not attached; blocklist drift reconciliation disabled")
		return nil
	}

	b.reportPinState()

	if report, err := b.Sync(ctx); err != nil {
		// Not fatal: failing the manager here would crash-loop the operator
		// over a transient API server read, which is the cure being worse
		// than the disease (ROADMAP.md Phase 2.5's first item). The next
		// tick retries.
		b.Log.Error(err, "initial blocklist reconciliation failed; retrying on the next interval")
	} else if report.Drifted() {
		b.Log.Info("re-applied eBPF enforcement state after startup",
			"reappliedIPs", report.MissingIPs, "reappliedTunnels", report.MissingTunnels,
			"removedIPs", report.ExtraIPs, "removedTunnels", report.ExtraTunnels)
	}

	interval := b.interval()
	if interval <= 0 {
		b.Log.Info("periodic blocklist reconciliation disabled; only the startup sync ran")
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			report, err := b.Sync(ctx)
			if err != nil {
				b.Log.Error(err, "blocklist reconciliation failed")
				continue
			}
			if report.Drifted() {
				b.Log.Info("corrected eBPF enforcement drift",
					"reappliedIPs", report.MissingIPs, "reappliedTunnels", report.MissingTunnels,
					"removedIPs", report.ExtraIPs, "removedTunnels", report.ExtraTunnels)
			}
		}
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable: false.
//
// The enforcement maps are node-local — in DaemonSet mode each Pod has its
// own XDP attachment on its own node's interface — so every replica has to
// reconcile its own kernel, not just the leader's. Same reasoning as
// pkg/ingestion.Publisher, and the opposite of ThreatScoreWatcher, which is
// single-active precisely because it writes to the API server.
func (b *BlocklistReconciler) NeedLeaderElection() bool { return false }

func (b *BlocklistReconciler) interval() time.Duration {
	if b.Interval == 0 {
		return defaultBlocklistReconcileInterval
	}
	return b.Interval
}

// reportPinState publishes whether the enforcement maps survive this
// process, and raises a warning when a pin set was discarded at attach.
func (b *BlocklistReconciler) reportPinState() {
	pinPath := b.Inspector.PinPath()
	if pinPath == "" {
		EnforcementPinned.Set(0)
		b.Log.Info("eBPF enforcement maps are NOT pinned; drops will not survive this process " +
			"and are only restored by the next reconciliation (set BPF_PIN_PATH)")
	} else {
		EnforcementPinned.Set(1)
		b.Log.Info("eBPF enforcement maps are pinned", "pinPath", pinPath)
	}

	if !b.Inspector.PinReset() {
		return
	}
	// Reached when an upgrade changed a map's shape: the pinned maps could
	// not be reused, so they were recreated empty and every drop that was
	// in force is gone. The sync immediately after this re-applies whatever
	// status still claims, but the window — and the fact that it happened
	// at all — should be visible, not inferred from a gap in a graph.
	b.Log.Info("existing pinned enforcement maps were incompatible with this build and were discarded; " +
		"all active drops were lost and are being re-applied from policy status")
	if b.Recorder != nil && b.PodRef != nil {
		b.Recorder.Eventf(b.PodRef, nil, corev1.EventTypeWarning, "EBPFPinsReset", "Attach",
			"Pinned enforcement maps were incompatible with this build and were recreated empty; "+
				"active drops were lost and are being re-applied from TelecomSecurityPolicy status")
	}
}

// Sync performs one comparison and correction pass.
func (b *BlocklistReconciler) Sync(ctx context.Context) (DriftReport, error) {
	var report DriftReport

	var policies securityv1alpha1.TelecomSecurityPolicyList
	if err := b.List(ctx, &policies); err != nil {
		// Returned, never partially acted on: a failed list looks exactly
		// like "no policy wants anything blocked", and acting on that would
		// flush every active mitigation out of the kernel because the API
		// server was briefly unreachable.
		return report, fmt.Errorf("list TelecomSecurityPolicies: %w", err)
	}

	desiredIPs, desiredTunnels := desiredEnforcement(policies.Items)
	report.DesiredIPs, report.DesiredTunnels = len(desiredIPs), len(desiredTunnels)

	kernelIPs, err := b.Inspector.BlockedIPs()
	if err != nil {
		return report, fmt.Errorf("read kernel blocklist: %w", err)
	}
	kernelTunnels, err := b.Inspector.BlockedTunnels()
	if err != nil {
		return report, fmt.Errorf("read kernel tunnel blocklist: %w", err)
	}
	report.KernelIPs, report.KernelTunnels = len(kernelIPs), len(kernelTunnels)

	actualIPs := make(map[string]net.IP, len(kernelIPs))
	for _, ip := range kernelIPs {
		actualIPs[ip.String()] = ip
	}
	actualTunnels := make(map[string]ebpf.BlockedTunnel, len(kernelTunnels))
	for _, t := range kernelTunnels {
		actualTunnels[formatTunnel(t.IP.String(), t.TEID)] = t
	}

	var errs []error

	// Additions are applied immediately; removals require the entry to have
	// been unclaimed on the PREVIOUS pass as well. The asymmetry is
	// deliberate and the direction of it is the point: both reads here race
	// the mitigation path, which blocks in the kernel BEFORE it writes the
	// status that claims the block (ThreatScoreWatcher.applyPolicy), and
	// which this reconciler then sees through a cache that lags the API
	// server again. A freshly placed drop is therefore briefly unclaimed by
	// anything this can see, and removing it on sight would silently revert
	// a live mitigation — the exact failure this whole mechanism exists to
	// prevent, reintroduced by its own repair. Requiring two consecutive
	// passes closes that window by an entire interval, which is orders of
	// magnitude longer than a status write plus a cache update.
	//
	// The reverse race (de-escalation unblocks, then clears status; this
	// sees status still claiming the drop and re-applies it) is left
	// immediate on purpose. Its cost is traffic staying blocked for a pass
	// or two longer than intended, and it self-corrects. Two-passing it as
	// well would also delay the startup replay by a full interval, which is
	// the one correction that must not wait.
	stillUnclaimed := make(map[string]bool)

	for _, key := range sortedKeys(desiredIPs) {
		if _, ok := actualIPs[key]; ok {
			continue
		}
		report.MissingIPs++
		BlocklistDrift.WithLabelValues("ip", "missing").Inc()
		if err := b.Blocklist.Block(desiredIPs[key]); err != nil {
			errs = append(errs, fmt.Errorf("re-apply block for %s: %w", key, err))
		}
	}
	for _, key := range sortedKeys(actualIPs) {
		if _, ok := desiredIPs[key]; ok {
			continue
		}
		stillUnclaimed["ip:"+key] = true
		if !b.unclaimed["ip:"+key] {
			report.PendingExtraIPs++
			continue
		}
		report.ExtraIPs++
		BlocklistDrift.WithLabelValues("ip", "extra").Inc()
		if err := b.Blocklist.Unblock(actualIPs[key]); err != nil {
			errs = append(errs, fmt.Errorf("remove unclaimed block for %s: %w", key, err))
		}
	}

	for _, key := range sortedKeys(desiredTunnels) {
		if _, ok := actualTunnels[key]; ok {
			continue
		}
		report.MissingTunnels++
		BlocklistDrift.WithLabelValues("tunnel", "missing").Inc()
		t := desiredTunnels[key]
		if err := b.Blocklist.BlockTunnel(t.IP, t.TEID); err != nil {
			errs = append(errs, fmt.Errorf("re-apply tunnel block for %s: %w", key, err))
		}
	}
	for _, key := range sortedKeys(actualTunnels) {
		if _, ok := desiredTunnels[key]; ok {
			continue
		}
		stillUnclaimed["tunnel:"+key] = true
		if !b.unclaimed["tunnel:"+key] {
			report.PendingExtraTunnels++
			continue
		}
		report.ExtraTunnels++
		BlocklistDrift.WithLabelValues("tunnel", "extra").Inc()
		t := actualTunnels[key]
		if err := b.Blocklist.UnblockTunnel(t.IP, t.TEID); err != nil {
			errs = append(errs, fmt.Errorf("remove unclaimed tunnel block for %s: %w", key, err))
		}
	}

	b.unclaimed = stillUnclaimed

	BlocklistEntries.WithLabelValues("ip", "desired").Set(float64(report.DesiredIPs))
	BlocklistEntries.WithLabelValues("tunnel", "desired").Set(float64(report.DesiredTunnels))
	BlocklistEntries.WithLabelValues("ip", "kernel").Set(float64(report.KernelIPs))
	BlocklistEntries.WithLabelValues("tunnel", "kernel").Set(float64(report.KernelTunnels))
	capIPs, capTunnels := b.Inspector.MapCapacity()
	BlocklistCapacity.WithLabelValues("ip").Set(float64(capIPs))
	BlocklistCapacity.WithLabelValues("tunnel").Set(float64(capTunnels))

	if len(errs) > 0 {
		BlocklistReconciles.WithLabelValues("error").Inc()
		// Joined rather than returned on the first failure: a single map
		// write failing (a full map, most likely) must not stop the other
		// corrections, since every one of them is independent and each one
		// left undone is a drop that is wrong in the kernel.
		return report, fmt.Errorf("blocklist reconciliation applied %d of %d corrections: %w",
			report.MissingIPs+report.MissingTunnels+report.ExtraIPs+report.ExtraTunnels-len(errs),
			report.MissingIPs+report.MissingTunnels+report.ExtraIPs+report.ExtraTunnels,
			errors.Join(errs...))
	}
	BlocklistReconciles.WithLabelValues("success").Inc()
	return report, nil
}

// desiredEnforcement unions every policy's status into the two sets the
// kernel should hold, keyed canonically so a status entry and a map entry
// for the same address compare equal regardless of how either was written
// ("::ffff:10.0.0.1" and "10.0.0.1" are the same endpoint, and
// blocklistMapAndKey routes both to the IPv4 map — see ipv6Key's comment in
// pkg/ebpf).
//
// Policies being deleted are excluded: their finalizer is already unblocking
// everything they hold, and re-applying it from here would race that to a
// standstill.
func desiredEnforcement(policies []securityv1alpha1.TelecomSecurityPolicy) (map[string]net.IP, map[string]ebpf.BlockedTunnel) {
	ips := map[string]net.IP{}
	tunnels := map[string]ebpf.BlockedTunnel{}

	for i := range policies {
		policy := &policies[i]
		if !policy.DeletionTimestamp.IsZero() {
			continue
		}
		for _, raw := range policy.Status.BlockedSourceIPs {
			ip := net.ParseIP(raw)
			if ip == nil {
				continue // Unparseable status entries are skipped here the same way tryDeEscalate skips them.
			}
			ips[ip.String()] = ip
		}
		for _, entry := range policy.Status.BlockedTunnels {
			ip, teid, err := parseTunnel(entry)
			if err != nil || teid == 0 {
				// teid 0 is rejected by BlockTunnel itself (it means "no
				// tunnel identity"), so filtering it here keeps the
				// reconcile from retrying a guaranteed failure every tick.
				continue
			}
			tunnels[formatTunnel(ip.String(), teid)] = ebpf.BlockedTunnel{IP: ip, TEID: teid}
		}
	}

	return ips, tunnels
}

// sortedKeys gives the two loops a deterministic order, so a log line or a
// test failure names the same entry twice in a row.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
