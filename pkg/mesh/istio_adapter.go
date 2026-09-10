package mesh

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// authorizationPolicyGVK targets Istio's security.istio.io/v1
// AuthorizationPolicy CRD. We talk to it via an unstructured client instead
// of istio.io/client-go to avoid vendoring Istio's full API surface for a
// single, narrow use case.
var authorizationPolicyGVK = schema.GroupVersionKind{
	Group:   "security.istio.io",
	Version: "v1",
	Kind:    "AuthorizationPolicy",
}

// quarantineNamePrefix marks every AuthorizationPolicy Sentinel5G owns, so
// reconciliation never touches policies created by anything else.
const quarantineNamePrefix = "sentinel5g-quarantine-"

// IstioAdapter implements Adapter by creating/deleting a deny-all
// AuthorizationPolicy scoped to the target workload's selector, which Istio's
// sidecars enforce at the mesh (L7) layer.
type IstioAdapter struct {
	client client.Client
}

// NewIstioAdapter builds an IstioAdapter from deps.
func NewIstioAdapter(deps AdapterDeps) *IstioAdapter {
	return &IstioAdapter{client: deps.Client}
}

// Quarantine creates (or updates) a deny-all AuthorizationPolicy for
// namespace/selector. `action: DENY` with a single empty rule (`{}`) matches
// every request to the selected workload, which is exactly the isolation
// behavior we want here; `spec.rules: []` (no rules at all) is rejected by
// Istio's validating webhook as "meaningless" since a DENY with nothing to
// match against never triggers.
func (a *IstioAdapter) Quarantine(ctx context.Context, namespace string, selector map[string]string) error {
	return createOrUpdateUnstructured(ctx, a.client, newAuthorizationPolicy(namespace, selector))
}

// Release deletes the quarantine AuthorizationPolicy for namespace/selector,
// if one exists.
func (a *IstioAdapter) Release(ctx context.Context, namespace string, selector map[string]string) error {
	return deleteUnstructuredIfExists(ctx, a.client, newAuthorizationPolicy(namespace, selector))
}

func newAuthorizationPolicy(namespace string, selector map[string]string) *unstructured.Unstructured {
	policy := &unstructured.Unstructured{}
	policy.SetGroupVersionKind(authorizationPolicyGVK)
	policy.SetNamespace(namespace)
	policy.SetName(quarantineName(selector))
	policy.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "sentinel5g"})

	matchLabels := make(map[string]interface{}, len(selector))
	for k, v := range selector {
		matchLabels[k] = v
	}

	_ = unstructured.SetNestedMap(policy.Object, matchLabels, "spec", "selector", "matchLabels")
	_ = unstructured.SetNestedSlice(policy.Object, []interface{}{map[string]interface{}{}}, "spec", "rules")
	_ = unstructured.SetNestedField(policy.Object, "DENY", "spec", "action")

	return policy
}

// quarantineName derives a deterministic, DNS-1123-safe name from selector so
// repeated Quarantine/Release calls for the same workload always target the
// same object, regardless of Go map iteration order.
func quarantineName(selector map[string]string) string {
	keys := make([]string, 0, len(selector))
	for k := range selector {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(selector[k])
		b.WriteByte(';')
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(b.String()))
	return fmt.Sprintf("%s%08x", quarantineNamePrefix, h.Sum32())
}
