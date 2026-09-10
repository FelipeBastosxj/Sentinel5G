// Package config loads operator runtime configuration from the environment,
// matching the variables documented in .env.example.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// OperatorConfig holds the operator's runtime configuration.
type OperatorConfig struct {
	MetricsBindAddress     string
	HealthProbeBindAddress string
	LeaderElect            bool

	NATSURL            string
	NATSEventsSubject  string
	NATSThreatsSubject string
	NATSStreamName     string

	// NATS authentication/TLS, all optional — see events.Config and
	// docs/integrations.md for why an unauthenticated bus is a real risk
	// outside of local dev.
	NATSCredentialsFile string
	NATSUsername        string
	NATSPassword        string
	NATSTLSCAFile       string
	NATSTLSCertFile     string
	NATSTLSKeyFile      string

	ThreatScoreThreshold float64
	MeshAdapter          string

	// HubbleAddr is the Hubble Observer gRPC endpoint (typically Hubble
	// Relay, e.g. "hubble-relay.kube-system.svc.cluster.local:80"). Empty
	// (the default) disables pkg/hubble.Observer entirely -- this is an
	// opt-in alternative capture path for clusters already running Cilium
	// as their CNI, not something to enable alongside eBPF's own XDP attach
	// on the same interface (see docs/integrations.md's "Capture layer"
	// section for why those two don't compose).
	HubbleAddr string
	// Hubble Relay is commonly deployed with mTLS -- see
	// https://docs.cilium.io/en/stable/observability/hubble/configuration/#tls-configuration.
	// All optional and empty by default, matching HubbleAddr's own
	// disabled-by-default posture; a plaintext connection is used when
	// HubbleAddr is set but none of these are.
	HubbleTLSCAFile   string
	HubbleTLSCertFile string
	HubbleTLSKeyFile  string

	// LogLevel is passed through as-is (zap vocabulary: debug/info/warn/
	// error/...) for cmd/operator/main.go to parse — validated there, not
	// here, since a bad value should log a warning through the very logger
	// being constructed, not fail config.Load() before any logger exists.
	LogLevel string

	// DeEscalationDwell is how long a TelecomSecurityPolicy must go without
	// a new mitigation before pkg/controller.Reconciler automatically
	// reverses its active ones (see that package's tryDeEscalate for why
	// this is a quiet-period timer, not a "sustained low score" check).
	DeEscalationDwell time.Duration
}

// Load reads OperatorConfig from the environment, applying the same defaults
// as .env.example so the operator runs out of the box in local dev.
func Load() (OperatorConfig, error) {
	threshold, err := parseFloatEnv("THREAT_SCORE_THRESHOLD", 0.85)
	if err != nil {
		return OperatorConfig{}, err
	}

	leaderElect, err := parseBoolEnv("LEADER_ELECT", false)
	if err != nil {
		return OperatorConfig{}, err
	}

	deEscalationDwell, err := parseDurationEnv("DE_ESCALATION_DWELL", 5*time.Minute)
	if err != nil {
		return OperatorConfig{}, err
	}

	cfg := OperatorConfig{
		MetricsBindAddress:     getEnv("METRICS_BIND_ADDRESS", ":8080"),
		HealthProbeBindAddress: getEnv("HEALTH_PROBE_BIND_ADDRESS", ":8081"),
		LeaderElect:            leaderElect,

		NATSURL:            getEnv("NATS_URL", "nats://localhost:4222"),
		NATSEventsSubject:  getEnv("NATS_EVENTS_SUBJECT", "sentinel5g.events.normalized"),
		NATSThreatsSubject: getEnv("NATS_THREATS_SUBJECT", "sentinel5g.threats.scored"),
		NATSStreamName:     getEnv("NATS_STREAM_NAME", "SENTINEL5G"),

		NATSCredentialsFile: getEnv("NATS_CREDENTIALS_FILE", ""),
		NATSUsername:        getEnv("NATS_USERNAME", ""),
		NATSPassword:        getEnv("NATS_PASSWORD", ""),
		NATSTLSCAFile:       getEnv("NATS_TLS_CA_FILE", ""),
		NATSTLSCertFile:     getEnv("NATS_TLS_CERT_FILE", ""),
		NATSTLSKeyFile:      getEnv("NATS_TLS_KEY_FILE", ""),

		ThreatScoreThreshold: threshold,
		MeshAdapter:          getEnv("MESH_ADAPTER", "istio"),
		LogLevel:             getEnv("LOG_LEVEL", "info"),

		HubbleAddr:        getEnv("HUBBLE_ADDR", ""),
		HubbleTLSCAFile:   getEnv("HUBBLE_TLS_CA_FILE", ""),
		HubbleTLSCertFile: getEnv("HUBBLE_TLS_CERT_FILE", ""),
		HubbleTLSKeyFile:  getEnv("HUBBLE_TLS_KEY_FILE", ""),

		DeEscalationDwell: deEscalationDwell,
	}

	if cfg.ThreatScoreThreshold < 0 || cfg.ThreatScoreThreshold > 1 {
		return OperatorConfig{}, fmt.Errorf("THREAT_SCORE_THRESHOLD must be within [0,1], got %f", cfg.ThreatScoreThreshold)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func parseFloatEnv(key string, fallback float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func parseBoolEnv(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func parseDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}
