// Package config loads operator runtime configuration from the environment,
// matching the variables documented in .env.example.
package config

import (
	"fmt"
	"os"
	"strconv"
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
