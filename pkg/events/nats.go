package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Bus wraps a NATS JetStream connection and provides typed publish/subscribe
// helpers for NormalizedEvent and ThreatScoreEvent. It is the single
// integration point pkg/controller and (conceptually) cmd/ai-engine use to
// talk to the Layer 2 messaging backbone.
type Bus struct {
	conn *nats.Conn
	js   nats.JetStreamContext
}

// Config controls how a Bus connects to NATS and which stream/subjects it uses.
type Config struct {
	URL            string
	StreamName     string
	EventsSubject  string
	ThreatsSubject string
	ConnectTimeout time.Duration

	// Authentication/transport security, all optional and empty by default —
	// which preserves the historical unauthenticated nats:// behavior for
	// local dev. In any deployment where the NATS bus is reachable by
	// anything other than this operator and the AI engine, set at least one
	// of these: an unauthenticated bus lets anyone who can reach it forge a
	// ThreatScoreEvent and trigger a real mitigation (see docs/integrations.md).
	CredentialsFile string // NATS .creds file (NKey/JWT); takes precedence over Username/Password.
	Username        string
	Password        string
	TLSCAFile       string
	TLSCertFile     string // Requires TLSKeyFile to also be set.
	TLSKeyFile      string
}

// DefaultConfig returns sane local-dev defaults matching .env.example.
func DefaultConfig() Config {
	return Config{
		URL:            nats.DefaultURL,
		StreamName:     "SENTINEL5G",
		EventsSubject:  "sentinel5g.events.normalized",
		ThreatsSubject: "sentinel5g.threats.scored",
		ConnectTimeout: 5 * time.Second,
	}
}

// Connect dials NATS, opens a JetStream context, and ensures the shared
// stream backing both the normalized-events and threat-scores subjects
// exists (creating it if this is the first connection to do so).
func Connect(cfg Config) (*Bus, error) {
	opts := []nats.Option{
		nats.Timeout(cfg.ConnectTimeout),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
	}

	switch {
	case cfg.CredentialsFile != "":
		opts = append(opts, nats.UserCredentials(cfg.CredentialsFile))
	case cfg.Username != "":
		opts = append(opts, nats.UserInfo(cfg.Username, cfg.Password))
	}
	if cfg.TLSCertFile != "" {
		opts = append(opts, nats.ClientCert(cfg.TLSCertFile, cfg.TLSKeyFile))
	}
	if cfg.TLSCAFile != "" {
		opts = append(opts, nats.RootCAs(cfg.TLSCAFile))
	}

	conn, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to nats at %q: %w", cfg.URL, err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open jetstream context: %w", err)
	}

	if err := ensureStream(js, cfg); err != nil {
		conn.Close()
		return nil, err
	}

	return &Bus{conn: conn, js: js}, nil
}

func ensureStream(js nats.JetStreamContext, cfg Config) error {
	subjects := []string{cfg.EventsSubject, cfg.ThreatsSubject}

	if _, err := js.StreamInfo(cfg.StreamName); err != nil {
		_, err := js.AddStream(&nats.StreamConfig{
			Name:      cfg.StreamName,
			Subjects:  subjects,
			Storage:   nats.FileStorage,
			Retention: nats.LimitsPolicy,
			MaxAge:    24 * time.Hour,
		})
		if err != nil {
			return fmt.Errorf("create jetstream stream %q: %w", cfg.StreamName, err)
		}
		return nil
	}

	return nil
}

// Close drains and closes the underlying NATS connection.
func (b *Bus) Close() {
	if b.conn != nil {
		_ = b.conn.Drain()
	}
}

// PublishNormalizedEvent publishes a Layer 2 event onto subject.
func (b *Bus) PublishNormalizedEvent(subject string, event NormalizedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal normalized event: %w", err)
	}
	if _, err := b.js.Publish(subject, payload); err != nil {
		return fmt.Errorf("publish normalized event to %q: %w", subject, err)
	}
	return nil
}

// PublishThreatScore publishes an AI-engine scoring result onto subject.
func (b *Bus) PublishThreatScore(subject string, event ThreatScoreEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal threat score event: %w", err)
	}
	if _, err := b.js.Publish(subject, payload); err != nil {
		return fmt.Errorf("publish threat score to %q: %w", subject, err)
	}
	return nil
}

// ThreatScoreHandler processes a single ThreatScoreEvent. Returning an error
// causes the message to be NAK'd and redelivered.
type ThreatScoreHandler func(ThreatScoreEvent) error

// maxThreatScoreDeliveries bounds JetStream redelivery of a ThreatScoreEvent
// whose handler keeps failing (e.g. a transient API server error). Without a
// cap, a message NAKs forever; this trades "retry a while" for "give up and
// stop paging the log," rather than looping indefinitely.
const maxThreatScoreDeliveries = 5

// SubscribeThreatScores creates (or reuses) a durable JetStream consumer on
// subject and delivers decoded ThreatScoreEvents to handler until the
// returned unsubscribe function is called.
func (b *Bus) SubscribeThreatScores(subject, durable string, handler ThreatScoreHandler) (func() error, error) {
	sub, err := b.js.Subscribe(subject, func(msg *nats.Msg) {
		var event ThreatScoreEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			// A malformed payload will never unmarshal on redelivery either;
			// Term (not Nak) drops it instead of retrying forever.
			_ = msg.Term()
			return
		}
		if err := handler(event); err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	}, nats.Durable(durable), nats.ManualAck(), nats.MaxDeliver(maxThreatScoreDeliveries))
	if err != nil {
		return nil, fmt.Errorf("subscribe to %q: %w", subject, err)
	}

	return sub.Unsubscribe, nil
}
