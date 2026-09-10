package mesh

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// serverGVK targets Linkerd's policy.linkerd.io/v1beta3 Server CRD —
// v1beta3 is the current *storage* version as of this writing; v1beta1
// (the version most third-party docs/examples still show) and v1beta2 are
// both deprecated in Linkerd's own CRD definition in favor of it. Confirmed
// directly from linkerd2's CRD source
// (charts/linkerd-crds/templates/policy/server.yaml), not assumed from
// docs prose — this project has already been burned once (see
// istio_adapter.go's top comment) by trusting policy-semantics assumptions
// that turned out subtly wrong.
var serverGVK = schema.GroupVersionKind{
	Group:   "policy.linkerd.io",
	Version: "v1beta3",
	Kind:    "Server",
}

// quarantineForLabel identifies which quarantineName(selector) hash a
// LinkerdAdapter-managed Server belongs to. Unlike IstioAdapter/
// CiliumAdapter (one object per Quarantine call, found again by its
// deterministic name), LinkerdAdapter creates *one Server per discovered
// port* (see the package doc comment on LinkerdAdapter for why), so Release
// needs to find all of them by label instead of by a single known name.
const quarantineForLabel = "sentinel5g.io/quarantine-for"

// LinkerdAdapter implements Adapter by creating a Server (with no
// accompanying AuthorizationPolicy) for every port Quarantine can discover
// on the target workload's Pods. Linkerd's own CRD schema documents
// Server.spec.accessPolicy's default as "deny": once a Server selects a
// port and no AuthorizationPolicy grants access to it, all traffic to that
// port is denied — this adapter sets accessPolicy explicitly rather than
// relying on the default, since a security control's intent should be
// visible in the object, not merely inherited from whatever the CRD schema
// currently defaults to.
//
// This is a materially weaker guarantee than IstioAdapter/CiliumAdapter,
// and that's a real, load-bearing limitation, not a rounding error: Server
// selects by a specific port (name or number) — there is no wildcard/
// all-ports Server, unlike Istio's AuthorizationPolicy or Cilium's
// ingressDeny/egressDeny, both of which deny a workload's traffic
// regardless of port in one object. LinkerdAdapter compensates by
// discovering every *declared* containerPort across the target Pods and
// creating one Server per port — but a Kubernetes Pod's `ports:` field is
// documentation, not enforcement: a container that listens on a port it
// never declared remains reachable after Quarantine. Prefer IstioAdapter or
// CiliumAdapter when the mesh choice is yours to make; use this one when
// Linkerd is the only mesh available and a best-effort, declared-ports-only
// quarantine is an acceptable trade-off for your workloads.
type LinkerdAdapter struct {
	client client.Client
}

// NewLinkerdAdapter builds a LinkerdAdapter from deps.
func NewLinkerdAdapter(deps AdapterDeps) *LinkerdAdapter {
	return &LinkerdAdapter{client: deps.Client}
}

// Quarantine creates (or updates) one deny-by-default Server per distinct
// containerPort declared across every Pod in namespace currently matching
// selector. Returns an error (rather than silently "succeeding" with zero
// effect) if no ports are discovered — see the package doc comment above
// for why an empty result can't be treated as a successful quarantine.
func (a *LinkerdAdapter) Quarantine(ctx context.Context, namespace string, selector map[string]string) error {
	ports, err := a.discoverPorts(ctx, namespace, selector)
	if err != nil {
		return err
	}
	if len(ports) == 0 {
		return fmt.Errorf("linkerd: no declared container ports found for selector %v in namespace %s -- "+
			"a Server-per-port quarantine has no effect with zero ports; either no Pods currently match, "+
			"or none declare a containerPort (see LinkerdAdapter's doc comment)", selector, namespace)
	}

	hash := quarantineName(selector)
	for _, port := range ports {
		srv := newLinkerdServer(namespace, hash, selector, port)
		if err := createOrUpdateUnstructured(ctx, a.client, srv); err != nil {
			return fmt.Errorf("quarantine port %d: %w", port, err)
		}
	}
	return nil
}

// Release deletes every Server this adapter created for namespace/selector
// — found by label (quarantineForLabel), not by re-discovering the target
// Pods' current ports, since by the time Release runs those Pods may have
// already changed or been deleted entirely, and any such Server would
// otherwise leak permanently.
func (a *LinkerdAdapter) Release(ctx context.Context, namespace string, selector map[string]string) error {
	hash := quarantineName(selector)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: serverGVK.Group, Version: serverGVK.Version, Kind: serverGVK.Kind + "List"})
	if err := a.client.List(ctx, list, client.InNamespace(namespace), client.MatchingLabels{quarantineForLabel: hash}); err != nil {
		return fmt.Errorf("list quarantine Servers for %s/%v: %w", namespace, selector, err)
	}

	for i := range list.Items {
		if err := deleteUnstructuredIfExists(ctx, a.client, &list.Items[i]); err != nil {
			return err
		}
	}
	return nil
}

// discoverPorts lists every Pod in namespace matching selector and returns
// the sorted, deduplicated set of containerPort values declared across
// their (non-init) containers.
func (a *LinkerdAdapter) discoverPorts(ctx context.Context, namespace string, selector map[string]string) ([]int32, error) {
	var pods corev1.PodList
	if err := a.client.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels(selector)); err != nil {
		return nil, fmt.Errorf("list pods for selector %v in %s: %w", selector, namespace, err)
	}

	seen := make(map[int32]struct{})
	for _, pod := range pods.Items {
		for _, c := range pod.Spec.Containers {
			for _, p := range c.Ports {
				seen[p.ContainerPort] = struct{}{}
			}
		}
	}

	ports := make([]int32, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	return ports, nil
}

func newLinkerdServer(namespace, quarantineHash string, selector map[string]string, port int32) *unstructured.Unstructured {
	srv := &unstructured.Unstructured{}
	srv.SetGroupVersionKind(serverGVK)
	srv.SetNamespace(namespace)
	srv.SetName(fmt.Sprintf("%s-%s", quarantineHash, strconv.Itoa(int(port))))
	srv.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "sentinel5g",
		quarantineForLabel:             quarantineHash,
	})

	matchLabels := make(map[string]interface{}, len(selector))
	for k, v := range selector {
		matchLabels[k] = v
	}

	_ = unstructured.SetNestedMap(srv.Object, matchLabels, "spec", "podSelector", "matchLabels")
	_ = unstructured.SetNestedField(srv.Object, int64(port), "spec", "port")
	_ = unstructured.SetNestedField(srv.Object, "deny", "spec", "accessPolicy")

	return srv
}
