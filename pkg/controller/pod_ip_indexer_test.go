package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPodIPIndexer_ReconcilePutsAKnownPodsIP(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "telecom-core", Name: "amf-0"},
		Status:     corev1.PodStatus{PodIP: "10.42.0.7"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(pod).WithStatusSubresource(&corev1.Pod{}).Build()

	index := NewPodIPIndex()
	r := &PodIPIndexer{Client: fakeClient, Index: index}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "telecom-core", Name: "amf-0"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ref, ok := index.Lookup("10.42.0.7")
	if !ok || ref.Name != "amf-0" {
		t.Fatalf("expected amf-0 to be indexed under its PodIP, got %+v (ok=%v)", ref, ok)
	}
}

// Regression guard: a Reconcile for a Pod that no longer exists must evict
// its entry from the index (see PodIPIndex.Remove's doc comment for why a
// reverse index is needed to make this possible from a bare NotFound).
func TestPodIPIndexer_ReconcileEvictsADeletedPodsEntry(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newScheme()).Build() // No Pod: simulates it having been deleted.

	index := NewPodIPIndex()
	index.Put("10.42.0.7", PodRef{Namespace: "telecom-core", Name: "amf-0"})

	r := &PodIPIndexer{Client: fakeClient, Index: index}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "telecom-core", Name: "amf-0"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := index.Lookup("10.42.0.7"); ok {
		t.Fatal("expected the deleted Pod's entry to be evicted from the index")
	}
}
