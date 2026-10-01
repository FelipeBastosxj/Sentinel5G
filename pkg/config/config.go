// Package config loads operator runtime configuration from the environment,
// matching the variables documented in .env.example.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
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

	// GTPUSourceFloodEnabled turns on the per-PEER aggregate detector
	// (pkg/detect.GTPUSourceFloodDetector). It is the counterpart to the
	// per-tunnel rule above, not a replacement: a per-tunnel threshold is
	// structurally blind to 200 tunnels at 999 pkt/s each, and a per-source
	// one is structurally blind to one subscriber flooding behind a busy
	// gNB. Neither threshold can be set to do the other's job, which is why
	// there are two (ROADMAP.md Phase 4).
	GTPUSourceFloodEnabled bool

	// GTPUSourceFloodPPS is the AGGREGATE packets-per-second from one source
	// address across every tunnel it carries. Reasoned, not measured against
	// a production N3 -- the same caveat GTPUTunnelFloodPPS carries, and a
	// more consequential one here, because the action that fits a per-peer
	// verdict is the source-wide drop that takes every subscriber behind
	// that gNB with it. Run a detection-only pilot and read
	// sentinel5g_threshold_crossings_total before enabling autoMitigate on
	// a policy that can act on this.
	GTPUSourceFloodPPS uint32

	// GTPUSourceFloodDistinctTunnels is how many distinct TEIDs one source
	// may use within the window before being reported regardless of rate.
	// A real gNB's TEID set is bounded by its active bearers and turns over
	// slowly; rapid rotation is the shape of TEID spoofing. Zero disables
	// this half of the rule.
	GTPUSourceFloodDistinctTunnels int

	// GTPUSourceFloodWindow is the measurement window for both thresholds
	// above. Defaults to 1s, matching the kernel's own rate window so the
	// numbers are in the same units.
	GTPUSourceFloodWindow time.Duration

	// GTPUSourceFloodCooldown is the minimum interval between events for one
	// source -- same cost control as GTPUTunnelFloodCooldown.
	GTPUSourceFloodCooldown time.Duration

	// NATSEventBatchSize coalesces up to this many NormalizedEvents into one
	// JetStream message instead of one per packet. The bus is the design's
	// first throughput ceiling (ROADMAP.md Phase 4), and a batch of N cuts
	// the message rate by N. 0 or 1 publishes per event. Off by default: it
	// is a wire-behaviour change the AI engine consumer must understand
	// first (it does, for any value), so enable it after both sides are on a
	// current build. The inline flood detectors are never batched -- they act
	// per packet regardless.
	NATSEventBatchSize int

	// NATSEventFlushInterval bounds how long a partial batch waits before it
	// is published anyway. Ignored when NATSEventBatchSize <= 1.
	NATSEventFlushInterval time.Duration

	// BPFPinPath is the bpffs directory the eBPF ENFORCEMENT maps
	// (blocklist, blocklist_v6, tunnel_blocklist, tunnel_blocklist_v6) are
	// pinned into, so an active drop survives this process. Empty disables
	// pinning, which is the pre-Phase-4 behaviour: a rollout, an OOM kill
	// or a crash then un-blocks everything the policies' status still
	// claims is blocked, until pkg/controller.BlocklistReconciler re-applies
	// it. See pkg/ebpf/pin_linux.go for which maps are pinned and why the
	// rate/observation maps deliberately are not.
	//
	// Needs /sys/fs/bpf mounted into the container (the Helm chart does this
	// when ebpf.enabled and ebpf.pinPath are both set). A pin path that is
	// not on a bpffs is NOT fatal -- cmd/operator/main.go falls back to an
	// unpinned attach with a warning Event, because losing XDP entirely over
	// a mount problem would be worse than losing restart survival.
	BPFPinPath string

	// BlocklistReconcileInterval is how often the operator compares the
	// kernel's enforcement maps against what the TelecomSecurityPolicy
	// status subresources say should be enforced, re-applying the
	// difference (pkg/controller.BlocklistReconciler). A sync always runs
	// once at startup regardless; this only sets the periodic cadence.
	// Negative disables the periodic pass and leaves only that startup one.
	BlocklistReconcileInterval time.Duration

	// ReactorStatusWritesPerSecond bounds non-meaningful status refreshes
	// per policy (ObservedThreatScore-only writes under a score storm), so a
	// busy source can't turn one policy's scores into apiserver pressure.
	// Meaningful writes (phase transitions, blocklist changes) are never
	// throttled. 0 disables the limit. See pkg/controller.ReactorLimiter.
	ReactorStatusWritesPerSecond float64

	// ReactorStatusWriteBurst is the token-bucket burst for the above.
	ReactorStatusWriteBurst int

	// KillSwitchConfigMapName is the ConfigMap whose presence with
	// engaged=true suppresses ALL mitigation globally (detection continues).
	// Empty disables the kill switch entirely. See
	// pkg/controller.KillSwitch -- the point is that engaging it is one
	// kubectl command with no operator restart.
	KillSwitchConfigMapName string

	// KillSwitchNamespace is where that ConfigMap is read from. Defaults to
	// the operator's own namespace (POD_NAMESPACE), so the switch lives
	// next to the operator and needs no cross-namespace RBAC.
	KillSwitchNamespace string

	// KillSwitchPollInterval bounds how often the ConfigMap is read and so
	// how quickly engaging it takes effect. Default 2s.
	KillSwitchPollInterval time.Duration

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

	sourceFloodEnabled, err := parseBoolEnv("GTPU_SOURCE_FLOOD_ENABLED", true)
	if err != nil {
		return OperatorConfig{}, err
	}

	sourceFloodPPS, err := parseUint32Env("GTPU_SOURCE_FLOOD_PPS", 20000)
	if err != nil {
		return OperatorConfig{}, err
	}

	sourceFloodDistinct, err := parseIntEnv("GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS", 256)
	if err != nil {
		return OperatorConfig{}, err
	}

	sourceFloodWindow, err := parseDurationEnv("GTPU_SOURCE_FLOOD_WINDOW", time.Second)
	if err != nil {
		return OperatorConfig{}, err
	}

	sourceFloodCooldown, err := parseDurationEnv("GTPU_SOURCE_FLOOD_COOLDOWN", 30*time.Second)
	if err != nil {
		return OperatorConfig{}, err
	}

	allowUnauthenticated, err := parseBoolEnv("NATS_ALLOW_UNAUTHENTICATED", false)
	if err != nil {
		return OperatorConfig{}, err
	}

	blocklistReconcileInterval, err := parseDurationEnv("BLOCKLIST_RECONCILE_INTERVAL", time.Minute)
	if err != nil {
		return OperatorConfig{}, err
	}

	killSwitchPoll, err := parseDurationEnv("KILL_SWITCH_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return OperatorConfig{}, err
	}

	eventBatchSize, err := parseIntEnv("NATS_EVENT_BATCH_SIZE", 1)
	if err != nil {
		return OperatorConfig{}, err
	}

	eventFlushInterval, err := parseDurationEnv("NATS_EVENT_FLUSH_INTERVAL", 250*time.Millisecond)
	if err != nil {
		return OperatorConfig{}, err
	}

	reactorRate, err := parseFloatEnv("REACTOR_STATUS_WRITES_PER_SECOND", 10)
	if err != nil {
		return OperatorConfig{}, err
	}

	reactorBurst, err := parseIntEnv("REACTOR_STATUS_WRITE_BURST", 20)
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

		// Defaults to pinning: the alternative is a restart that silently
		// un-blocks every active mitigation, which is the exact fail-open
		// the enforcement maps' HASH-not-LRU choice exists to prevent.
		// BPF_PIN_PATH="" opts back out.
		BPFPinPath:                 getEnvAllowEmpty("BPF_PIN_PATH", ebpf.DefaultPinPath),
		BlocklistReconcileInterval: blocklistReconcileInterval,

		NATSEventBatchSize:     eventBatchSize,
		NATSEventFlushInterval: eventFlushInterval,

		ReactorStatusWritesPerSecond: reactorRate,
		ReactorStatusWriteBurst:      reactorBurst,

		KillSwitchConfigMapName: getEnvAllowEmpty("KILL_SWITCH_CONFIGMAP_NAME", "sentinel5g-killswitch"),
		KillSwitchNamespace:     getEnv("KILL_SWITCH_NAMESPACE", os.Getenv("POD_NAMESPACE")),
		KillSwitchPollInterval:  killSwitchPoll,

		GTPUTunnelFloodEnabled:  tunnelFloodEnabled,
		GTPUTunnelFloodPPS:      tunnelFloodPPS,
		GTPUTunnelFloodCooldown: tunnelFloodCooldown,

		GTPUSourceFloodEnabled:         sourceFloodEnabled,
		GTPUSourceFloodPPS:             sourceFloodPPS,
		GTPUSourceFloodDistinctTunnels: sourceFloodDistinct,
		GTPUSourceFloodWindow:          sourceFloodWindow,
		GTPUSourceFloodCooldown:        sourceFloodCooldown,
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

	// Both thresholds zero leaves the detector enabled but structurally
	// unable to fire, which is the kind of configuration that looks
	// protective and is not. Refused rather than logged, matching
	// GTPU_TUNNEL_FLOOD_PPS above.
	if cfg.GTPUSourceFloodEnabled && cfg.GTPUSourceFloodPPS == 0 && cfg.GTPUSourceFloodDistinctTunnels == 0 {
		return OperatorConfig{}, fmt.Errorf(
			"GTPU_SOURCE_FLOOD_PPS and GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS cannot both be 0 " +
				"when GTPU_SOURCE_FLOOD_ENABLED is true: set at least one, or set " +
				"GTPU_SOURCE_FLOOD_ENABLED=false to turn the detector off explicitly")
	}

	if cfg.GTPUSourceFloodWindow <= 0 {
		return OperatorConfig{}, fmt.Errorf("GTPU_SOURCE_FLOOD_WINDOW must be greater than 0, got %s", cfg.GTPUSourceFloodWindow)
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

// getEnvAllowEmpty is getEnv for a setting where the EMPTY string is a
// meaningful value rather than "unset". getEnv treats BPF_PIN_PATH="" as
// unset and hands back the default, which would make the documented way to
// turn pinning off silently turn it on.
func getEnvAllowEmpty(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
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

// parseIntEnv is parseUint32Env's counterpart for a count that is naturally
// an int (a map size, a cardinality threshold) rather than a kernel-facing
// u32. Negative is rejected: it would mean "fires on every packet" for a
// >= comparison, which is the opposite of the intent.
func parseIntEnv(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("invalid %s: must not be negative, got %d", key, parsed)
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
