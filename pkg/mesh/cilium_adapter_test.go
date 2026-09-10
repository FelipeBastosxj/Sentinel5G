package mesh

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCiliumAdapter_QuarantineCreatesThenUpdatesIdempotently(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewCiliumAdapter(AdapterDeps{Client: fakeClient})
	ctx := context.Background()

	selector := map[string]string{"app": "amf-service"}

	if err := adapter.Quarantine(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("first Quarantine returned error: %v", err)
	}
	// Calling it again must not error (Adapter's documented idempotency
	// contract) and must hit the update path, not a duplicate create.
	if err := adapter.Quarantine(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("second Quarantine (idempotent replay) returned error: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ciliumNetworkPolicyGVK)
	name := quarantineName(selector)
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "telecom-core", Name: name}, got); err != nil {
		t.Fatalf("get CiliumNetworkPolicy after Quarantine: %v", err)
	}

	matchLabels, _, _ := unstructured.NestedStringMap(got.Object, "spec", "endpointSelector", "matchLabels")
	if matchLabels["app"] != "amf-service" {
		t.Fatalf("expected endpointSelector.matchLabels app=amf-service, got %v", matchLabels)
	}

	// The exact "all" reserved entity, on both ingressDeny and egressDeny --
	// see cilium_adapter.go's doc comment for why "all" specifically (covers
	// cluster-internal AND external/world traffic) rather than an empty
	// endpointSelector (cluster-internal only).
	ingressDeny, _, _ := unstructured.NestedSlice(got.Object, "spec", "ingressDeny")
	if len(ingressDeny) != 1 {
		t.Fatalf("expected exactly one ingressDeny rule, got %v", ingressDeny)
	}
	ingressDenyRule, ok := ingressDeny[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected ingressDeny[0] to be a map, got %T", ingressDeny[0])
	}
	fromEntities, _, _ := unstructured.NestedStringSlice(ingressDenyRule, "fromEntities")
	if len(fromEntities) != 1 || fromEntities[0] != "all" {
		t.Fatalf("expected ingressDeny[0].fromEntities == [\"all\"], got %v", fromEntities)
	}

	egressDeny, _, _ := unstructured.NestedSlice(got.Object, "spec", "egressDeny")
	if len(egressDeny) != 1 {
		t.Fatalf("expected exactly one egressDeny rule, got %v", egressDeny)
	}
	egressDenyRule, ok := egressDeny[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected egressDeny[0] to be a map, got %T", egressDeny[0])
	}
	toEntities, _, _ := unstructured.NestedStringSlice(egressDenyRule, "toEntities")
	if len(toEntities) != 1 || toEntities[0] != "all" {
		t.Fatalf("expected egressDeny[0].toEntities == [\"all\"], got %v", toEntities)
	}
}

// TestCiliumAdapter_ReleaseIsIdempotentWhenNeverQuarantined covers the case
// the finalizer and tryDeEscalate both rely on: Release must be safe to call
// unconditionally, even for a workload that was never quarantined.
func TestCiliumAdapter_ReleaseIsIdempotentWhenNeverQuarantined(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewCiliumAdapter(AdapterDeps{Client: fakeClient})

	if err := adapter.Release(context.Background(), "telecom-core", map[string]string{"app": "never-quarantined"}); err != nil {
		t.Fatalf("expected Release on a never-quarantined workload to be a no-op, got error: %v", err)
	}
}

func TestCiliumAdapter_ReleaseDeletesExistingQuarantine(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewCiliumAdapter(AdapterDeps{Client: fakeClient})
	ctx := context.Background()

	selector := map[string]string{"app": "amf-service"}
	if err := adapter.Quarantine(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("Quarantine returned error: %v", err)
	}
	if err := adapter.Release(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ciliumNetworkPolicyGVK)
	name := quarantineName(selector)
	err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "telecom-core", Name: name}, got)
	if err == nil {
		t.Fatal("expected the CiliumNetworkPolicy to be gone after Release, but Get succeeded")
	}
}
