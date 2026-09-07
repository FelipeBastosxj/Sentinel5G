package controller

import (
	"context"
	"fmt"
	"net"

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
	Bus           *events.Bus
	Subject       string
	Blocklist     ebpf.BlocklistUpdater
	Mesh          mesh.Adapter
	BaseThreshold float64
}

// Start implements manager.Runnable so the watcher's lifecycle is tied to
// the controller-runtime manager (started after the informer cache syncs,
// stopped on shutdown).
func (w *ThreatScoreWatcher) Start(ctx context.Context) error {
	unsubscribe, err := w.Bus.SubscribeThreatScores(w.Subject, "sentinel5g-operator", func(event events.ThreatScoreEvent) error {
		return w.handle(ctx, event)
	})
	if err != nil {
		return fmt.Errorf("subscribe to threat scores on %q: %w", w.Subject, err)
	}
	defer func() { _ = unsubscribe() }()

	<-ctx.Done()
	return nil
}

func (w *ThreatScoreWatcher) handle(ctx context.Context, event events.ThreatScoreEvent) error {
	log := w.Log.WithValues("namespace", event.Namespace, "pod", event.PodName, "sourceIp", event.SourceIP)

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
		latest.Status.Phase = securityv1alpha1.PolicyPhaseMonitoring
		return w.updateStatus(ctx, latest)
	}

	if !policy.Spec.ThreatDetection.AutoMitigate {
		latest.Status.Phase = securityv1alpha1.PolicyPhaseDegraded
		return w.updateStatus(ctx, latest)
	}

	if policy.Spec.Actions.EbpfBlock {
		if ip := net.ParseIP(event.SourceIP); ip != nil {
			if err := w.Blocklist.Block(ip); err != nil {
				return fmt.Errorf("ebpf block %s: %w", event.SourceIP, err)
			}
			latest.Status.BlockedSourceIPs = appendUnique(latest.Status.BlockedSourceIPs, event.SourceIP)
		}
	}

	if policy.Spec.Actions.IsolatePod {
		if selector := firstMatchLabels(policy.Spec.TargetWorkloads); selector != nil {
			if err := w.Mesh.Quarantine(ctx, policy.Namespace, selector); err != nil {
				return fmt.Errorf("mesh quarantine for %s/%s: %w", policy.Namespace, policy.Name, err)
			}
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
