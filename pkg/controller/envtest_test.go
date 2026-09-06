package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// startEnvtest boots a real kube-apiserver + etcd (via KUBEBUILDER_ASSETS,
// see setup-envtest: sigs.k8s.io/controller-runtime/tools/setup-envtest)
// with our hand-written CRD installed, and returns a real client.Client
// talking to it. Unlike pkg/controller's other tests, this is not a fake
// client — every Create/Get/Update below is a real HTTP round trip to a
// real API server backed by a real etcd, so it also validates that the
// controller-gen-generated CRD YAML (config/crd/bases, `make manifests`)
// and the Go types' JSON serialization actually agree with each other.
func startEnvtest(t *testing.T) client.Client {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping real-API-server test (see: go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use)")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest environment: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := testEnv.Stop(); stopErr != nil {
			t.Logf("stop envtest environment: %v", stopErr)
		}
	})

	c, err := client.New(cfg, client.Options{Scheme: newScheme()})
	if err != nil {
		t.Fatalf("build client for real API server: %v", err)
	}
	return c
}

// TestReconciler_RealAPIServer_TransitionsPendingToMonitoring exercises the
// exact same Reconciler as the fake-client tests, but against a real
// kube-apiserver + etcd: this is the test that would have caught a
// hand-written CRD schema drifting from the Go types (e.g. a field the API
// server rejects or silently drops), which a fake client can never catch
// since it does no schema validation at all.
func TestReconciler_RealAPIServer_TransitionsPendingToMonitoring(t *testing.T) {
	c := startEnvtest(t)
	ctx := context.Background()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "telecom-core"}}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	policy := &securityv1alpha1.TelecomSecurityPolicy{}
	policy.Namespace = "telecom-core"
	policy.Name = "protect-amf-core"
	policy.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	policy.Spec.ThreatDetection = securityv1alpha1.ThreatDetectionSpec{
		Sensitivity:  securityv1alpha1.SensitivityHigh,
		AutoMitigate: true,
	}
	policy.Spec.Actions = securityv1alpha1.ActionsSpec{EbpfBlock: true, IsolatePod: true}

	if err := c.Create(ctx, policy); err != nil {
		t.Fatalf("create policy on real API server: %v", err)
	}

	r := &Reconciler{Client: c, Log: testr.New(t), Index: NewPolicyIndex()}
	req := reconcileRequest(policy)

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile against real API server: %v", err)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := c.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("expected phase Monitoring on real API server, got %q", got.Status.Phase)
	}
}

