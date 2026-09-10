package main

import (
	"fmt"
	"os"
	"time"
)

// config holds falco-bridge's runtime configuration, loaded from the
// environment. Deliberately not pkg/config.OperatorConfig: that type is
// documented as operator-specific (leader election, mesh adapter, threat
// threshold -- none of which this binary needs), and pulling it in here
// would mean either ignoring most of its fields or growing it with
// falco-bridge-only concerns it has no business carrying.
type config struct {
	listenAddr   string
	sharedSecret string
	logLevel     string

	natsURL             string
	natsStreamName      string
	natsEventsSubject   string
	natsThreatsSubject  string
	natsConnectTimeout  time.Duration
	natsCredentialsFile string
	natsUsername        string
	natsPassword        string
	natsTLSCAFile       string
	natsTLSCertFile     string
	natsTLSKeyFile      string
}

// loadConfig reads config from the environment, matching .env.example's
// FALCO_BRIDGE_* variables and reusing the same NATS_* variables
// cmd/operator and cmd/ai-engine already share, so a single NATS connection
// configuration works across every Sentinel5G component in a deployment.
func loadConfig() config {
	return config{
		listenAddr:   getEnv("FALCO_BRIDGE_LISTEN_ADDR", ":8091"),
		sharedSecret: getEnv("FALCO_BRIDGE_SHARED_SECRET", ""),
		logLevel:     getEnv("LOG_LEVEL", "info"),

		natsURL:             getEnv("NATS_URL", "nats://localhost:4222"),
		natsStreamName:      getEnv("NATS_STREAM_NAME", "SENTINEL5G"),
		natsEventsSubject:   getEnv("NATS_EVENTS_SUBJECT", "sentinel5g.events.normalized"),
		natsThreatsSubject:  getEnv("NATS_THREATS_SUBJECT", "sentinel5g.threats.scored"),
		natsConnectTimeout:  getDurationEnv("NATS_CONNECT_TIMEOUT", 5*time.Second),
		natsCredentialsFile: getEnv("NATS_CREDENTIALS_FILE", ""),
		natsUsername:        getEnv("NATS_USERNAME", ""),
		natsPassword:        getEnv("NATS_PASSWORD", ""),
		natsTLSCAFile:       getEnv("NATS_TLS_CA_FILE", ""),
		natsTLSCertFile:     getEnv("NATS_TLS_CERT_FILE", ""),
		natsTLSKeyFile:      getEnv("NATS_TLS_KEY_FILE", ""),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s=%q, using default %s: %v\n", key, v, fallback, err)
		return fallback
	}
	return parsed
}
