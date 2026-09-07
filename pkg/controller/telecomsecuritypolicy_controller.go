// Package controller implements the TelecomSecurityPolicy reconciliation
// loop and the threat-score-driven closed-loop mitigation described in
// docs/architecture.md (Layer 4).
package controller

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/mesh"
)

// telecomSecurityPolicyFinalizer ensures the operator releases any active
// eBPF blocklist entries / mesh quarantine before a TelecomSecurityPolicy is
// actually removed, rather than leaving that to a manual `kubectl delete
// authorizationpolicy` / operator restart (see ROADMAP.md Phase 1,
// "Finalizer-based cleanup"). Deliberately separate from de-escalation
// below: this only fires on policy *deletion*, unconditionally; de-escalation
// only fires while the policy still exists, gated on a quiet period.
const telecomSecurityPolicyFinalizer = "security.sentinel5g.io/finalizer"

// defaultDeEscalationDwell is used when Reconciler.DeEscalationDwell is
// zero (e.g. a Reconciler built without wiring cmd/operator/main.go's
// config). See tryDeEscalate's doc comment for why this is a quiet-period
// timer, not a "wait for a sustained low score" one — a reasoned, not
// empirically tuned, default (same honesty caveat as
// bpf/headers/common.h's SCAN_EMIT_THRESHOLD).
const defaultDeEscalationDwell = 5 * time.Minute

// Reconciler reconciles a TelecomSecurityPolicy object: it validates the
// spec and keeps Status.Phase in sync, mirrors every observed policy into
// Index so ThreatScoreWatcher can match incoming threat scores without
// hitting the API server on the hot path, and — via telecomSecurityPolicyFinalizer
// — releases any mitigations the policy caused before it's actually deleted.
type Reconciler struct {
	client.Client
	Log       logr.Logger
	Index     *PolicyIndex
	Blocklist ebpf.BlocklistUpdater
	Mesh      mesh.Adapter

	// DeEscalationDwell is how long a policy must go without a new
	// mitigation before tryDeEscalate reverses its active ones. Zero uses
	// defaultDeEscalationDwell.
	DeEscalationDwell time.Duration
	// Now returns the current time; nil uses time.Now. Overridden in tests
	// for deterministic dwell-time assertions without a real clock.
	Now func() time.Time
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reconciler) deEscalationDwell() time.Duration {
	if r.DeEscalationDwell > 0 {
		return r.DeEscalationDwell
	}
	return defaultDeEscalationDwell
}

// Reconcile implements the controller-runtime Reconciler interface.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("telecomsecuritypolicy", req.NamespacedName)

	var policy securityv1alpha1.TelecomSecurityPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if apierrors.IsNotFound(err) {
			r.Index.Remove(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TelecomSecurityPolicy %s: %w", req.NamespacedName, err)
	}

	if !policy.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, log, &policy)
	}

	if !controllerutil.ContainsFinalizer(&policy, telecomSecurityPolicyFinalizer) {
		controllerutil.AddFinalizer(&policy, telecomSecurityPolicyFinalizer)
		if err := r.Update(ctx, &policy); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer to %s: %w", req.NamespacedName, err)
		}
	}

	r.Index.Put(&policy)

	if policy.Status.Phase == "" {
		policy.Status.Phase = securityv1alpha1.PolicyPhasePending
	}

	if policy.Status.Phase == securityv1alpha1.PolicyPhasePending {
		policy.Status.Phase = securityv1alpha1.PolicyPhaseMonitoring
		if err := r.Status().Update(ctx, &policy); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status for %s: %w", req.NamespacedName, err)
		}
		r.Index.Put(&policy)
		log.Info("policy is now under active monitoring")
	}

	if policy.Status.Phase == securityv1alpha1.PolicyPhaseMitigating {
		return r.tryDeEscalate(ctx, log, &policy)
	}

	return ctrl.Result{}, nil
}

