// Package mesh isolates compromised workloads at the service-mesh layer as
// part of Sentinel5G's closed-loop remediation, independent of the eBPF
// blocklist in pkg/ebpf (which acts at the node/kernel level).
package mesh

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AdapterDeps are an Adapter implementation's external dependencies,
// injected so each can be unit-tested against a fake controller-runtime
// client. Shared across every Adapter kind (not just Istio's) since they all
// only need a client.Client today -- a new kind that genuinely needs more
// should extend this struct rather than inventing its own, so NewAdapter's
// signature doesn't have to change per kind.
type AdapterDeps struct {
	Client client.Client
}

// Adapter quarantines or releases a workload identified by a label selector
// within a namespace. Implementations must be idempotent: calling Quarantine
// twice, or Release on a workload that was never quarantined, must not error.
type Adapter interface {
	// Quarantine denies all traffic to/from pods matching selector in namespace.
	Quarantine(ctx context.Context, namespace string, selector map[string]string) error
	// Release removes a previously applied quarantine for selector in namespace.
	Release(ctx context.Context, namespace string, selector map[string]string) error
}

// NewAdapter builds the Adapter named by kind ("istio", "cilium",
// "linkerd", or "noop"), matching the MESH_ADAPTER environment variable
// documented in .env.example.
func NewAdapter(kind string, deps AdapterDeps) (Adapter, error) {
	switch kind {
	case "istio":
		return NewIstioAdapter(deps), nil
	case "cilium":
		return NewCiliumAdapter(deps), nil
	case "linkerd":
		return NewLinkerdAdapter(deps), nil
	case "noop", "":
		return NoopAdapter{}, nil
	default:
		return nil, unsupportedAdapterError{kind: kind}
	}
}

// createOrUpdateUnstructured creates obj if no object with its GVK/
// namespace/name exists yet, or updates the existing one in place otherwise
// (carrying over its ResourceVersion, required for a real API server's
// optimistic-concurrency check to accept the update). This is the shared
// Get-or-Create-or-Update pattern every Adapter's Quarantine uses to stay
// idempotent regardless of how many times it's called for the same target
// (IstioAdapter, CiliumAdapter, LinkerdAdapter all had their own identical
// copy of this before it was factored out here).
func createOrUpdateUnstructured(ctx context.Context, c client.Client, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := c.Create(ctx, obj); createErr != nil {
			return fmt.Errorf("create %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get existing %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	if err := c.Update(ctx, obj); err != nil {
		return fmt.Errorf("update %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
	}
	return nil
}

// deleteUnstructuredIfExists deletes obj, tolerating it already being gone
// -- the shared idempotent-delete pattern every Adapter's Release uses.
func deleteUnstructuredIfExists(ctx context.Context, c client.Client, obj *unstructured.Unstructured) error {
	if err := c.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
	}
	return nil
}

type unsupportedAdapterError struct{ kind string }

func (e unsupportedAdapterError) Error() string {
	return "mesh: unsupported adapter kind " + e.kind + " (expected \"istio\", \"cilium\", \"linkerd\", or \"noop\")"
}

// NoopAdapter never touches the cluster; it exists so MESH_ADAPTER=noop lets
// operators run Sentinel5G in detection-only mode without an Istio dependency.
type NoopAdapter struct{}

func (NoopAdapter) Quarantine(ctx context.Context, namespace string, selector map[string]string) error {
	return nil
}

func (NoopAdapter) Release(ctx context.Context, namespace string, selector map[string]string) error {
	return nil
}

var _ Adapter = NoopAdapter{}
