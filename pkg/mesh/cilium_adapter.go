package mesh

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ciliumNetworkPolicyGVK targets Cilium's cilium.io/v2 CiliumNetworkPolicy
// CRD, namespaced (matching this adapter's namespace/selector-scoped
// Quarantine signature) — same unstructured-client-instead-of-vendored-SDK
// approach as IstioAdapter, and for the same reason (avoid pulling in
// github.com/cilium/cilium's full API module for one narrow CRD).
var ciliumNetworkPolicyGVK = schema.GroupVersionKind{
	Group:   "cilium.io",
	Version: "v2",
	Kind:    "CiliumNetworkPolicy",
}

// CiliumAdapter implements Adapter by creating/deleting a CiliumNetworkPolicy
// that denies all ingress and egress for the target workload's selector,
// which Cilium's eBPF datapath enforces directly (no sidecar involved,
// unlike IstioAdapter).
//
// Uses ingressDeny/egressDeny with the "all" reserved entity, not an empty
// endpointSelector/ingress/egress ("allow nothing" via default-deny-on-select
// alone): Cilium's own docs are explicit that "Deny policies take precedence
// over allow policies" -- an allow-nothing policy doesn't override an
// *existing* allow policy for the same pod from elsewhere (e.g. a baseline
// "allow same-namespace" policy many clusters run), it just adds nothing of
// its own, so the pre-existing allow would still let that traffic through.
// An explicit deny does override it. "all" (not an empty selector) is used
// deliberately: an empty `fromEndpoints`/`toEndpoints` selector only covers
// Cilium-managed endpoints inside the cluster, not the "world" entity
// (outside the cluster) -- "all" is documented as covering both ("the
// combination of all known clusters as well [as] world"), matching
// IstioAdapter's full deny-all semantics rather than an intra-cluster-only
// one that would leave external egress/ingress unblocked.
type CiliumAdapter struct {
	client client.Client
}

// NewCiliumAdapter builds a CiliumAdapter from deps.
func NewCiliumAdapter(deps AdapterDeps) *CiliumAdapter {
	return &CiliumAdapter{client: deps.Client}
}

// Quarantine creates (or updates) a deny-all CiliumNetworkPolicy for
// namespace/selector.
func (a *CiliumAdapter) Quarantine(ctx context.Context, namespace string, selector map[string]string) error {
	policy := newCiliumNetworkPolicy(namespace, selector)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(ciliumNetworkPolicyGVK)
	err := a.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: policy.GetName()}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := a.client.Create(ctx, policy); createErr != nil {
			return fmt.Errorf("create quarantine CiliumNetworkPolicy %s/%s: %w", namespace, policy.GetName(), createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get existing CiliumNetworkPolicy %s/%s: %w", namespace, policy.GetName(), err)
	}

	policy.SetResourceVersion(existing.GetResourceVersion())
	if err := a.client.Update(ctx, policy); err != nil {
		return fmt.Errorf("update quarantine CiliumNetworkPolicy %s/%s: %w", namespace, policy.GetName(), err)
	}
	return nil
}

// Release deletes the quarantine CiliumNetworkPolicy for namespace/selector,
// if one exists.
func (a *CiliumAdapter) Release(ctx context.Context, namespace string, selector map[string]string) error {
	policy := newCiliumNetworkPolicy(namespace, selector)

	err := a.client.Delete(ctx, policy)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete quarantine CiliumNetworkPolicy %s/%s: %w", namespace, policy.GetName(), err)
	}
	return nil
}

func newCiliumNetworkPolicy(namespace string, selector map[string]string) *unstructured.Unstructured {
	policy := &unstructured.Unstructured{}
	policy.SetGroupVersionKind(ciliumNetworkPolicyGVK)
	policy.SetNamespace(namespace)
	// Reuses IstioAdapter's naming scheme (quarantineName/quarantineNamePrefix
	// in istio_adapter.go): deterministic regardless of selector map iteration
	// order, and the "sentinel5g-quarantine-" prefix convention is
	// adapter-agnostic, not Istio-specific.
	policy.SetName(quarantineName(selector))
	policy.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "sentinel5g"})

	matchLabels := make(map[string]interface{}, len(selector))
	for k, v := range selector {
		matchLabels[k] = v
	}

	allEntity := []interface{}{"all"}
	_ = unstructured.SetNestedMap(policy.Object, matchLabels, "spec", "endpointSelector", "matchLabels")
	_ = unstructured.SetNestedSlice(policy.Object, []interface{}{
		map[string]interface{}{"fromEntities": allEntity},
	}, "spec", "ingressDeny")
	_ = unstructured.SetNestedSlice(policy.Object, []interface{}{
		map[string]interface{}{"toEntities": allEntity},
	}, "spec", "egressDeny")

	return policy
}