// tryDeEscalate reverses a policy's active mitigations (eBPF unblock, mesh
// release) once it's gone Reconciler.deEscalationDwell without a new one,
// and returns Phase to Monitoring — the automatic de-escalation ROADMAP.md
// Phase 1 flagged as a one-way door before this.
//
// This is a quiet-period timer on Status.LastMitigationTime (policy-wide,
// covering every source this policy has ever mitigated), not a
// per-source "wait for that source's score to sustain low" check, and
// that's a considered choice, not a shortcut: once ThreatScoreWatcher
// pushes a source into the eBPF blocklist, bpf/packet_filter.c's XDP
// program drops every subsequent packet from it (xdp_packet_filter's
// blocklist check runs before any protocol parsing) — so a blocked source
// produces zero further SignalingEvents/ThreatScoreEvents, and "wait for
// its next low score" is not observable even in principle. A mesh-only
// quarantine (IsolatePod without EbpfBlock) doesn't silence the source the
// same way (Istio enforces at L7, downstream of XDP), so a per-source
// signal would in theory be available there — but running two different
// de-escalation mechanisms depending on which action fired is real added
// complexity for a case Actions can already combine, so both share this one
// timer. The trade-off, stated plainly: any new mitigation for *any* source
// under this policy resets the quiet-period clock for *every* currently
// mitigated source, not just the new one — deliberately conservative
// (don't start unwinding anything while the policy is still actively
// seeing new threats), not an oversight. A pure elapsed-time gate (nothing
// the event stream itself can influence beyond staying quiet) is also
// resistant to the "send one benign packet to get unblocked" evasion this
// item was originally written to guard against.
func (r *Reconciler) tryDeEscalate(ctx context.Context, log logr.Logger, policy *securityv1alpha1.TelecomSecurityPolicy) (ctrl.Result, error) {
	if policy.Status.LastMitigationTime == nil {
		return ctrl.Result{}, nil // Mitigating with no recorded time shouldn't happen; nothing to gate on.
	}

	elapsed := r.now().Sub(policy.Status.LastMitigationTime.Time)
	if remaining := r.deEscalationDwell() - elapsed; remaining > 0 {
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// Blocklist/Mesh are only nil in tests that don't exercise mitigation
	// actions — same convention as finalize() above.
	if r.Blocklist != nil {
		for _, raw := range policy.Status.BlockedSourceIPs {
			ip := net.ParseIP(raw)
			if ip == nil {
				continue
			}
			if err := r.Blocklist.Unblock(ip); err != nil {
				return ctrl.Result{}, fmt.Errorf("de-escalate: unblock %s for %s/%s: %w", raw, policy.Namespace, policy.Name, err)
			}
		}
	}

	if r.Mesh != nil && policy.Spec.Actions.IsolatePod {
		if selector := firstMatchLabels(policy.Spec.TargetWorkloads); selector != nil {
			if err := r.Mesh.Release(ctx, policy.Namespace, selector); err != nil {
				return ctrl.Result{}, fmt.Errorf("de-escalate: release mesh quarantine for %s/%s: %w", policy.Namespace, policy.Name, err)
			}
		}
	}

	unblockedCount := len(policy.Status.BlockedSourceIPs)
	policy.Status.BlockedSourceIPs = nil
	policy.Status.Phase = securityv1alpha1.PolicyPhaseMonitoring
	if err := r.Status().Update(ctx, policy); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status after de-escalating %s/%s: %w", policy.Namespace, policy.Name, err)
	}
	r.Index.Put(policy)

	log.Info("de-escalated policy after quiet dwell period",
		"dwell", r.deEscalationDwell(), "unblockedIPs", unblockedCount, "meshReleaseAttempted", policy.Spec.Actions.IsolatePod)
	return ctrl.Result{}, nil
}

// finalize releases any mesh quarantine and eBPF blocklist entries this
// policy caused, then removes telecomSecurityPolicyFinalizer so the object
// can actually be deleted. Mesh.Release and Blocklist.Unblock are both
// documented as idempotent (see pkg/mesh.Adapter, pkg/ebpf.BlocklistUpdater),
// so this runs unconditionally rather than tracking whether isolation was
// ever actually applied — safe even for a policy that never mitigated
// anything. Blocklist/Mesh are only nil in tests that don't exercise
// deletion; cmd/operator/main.go always wires both (Mesh falls back to a
// no-op adapter, Blocklist to a no-op loader, when the real thing isn't
// configured — see attachBlocklist and mesh.NewAdapter).
func (r *Reconciler) finalize(ctx context.Context, log logr.Logger, policy *securityv1alpha1.TelecomSecurityPolicy) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(policy, telecomSecurityPolicyFinalizer) {
		return ctrl.Result{}, nil
	}

	if r.Mesh != nil {
		if selector := firstMatchLabels(policy.Spec.TargetWorkloads); selector != nil {
			if err := r.Mesh.Release(ctx, policy.Namespace, selector); err != nil {
				return ctrl.Result{}, fmt.Errorf("release mesh quarantine for %s/%s: %w", policy.Namespace, policy.Name, err)
			}
		}
	}

	if r.Blocklist != nil {
		for _, raw := range policy.Status.BlockedSourceIPs {
			ip := net.ParseIP(raw)
			if ip == nil {
				continue
			}
			if err := r.Blocklist.Unblock(ip); err != nil {
				return ctrl.Result{}, fmt.Errorf("unblock %s for %s/%s: %w", raw, policy.Namespace, policy.Name, err)
			}
		}
	}

	log.Info("released mitigations for deleted policy",
		"quarantineReleaseAttempted", policy.Spec.Actions.IsolatePod,
		"unblockedIPs", len(policy.Status.BlockedSourceIPs))

	controllerutil.RemoveFinalizer(policy, telecomSecurityPolicyFinalizer)
	if err := r.Update(ctx, policy); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer from %s/%s: %w", policy.Namespace, policy.Name, err)
	}

	r.Index.Remove(types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name})
	return ctrl.Result{}, nil
}

// SetupWithManager wires the Reconciler into mgr.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&securityv1alpha1.TelecomSecurityPolicy{}).
		Complete(r)
}
