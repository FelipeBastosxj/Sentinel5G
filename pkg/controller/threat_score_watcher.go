package controller

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/mesh"
)

// sensitivityMultiplier biases the operator-wide ThreatScoreThreshold per
// policy: "high" sensitivity fires earlier (lower effective threshold),
// "low" fires later. Values outside [0,1] after multiplication are clamped.
var sensitivityMultiplier = map[securityv1alpha1.Sensitivity]float64{
	securityv1alpha1.SensitivityHigh:   0.8,
	securityv1alpha1.SensitivityMedium: 1.0,
	securityv1alpha1.SensitivityLow:    1.2,
}

// ThreatScoreWatcher subscribes to the AI engine's scored-threat subject and
// drives closed-loop mitigation: it resolves the scored Pod's labels,
// matches them against Index, and — when a matching policy allows it —
// pushes the offending source IP into the eBPF blocklist and/or quarantines
// the workload at the mesh layer, reflecting the outcome back onto the
// policy's Status.
//
// NATS JetStream (pkg/events.Bus) only guarantees at-least-once delivery, so
// handle/applyPolicy below WILL occasionally run more than once for the
// same ThreatScoreEvent. Blocklist and Mesh implementations must therefore
// be idempotent — pkg/ebpf.Loader.Block is a map upsert keyed by IP, and
// pkg/mesh.IstioAdapter.Quarantine is a name-based upsert keyed by selector
// — so a duplicate delivery is a harmless no-op rather than a double side
// effect. This is exercised for real in pkg/controller/envtest_test.go's
// closed-loop test.
type ThreatScoreWatcher struct {
	client.Client
	Log           logr.Logger
	Index         *PolicyIndex
	Bus           *events.Connector
	Subject       string
	Blocklist     ebpf.BlocklistUpdater
	Mesh          mesh.Adapter
	BaseThreshold float64

	// Scoring records that the AI engine is actually publishing, backing the
	// ScoringPipelineReady condition Reconciler writes onto every policy (see
	// scoring_pipeline.go). Nil is fine -- the tracking is skipped.
	Scoring *ScoringPipelineTracker
}

// Start implements manager.Runnable so the watcher's lifecycle is tied to
// the controller-runtime manager (started after the informer cache syncs,
// stopped on shutdown).
func (w *ThreatScoreWatcher) Start(ctx context.Context) error {
	// Before touching the bus: Index is populated by Reconciler as it
	// reconciles each policy, and on a fresh process that happens
	// concurrently with this Runnable starting. JetStream delivers every
	// score buffered while the operator was down the instant the durable
	// re-subscribes below -- so without this, an operator restart consumed
	// those scores against an empty index, found no matching policy for any
	// of them, acked, and dropped them with nothing but a V(1) log line.
	// Found by watching a real score vanish on a real cluster right after a
	// rollout. Leader-gated runnables start after the cache has synced, so a
	// List from the cached client here is complete.
	if err := w.warmIndex(ctx); err != nil {
		return fmt.Errorf("populate policy index before subscribing: %w", err)
	}
	if w.Scoring != nil {
		// The grace period starts when THIS process can actually receive a
		// score, which is here -- not when it was constructed, possibly
		// hours ago as a standby replica. See ScoringPipelineTracker.
		w.Scoring.MarkStarted()
	}

	bus, err := w.Bus.Wait(ctx)
	if err != nil {
		// ctx done before NATS ever connected -- ordinary shutdown, not a
		// Start failure (returning an error here would take the whole
		// manager down with it).
		return nil
	}

	unsubscribe, err := bus.SubscribeThreatScores(w.Subject, "sentinel5g-operator", func(event events.ThreatScoreEvent) error {
		return w.handle(ctx, event)
	})
	if err != nil {
		return fmt.Errorf("subscribe to threat scores on %q: %w", w.Subject, err)
	}
	defer func() { _ = unsubscribe() }()

	<-ctx.Done()
	return nil
}

// warmIndex seeds Index with every existing policy so scores delivered
// before Reconciler has visited each one still match. Reconciler's own Put
// calls remain authoritative afterwards; this only removes the window in
// which the index is emptier than the cluster.
func (w *ThreatScoreWatcher) warmIndex(ctx context.Context) error {
	var policies securityv1alpha1.TelecomSecurityPolicyList
	if err := w.List(ctx, &policies); err != nil {
		return err
	}
	for i := range policies.Items {
		if !policies.Items[i].DeletionTimestamp.IsZero() {
			continue // Reconciler.finalize is about to remove it; don't resurrect it.
		}
		w.Index.Put(&policies.Items[i])
	}
	w.Log.V(1).Info("policy index warmed before subscribing", "policies", len(policies.Items))
	return nil
}

