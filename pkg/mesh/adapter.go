// Package mesh isolates compromised workloads at the service-mesh layer as
// part of Sentinel5G's closed-loop remediation, independent of the eBPF
// blocklist in pkg/ebpf (which acts at the node/kernel level).
package mesh

import (
	"context"

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

// NewAdapter builds the Adapter named by kind ("istio", "cilium", or
// "noop"), matching the MESH_ADAPTER environment variable documented in
// .env.example.
func NewAdapter(kind string, deps AdapterDeps) (Adapter, error) {
	switch kind {
	case "istio":
		return NewIstioAdapter(deps), nil
	case "cilium":
		return NewCiliumAdapter(deps), nil
	case "noop", "":
		return NoopAdapter{}, nil
	default:
		return nil, unsupportedAdapterError{kind: kind}
	}
}

type unsupportedAdapterError struct{ kind string }

func (e unsupportedAdapterError) Error() string {
	return "mesh: unsupported adapter kind " + e.kind + " (expected \"istio\", \"cilium\", or \"noop\")"
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
