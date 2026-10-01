package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr/testr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Fault-injection tests for ROADMAP.md Phase 4's "no chaos testing" item:
// the apiserver unavailable when a status write is due, an action failing
// after status has recorded it, and a node rebooting with active blocks.
// Each asserts the convergence story the comments claim, rather than
// trusting it.

// The fault that justified write-ahead ordering. The apiserver is
// unavailable exactly when the status write is due. Because status is
// recorded BEFORE the kernel is touched, a failed write must mean NOTHING
// was blocked -- otherwise the BlocklistReconciler (status = desired) would
// reconcile the orphaned kernel entry away and silently undo the mitigation.
func TestChaos_StatusWriteFailsBeforeAnythingIsBlocked(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	pod := newTestPod(policy.Namespace, "upf-0")

	failStatus := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy, pod).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
				return fmt.Errorf("apiserver unavailable")
			},
		}).
		Build()

	idx := NewPolicyIndex()
	idx.Put(policy)
	blocklist := &recordingBlocklist{}
	w := &ThreatScoreWatcher{Client: failStatus, Log: testr.New(t), Index: idx, Blocklist: blocklist, Mesh: &recordingMesh{}, BaseThreshold: 0.85}

	err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		TEID: 0x4D84, Score: 1.0, Model: "rule:x",
	})
	if err == nil {
		t.Fatal("expected applyPolicy to surface the status-write failure")
	}
	// The crux: nothing in the kernel. Status could not record the intent,
	// so the intent was not carried out -- there is nothing for the
	// reconciler to later find orphaned and remove.
	if len(blocklist.blockedTunnels) != 0 || len(blocklist.blocked) != 0 {
		t.Fatalf("the kernel was modified despite the status write failing: tunnels=%v ips=%v -- "+
			"the reconciler would reconcile this orphan away and undo the mitigation",
			blocklist.blockedTunnels, blocklist.blocked)
	}
}

// The complementary fault: the status write SUCCEEDS (intent recorded) but
// the kernel action then fails. Status claims the block, so the
// BlocklistReconciler must re-apply it rather than leave the policy claiming
// a block that isn't in force. This ties the watcher's write-ahead to the
// reconciler's re-apply into one convergence proof.
func TestChaos_ActionFailsAfterStatusRecordsIt_ReconcilerReapplies(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, false, false)
	policy.Spec.Actions.EbpfBlockTunnel = true
	pod := newTestPod(policy.Namespace, "upf-0")

	apiClient := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy, pod).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()
	idx := NewPolicyIndex()
	idx.Put(policy)

	// A kernel whose BlockTunnel fails: the action-failure fault.
	kernel := newFakeKernel()
	kernel.blockErr = fmt.Errorf("transient kernel error")
	w := &ThreatScoreWatcher{Client: apiClient, Log: testr.New(t), Index: idx, Blocklist: kernel, Mesh: &recordingMesh{}, BaseThreshold: 0.85}

	err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		TEID: 0x4D84, Score: 1.0, Model: "rule:x",
	})
	if err == nil {
		t.Fatal("expected the kernel action failure to surface")
	}

	// Status recorded the intent despite the action failing.
	var got securityv1alpha1.TelecomSecurityPolicy
	if err := apiClient.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Status.BlockedTunnels) != 1 {
		t.Fatalf("status did not record the intended block: %v", got.Status.BlockedTunnels)
	}

	// Now the kernel recovers, and the reconciler re-applies from status.
	kernel.blockErr = nil
	recon := &BlocklistReconciler{Client: apiClient, Log: testr.New(t), Blocklist: kernel, Inspector: kernel}
	if _, err := recon.Sync(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(kernel.tunnels) != 1 {
		t.Fatalf("the reconciler did not re-apply the block status claimed: %v", kernel.tunnels)
	}
}

// A node rebooting with active blocks: the kernel comes back empty while
// status still asserts the blocks. This is the two halves -- the privileged
// pin test proves the maps survive a loader restart; this proves the
// userspace path re-applies when they did NOT (unpinned, or a pin reset).
func TestChaos_NodeRebootWithActiveBlocks_Reapplied(t *testing.T) {
	policy := mitigatingPolicy("gtpu-guard", []string{"203.0.113.7"}, []string{"10.0.0.1/0x4d84"})
	apiClient := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(policy).
		WithStatusSubresource(&securityv1alpha1.TelecomSecurityPolicy{}).
		Build()

	// Fresh kernel = post-reboot empty maps.
	kernel := newFakeKernel()
	recon := &BlocklistReconciler{Client: apiClient, Log: testr.New(t), Blocklist: kernel, Inspector: kernel}

	if _, err := recon.Sync(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(kernel.ips) != 1 || len(kernel.tunnels) != 1 {
		t.Fatalf("post-reboot re-apply incomplete: ips=%v tunnels=%v", kernel.ips, kernel.tunnels)
	}
}

// NATS dropping does not affect an in-flight mitigation: the mitigation path
// (kernel block + apiserver status) does not touch the bus, so a bus that is
// disconnected mid-handle completes the mitigation and only the ACK is
// deferred (JetStream redelivers). Modelled by driving applyPolicy with no
// usable bus wired at all -- it must still block and record.
func TestChaos_BusDisconnectDoesNotBlockMitigation(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, false)
	pod := newTestPod(policy.Namespace, "upf-0")
	w, blocklist, _ := newWatcherFixture(t, policy, pod) // Bus is nil in the fixture

	if err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		Score: 1.0, Model: "rule:x",
	}); err != nil {
		t.Fatalf("applyPolicy with no bus: %v", err)
	}
	if len(blocklist.blocked) != 1 {
		t.Fatal("the mitigation did not complete while the bus was unavailable")
	}
}
