// Package controller implements the TelecomSecurityPolicy reconciliation
// loop and the threat-score-driven closed-loop mitigation described in
// docs/architecture.md (Layer 4).
package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	securityv1alpha1 "github.com/sentinel5g/sentinel5g/api/v1alpha1"
)

// Reconciler reconciles a TelecomSecurityPolicy object: it validates the
// spec and keeps Status.Phase in sync, and mirrors every observed policy
// into Index so ThreatScoreWatcher can match incoming threat scores without
// hitting the API server on the hot path.
type Reconciler struct {
	client.Client
	Log   logr.Logger
	Index *PolicyIndex
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

// SetupWithManager wires the Reconciler into mgr.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&securityv1alpha1.TelecomSecurityPolicy{}).
		Complete(r)
}
