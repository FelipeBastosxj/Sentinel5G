package events

import (
	"context"
	"time"

	"github.com/go-logr/logr"
)

// Connector establishes a Bus connection in the background, retrying with
// backoff instead of failing outright. Before Phase 2.5, a NATS instance
// that wasn't up yet (or was briefly unreachable, e.g. during the ordering
// window right after `helm install`) made cmd/operator hard-exit -- this is
// the "retry and degrade" replacement, matching how attachBlocklist and
// buildMeshAdapter already treat their own missing dependency as non-fatal.
// See docs/production-install.md.
type Connector struct {
	cfg Config
	log logr.Logger

	ready chan struct{}
	bus   *Bus // set exactly once, always before ready is closed
}

// NewConnector returns a Connector that hasn't started connecting yet --
// call Run (typically in its own goroutine) to begin.
func NewConnector(cfg Config, log logr.Logger) *Connector {
	return &Connector{cfg: cfg, log: log, ready: make(chan struct{})}
}

// NewConnectedConnector wraps an already-established Bus as a Connector
// that reports connected immediately. Useful for callers (and tests) that
// already have a *Bus and don't need Run's retry/backoff behavior.
func NewConnectedConnector(bus *Bus) *Connector {
	c := &Connector{ready: make(chan struct{}), bus: bus}
	close(c.ready)
	return c
}

// Run attempts Connect in a loop with exponential backoff (capped at 30s)
// until it succeeds or ctx is done. Intended to be started once, in its own
// goroutine, at operator startup -- it returns as soon as either happens,
// it does not itself watch for the connection later dropping (Connect's own
// nats.Option set already configures indefinite background reconnection on
// the resulting *Bus for that).
func (c *Connector) Run(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		bus, err := Connect(c.cfg)
		if err == nil {
			c.bus = bus
			close(c.ready)
			c.log.Info("connected to NATS JetStream", "url", c.cfg.URL)
			return
		}

		c.log.Info("unable to connect to NATS JetStream, will retry",
			"url", c.cfg.URL, "error", err.Error(), "retryIn", backoff.String())

		select {
		case <-ctx.Done():
			return
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

// Connected reports whether the underlying Bus has connected yet, without
// blocking -- backs a readyz check (see cmd/operator/main.go) and lets a
// hot loop (e.g. pkg/ingestion.Publisher, pkg/hubble.Observer) decide
// per-event whether to publish or drop rather than blocking on Wait.
func (c *Connector) Connected() bool {
	select {
	case <-c.ready:
		return true
	default:
		return false
	}
}

// Bus returns the connected Bus and true once available, or (nil, false)
// before that -- the non-blocking counterpart to Wait.
func (c *Connector) Bus() (*Bus, bool) {
	select {
	case <-c.ready:
		return c.bus, true
	default:
		return nil, false
	}
}

// Wait blocks until the Bus connects or ctx is done, whichever comes
// first. Suited to a one-time setup step (e.g. subscribing) at the start of
// a manager.Runnable's Start method; ctx being done before a connection is
// established is the normal shutdown case, not a failure, so callers should
// generally treat that error as "stop quietly," not "fail Start."
func (c *Connector) Wait(ctx context.Context) (*Bus, error) {
	select {
	case <-c.ready:
		return c.bus, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
