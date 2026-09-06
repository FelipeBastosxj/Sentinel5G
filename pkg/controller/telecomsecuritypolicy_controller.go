// Package controller implements the TelecomSecurityPolicy reconciliation
// loop and the threat-score-driven closed-loop mitigation described in
// docs/architecture.md (Layer 4).
package controller

import (
	"context"
	"fmt"
	"net"

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
// "Finalizer-based cleanup"). This is deliberately separate from the
// automatic-de-escalation design work ROADMAP.md also flags as still open:
// this only fires on policy *deletion*, never on a later low score.
const telecomSecurityPolicyFinalizer = "security.sentinel5g.io/finalizer"

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
