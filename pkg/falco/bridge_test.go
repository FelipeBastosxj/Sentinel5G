package falco

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Regression guard: Bridge must NOT be leader-gated, matching
// pkg/ingestion.Publisher's identical reasoning -- Falco commonly runs as a
// DaemonSet, and each node's alerts need publishing regardless of which
// replica happens to be elected leader if run under a manager.
func TestBridge_NeedLeaderElection_False(t *testing.T) {
	b := &Bridge{}
	if b.NeedLeaderElection() {
		t.Fatal("expected Bridge.NeedLeaderElection() = false")
	}
}

func TestBridge_FromAlert_MapsFieldsAndClassifiesGTPU(t *testing.T) {
	b := &Bridge{NodeName: "fallback-node", Log: logr.Discard()}
	observedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	alert := Alert{
		Rule:     "Unexpected outbound GTP-U connection",
		Priority: "Warning",
		Time:     observedAt,
		Hostname: "worker-node-1",
		OutputFields: map[string]interface{}{
			"fd.sip":       "10.42.0.7",
			"fd.dip":       "10.42.0.9",
			"fd.dport":     float64(2152),
			"k8s.ns.name":  "telecom-core",
			"k8s.pod.name": "amf-0",
		},
	}

	got := b.FromAlert(alert)

	if got.EventID == "" {
		t.Fatal("expected a non-empty generated eventId")
	}
	if !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("expected observedAt %v, got %v", observedAt, got.ObservedAt)
	}
	if got.NodeName != "worker-node-1" {
		t.Fatalf("expected hostname to take precedence over Bridge.NodeName, got %q", got.NodeName)
	}
	if got.Namespace != "telecom-core" || got.PodName != "amf-0" {
		t.Fatalf("expected resolved namespace/podName, got %q/%q", got.Namespace, got.PodName)
	}
	if got.SourceIP != "10.42.0.7" || got.DestIP != "10.42.0.9" {
		t.Fatalf("expected source/dest IPs to round-trip, got %q/%q", got.SourceIP, got.DestIP)
	}
	if got.DestPort != 2152 {
		t.Fatalf("expected destPort 2152, got %d", got.DestPort)
	}
	if got.Protocol != events.ProtocolGTPU {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolGTPU, got.Protocol)
	}
	if got.PayloadSize != 0 || got.RatePerSecond != 0 || got.Malformed || got.VLANID != 0 {
		t.Fatalf("expected fields with no syscall-level equivalent to stay at zero value, got %+v", got)
	}
}

func TestBridge_FromAlert_ClassifiesSIP(t *testing.T) {
	b := &Bridge{Log: logr.Discard()}
	alert := Alert{
		OutputFields: map[string]interface{}{"fd.lport": "5060"},
	}
	got := b.FromAlert(alert)
	if got.Protocol != events.ProtocolSIP {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolSIP, got.Protocol)
	}
	if got.DestPort != 5060 {
		t.Fatalf("expected destPort 5060, got %d", got.DestPort)
	}
}

func TestBridge_FromAlert_UnknownPortClassifiesUnknown(t *testing.T) {
	b := &Bridge{Log: logr.Discard()}
	alert := Alert{
		OutputFields: map[string]interface{}{"fd.dport": float64(4444)},
	}
	got := b.FromAlert(alert)
	if got.Protocol != events.ProtocolUnknown {
		t.Fatalf("expected protocol %q, got %q", events.ProtocolUnknown, got.Protocol)
	}
}

func TestBridge_FromAlert_MissingHostnameFallsBackToNodeName(t *testing.T) {
	b := &Bridge{NodeName: "fallback-node", Log: logr.Discard()}
	got := b.FromAlert(Alert{})
	if got.NodeName != "fallback-node" {
		t.Fatalf("expected fallback to Bridge.NodeName, got %q", got.NodeName)
	}
}

func TestBridge_FromAlert_ZeroTimeDefaultsToNow(t *testing.T) {
	b := &Bridge{Log: logr.Discard()}
	before := time.Now().UTC()
	got := b.FromAlert(Alert{})
	after := time.Now().UTC()

	if got.ObservedAt.Before(before) || got.ObservedAt.After(after) {
		t.Fatalf("expected observedAt to default to now() when Alert.Time is zero, got %v (window %v-%v)", got.ObservedAt, before, after)
	}
}

