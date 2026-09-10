package mesh

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// serverListGVK is the ServerList counterpart to serverGVK, mirroring
// exactly what LinkerdAdapter.Release itself constructs to list every
// quarantine Server by label.
var serverListGVK = schema.GroupVersionKind{Group: serverGVK.Group, Version: serverGVK.Version, Kind: serverGVK.Kind + "List"}

func podWithPorts(namespace, name string, labels map[string]string, ports ...int32) *corev1.Pod {
	var containerPorts []corev1.ContainerPort
	for _, p := range ports {
		containerPorts = append(containerPorts, corev1.ContainerPort{ContainerPort: p})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: labels},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "example", Ports: containerPorts}},
		},
	}
}

func TestLinkerdAdapter_QuarantineWithNoMatchingPodsReturnsAnError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewLinkerdAdapter(AdapterDeps{Client: fakeClient})

	err := adapter.Quarantine(context.Background(), "telecom-core", map[string]string{"app": "amf-service"})
	if err == nil {
		t.Fatal("expected an error when no Pods match the selector (a zero-port quarantine would silently do nothing)")
	}
}

func TestLinkerdAdapter_QuarantineWithUndeclaredPortsReturnsAnError(t *testing.T) {
	selector := map[string]string{"app": "amf-service"}
	pod := podWithPorts("telecom-core", "amf-0", selector) // No declared ports.
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).WithObjects(pod).Build()
	adapter := NewLinkerdAdapter(AdapterDeps{Client: fakeClient})

	err := adapter.Quarantine(context.Background(), "telecom-core", selector)
	if err == nil {
		t.Fatal("expected an error when matching Pods declare no container ports")
	}
}

func TestLinkerdAdapter_QuarantineCreatesOneServerPerDistinctPort(t *testing.T) {
	selector := map[string]string{"app": "amf-service"}
	// Two Pods behind the same selector: one on GTP-U (2152), one also
	// exposing an extra port -- discoverPorts must dedupe the shared port
	// and still find both distinct ones.
	pod1 := podWithPorts("telecom-core", "amf-0", selector, 2152)
	pod2 := podWithPorts("telecom-core", "amf-1", selector, 2152, 8080)
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).WithObjects(pod1, pod2).Build()
	adapter := NewLinkerdAdapter(AdapterDeps{Client: fakeClient})
	ctx := context.Background()

	if err := adapter.Quarantine(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("Quarantine returned error: %v", err)
	}
	// Idempotent replay must not error and must not duplicate objects.
	if err := adapter.Quarantine(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("second Quarantine (idempotent replay) returned error: %v", err)
	}

	hash := quarantineName(selector)
	for _, port := range []string{"2152", "8080"} {
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(serverGVK)
		name := hash + "-" + port
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "telecom-core", Name: name}, got); err != nil {
			t.Fatalf("expected a Server named %q for port %s, get failed: %v", name, port, err)
		}
		accessPolicy, _, _ := unstructured.NestedString(got.Object, "spec", "accessPolicy")
		if accessPolicy != "deny" {
			t.Fatalf("expected spec.accessPolicy=deny for port %s, got %q", port, accessPolicy)
		}
		matchLabels, _, _ := unstructured.NestedStringMap(got.Object, "spec", "podSelector", "matchLabels")
		if matchLabels["app"] != "amf-service" {
			t.Fatalf("expected podSelector.matchLabels app=amf-service for port %s, got %v", port, matchLabels)
		}
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(serverListGVK)
	if err := fakeClient.List(ctx, list); err != nil {
		t.Fatalf("list Servers: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected exactly 2 Servers (one per distinct port), got %d", len(list.Items))
	}
}

// TestLinkerdAdapter_ReleaseIsIdempotentWhenNeverQuarantined covers the case
// the finalizer and tryDeEscalate both rely on: Release must be safe to call
// unconditionally, even for a workload that was never quarantined.
func TestLinkerdAdapter_ReleaseIsIdempotentWhenNeverQuarantined(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewLinkerdAdapter(AdapterDeps{Client: fakeClient})

	if err := adapter.Release(context.Background(), "telecom-core", map[string]string{"app": "never-quarantined"}); err != nil {
		t.Fatalf("expected Release on a never-quarantined workload to be a no-op, got error: %v", err)
	}
}

// TestLinkerdAdapter_ReleaseDeletesAllServersEvenIfPortsNoLongerMatch is the
// regression guard for Release finding Servers by label rather than by
// re-discovering the target Pods' current ports (see Release's doc
// comment): the Pods are gone by the time Release runs, yet every Server
// Quarantine created must still be cleaned up.
func TestLinkerdAdapter_ReleaseDeletesAllServersEvenIfPortsNoLongerMatch(t *testing.T) {
	selector := map[string]string{"app": "amf-service"}
	pod := podWithPorts("telecom-core", "amf-0", selector, 2152, 5060)
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).WithObjects(pod).Build()
	adapter := NewLinkerdAdapter(AdapterDeps{Client: fakeClient})
	ctx := context.Background()

	if err := adapter.Quarantine(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("Quarantine returned error: %v", err)
	}
	// Simulate the Pod having disappeared (e.g. rescheduled/deleted) before
	// Release runs -- Release must not need to re-list Pods to find what to clean up.
	if err := fakeClient.Delete(ctx, pod); err != nil {
		t.Fatalf("delete pod: %v", err)
	}

	if err := adapter.Release(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(serverListGVK)
	if err := fakeClient.List(ctx, list); err != nil {
		t.Fatalf("list Servers: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected every quarantine Server to be deleted, found %d remaining", len(list.Items))
	}
}
