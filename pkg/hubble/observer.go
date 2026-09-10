package hubble

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"time"

	observerpb "github.com/cilium/cilium/api/v1/observer"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
)

// Observer streams flows from a Hubble Observer gRPC endpoint (typically
// Hubble Relay, which already aggregates every node's own Hubble agent into
// one stream) and publishes the matching ones (see FromFlow) as
// events.NormalizedEvent.
type Observer struct {
	// Addr is the Hubble Observer gRPC endpoint, e.g.
	// "hubble-relay.kube-system.svc.cluster.local:80".
	Addr string
	// TLSConfig enables a TLS connection when set (Hubble Relay is commonly
	// deployed with mTLS -- see
	// https://docs.cilium.io/en/stable/observability/hubble/configuration/#tls-configuration);
	// nil means a plaintext connection, matching this package's local-dev
	// default the same way events.Config's own auth/TLS fields default to
	// unauthenticated.
	TLSConfig *tls.Config

	Bus     *events.Bus
	Subject string
	Log     logr.Logger

	// extraDialOpts is appended after the transport credentials this
	// package sets itself -- nil in production, set only by tests to
	// connect to an in-process bufconn listener instead of a real network
	// address.
	extraDialOpts []grpc.DialOption
}

// NeedLeaderElection implements manager.LeaderElectionRunnable. Unlike
// pkg/ingestion.Publisher and pkg/falco.Bridge (both of which must run on
// every replica -- eBPF only sees its own node's traffic, and Falco
// commonly runs one instance per node), Observer's source is Hubble Relay,
// which has already aggregated every node's flows into a single stream:
// running more than one Observer against the same Relay would double-
// publish the same flow. true here means a manager only runs this on the
// elected leader; deploying it as a plain single-replica Deployment (no
// manager, no leader election) is equally correct and simpler.
func (o *Observer) NeedLeaderElection() bool { return true }

// Start dials the Observer gRPC endpoint and streams flows until ctx is
// done, retrying the stream with backoff on any transient error rather than
// exiting outright -- a single dropped connection to Hubble Relay shouldn't
// crash-loop this component.
func (o *Observer) Start(ctx context.Context) error {
	creds := insecure.NewCredentials()
	if o.TLSConfig != nil {
		creds = credentials.NewTLS(o.TLSConfig)
	}

	dialOpts := append([]grpc.DialOption{grpc.WithTransportCredentials(creds)}, o.extraDialOpts...)
	conn, err := grpc.NewClient(o.Addr, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial hubble observer at %q: %w", o.Addr, err)
	}
	defer conn.Close()

	client := observerpb.NewObserverClient(conn)

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		err := o.streamFlows(ctx, client)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			o.Log.Error(err, "hubble flow stream ended, retrying", "backoff", backoff.String())
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (o *Observer) streamFlows(ctx context.Context, client observerpb.ObserverClient) error {
	stream, err := client.GetFlows(ctx, &observerpb.GetFlowsRequest{Follow: true})
	if err != nil {
		return fmt.Errorf("start hubble flow stream: %w", err)
	}

	// A successful stream start resets the backoff for the next retry --
	// only a stream that never got going at all (the err != nil branch
	// above) should keep backing off further.
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("receive from hubble flow stream: %w", err)
		}

		flow := resp.GetFlow()
		if flow == nil {
			// GetFlowsResponse can also carry a NodeStatus or LostEvents
			// payload instead of a Flow -- not an error, just nothing to
			// publish for this message.
			continue
		}

		event, ok := FromFlow(flow)
		if !ok {
			continue
		}
		if err := o.Bus.PublishNormalizedEvent(o.Subject, event); err != nil {
			o.Log.Error(err, "failed to publish NormalizedEvent from a Hubble flow")
		}
	}
}
