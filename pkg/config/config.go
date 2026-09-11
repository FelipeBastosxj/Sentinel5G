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

	// NATSAllowUnauthenticated must be explicitly set to true to run against
	// a NATS bus with none of the credential/TLS fields above configured.
	// Defaults to false: anything able to reach an unauthenticated NATS_URL
	// can forge a ThreatScoreEvent and trigger a real automated mitigation
	// (see docs/integrations.md), so Load() fails fast instead of silently
	// starting insecure.
	NATSAllowUnauthenticated bool

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

	// GTPUTunnelFloodEnabled turns on the deterministic, non-ML GTP-U
	// tunnel-flood detector (pkg/detect). On by default: it exists because
	// the autoencoder provably cannot catch this class -- a real in-tunnel
	// flood reconstructs BETTER than normal traffic, so no threshold on its
	// score separates them (ROADMAP.md Phase 2.5) -- and a detector shipped
	// off closes nothing. It still goes through full policy gating, so a
	// policy with autoMitigate: false only ever reaches Phase: Alerting.
	GTPUTunnelFloodEnabled bool

	// GTPUTunnelFloodPPS is the per-TUNNEL packets-per-second threshold.
	// Per tunnel, not per source: on a real N3 every subscriber shares the
	// peer gNB's source IP, so a per-source threshold would have to sit
	// above their combined load -- which is exactly the aggregation that
	// hides a single-tunnel flood.
	//
	// Reasoned, not empirically tuned against a production N3 interface --
	// the same honesty caveat bpf/headers/common.h's SCAN_EMIT_THRESHOLD
	// carries, and for the same reason: the real captures this project has
	// were taken through a WSL2 tunnel that capped at ~60 pkt/s, which is a
	// property of that environment rather than of what an attacker can
	// sustain. See docs/paper-data/02-ai-training-inference.md.
	GTPUTunnelFloodPPS uint32

	// GTPUTunnelFloodCooldown is the minimum interval between events for one
	// (source IP, TEID) pair -- a cost control, so a sustained flood doesn't
	// publish thousands of identical scores per second.
	GTPUTunnelFloodCooldown time.Duration

	// ScoringPipelineGrace is how long after startup the operator waits
	// before reporting ScoringPipelineReady=False on every policy (see
	// pkg/controller's scoring_pipeline.go). It only covers the window where
	// "no score yet" is still expected -- an install where the AI engine
	// happens to come up after the operator.
	ScoringPipelineGrace time.Duration
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

	scoringPipelineGrace, err := parseDurationEnv("SCORING_PIPELINE_GRACE", 10*time.Minute)
	if err != nil {
		return OperatorConfig{}, err
	}

	tunnelFloodEnabled, err := parseBoolEnv("GTPU_TUNNEL_FLOOD_ENABLED", true)
	if err != nil {
		return OperatorConfig{}, err
	}

	tunnelFloodPPS, err := parseUint32Env("GTPU_TUNNEL_FLOOD_PPS", 1000)
	if err != nil {
		return OperatorConfig{}, err
	}

	tunnelFloodCooldown, err := parseDurationEnv("GTPU_TUNNEL_FLOOD_COOLDOWN", 30*time.Second)
	if err != nil {
		return OperatorConfig{}, err
	}

	allowUnauthenticated, err := parseBoolEnv("NATS_ALLOW_UNAUTHENTICATED", false)
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

		NATSAllowUnauthenticated: allowUnauthenticated,

		ThreatScoreThreshold: threshold,
		MeshAdapter:          getEnv("MESH_ADAPTER", "istio"),
		LogLevel:             getEnv("LOG_LEVEL", "info"),

		HubbleAddr:        getEnv("HUBBLE_ADDR", ""),
		HubbleTLSCAFile:   getEnv("HUBBLE_TLS_CA_FILE", ""),
		HubbleTLSCertFile: getEnv("HUBBLE_TLS_CERT_FILE", ""),
		HubbleTLSKeyFile:  getEnv("HUBBLE_TLS_KEY_FILE", ""),

		DeEscalationDwell:    deEscalationDwell,
		ScoringPipelineGrace: scoringPipelineGrace,

		GTPUTunnelFloodEnabled:  tunnelFloodEnabled,
		GTPUTunnelFloodPPS:      tunnelFloodPPS,
		GTPUTunnelFloodCooldown: tunnelFloodCooldown,
	}

	if cfg.ThreatScoreThreshold < 0 || cfg.ThreatScoreThreshold > 1 {
		return OperatorConfig{}, fmt.Errorf("THREAT_SCORE_THRESHOLD must be within [0,1], got %f", cfg.ThreatScoreThreshold)
	}

	// Refused rather than silently treated as "not configured": a zero
	// threshold would make every single GTP-U packet a flood, and the
	// resulting mitigation would be immediate and total. Set
	// GTPU_TUNNEL_FLOOD_ENABLED=false to turn the detector off.
	if cfg.GTPUTunnelFloodEnabled && cfg.GTPUTunnelFloodPPS == 0 {
		return OperatorConfig{}, fmt.Errorf("GTPU_TUNNEL_FLOOD_PPS must be greater than 0 when GTPU_TUNNEL_FLOOD_ENABLED is true")
	}

	if !cfg.NATSAllowUnauthenticated && !natsHasCredentials(cfg) {
		return OperatorConfig{}, fmt.Errorf(
			"refusing to start with an unauthenticated NATS connection: set NATS_CREDENTIALS_FILE, " +
				"NATS_USERNAME+NATS_PASSWORD, or NATS_TLS_CERT_FILE+NATS_TLS_KEY_FILE, " +
				"or set NATS_ALLOW_UNAUTHENTICATED=true to opt into it explicitly " +
				"(see docs/integrations.md's \"Securing the NATS message bus\")",
		)
	}

	return cfg, nil
}

func natsHasCredentials(cfg OperatorConfig) bool {
	return cfg.NATSCredentialsFile != "" ||
		cfg.NATSUsername != "" ||
		cfg.NATSTLSCertFile != "" ||
		cfg.NATSTLSCAFile != ""
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

func parseUint32Env(key string, fallback uint32) (uint32, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	// ParseUint with an explicit 32-bit size, so an out-of-range value is a
	// startup error naming the variable rather than a silent wraparound into
	// an absurdly low packets-per-second threshold.
	parsed, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return uint32(parsed), nil
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
