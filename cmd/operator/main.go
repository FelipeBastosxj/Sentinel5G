// Command operator is the Sentinel5G Kubernetes operator entrypoint: it
// reconciles TelecomSecurityPolicy custom resources and drives closed-loop
// mitigation in response to threat scores published by cmd/ai-engine.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/config"
	sentinelcontroller "github.com/FelipeBastosxj/Sentinel5G/pkg/controller"
	sentinelebpf "github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/ingestion"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/mesh"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(securityv1alpha1.AddToScheme(scheme))
}

func main() {
	var bpfObjectPath, bpfInterface string
	flag.StringVar(&bpfObjectPath, "bpf-object", "/var/run/sentinel5g/packet_filter.o", "path to the compiled bpf/packet_filter.c object")
	flag.StringVar(&bpfInterface, "bpf-interface", "eth0", "network interface to attach the XDP program to")
	flag.Parse()

	// config.Load() must happen before the logger is constructed (its
	// result decides the log level), so a load failure here can't yet go
	// through ctrl.Log -- plain stderr, matching how flag-parsing errors
	// above are also reported before any logger exists.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// LogLevel accepts the full zap vocabulary (debug/info/warn/error/...)
	// via zapcore.Level's standard UnmarshalText -- broader than
	// controller-runtime's own --zap-log-level flag, which only recognizes
	// debug/info/error plus numeric verbosity. UseDevMode(false): JSON
	// encoding is the production-appropriate default for every deployment
	// path (Helm chart included), not just a Helm-specific setting.
	logLevel := zapcore.InfoLevel
	logLevelErr := logLevel.UnmarshalText([]byte(cfg.LogLevel))

	ctrl.SetLogger(zap.New(zap.UseDevMode(false), zap.Level(logLevel)))
	log := ctrl.Log.WithName("sentinel5g-operator")

	if logLevelErr != nil {
		log.Info("invalid LOG_LEVEL, defaulting to info", "value", cfg.LogLevel, "error", logLevelErr.Error())
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: cfg.MetricsBindAddress},
		HealthProbeBindAddress: cfg.HealthProbeBindAddress,
		LeaderElection:         cfg.LeaderElect,
		LeaderElectionID:       "sentinel5g-operator-lock",
	})
	if err != nil {
		log.Error(err, "unable to start manager")
		os.Exit(1)
	}

	bus, err := events.Connect(events.Config{
		URL:            cfg.NATSURL,
		StreamName:     cfg.NATSStreamName,
		EventsSubject:  cfg.NATSEventsSubject,
		ThreatsSubject: cfg.NATSThreatsSubject,
		ConnectTimeout: events.DefaultConfig().ConnectTimeout,

		CredentialsFile: cfg.NATSCredentialsFile,
		Username:        cfg.NATSUsername,
		Password:        cfg.NATSPassword,
		TLSCAFile:       cfg.NATSTLSCAFile,
		TLSCertFile:     cfg.NATSTLSCertFile,
		TLSKeyFile:      cfg.NATSTLSKeyFile,
	})
	if err != nil {
		log.Error(err, "unable to connect to NATS JetStream")
		os.Exit(1)
	}
	defer bus.Close()

	blocklist := attachBlocklist(log, bpfObjectPath, bpfInterface)
	defer blocklist.Close()

	podIndex := sentinelcontroller.NewPodIPIndex()
	podIndexer := &sentinelcontroller.PodIPIndexer{Client: mgr.GetClient(), Index: podIndex}
	if indexerErr := podIndexer.SetupWithManager(mgr); indexerErr != nil {
		log.Error(indexerErr, "unable to create controller", "controller", "PodIPIndexer")
		os.Exit(1)
	}

	// Layer 1 -> Layer 2 bridge (pkg/ingestion): only runs when eBPF is
	// actually attached (attachBlocklist's noop fallback doesn't implement
	// EventSource), same "degrade gracefully" rule as EbpfBlock actions.
	if source, ok := blocklist.(sentinelebpf.EventSource); ok {
		nodeName, hostnameErr := os.Hostname()
		if hostnameErr != nil {
			log.Error(hostnameErr, "unable to determine node name for ingested events")
			os.Exit(1)
		}
		publisher := &ingestion.Publisher{
			Source:   source,
			PodIndex: podIndex,
			Bus:      bus,
			Subject:  cfg.NATSEventsSubject,
			NodeName: nodeName,
			Log:      log.WithName("ingestion-publisher"),
		}
		if addErr := mgr.Add(publisher); addErr != nil {
			log.Error(addErr, "unable to register ingestion publisher")
			os.Exit(1)
		}
	} else {
		log.Info("eBPF not attached; Layer 1 -> Layer 2 event publishing disabled")
	}

	meshAdapter := buildMeshAdapter(log, cfg.MeshAdapter, mesh.IstioAdapterDeps{Client: mgr.GetClient()}, mgr.GetRESTMapper())

	index := sentinelcontroller.NewPolicyIndex()

	reconciler := &sentinelcontroller.Reconciler{
		Client:            mgr.GetClient(),
		Log:               log.WithName("controller"),
		Index:             index,
		Blocklist:         blocklist,
		Mesh:              meshAdapter,
		DeEscalationDwell: cfg.DeEscalationDwell,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "TelecomSecurityPolicy")
		os.Exit(1)
	}

	watcher := &sentinelcontroller.ThreatScoreWatcher{
		Client:        mgr.GetClient(),
		Log:           log.WithName("threat-score-watcher"),
		Index:         index,
		Bus:           bus,
		Subject:       cfg.NATSThreatsSubject,
		Blocklist:     blocklist,
		Mesh:          meshAdapter,
		BaseThreshold: cfg.ThreatScoreThreshold,
	}
	if err := mgr.Add(watcher); err != nil {
		log.Error(err, "unable to register threat score watcher")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	log.Info("starting Sentinel5G operator", "threatScoreThreshold", cfg.ThreatScoreThreshold, "meshAdapter", cfg.MeshAdapter, "deEscalationDwell", cfg.DeEscalationDwell, "logLevel", logLevel)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "manager exited with an error")
		os.Exit(1)
	}
}

