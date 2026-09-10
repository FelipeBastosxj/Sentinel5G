// Command operator is the Sentinel5G Kubernetes operator entrypoint: it
// reconciles TelecomSecurityPolicy custom resources and drives closed-loop
// mitigation in response to threat scores published by cmd/ai-engine.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clientgoevents "k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/config"
	sentinelcontroller "github.com/FelipeBastosxj/Sentinel5G/pkg/controller"
	sentinelebpf "github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/hubble"
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
	// `manager healthcheck` is a separate mode entirely, not a flag: it's
	// invoked by the Dockerfile's HEALTHCHECK against an already-running
	// container's own process, so it must exit immediately with a plain 0/1
	// rather than going anywhere near flag.Parse()/manager startup. See
	// runHealthcheck's doc comment (healthcheck.go) for why this exists at
	// all instead of a shell command.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

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

	// Captured once, here, rather than inlined into mgr.Start below: the NATS
	// connector goroutine started right after this needs the same
	// cancel-on-SIGTERM/SIGINT context the manager itself eventually runs
	// under, so both shut down together.
	ctx := ctrl.SetupSignalHandler()

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

	// Connects in the background with retry/backoff rather than blocking (or
	// failing) startup here: reconciling TelecomSecurityPolicy status
	// doesn't itself need the bus, only ThreatScoreWatcher/Publisher/
	// Observer below do, and each of those already tolerates NATS not being
	// connected yet (see events.Connector's doc comment). A chart's default
	// nats.url pointing at a JetStream that isn't there yet -- the common
	// case right after a fresh `helm install` -- now surfaces as a
	// not-yet-ready Pod that self-heals, not a CrashLoopBackOff.
	busConnector := events.NewConnector(events.Config{
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
	}, log.WithName("nats"))
	go busConnector.Run(ctx)
	defer func() {
		if bus, connected := busConnector.Bus(); connected {
			bus.Close()
		}
	}()

	blocklist := attachBlocklist(log, mgr.GetEventRecorder("sentinel5g-operator"), operatorPodRef(), bpfObjectPath, bpfInterface)
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
			Bus:      busConnector,
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

	// Cilium-native capture path (pkg/hubble.Observer): an alternative to
	// eBPF's own standalone XDP attach above, not additive to it -- see
	// docs/integrations.md's "Capture layer" section for why a cluster
	// picks one or the other. Opt-in via HUBBLE_ADDR; empty (the default)
	// leaves this disabled entirely, same "degrade gracefully" posture
	// buildMeshAdapter and attachBlocklist already follow.
	if cfg.HubbleAddr != "" {
		observer := &hubble.Observer{
			Addr:      cfg.HubbleAddr,
			TLSConfig: hubbleTLSConfig(log, cfg),
			Bus:       busConnector,
			Subject:   cfg.NATSEventsSubject,
			Log:       log.WithName("hubble-observer"),
		}
		if addErr := mgr.Add(observer); addErr != nil {
			log.Error(addErr, "unable to register hubble observer")
			os.Exit(1)
		}
	}

	meshAdapter := buildMeshAdapter(log, cfg.MeshAdapter, mesh.AdapterDeps{Client: mgr.GetClient()}, mgr.GetRESTMapper())

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
		Bus:           busConnector,
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
	// Surfaces "connected to NATS at least once" on /readyz, so a Pod stuck
	// waiting on busConnector (e.g. NATS not deployed yet, or a wrong
	// nats.url) shows as 0/1 Ready with a legible condition instead of the
	// CrashLoopBackOff this replaced.
	if err := mgr.AddReadyzCheck("nats", natsReadyzCheck(busConnector)); err != nil {
		log.Error(err, "unable to set up NATS ready check")
		os.Exit(1)
	}

	log.Info("starting Sentinel5G operator", "threatScoreThreshold", cfg.ThreatScoreThreshold, "meshAdapter", cfg.MeshAdapter, "deEscalationDwell", cfg.DeEscalationDwell, "logLevel", logLevel)
	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "manager exited with an error")
		os.Exit(1)
	}
}

// natsReadyzCheck reports not-ready until busConnector has connected to
// NATS at least once.
func natsReadyzCheck(busConnector *events.Connector) healthz.Checker {
	return func(_ *http.Request) error {
		if !busConnector.Connected() {
			return fmt.Errorf("not yet connected to NATS JetStream")
		}
		return nil
	}
}

