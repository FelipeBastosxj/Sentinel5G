package controller

import (
	"context"
	"net"
	"testing"

	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// recordingBlocklist and recordingMesh are in-memory test doubles standing
// in for pkg/ebpf.BlocklistUpdater and pkg/mesh.Adapter, so applyPolicy's
// decisions can be asserted without a real kernel or Istio control plane.
type recordingBlocklist struct{ blocked []string }

func (r *recordingBlocklist) Block(ip net.IP) error {
	r.blocked = append(r.blocked, ip.String())
	return nil
}
func (r *recordingBlocklist) Unblock(ip net.IP) error { return nil }
func (r *recordingBlocklist) Close() error            { return nil }

type recordingMesh struct{ quarantined []string }

func (r *recordingMesh) Quarantine(_ context.Context, namespace string, _ map[string]string) error {
	r.quarantined = append(r.quarantined, namespace)
	return nil
}
func (r *recordingMesh) Release(_ context.Context, _ string, _ map[string]string) error { return nil }

func newWatcherFixture(t *testing.T, policy *securityv1alpha1.TelecomSecurityPolicy, pod *corev1.Pod) (*ThreatScoreWatcher, *recordingBlocklist, *recordingMesh) {
	t.Helper()
	scheme := newScheme()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy, pod).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	idx := NewPolicyIndex()
	idx.Put(policy)

	blocklist := &recordingBlocklist{}
	meshAdapter := &recordingMesh{}

	w := &ThreatScoreWatcher{
		Client:        fakeClient,
		Log:           testr.New(t),
		Index:         idx,
		Blocklist:     blocklist,
		Mesh:          meshAdapter,
		BaseThreshold: 0.85,
	}
	return w, blocklist, meshAdapter
}

func newTestPolicy(sensitivity securityv1alpha1.Sensitivity, autoMitigate, ebpfBlock, isolatePod bool) *securityv1alpha1.TelecomSecurityPolicy {
	p := &securityv1alpha1.TelecomSecurityPolicy{}
	p.Namespace = "telecom-core"
	p.Name = "protect-amf-core"
	p.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	p.Spec.ThreatDetection = securityv1alpha1.ThreatDetectionSpec{Sensitivity: sensitivity, AutoMitigate: autoMitigate}
	p.Spec.Actions = securityv1alpha1.ActionsSpec{EbpfBlock: ebpfBlock, IsolatePod: isolatePod}
	return p
}

func newTestPod(namespace, name string) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Namespace = namespace
	pod.Name = name
	pod.Labels = map[string]string{"app": "amf-service"}
	return pod
}

func TestApplyPolicy_BelowThresholdOnlyUpdatesScore(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	event := events.ThreatScoreEvent{
		Namespace: policy.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     0.2,
	}

	if err := w.applyPolicy(context.Background(), policy, event); err != nil {
		t.Fatalf("applyPolicy returned error: %v", err)
	}

	if len(blocklist.blocked) != 0 || len(meshAdapter.quarantined) != 0 {
		t.Fatalf("expected no mitigation below threshold, got blocked=%v quarantined=%v", blocklist.blocked, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get after applyPolicy: %v", err)
	}
	if got.Status.ObservedThreatScore != "0.2000" {
		t.Fatalf("expected observed score 0.2000, got %q", got.Status.ObservedThreatScore)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMonitoring {
		t.Fatalf("expected phase Monitoring, got %q", got.Status.Phase)
	}
}

func TestApplyPolicy_AboveThresholdWithoutAutoMitigateOnlyDegrades(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, false, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	event := events.ThreatScoreEvent{
		Namespace: policy.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     0.99,
	}

	if err := w.applyPolicy(context.Background(), policy, event); err != nil {
		t.Fatalf("applyPolicy returned error: %v", err)
	}

	if len(blocklist.blocked) != 0 || len(meshAdapter.quarantined) != 0 {
		t.Fatalf("expected no mitigation when AutoMitigate is false, got blocked=%v quarantined=%v", blocklist.blocked, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get after applyPolicy: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseDegraded {
		t.Fatalf("expected phase Degraded, got %q", got.Status.Phase)
	}
}

func TestApplyPolicy_AboveThresholdWithAutoMitigateBlocksAndQuarantines(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityHigh, true, true, true)
	pod := newTestPod(policy.Namespace, "amf-0")
	w, blocklist, meshAdapter := newWatcherFixture(t, policy, pod)

	event := events.ThreatScoreEvent{
		Namespace: policy.Namespace,
		PodName:   pod.Name,
		SourceIP:  "203.0.113.7",
		Score:     0.9,
	}

	if err := w.applyPolicy(context.Background(), policy, event); err != nil {
		t.Fatalf("applyPolicy returned error: %v", err)
	}

	if len(blocklist.blocked) != 1 || blocklist.blocked[0] != "203.0.113.7" {
		t.Fatalf("expected 203.0.113.7 to be blocked, got %v", blocklist.blocked)
	}
	if len(meshAdapter.quarantined) != 1 || meshAdapter.quarantined[0] != policy.Namespace {
		t.Fatalf("expected namespace %q to be quarantined, got %v", policy.Namespace, meshAdapter.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatalf("get after applyPolicy: %v", err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseMitigating {
		t.Fatalf("expected phase Mitigating, got %q", got.Status.Phase)
	}
	if got.Status.LastMitigationTime == nil {
		t.Fatalf("expected LastMitigationTime to be set")
	}
}