// attachBlocklist attempts to load and attach bpf/packet_filter.c. Failure
// to attach (missing object file, non-Linux dev machine, insufficient
// privileges) is logged but non-fatal: the operator still reconciles
// TelecomSecurityPolicy status and can still drive mesh-layer isolation.
func attachBlocklist(log logr.Logger, objectPath, iface string) sentinelebpf.BlocklistUpdater {
	loader, err := sentinelebpf.Attach(objectPath, iface)
	if err != nil {
		log.Info("eBPF blocklist not attached; EbpfBlock actions will be no-ops",
			"cause", sentinelebpf.ClassifyAttachError(err), "reason", err.Error())
		return noopBlocklist{}
	}
	return loader
}

// buildMeshAdapter builds the configured mesh.Adapter, but for "istio"
// specifically checks first whether the AuthorizationPolicy CRD is actually
// registered on this cluster -- mirroring attachBlocklist's exact
// try-then-degrade-gracefully pattern above. Without this check, IsolatePod
// actions on a cluster that never installed Istio fail every single time
// with "no matches for kind AuthorizationPolicy" -- and because
// ThreatScoreWatcher.applyPolicy (threat_score_watcher.go) returns on the
// first action error rather than continuing past a partial failure, that
// also silently prevents Status.Phase from ever reaching Mitigating, even
// when EbpfBlock (if also enabled on the same policy) succeeded. Found by
// running the real quickstart flow against a real kind cluster with no
// Istio installed, not by inspection -- config/samples/
// security_v1alpha1_telecomsecuritypolicy.yaml sets isolatePod: true, and
// MESH_ADAPTER defaults to "istio", so this is the default experience for
// anyone following docs/getting-started.md or README.md without Istio,
// not an edge case.
func buildMeshAdapter(log logr.Logger, kind string, deps mesh.IstioAdapterDeps, restMapper meta.RESTMapper) mesh.Adapter {
	if kind == "istio" {
		gvk := schema.GroupVersionKind{Group: "security.istio.io", Version: "v1", Kind: "AuthorizationPolicy"}
		if _, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			log.Info("Istio AuthorizationPolicy CRD not found on this cluster; IsolatePod actions will be no-ops", "reason", err.Error())
			return mesh.NoopAdapter{}
		}
	}

	adapter, err := mesh.NewAdapter(kind, deps)
	if err != nil {
		log.Error(err, "unable to build mesh adapter")
		os.Exit(1)
	}
	return adapter
}

type noopBlocklist struct{}

func (noopBlocklist) Block(net.IP) error   { return nil }
func (noopBlocklist) Unblock(net.IP) error { return nil }
func (noopBlocklist) Close() error         { return nil }
