package config

import (
	"os"
	"testing"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
)

// clearNATSAuthEnv resets every env var natsHasCredentials/Load consult, so
// a developer's own shell (or CI) having one of these set doesn't make the
// test's "no credentials configured" cases flaky.
func clearNATSAuthEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"NATS_CREDENTIALS_FILE",
		"NATS_USERNAME",
		"NATS_PASSWORD",
		"NATS_TLS_CA_FILE",
		"NATS_TLS_CERT_FILE",
		"NATS_TLS_KEY_FILE",
		"NATS_ALLOW_UNAUTHENTICATED",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_RefusesUnauthenticatedNATSByDefault(t *testing.T) {
	clearNATSAuthEnv(t)

	if _, err := Load(); err == nil {
		t.Fatal("Load() with no NATS credentials and NATS_ALLOW_UNAUTHENTICATED unset = nil error, want an error")
	}
}

func TestLoad_AllowsUnauthenticatedNATSWhenExplicitlyOptedIn(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with NATS_ALLOW_UNAUTHENTICATED=true = %v, want no error", err)
	}
	if !cfg.NATSAllowUnauthenticated {
		t.Fatal("cfg.NATSAllowUnauthenticated = false, want true")
	}
}

func TestLoad_AllowsUnauthenticatedNATSValidationWhenCredentialsPresent(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"credentials file", map[string]string{"NATS_CREDENTIALS_FILE": "/etc/nats/creds.creds"}},
		{"username", map[string]string{"NATS_USERNAME": "sentinel5g"}},
		{"tls cert", map[string]string{"NATS_TLS_CERT_FILE": "/etc/nats/tls.crt"}},
		{"tls ca", map[string]string{"NATS_TLS_CA_FILE": "/etc/nats/ca.crt"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearNATSAuthEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			if _, err := Load(); err != nil {
				t.Fatalf("Load() with %s set = %v, want no error", tc.name, err)
			}
		})
	}
}

func TestLoad_RejectsThreatScoreThresholdOutOfRange(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("THREAT_SCORE_THRESHOLD", "1.5")

	if _, err := Load(); err == nil {
		t.Fatal("Load() with THREAT_SCORE_THRESHOLD=1.5 = nil error, want an error")
	}
}

// A zero threshold would classify every GTP-U packet as a flood. Refusing to
// start beats accepting it and mitigating everything.
func TestLoad_RejectsAZeroTunnelFloodThreshold(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("GTPU_TUNNEL_FLOOD_ENABLED", "true")
	t.Setenv("GTPU_TUNNEL_FLOOD_PPS", "0")

	if _, err := Load(); err == nil {
		t.Fatal("expected Load to refuse a zero GTPU_TUNNEL_FLOOD_PPS while the detector is enabled")
	}
}

// ...but a zero threshold with the detector off is not a misconfiguration,
// it's simply unused.
func TestLoad_AllowsAZeroTunnelFloodThresholdWhenDisabled(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("GTPU_TUNNEL_FLOOD_ENABLED", "false")
	t.Setenv("GTPU_TUNNEL_FLOOD_PPS", "0")

	if _, err := Load(); err != nil {
		t.Fatalf("expected Load to accept an unused zero threshold, got %v", err)
	}
}

func TestLoad_TunnelFloodDefaults(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.GTPUTunnelFloodEnabled {
		t.Error("expected the tunnel flood detector on by default")
	}
	if cfg.GTPUTunnelFloodPPS != 1000 {
		t.Errorf("GTPUTunnelFloodPPS = %d, want 1000", cfg.GTPUTunnelFloodPPS)
	}
	if cfg.GTPUTunnelFloodCooldown != 30*time.Second {
		t.Errorf("GTPUTunnelFloodCooldown = %v, want 30s", cfg.GTPUTunnelFloodCooldown)
	}
}

// A value above uint32 must fail loudly rather than wrap into an absurdly
// low threshold that would then fire on ordinary traffic.
func TestLoad_RejectsAnOutOfRangeTunnelFloodThreshold(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("GTPU_TUNNEL_FLOOD_PPS", "4294967296")

	if _, err := Load(); err == nil {
		t.Fatal("expected Load to reject a GTPU_TUNNEL_FLOOD_PPS above uint32")
	}
}

// unsetEnv removes key for the duration of the test and restores whatever
// the developer's shell had afterwards.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	if old, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { _ = os.Setenv(key, old) })
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}

