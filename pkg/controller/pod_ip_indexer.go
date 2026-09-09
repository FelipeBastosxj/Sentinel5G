package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodIPIndexer keeps Index in sync with every Pod's IP address, cluster-wide
// — unlike Reconciler (scoped to TelecomSecurityPolicy targets), it watches
// all Pods, since pkg/ingestion needs to attribute kernel-observed traffic
// from any workload a NormalizedEvent might reference, not just ones a
// policy currently targets.
type PodIPIndexer struct {
	client.Client
	Index *PodIPIndex
}

// Reconcile implements the controller-runtime Reconciler interface.
func (r *PodIPIndexer) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			// PodIPIndex.Remove uses its own reverse index to find the
			// deleted Pod's last known IP -- a NotFound response alone
			// carries no IP to key off directly.
			r.Index.Remove(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get pod %s: %w", req.NamespacedName, err)
	}

	if pod.Status.PodIP == "" {
		return ctrl.Result{}, nil
	}
	r.Index.Put(pod.Status.PodIP, PodRef{Namespace: pod.Namespace, Name: pod.Name})
	return ctrl.Result{}, nil
}

// SetupWithManager wires the PodIPIndexer into mgr.
func (r *PodIPIndexer) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
