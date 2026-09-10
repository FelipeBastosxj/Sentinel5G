// Command falco-bridge runs pkg/falco.Bridge as a standalone process: an
// HTTP server Falco's own http_output posts JSON alerts to, translated into
// events.NormalizedEvent and published on NATS_EVENTS_SUBJECT alongside
// whatever cmd/operator's own eBPF-sourced pkg/ingestion.Publisher is
// already publishing there.
//
// This is a separate binary from cmd/operator, not a mode of it: unlike the
// operator, it needs no Kubernetes RBAC, no CRD watching, no leader
// election machinery, no manager -- it's a pure HTTP-to-NATS translator, so
// pulling in controller-runtime's manager for it would be a dependency with
// nothing behind it. Its own env-var config loader below is intentionally
// separate from pkg/config, which is documented as operator-specific.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/falco"
)

func main() {
	// `falco-bridge healthcheck` is a separate mode entirely, invoked by the
	// Dockerfile's HEALTHCHECK against an already-running container's own
	// process -- see runHealthcheck's doc comment (healthcheck.go), same
	// pattern as cmd/operator/main.go's identical `manager healthcheck`.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

	cfg := loadConfig()

	logLevel := zapcore.InfoLevel
	logLevelErr := logLevel.UnmarshalText([]byte(cfg.logLevel))

	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(logLevel)
	zapLogger, err := zapConfig.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = zapLogger.Sync() }()

	log := zapr.NewLogger(zapLogger).WithName("falco-bridge")
	if logLevelErr != nil {
		log.Info("invalid LOG_LEVEL, defaulting to info", "value", cfg.logLevel, "error", logLevelErr.Error())
	}

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		if hostname, hostnameErr := os.Hostname(); hostnameErr == nil {
			nodeName = hostname
		}
	}

	bus, err := events.Connect(events.Config{
		URL:            cfg.natsURL,
		StreamName:     cfg.natsStreamName,
		EventsSubject:  cfg.natsEventsSubject,
		ThreatsSubject: cfg.natsThreatsSubject,
		ConnectTimeout: cfg.natsConnectTimeout,

		CredentialsFile: cfg.natsCredentialsFile,
		Username:        cfg.natsUsername,
		Password:        cfg.natsPassword,
		TLSCAFile:       cfg.natsTLSCAFile,
		TLSCertFile:     cfg.natsTLSCertFile,
		TLSKeyFile:      cfg.natsTLSKeyFile,
	})
	if err != nil {
		log.Error(err, "failed to connect to NATS")
		os.Exit(1)
	}

	bridge := &falco.Bridge{
		Addr:         cfg.listenAddr,
		Bus:          bus,
		Subject:      cfg.natsEventsSubject,
		NodeName:     nodeName,
		SharedSecret: cfg.sharedSecret,
		Log:          log,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("falco-bridge listening", "addr", cfg.listenAddr, "natsUrl", cfg.natsURL, "subject", cfg.natsEventsSubject)
	if err := bridge.Start(ctx); err != nil {
		log.Error(err, "falco-bridge exited with error")
		os.Exit(1)
	}
}