// Pinning is on by default, because the alternative is a restart that
// silently un-blocks every active mitigation -- the same fail-open the
// enforcement maps' HASH-not-LRU choice exists to prevent, arriving through
// the back door (ROADMAP.md Phase 4).
func TestLoad_PinsEnforcementMapsByDefault(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	// Unset, not set-to-empty: empty is a MEANING for this variable
	// ("disable pinning"), so t.Setenv(key, "") would assert the opposite of
	// what this test is about.
	unsetEnv(t, "BPF_PIN_PATH")
	unsetEnv(t, "BLOCKLIST_RECONCILE_INTERVAL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BPFPinPath != ebpf.DefaultPinPath {
		t.Errorf("BPFPinPath = %q, want %q", cfg.BPFPinPath, ebpf.DefaultPinPath)
	}
	if cfg.BlocklistReconcileInterval != time.Minute {
		t.Errorf("BlocklistReconcileInterval = %v, want 1m", cfg.BlocklistReconcileInterval)
	}
}

// The documented way to turn pinning off is BPF_PIN_PATH="". getEnv treats
// an empty value as "unset" and would hand back the default, which would
// make the documented opt-out silently an opt-in -- hence
// getEnvAllowEmpty, and hence this test.
func TestLoad_EmptyPinPathDisablesPinning(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("BPF_PIN_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BPFPinPath != "" {
		t.Errorf("BPF_PIN_PATH=\"\" left BPFPinPath = %q; pinning would still be on", cfg.BPFPinPath)
	}
}

// A negative interval is the documented way to keep only the startup
// replay, so it must survive Load rather than being normalized away.
func TestLoad_NegativeReconcileIntervalIsPreserved(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("BLOCKLIST_RECONCILE_INTERVAL", "-1s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BlocklistReconcileInterval != -time.Second {
		t.Errorf("BlocklistReconcileInterval = %v, want -1s", cfg.BlocklistReconcileInterval)
	}
}

// The per-peer detector is the counterpart to the per-tunnel one, on by
// default for the same reason: the evasion it closes (a flood spread across
// many TEIDs) is free to attempt, and a detector shipped off closes
// nothing (ROADMAP.md Phase 4).
func TestLoad_SourceFloodDefaults(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.GTPUSourceFloodEnabled {
		t.Error("expected the source flood detector on by default")
	}
	if cfg.GTPUSourceFloodPPS != 20000 {
		t.Errorf("GTPUSourceFloodPPS = %d, want 20000", cfg.GTPUSourceFloodPPS)
	}
	if cfg.GTPUSourceFloodDistinctTunnels != 256 {
		t.Errorf("GTPUSourceFloodDistinctTunnels = %d, want 256", cfg.GTPUSourceFloodDistinctTunnels)
	}
	if cfg.GTPUSourceFloodWindow != time.Second {
		t.Errorf("GTPUSourceFloodWindow = %v, want 1s", cfg.GTPUSourceFloodWindow)
	}
}

// Both thresholds at zero is a detector that is "enabled" and can never
// fire -- a configuration that looks protective and is not. Refused loudly,
// matching the GTPU_TUNNEL_FLOOD_PPS==0 rule.
func TestLoad_RejectsAnInertSourceFloodDetector(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("GTPU_SOURCE_FLOOD_PPS", "0")
	t.Setenv("GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS", "0")

	if _, err := Load(); err == nil {
		t.Fatal("expected Load to reject a source flood detector with both thresholds at 0")
	}
}

// One half alone is a valid configuration -- cardinality-only catches TEID
// rotation at rates an aggregate threshold would never see.
func TestLoad_SourceFloodCardinalityOnlyIsValid(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("GTPU_SOURCE_FLOOD_PPS", "0")
	t.Setenv("GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS", "128")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GTPUSourceFloodPPS != 0 || cfg.GTPUSourceFloodDistinctTunnels != 128 {
		t.Fatalf("unexpected thresholds: pps=%d distinct=%d", cfg.GTPUSourceFloodPPS, cfg.GTPUSourceFloodDistinctTunnels)
	}
}

// A negative count is nonsense for a >= comparison (it would mean "fires on
// every packet") and must fail loudly rather than being coerced.
func TestLoad_RejectsNegativeDistinctTunnels(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS", "-1")

	if _, err := Load(); err == nil {
		t.Fatal("expected Load to reject a negative GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS")
	}
}
