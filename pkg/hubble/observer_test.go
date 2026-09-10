package hubble

import (
	"context"
	"net"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Regression guard: Observer must be leader-gated, the opposite of
// pkg/ingestion.Publisher and pkg/falco.Bridge -- see NeedLeaderElection's
// doc comment for why (Hubble Relay already aggregates every node's flows
// into one stream, so more than one live Observer would double-publish).
func TestObserver_NeedLeaderElection_True(t *testing.T) {
	o := &Observer{}
	if !o.NeedLeaderElection() {
		t.Fatal("expected Observer.NeedLeaderElection() = true")
	}
}

// fakeObserverServer implements observerpb.ObserverServer against a fixed,
// pre-built slice of responses -- a real gRPC server, not a mock of
// Observer's own code, so the test below exercises the actual generated
// client/server wire path (dial, stream, Recv loop, GetFlowsResponse's
// oneof, stream close) rather than only FromFlow's pure mapping logic
// (already covered by flow_test.go).
type fakeObserverServer struct {
	observerpb.UnimplementedObserverServer
	responses []*observerpb.GetFlowsResponse
}

func (s *fakeObserverServer) GetFlows(_ *observerpb.GetFlowsRequest, stream grpc.ServerStreamingServer[observerpb.GetFlowsResponse]) error {
	for _, resp := range s.responses {
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
	return nil
}

// TestObserver_StreamFlows_PublishesMatchingFlowsOverBufconn is a real
// integration test: an in-process gRPC server (google.golang.org/grpc/test/
// bufconn, not a real network listener) serves hand-built but
// schema-accurate flow.Flow messages through the actual generated
// observerpb.ObserverClient, and Observer.Start's real Recv loop consumes
// them and publishes matching ones to a live NATS JetStream server (skip
// rather than fail if none is reachable -- same convention as
// pkg/events.TestBus_PublishNormalizedEvent and
// pkg/falco.TestBridge_Handle_PublishesToNATS).
//
// This is the verification pkg/doc.go's top comment refers to: a live
// Hubble/Cilium deployment was not available (see that comment and
// docs/integrations.md), so this proves the gRPC wiring and message
// handling for real instead, against hand-built protobuf messages that
// match the real upstream schema (verified by reading
// github.com/cilium/cilium/api/v1/flow's actual generated Go types, not
// assumed).
func TestObserver_StreamFlows_PublishesMatchingFlowsOverBufconn(t *testing.T) {
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

	rawConn, err := nats.Connect(cfg.URL)
	if err != nil {
		t.Fatalf("raw nats connect for verification subscriber: %v", err)
	}
	defer rawConn.Close()

	received := make(chan *nats.Msg, 4)
	sub, err := rawConn.ChanSubscribe(cfg.EventsSubject, received)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	responses := []*observerpb.GetFlowsResponse{
		{
			ResponseTypes: &observerpb.GetFlowsResponse_Flow{
				Flow: &flowpb.Flow{
					NodeName: "worker-node-1",
					IP:       &flowpb.IP{Source: "10.42.0.7", Destination: "10.42.0.9"},
					L4:       &flowpb.Layer4{Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{DestinationPort: 2152}}},
					Source:   &flowpb.Endpoint{Namespace: "telecom-core", PodName: "amf-0"},
				},
			},
		},
		// A non-signaling flow: must NOT be published (FromFlow's own
		// filtering, exercised here through the real stream, not a direct
		// call).
		{
			ResponseTypes: &observerpb.GetFlowsResponse_Flow{
				Flow: &flowpb.Flow{
					IP: &flowpb.IP{Source: "10.42.0.7", Destination: "10.42.0.9"},
					L4: &flowpb.Layer4{Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{DestinationPort: 4444}}},
				},
			},
		},
		// A NodeStatus payload instead of a Flow: must be skipped, not
		// crash resp.GetFlow()'s nil-oneof handling.
		{ResponseTypes: &observerpb.GetFlowsResponse_NodeStatus{}},
	}

	srv := grpc.NewServer()
	observerpb.RegisterObserverServer(srv, &fakeObserverServer{responses: responses})

	listener := bufconn.Listen(1024 * 1024)
	go func() { _ = srv.Serve(listener) }()
	defer srv.Stop()

	dialer := func(context.Context, string) (net.Conn, error) { return listener.Dial() }

	o := &Observer{
		Addr:          "passthrough:///bufnet",
		Bus:           events.NewConnectedConnector(bus),
		Subject:       cfg.EventsSubject,
		Log:           logr.Discard(),
		extraDialOpts: []grpc.DialOption{grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- o.Start(ctx) }()

	select {
	case msg := <-received:
		if len(msg.Data) == 0 {
			t.Fatal("expected a non-empty NormalizedEvent payload")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for the matching flow to be published")
	}

	// Cancel immediately, before Observer's own retry-with-backoff loop can
	// re-run the (fully-consumed) fake stream and republish the same
	// matching flow a second time -- that would be Start's own designed
	// retry behavior working as intended, not a bug in what's being
	// asserted next, but it would make the "exactly one" check below racy
	// against the 1s initial backoff.
	cancel()
	<-done

	select {
	case extra := <-received:
		t.Fatalf("expected only the one matching flow to be published, got an extra message: %s", extra.Data)
	default:
	}
}
