package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	securityv1alpha1 "github.com/sentinel5g/sentinel5g/api/v1alpha1"
)

func TestReconciler_TransitionsPendingToMonitoring(t *testing.T) {
	scheme := newScheme()

	policy := &securityv1alpha1.TelecomSecurityPolicy{}
	policy.Namespace = "telecom-core"
	policy.Name = "protect-amf-core"
	policy.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	r := &Reconciler{
		Client: fakeClient,
		Log:    testr.New(t),
		Index:  NewPolicyIndex(),
	}

	req := ctrl.Request{NamespacedName: nnFor(policy)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := fakeClient.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}

	if got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("expected phase %q, got %q", securityv1alpha1.PolicyPhaseMonitoring, got.Status.Phase)
	}

	matches := r.Index.MatchingPolicies("telecom-core", map[string]string{"app": "amf-service"})
	if len(matches) != 1 {
		t.Fatalf("expected policy to be indexed after reconcile, got %d matches", len(matches))
	}
}

func TestReconciler_DeletedPolicyIsRemovedFromIndex(t *testing.T) {
	scheme := newScheme()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	idx := NewPolicyIndex()
	ghost := &securityv1alpha1.TelecomSecurityPolicy{}
	ghost.Namespace = "telecom-core"
	ghost.Name = "already-deleted"
	ghost.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	idx.Put(ghost)

	r := &Reconciler{Client: fakeClient, Log: testr.New(t), Index: idx}

	req := ctrl.Request{NamespacedName: nnFor(ghost)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile returned error for a not-found object: %v", err)
	}

	if matches := idx.MatchingPolicies("telecom-core", map[string]string{"app": "amf-service"}); len(matches) != 0 {
		t.Fatalf("expected index to drop the deleted policy, got %+v", matches)
	}
}