// attachBlocklist attempts to load and attach bpf/packet_filter.c. Failure
// to attach (missing object file, non-Linux dev machine, insufficient
// privileges, wrong --bpf-interface) is logged AND recorded as a Kubernetes
// Event on ref (when ref is non-nil -- see operatorPodRef) but non-fatal:
// the operator still reconciles TelecomSecurityPolicy status and can still
// drive mesh-layer isolation. The Event is what makes this a "Pod condition
// or event," not just a log line, per docs/integrations.md's eBPF preflight
// section: `kubectl describe pod`/`kubectl get events` surface it directly.
func attachBlocklist(log logr.Logger, recorder clientgoevents.EventRecorder, ref runtime.Object, objectPath, iface string) sentinelebpf.BlocklistUpdater {
	loader, err := sentinelebpf.Attach(objectPath, iface)
	if err != nil {
		cause := sentinelebpf.ClassifyAttachError(err)
		log.Info("eBPF blocklist not attached; EbpfBlock actions will be no-ops",
			"cause", cause, "reason", err.Error())
		if ref != nil {
			recorder.Eventf(ref, nil, corev1.EventTypeWarning, "EBPFAttachFailed", "AttachXDP", "%s: %s", cause, err.Error())
		}
		return noopBlocklist{}
	}
	return loader
}

// operatorPodRef builds an object reference to the operator's own Pod from
// the POD_NAME/POD_NAMESPACE Downward API env vars (see
// charts/sentinel5g-operator/templates/deployment.yaml and
// config/manager/manager.yaml), for attachBlocklist's Event above. Returns
// nil when either is unset -- e.g. `go run ./cmd/operator` against a local
// cluster per docs/getting-started.md, which isn't running as a Pod at all
// -- so attachBlocklist just skips recording an Event rather than emitting
// one against a nonexistent object.
func operatorPodRef() runtime.Object {
	name, namespace := os.Getenv("POD_NAME"), os.Getenv("POD_NAMESPACE")
	if name == "" || namespace == "" {
		return nil
	}
	return &corev1.ObjectReference{
		APIVersion: "v1",
		Kind:       "Pod",
		Name:       name,
		Namespace:  namespace,
	}
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
func buildMeshAdapter(log logr.Logger, kind string, deps mesh.AdapterDeps, restMapper meta.RESTMapper) mesh.Adapter {
	switch kind {
	case "istio":
		gvk := schema.GroupVersionKind{Group: "security.istio.io", Version: "v1", Kind: "AuthorizationPolicy"}
		if _, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			log.Info("Istio AuthorizationPolicy CRD not found on this cluster; IsolatePod actions will be no-ops", "reason", err.Error())
			return mesh.NoopAdapter{}
		}
	case "cilium":
		gvk := schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}
		if _, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			log.Info("Cilium CiliumNetworkPolicy CRD not found on this cluster; IsolatePod actions will be no-ops", "reason", err.Error())
			return mesh.NoopAdapter{}
		}
	case "linkerd":
		gvk := schema.GroupVersionKind{Group: "policy.linkerd.io", Version: "v1beta3", Kind: "Server"}
		if _, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			log.Info("Linkerd Server CRD (policy.linkerd.io/v1beta3) not found on this cluster; IsolatePod actions will be no-ops", "reason", err.Error())
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

// hubbleTLSConfig builds the *tls.Config pkg/hubble.Observer dials with, or
// nil for a plaintext connection when none of HUBBLE_TLS_* are set --
// mirroring events.Config's own optional-TLS posture for the NATS
// connection above. A cert/key pair without a CA file is valid (mTLS to a
// Relay whose server cert chains to a well-known root); a CA file alone,
// with no client cert, is also valid (TLS without client auth).
func hubbleTLSConfig(log logr.Logger, cfg config.OperatorConfig) *tls.Config {
	if cfg.HubbleTLSCAFile == "" && cfg.HubbleTLSCertFile == "" && cfg.HubbleTLSKeyFile == "" {
		return nil
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.HubbleTLSCAFile != "" {
		caCert, err := os.ReadFile(cfg.HubbleTLSCAFile)
		if err != nil {
			log.Error(err, "unable to read HUBBLE_TLS_CA_FILE")
			os.Exit(1)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			log.Error(fmt.Errorf("no certificates found in %q", cfg.HubbleTLSCAFile), "invalid HUBBLE_TLS_CA_FILE")
			os.Exit(1)
		}
		tlsConfig.RootCAs = pool
	}

	if cfg.HubbleTLSCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.HubbleTLSCertFile, cfg.HubbleTLSKeyFile)
		if err != nil {
			log.Error(err, "unable to load HUBBLE_TLS_CERT_FILE/HUBBLE_TLS_KEY_FILE")
			os.Exit(1)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig
}

type noopBlocklist struct{}

func (noopBlocklist) Block(net.IP) error   { return nil }
func (noopBlocklist) Unblock(net.IP) error { return nil }
func (noopBlocklist) Close() error         { return nil }
