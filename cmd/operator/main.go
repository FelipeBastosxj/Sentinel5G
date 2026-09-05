// Command operator is the Sentinel5G Kubernetes operator entrypoint: it
// reconciles TelecomSecurityPolicy custom resources and drives closed-loop
// mitigation in response to threat scores published by cmd/ai-engine.
package main

import (
	"flag"
	"net"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	securityv1alpha1 "github.com/sentinel5g/sentinel5g/api/v1alpha1"
	"github.com/sentinel5g/sentinel5g/pkg/config"
	sentinelcontroller "github.com/sentinel5g/sentinel5g/pkg/controller"
	sentinelebpf "github.com/sentinel5g/sentinel5g/pkg/ebpf"
	"github.com/sentinel5g/sentinel5g/pkg/events"
	"github.com/sentinel5g/sentinel5g/pkg/mesh"
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

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log := ctrl.Log.WithName("sentinel5g-operator")

	cfg, err := config.Load()
	if err != nil {
		log.Error(err, "invalid configuration")
		os.Exit(1)
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
	})
	if err != nil {
		log.Error(err, "unable to connect to NATS JetStream")
		os.Exit(1)
	}
	defer bus.Close()

	blocklist := attachBlocklist(log, bpfObjectPath, bpfInterface)
	defer blocklist.Close()

	meshAdapter, err := mesh.NewAdapter(cfg.MeshAdapter, mesh.IstioAdapterDeps{Client: mgr.GetClient()})
	if err != nil {
		log.Error(err, "unable to build mesh adapter")
		os.Exit(1)
	}

	index := sentinelcontroller.NewPolicyIndex()

	reconciler := &sentinelcontroller.Reconciler{
		Client: mgr.GetClient(),
		Log:    log.WithName("controller"),
		Index:  index,
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

	log.Info("starting Sentinel5G operator", "threatScoreThreshold", cfg.ThreatScoreThreshold, "meshAdapter", cfg.MeshAdapter)
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
		log.Info("eBPF blocklist not attached; EbpfBlock actions will be no-ops", "reason", err.Error())
		return noopBlocklist{}
	}
	return loader
}

type noopBlocklist struct{}

func (noopBlocklist) Block(net.IP) error   { return nil }
func (noopBlocklist) Unblock(net.IP) error { return nil }
func (noopBlocklist) Close() error         { return nil }
