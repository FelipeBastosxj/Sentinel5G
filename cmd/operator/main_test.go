package main

import (
	"testing"

	"github.com/go-logr/logr/testr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/mesh"
)

var authorizationPolicyGVK = schema.GroupVersionKind{
	Group:   "security.istio.io",
	Version: "v1",
	Kind:    "AuthorizationPolicy",
}

// TestBuildMeshAdapter_IstioFallsBackToNoopWithoutTheCRD is a regression
// guard for a real bug found by running the quickstart flow against a real
// kind cluster with no Istio installed: MESH_ADAPTER=istio (the default)
// used to build a real IstioAdapter unconditionally, which then failed
// every Quarantine call with "no matches for kind AuthorizationPolicy" --
// and because ThreatScoreWatcher.applyPolicy aborts on the first action
// error, that silently kept Status.Phase from ever reaching Mitigating.
func TestBuildMeshAdapter_IstioFallsBackToNoopWithoutTheCRD(t *testing.T) {
	restMapper := meta.NewDefaultRESTMapper(nil) // no GVKs registered -- CRD doesn't exist
	log := testr.New(t)

	adapter := buildMeshAdapter(log, "istio", mesh.IstioAdapterDeps{}, restMapper)

	if _, ok := adapter.(mesh.NoopAdapter); !ok {
		t.Fatalf("expected a NoopAdapter fallback when the AuthorizationPolicy CRD isn't registered, got %T", adapter)
	}
}

func TestBuildMeshAdapter_IstioUsesRealAdapterWhenCRDExists(t *testing.T) {
	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{authorizationPolicyGVK.GroupVersion()})
	restMapper.Add(authorizationPolicyGVK, meta.RESTScopeNamespace)
	log := testr.New(t)

	adapter := buildMeshAdapter(log, "istio", mesh.IstioAdapterDeps{}, restMapper)

	if _, ok := adapter.(*mesh.IstioAdapter); !ok {
		t.Fatalf("expected a real *mesh.IstioAdapter when the AuthorizationPolicy CRD is registered, got %T", adapter)
	}
}

func TestBuildMeshAdapter_NoopKindNeverConsultsTheRESTMapper(t *testing.T) {
	// A nil RESTMapper would panic if buildMeshAdapter tried to use it --
	// this asserts the istio-specific check is skipped entirely for any
	// other configured kind.
	log := testr.New(t)

	adapter := buildMeshAdapter(log, "noop", mesh.IstioAdapterDeps{}, nil)

	if _, ok := adapter.(mesh.NoopAdapter); !ok {
		t.Fatalf("expected NoopAdapter for kind=noop, got %T", adapter)
	}
}
