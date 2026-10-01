package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

var ksKey = types.NamespacedName{Namespace: "sentinel5g-system", Name: "sentinel5g-killswitch"}

func killSwitchConfigMap(engaged string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: ksKey.Namespace, Name: ksKey.Name},
		Data:       map[string]string{"engaged": engaged},
	}
}

func newKillSwitch(t *testing.T, objs ...client.Object) *KillSwitch {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(newScheme())
	if len(objs) > 0 {
		b = b.WithObjects(objs...)
	}
	return &KillSwitch{Reader: b.Build(), Key: ksKey, TTL: time.Minute}
}

// A nil switch (none configured) and an absent ConfigMap both mean
// disengaged -- the unarmed state. Protection must not depend on a
// ConfigMap existing.
func TestKillSwitch_NilAndAbsentAreDisengaged(t *testing.T) {
	var nilSwitch *KillSwitch
	if nilSwitch.Engaged(context.Background()) {
		t.Fatal("a nil kill switch reported engaged")
	}
	if newKillSwitch(t).Engaged(context.Background()) {
		t.Fatal("an absent ConfigMap reported engaged")
	}
}

func TestKillSwitch_EngagedOnlyWhenExplicitlyTrue(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"false", false},
		{"", false},
		{"TRUE", false}, // exactly "true", not a loose parse -- a typo must not arm or disarm it by accident
		{"1", false},
	} {
		ks := newKillSwitch(t, killSwitchConfigMap(tc.value))
		if got := ks.Engaged(context.Background()); got != tc.want {
			t.Errorf("engaged=%q -> %v, want %v", tc.value, got, tc.want)
		}
	}
}

// countingReader wraps a Reader and counts Get calls, to prove the TTL
// actually suppresses reads rather than hitting the API on every score.
type countingReader struct {
	client.Reader
	gets int
}

func (c *countingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets++
	return c.Reader.Get(ctx, key, obj, opts...)
}

func TestKillSwitch_CachesWithinTTL(t *testing.T) {
	inner := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(killSwitchConfigMap("true")).Build()
	counter := &countingReader{Reader: inner}

	now := time.Unix(0, 0)
	ks := &KillSwitch{Reader: counter, Key: ksKey, TTL: 2 * time.Second, Now: func() time.Time { return now }}

	for i := 0; i < 5; i++ {
		if !ks.Engaged(context.Background()) {
			t.Fatal("expected engaged")
		}
	}
	if counter.gets != 1 {
		t.Fatalf("made %d reads within the TTL, want 1 (a score storm would hammer the API)", counter.gets)
	}

	now = now.Add(3 * time.Second) // past the TTL
	ks.Engaged(context.Background())
	if counter.gets != 2 {
		t.Fatalf("made %d reads after the TTL elapsed, want 2", counter.gets)
	}
}

// erroringReader returns a non-NotFound error, the API-flap case.
type erroringReader struct{}

func (erroringReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return fmt.Errorf("apiserver unavailable")
}
func (erroringReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("apiserver unavailable")
}

// A transient read error must NOT flip the switch: a deliberately engaged
// switch has to survive an API flap, and an unarmed one must not silently
// arm. The last known value is kept; with no prior read, default disengaged
// (fail toward protection staying on).
func TestKillSwitch_TransientErrorKeepsLastValue(t *testing.T) {
	now := time.Unix(0, 0)
	inner := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(killSwitchConfigMap("true")).Build()
	counter := &countingReader{Reader: inner}
	ks := &KillSwitch{Reader: counter, Key: ksKey, TTL: time.Second, Now: func() time.Time { return now }}

	if !ks.Engaged(context.Background()) {
		t.Fatal("expected engaged on the first read")
	}
	// API starts failing; past the TTL so a re-read is attempted.
	ks.Reader = erroringReader{}
	now = now.Add(2 * time.Second)
	if !ks.Engaged(context.Background()) {
		t.Fatal("a transient error dropped a deliberately engaged switch")
	}

	// A switch that has NEVER read successfully defaults disengaged.
	fresh := &KillSwitch{Reader: erroringReader{}, Key: ksKey, TTL: time.Second}
	if fresh.Engaged(context.Background()) {
		t.Fatal("a never-read switch defaulted to engaged; that would disable protection on a startup API error")
	}
}

// The behaviour that matters: when engaged, a fully-armed policy
// (autoMitigate + ebpfBlock) takes NO action, still counts the crossing as
// Alerting, and increments the suppressed counter.
func TestApplyPolicy_KillSwitchSuppressesEveryAction(t *testing.T) {
	MitigationsSuppressed.Reset()

	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, true)
	policy.Spec.Actions.EbpfBlockTunnel = true
	pod := newTestPod(policy.Namespace, "upf-0")
	w, blocklist, mesh := newWatcherFixture(t, policy, pod)
	w.KillSwitch = newKillSwitch(t, killSwitchConfigMap("true"))

	err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		TEID: 0x4D84, Score: 1.0, Model: "rule:gtpu-tunnel-flood",
	})
	if err != nil {
		t.Fatalf("applyPolicy: %v", err)
	}

	if len(blocklist.blocked)+len(blocklist.blockedTunnels)+len(mesh.quarantined) != 0 {
		t.Fatalf("the kill switch did not suppress actions: block=%v tunnels=%v quarantine=%v",
			blocklist.blocked, blocklist.blockedTunnels, mesh.quarantined)
	}

	var got securityv1alpha1.TelecomSecurityPolicy
	if err := w.Get(context.Background(), nnFor(policy), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != securityv1alpha1.PolicyPhaseAlerting {
		t.Fatalf("phase = %q, want Alerting (detection continues under the kill switch)", got.Status.Phase)
	}
	// Three actions were armed; all three suppressed.
	for _, action := range []string{"ebpf_block_tunnel", "ebpf_block", "mesh_quarantine"} {
		if got := testutil.ToFloat64(MitigationsSuppressed.WithLabelValues(action)); got != 1 {
			t.Errorf("suppressed{%s} = %v, want 1", action, got)
		}
	}
}

// And the control: with the switch disengaged, the same policy acts.
func TestApplyPolicy_DisengagedKillSwitchDoesNotInterfere(t *testing.T) {
	policy := newTestPolicy(securityv1alpha1.SensitivityMedium, true, true, false)
	pod := newTestPod(policy.Namespace, "upf-0")
	w, blocklist, _ := newWatcherFixture(t, policy, pod)
	w.KillSwitch = newKillSwitch(t, killSwitchConfigMap("false"))

	if err := w.applyPolicy(context.Background(), policy, events.ThreatScoreEvent{
		Namespace: policy.Namespace, PodName: pod.Name, SourceIP: "10.0.0.1",
		Score: 1.0, Model: "rule:x",
	}); err != nil {
		t.Fatalf("applyPolicy: %v", err)
	}
	if len(blocklist.blocked) != 1 {
		t.Fatalf("a disengaged kill switch suppressed a real block: %v", blocklist.blocked)
	}
}