func (w *ThreatScoreWatcher) handle(ctx context.Context, event events.ThreatScoreEvent) error {
	log := w.Log.WithValues("namespace", event.Namespace, "pod", event.PodName, "sourceIp", event.SourceIP)

	// Counted here, before the Pod lookup and policy matching below, on
	// purpose: this is the "the scoring pipeline is alive" signal, and an
	// event for a Pod that has since been deleted (dropped a few lines down)
	// still proves the AI engine is publishing. See metrics.go.
	ThreatScoresReceived.WithLabelValues(scoreSourceLabel(event.Model)).Inc()
	ThreatScore.Observe(event.Score)
	if w.Scoring != nil {
		w.Scoring.Observe()
	}

	var pod corev1.Pod
	if err := w.Get(ctx, types.NamespacedName{Namespace: event.Namespace, Name: event.PodName}, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("dropping threat score for a pod that no longer exists")
			return nil
		}
		return fmt.Errorf("get pod %s/%s: %w", event.Namespace, event.PodName, err)
	}

	for _, policy := range w.Index.MatchingPolicies(event.Namespace, pod.Labels) {
		if err := w.applyPolicy(ctx, policy, event); err != nil {
			log.Error(err, "failed to apply policy", "policy", policy.Name)
		}
	}
	return nil
}

func (w *ThreatScoreWatcher) applyPolicy(ctx context.Context, policy *securityv1alpha1.TelecomSecurityPolicy, event events.ThreatScoreEvent) error {
	threshold := w.BaseThreshold * sensitivityMultiplierOrDefault(policy.Spec.ThreatDetection.Sensitivity)
	if threshold > 1 {
		threshold = 1
	}

	latest := policy.DeepCopy()
	latest.Status.ObservedThreatScore = fmt.Sprintf("%.4f", event.Score)

	if event.Score < threshold {
		// A benign score does NOT end a mitigation. Reversal is
		// Reconciler.tryDeEscalate's job, on a quiet-period timer, and it
		// only runs while Phase == Mitigating -- so flipping to Monitoring
		// here would leave Status.BlockedSourceIPs populated and the kernel
		// blocklist entries in place with nothing ever scheduled to remove
		// them. That was latent while scores were rare; with the AI engine
		// scoring every NormalizedEvent from every source hitting the same
		// Pod, a Mitigating policy sees a sub-threshold score within
		// milliseconds, and the block it just placed would be orphaned
		// every time. Alerting (no side effects) does return to Monitoring.
		if latest.Status.Phase != securityv1alpha1.PolicyPhaseMitigating {
			latest.Status.Phase = securityv1alpha1.PolicyPhaseMonitoring
		}
		return w.updateStatus(ctx, latest)
	}

	if !policy.Spec.ThreatDetection.AutoMitigate {
		// Threshold crossed, action withheld by policy (detection-only pilot),
		// not a failure -- PolicyPhaseDegraded is reserved for genuine
		// operator-side failures (a mesh/eBPF action erroring below).
		//
		// This increment is the shadow-mode false-positive measurement
		// ROADMAP.md Phase 2.5 asked for: one crossing that WOULD have
		// mitigated had autoMitigate been on. See metrics.go.
		ThresholdCrossings.WithLabelValues(policy.Namespace, policy.Name, "alerting").Inc()
		latest.Status.Phase = securityv1alpha1.PolicyPhaseAlerting
		return w.updateStatus(ctx, latest)
	}

	ThresholdCrossings.WithLabelValues(policy.Namespace, policy.Name, "mitigating").Inc()

	// Per-tunnel first: it is the precise action, and a policy that enables
	// both wants the source-wide block only as the fallback for a score
	// that carried no tunnel identity.
	if policy.Spec.Actions.EbpfBlockTunnel {
		switch ip := net.ParseIP(event.SourceIP); {
		case ip == nil:
		case event.TEID == 0:
			// Deliberately not a silent widening to Block(sourceIP): the
			// operator asked for one subscriber and would get the whole
			// gNB. Logged so a policy configured for tunnel blocking
			// against a capture path that cannot produce a TEID (Hubble,
			// Falco) is visible rather than mysteriously inert.
			w.Log.V(1).Info("tunnel block requested but the score carries no TEID; taking no per-tunnel action",
				"policy", policy.Name, "sourceIp", event.SourceIP)
			Mitigations.WithLabelValues("ebpf_block_tunnel", "no_teid").Inc()
		default:
			if err := w.Blocklist.BlockTunnel(ip, event.TEID); err != nil {
				Mitigations.WithLabelValues("ebpf_block_tunnel", "error").Inc()
				return fmt.Errorf("ebpf block tunnel %s/%#x: %w", event.SourceIP, event.TEID, err)
			}
			Mitigations.WithLabelValues("ebpf_block_tunnel", "success").Inc()
			latest.Status.BlockedTunnels = appendUnique(latest.Status.BlockedTunnels, formatTunnel(event.SourceIP, event.TEID))
		}
	}

	if policy.Spec.Actions.EbpfBlock {
		if ip := net.ParseIP(event.SourceIP); ip != nil {
			if err := w.Blocklist.Block(ip); err != nil {
				Mitigations.WithLabelValues("ebpf_block", "error").Inc()
				return fmt.Errorf("ebpf block %s: %w", event.SourceIP, err)
			}
			Mitigations.WithLabelValues("ebpf_block", "success").Inc()
			latest.Status.BlockedSourceIPs = appendUnique(latest.Status.BlockedSourceIPs, event.SourceIP)
		}
	}

	if policy.Spec.Actions.IsolatePod {
		if selector := firstMatchLabels(policy.Spec.TargetWorkloads); selector != nil {
			if err := w.Mesh.Quarantine(ctx, policy.Namespace, selector); err != nil {
				Mitigations.WithLabelValues("mesh_quarantine", "error").Inc()
				return fmt.Errorf("mesh quarantine for %s/%s: %w", policy.Namespace, policy.Name, err)
			}
			Mitigations.WithLabelValues("mesh_quarantine", "success").Inc()
		}
	}

	now := metav1.Now()
	latest.Status.Phase = securityv1alpha1.PolicyPhaseMitigating
	latest.Status.LastMitigationTime = &now

	return w.updateStatus(ctx, latest)
}