func TestBridge_FromAlert_UnattributedAlertStillProducesEvent(t *testing.T) {
	// Mirrors pkg/ingestion.FromSignalingEvent's identical tolerance: an
	// alert with no k8s.* fields (e.g. a host-level Falco rule, not a
	// container-scoped one) must still produce a NormalizedEvent rather
	// than being dropped.
	b := &Bridge{Log: logr.Discard()}
	got := b.FromAlert(Alert{})
	if got.Namespace != "" || got.PodName != "" {
		t.Fatalf("expected empty namespace/podName for an unattributed alert, got %q/%q", got.Namespace, got.PodName)
	}
	if got.EventID == "" {
		t.Fatal("expected a non-empty generated eventId even for an unattributed alert")
	}
}

func TestBridge_Handle_RejectsWrongSharedSecret(t *testing.T) {
	b := &Bridge{SharedSecret: "correct-secret", Log: logr.Discard()}

	req := httptest.NewRequest(http.MethodPost, "/falco", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Sentinel5g-Shared-Secret", "wrong-secret")
	rec := httptest.NewRecorder()

	b.handle(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong shared secret, got %d", rec.Code)
	}
}

func TestBridge_Handle_RejectsMissingSharedSecretWhenRequired(t *testing.T) {
	b := &Bridge{SharedSecret: "correct-secret", Log: logr.Discard()}

	req := httptest.NewRequest(http.MethodPost, "/falco", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()

	b.handle(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when no shared secret header is sent but one is required, got %d", rec.Code)
	}
}

func TestBridge_Handle_RejectsMalformedJSON(t *testing.T) {
	b := &Bridge{Log: logr.Discard()}

	req := httptest.NewRequest(http.MethodPost, "/falco", bytes.NewReader([]byte(`{not-json`)))
	rec := httptest.NewRecorder()

	b.handle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rec.Code)
	}
}

func TestBridge_HealthzServesOKWithoutTouchingNATS(t *testing.T) {
	// Bridge.Bus is deliberately left nil here: /healthz must respond
	// without touching NATS at all (it's a liveness probe for the HTTP
	// server itself, not a NATS-connectivity readiness probe), so this
	// also guards against a future change accidentally routing /healthz
	// through b.Bus and panicking on a nil Bus during startup.
	b := &Bridge{Addr: "127.0.0.1:18099", Log: logr.Discard()}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- b.Start(ctx) }()

	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://" + b.Addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /healthz, got %d", resp.StatusCode)
	}
}

// TestBridge_Handle_PublishesToNATS is a real integration test against a
// live NATS JetStream server (NATS_URL, default nats://localhost:4222),
// following the same convention as pkg/events.TestBus_PublishNormalizedEvent
// (skip rather than fail if no server is reachable — see that test's doc
// comment and .github/workflows/ci.yml for how CI guarantees one runs).
// It exercises the full HTTP POST -> Alert decode -> FromAlert ->
// Bus.PublishNormalizedEvent path end to end through a real httptest server,
// not just FromAlert's pure translation logic.
func TestBridge_Handle_PublishesToNATS(t *testing.T) {
	cfg := events.DefaultConfig()
	cfg.StreamName = "SENTINEL5G_TEST_" + t.Name()
	cfg.EventsSubject = "sentinel5g.test.events." + t.Name()
	cfg.ThreatsSubject = "sentinel5g.test.threats." + t.Name()
	cfg.ConnectTimeout = 2 * time.Second

	bus, err := events.Connect(cfg)
	if err != nil {
		t.Skipf("no reachable NATS server at %s, skipping integration test: %v", cfg.URL, err)
	}
	defer bus.Close()

	b := &Bridge{
		Bus:     bus,
		Subject: cfg.EventsSubject,
		Log:     logr.Discard(),
	}

	srv := httptest.NewServer(http.HandlerFunc(b.handle))
	defer srv.Close()

	body := []byte(`{
		"rule": "Unexpected outbound GTP-U connection",
		"priority": "Warning",
		"hostname": "worker-node-1",
		"output_fields": {"fd.sip": "10.42.0.7", "fd.dip": "10.42.0.9", "fd.dport": 2152}
	}`)

	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /falco: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from a valid Falco alert POST, got %d", resp.StatusCode)
	}
}