// TestClosedLoop_RealAPIServer_RealNATS_MitigatesOnHighThreatScore is the
// most realistic test in this repository: a real kube-apiserver + etcd
// (envtest) and a real NATS JetStream server (NATS_URL, expected already
// running — see docs/getting-started.md) are wired together exactly as
// cmd/operator would, and a real ThreatScoreEvent is published and consumed
// over the real wire. Only the two things this Windows dev machine
// fundamentally cannot do are stood in for: attaching an XDP program
// (Linux-only, see pkg/ebpf) and an Istio control plane (mesh.NoopAdapter),
// both exercised as recording test doubles so the mitigation decision
// itself is still asserted for real.
func TestClosedLoop_RealAPIServer_RealNATS_MitigatesOnHighThreatScore(t *testing.T) {
	c := startEnvtest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Every name below is suffixed with a fresh run ID: the NATS server this
	// test runs against typically uses durable file storage (see
	// docs/getting-started.md), so a fixed stream/subject name would let an
	// unacked message from a *previous* run of this same test get
	// redelivered alongside the new one — which is exactly what happened
	// the first time this test was written (see the at-least-once note
	// below for why that's a test-isolation bug, not a product bug).
	runID := time.Now().UnixNano()
	cfg := events.DefaultConfig()
	cfg.StreamName = fmt.Sprintf("SENTINEL5G_TEST_CLOSEDLOOP_%d", runID)
	cfg.EventsSubject = fmt.Sprintf("sentinel5g.test.closedloop.events.%d", runID)
	cfg.ThreatsSubject = fmt.Sprintf("sentinel5g.test.closedloop.threats.%d", runID)
	cfg.ConnectTimeout = 2 * time.Second

	bus, err := events.Connect(cfg)
	if err != nil {
		t.Skipf("no reachable NATS server at %s, skipping closed-loop test: %v", cfg.URL, err)
	}
	defer bus.Close()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "telecom-core-closedloop"}}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "amf-0",
			Labels:    map[string]string{"app": "amf-service"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "amf", Image: "example.invalid/amf:latest"}},
		},
	}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	policy := &securityv1alpha1.TelecomSecurityPolicy{}
	policy.Namespace = ns.Name
	policy.Name = "protect-amf-core"
	policy.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	policy.Spec.ThreatDetection = securityv1alpha1.ThreatDetectionSpec{
		Sensitivity:  securityv1alpha1.SensitivityHigh,
		AutoMitigate: true,
	}
	policy.Spec.Actions = securityv1alpha1.ActionsSpec{EbpfBlock: true, IsolatePod: true}
	if err := c.Create(ctx, policy); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	index := NewPolicyIndex()
	index.Put(policy)

	blocklist := &recordingBlocklist{}
	meshAdapter := &recordingMesh{}

	watcher := &ThreatScoreWatcher{
		Client:        c,
		Log:           testr.New(t),
		Index:         index,
		Bus:           bus,
		Subject:       cfg.ThreatsSubject,
		Blocklist:     blocklist,
		Mesh:          meshAdapter,
		BaseThreshold: 0.85,
	}

	watcherDone := make(chan error, 1)
	go func() { watcherDone <- watcher.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-watcherDone
	})

	// Give the subscription a moment to actually register before publishing.
	time.Sleep(200 * time.Millisecond)

	if err := bus.PublishThreatScore(cfg.ThreatsSubject, events.ThreatScoreEvent{
		SourceEventID: "evt-real-1",
		Namespace:     ns.Name,
		PodName:       pod.Name,
		SourceIP:      "203.0.113.7",
		Score:         0.93,
		Model:         "autoencoder-v1",
		DetectedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("publish real threat score event: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var got securityv1alpha1.TelecomSecurityPolicy
		if err := c.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}, &got); err != nil {
			t.Fatalf("get policy: %v", err)
		}
		if got.Status.Phase == securityv1alpha1.PolicyPhaseMitigating {
			// NATS JetStream guarantees at-least-once delivery, not
			// exactly-once: a slow handler, a redelivery race on shutdown,
			// or a broker retry can all cause this callback to fire more
			// than once for the same event. The mitigation actions
			// (pkg/ebpf.Loader.Block, pkg/mesh.IstioAdapter.Quarantine) are
			// deliberately idempotent for exactly this reason, so we assert
			// "at least once, always the right value" — not "exactly one".
			if len(blocklist.blocked) < 1 {
				t.Fatalf("expected 203.0.113.7 to be blocked at least once, got %v", blocklist.blocked)
			}
			for _, ip := range blocklist.blocked {
				if ip != "203.0.113.7" {
					t.Fatalf("expected every blocked IP to be 203.0.113.7, got %v", blocklist.blocked)
				}
			}
			if len(meshAdapter.quarantined) < 1 {
				t.Fatalf("expected at least one quarantine call, got %v", meshAdapter.quarantined)
			}
			for _, nsName := range meshAdapter.quarantined {
				if nsName != policy.Namespace {
					t.Fatalf("expected every quarantine call to target %q, got %v", policy.Namespace, meshAdapter.quarantined)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("timed out waiting for real ThreatScoreEvent, over real NATS, to reflect Mitigating status on the real API server")
}

func reconcileRequest(policy *securityv1alpha1.TelecomSecurityPolicy) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}}
}