func (w *ThreatScoreWatcher) updateStatus(ctx context.Context, policy *securityv1alpha1.TelecomSecurityPolicy) error {
	if err := w.Status().Update(ctx, policy); err != nil {
		return fmt.Errorf("update status for %s/%s: %w", policy.Namespace, policy.Name, err)
	}
	w.Index.Put(policy)
	// Every phase change this watcher makes funnels through here, so the
	// gauge is set in one place rather than at each of applyPolicy's three
	// branches. The Reconciler sets it for the phases it owns (Pending ->
	// Monitoring, and de-escalation back to Monitoring).
	SetPolicyPhase(policy.Namespace, policy.Name, policy.Status.Phase)
	return nil
}

func sensitivityMultiplierOrDefault(s securityv1alpha1.Sensitivity) float64 {
	if m, ok := sensitivityMultiplier[s]; ok {
		return m
	}
	return sensitivityMultiplier[securityv1alpha1.SensitivityMedium]
}

// appendUnique appends ip to ips unless it's already present — Status.
// BlockedSourceIPs must stay a set (repeated ThreatScoreEvents for the same
// attacker shouldn't grow it unbounded; NATS only guarantees at-least-once
// delivery, see this file's package doc comment).
func appendUnique(ips []string, ip string) []string {
	for _, existing := range ips {
		if existing == ip {
			return ips
		}
	}
	return append(ips, ip)
}

// formatTunnel / parseTunnel are the single definition of how a blocked
// tunnel is written into Status.BlockedTunnels ("<ip>/<teid hex>"). The
// status field is the only record the finalizer and the de-escalation timer
// have of what to undo, so the two directions live next to each other and
// are round-tripped by a test.
func formatTunnel(sourceIP string, teid uint32) string {
	return fmt.Sprintf("%s/%#x", sourceIP, teid)
}

func parseTunnel(entry string) (net.IP, uint32, error) {
	sourceIP, rawTEID, found := strings.Cut(entry, "/")
	if !found {
		return nil, 0, fmt.Errorf("malformed tunnel entry %q: want \"<ip>/<teid>\"", entry)
	}
	ip := net.ParseIP(sourceIP)
	if ip == nil {
		return nil, 0, fmt.Errorf("malformed tunnel entry %q: %q is not an IP", entry, sourceIP)
	}
	teid, err := strconv.ParseUint(strings.TrimPrefix(rawTEID, "0x"), 16, 32)
	if err != nil {
		return nil, 0, fmt.Errorf("malformed tunnel entry %q: %w", entry, err)
	}
	return ip, uint32(teid), nil
}

func firstMatchLabels(selectors []securityv1alpha1.WorkloadSelector) map[string]string {
	for _, sel := range selectors {
		if sel.App != "" {
			return map[string]string{"app": sel.App}
		}
		if len(sel.MatchLabels) > 0 {
			return sel.MatchLabels
		}
	}
	return nil
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
// ThreatScoreWatcher drives cluster-wide mitigation decisions and must run
// as exactly one active instance — controller-runtime's default for a
// plain manager.Runnable already achieves this (an un-annotated Runnable
// lands in the leader-gated group), so this override doesn't change
// behavior today. It's here so that guarantee is explicit and doesn't rest
// on an unexported third-party default that could change, rather than
// implicit — see pkg/ingestion.Publisher's NeedLeaderElection for the
// contrasting case (a Runnable that must NOT be leader-gated).
func (w *ThreatScoreWatcher) NeedLeaderElection() bool { return true }

var _ manager.Runnable = (*ThreatScoreWatcher)(nil)
var _ manager.LeaderElectionRunnable = (*ThreatScoreWatcher)(nil)
