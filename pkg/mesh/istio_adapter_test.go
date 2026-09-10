package mesh

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newMeshTestScheme registers every mesh adapter's CRD GVK as an
// unstructured type so the fake client can Get/Create/Update/Delete them
// without vendoring each mesh's own typed client-go — mirroring how
// IstioAdapter/CiliumAdapter themselves talk to the real API server (see
// istio_adapter.go's package doc comment).
func newMeshTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	s.AddKnownTypeWithName(authorizationPolicyGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(ciliumNetworkPolicyGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(serverGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: serverGVK.Group, Version: serverGVK.Version, Kind: serverGVK.Kind + "List"},
		&unstructured.UnstructuredList{},
	)
	return s
}

func TestIstioAdapter_QuarantineCreatesThenUpdatesIdempotently(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewIstioAdapter(AdapterDeps{Client: fakeClient})
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
	got.SetGroupVersionKind(authorizationPolicyGVK)
	name := quarantineName(selector)
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "telecom-core", Name: name}, got); err != nil {
		t.Fatalf("get AuthorizationPolicy after Quarantine: %v", err)
	}

	action, _, _ := unstructured.NestedString(got.Object, "spec", "action")
	if action != "DENY" {
		t.Fatalf("expected spec.action=DENY, got %q", action)
	}
	matchLabels, _, _ := unstructured.NestedStringMap(got.Object, "spec", "selector", "matchLabels")
	if matchLabels["app"] != "amf-service" {
		t.Fatalf("expected matchLabels app=amf-service, got %v", matchLabels)
	}
	rules, _, _ := unstructured.NestedSlice(got.Object, "spec", "rules")
	if len(rules) != 1 {
		t.Fatalf("expected exactly one (empty, match-everything) rule, got %v", rules)
	}
}

// TestIstioAdapter_ReleaseIsIdempotentWhenNeverQuarantined covers the case
// the finalizer and tryDeEscalate both rely on: Release must be safe to call
// unconditionally, even for a workload that was never quarantined.
func TestIstioAdapter_ReleaseIsIdempotentWhenNeverQuarantined(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewIstioAdapter(AdapterDeps{Client: fakeClient})

	if err := adapter.Release(context.Background(), "telecom-core", map[string]string{"app": "never-quarantined"}); err != nil {
		t.Fatalf("expected Release on a never-quarantined workload to be a no-op, got error: %v", err)
	}
}

func TestIstioAdapter_ReleaseDeletesExistingQuarantine(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(newMeshTestScheme()).Build()
	adapter := NewIstioAdapter(AdapterDeps{Client: fakeClient})
	ctx := context.Background()

	selector := map[string]string{"app": "amf-service"}
	if err := adapter.Quarantine(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("Quarantine returned error: %v", err)
	}
	if err := adapter.Release(ctx, "telecom-core", selector); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(authorizationPolicyGVK)
	name := quarantineName(selector)
	err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "telecom-core", Name: name}, got)
	if err == nil {
		t.Fatal("expected the AuthorizationPolicy to be gone after Release, but Get succeeded")
	}
}

func TestQuarantineName_DeterministicRegardlessOfMapOrder(t *testing.T) {
	a := quarantineName(map[string]string{"app": "amf-service", "tier": "core"})
	b := quarantineName(map[string]string{"tier": "core", "app": "amf-service"})
	if a != b {
		t.Fatalf("expected the same name regardless of map insertion order, got %q vs %q", a, b)
	}

	different := quarantineName(map[string]string{"app": "smf-service"})
	if different == a {
		t.Fatalf("expected different selectors to produce different names, both got %q", a)
	}
}
