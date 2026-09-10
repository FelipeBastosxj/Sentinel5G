package falco

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Bridge is Falco's Layer 1 -> Layer 2 bridge: an HTTP server Falco's own
// http_output posts JSON alerts to (configure Falco with
// `http_output.enabled: true` and `http_output.url` pointed at this
// Bridge's /falco endpoint), translated into events.NormalizedEvent and
// published to NATS. This is Falco's syscall-level counterpart to
// pkg/ingestion.Publisher (the eBPF-sourced bridge) — the two are
// independent and not mutually exclusive: a cluster can run both,
// publishing onto the same subject, since NormalizedEvent exists
// specifically so Layer 3 doesn't need to know which Layer 1 source
// produced an event (see docs/architecture.md).
//
// Falco's http_output posts unauthenticated by default. Set SharedSecret
// (and configure Falco's own http_output.headers with a matching
// X-Sentinel5g-Shared-Secret value) if this Bridge is reachable by
// anything other than Falco itself -- the same "an unauthenticated ingress
// is a real risk" posture this project already documents for the NATS bus
// (see docs/integrations.md): anything that can POST here can forge a
// NormalizedEvent.
type Bridge struct {
	Addr         string
	Bus          *events.Bus
	Subject      string
	NodeName     string
	SharedSecret string
	Log          logr.Logger

	server *http.Server
}

// NeedLeaderElection implements manager.LeaderElectionRunnable: Falco
// commonly runs as a DaemonSet (one per node), and each node's alerts need
// publishing regardless of which replica of this Bridge happens to be
// elected leader if run under a controller-runtime manager -- the same
// reasoning pkg/ingestion.Publisher documents for the identical override.
func (b *Bridge) NeedLeaderElection() bool { return false }

// Start implements manager.Runnable: serves POST /falco until ctx is done.
func (b *Bridge) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/falco", b.handle)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	b.server = &http.Server{
		Addr:              b.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := b.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return b.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return fmt.Errorf("falco bridge http server: %w", err)
	}
}

func (b *Bridge) handle(w http.ResponseWriter, r *http.Request) {
	if b.SharedSecret != "" && r.Header.Get("X-Sentinel5g-Shared-Secret") != b.SharedSecret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var alert Alert
	if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
		b.Log.Info("dropping malformed Falco alert", "error", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	event := b.FromAlert(alert)
	if err := b.Bus.PublishNormalizedEvent(b.Subject, event); err != nil {
		b.Log.Error(err, "failed to publish NormalizedEvent from Falco alert")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// FromAlert converts a single Falco alert into a NormalizedEvent. Exported
// (unlike an unexported helper) specifically so it's directly unit-testable
// against constructed Alert values without needing a real HTTP round trip
// — mirrors pkg/ingestion.FromSignalingEvent's role for the eBPF path.
//
// Fields Falco's syscall-level view has no equivalent of are left at their
// zero value, not guessed at: PayloadSize (Falco doesn't expose payload
// bytes for a network syscall event), RatePerSecond (Falco emits one alert
// per triggering event, not a windowed rate), Malformed (not a concept
// syscall tracing carries — that's specific to the eBPF layer's own
// protocol-framing validation), and VLANID (a Layer 2 frame detail
// invisible to a syscall tracer).
func (b *Bridge) FromAlert(alert Alert) events.NormalizedEvent {
	fields := alert.OutputFields

	nodeName := alert.Hostname
	if nodeName == "" {
		nodeName = b.NodeName
	}

	observedAt := alert.Time
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	destPort := fieldPort(fields, "fd.dport", "fd.lport")

	return events.NormalizedEvent{
		EventID:    uuid.NewString(),
		ObservedAt: observedAt,
		Namespace:  fieldString(fields, "k8s.ns.name"),
		PodName:    fieldString(fields, "k8s.pod.name"),
		NodeName:   nodeName,
		SourceIP:   fieldString(fields, "fd.sip", "fd.rip"),
		DestIP:     fieldString(fields, "fd.dip", "fd.lip"),
		DestPort:   destPort,
		Protocol:   protocolForPort(destPort),
	}
}

// protocolForPort classifies by the same two telecom signaling ports
// bpf/packet_filter.c's is_signaling_port() recognizes -- a Falco rule
// alerting on GTP-U/SIP traffic reported through fd.dport/fd.lport gets the
// same Protocol classification the eBPF path would have given it, so
// downstream (the AI engine's feature extraction) doesn't need to
// special-case which Layer 1 source produced the event.
func protocolForPort(port uint16) events.Protocol {
	switch port {
	case 2152:
		return events.ProtocolGTPU
	case 5060:
		return events.ProtocolSIP
	default:
		return events.ProtocolUnknown
	}
}
