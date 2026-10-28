# Sentinel5G

**Kernel-level threat detection and automated mitigation for cloud-native 5G/LTE workloads.**

[![CI](https://github.com/FelipeBastosxj/Sentinel5G/actions/workflows/ci.yml/badge.svg)](https://github.com/FelipeBastosxj/Sentinel5G/actions/workflows/ci.yml)
[![E2E](https://github.com/FelipeBastosxj/Sentinel5G/actions/workflows/e2e.yml/badge.svg)](https://github.com/FelipeBastosxj/Sentinel5G/actions/workflows/e2e.yml)
[![Release](https://img.shields.io/github/v/release/FelipeBastosxj/Sentinel5G)](https://github.com/FelipeBastosxj/Sentinel5G/releases)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

Sentinel5G is an open-source Kubernetes operator that protects containerized
telecom workloads (AMF, UPF, SMF, IMS) against signaling storms, GTP-U
floods and protocol anomalies. It inspects traffic in the kernel with eBPF,
scores it with an unsupervised ONNX model, and closes the loop on its own:
dropping the offending source or GTP-U tunnel in XDP and, optionally,
isolating the workload through the service mesh — all driven by a
declarative `TelecomSecurityPolicy` resource.

---

## Contents

- [Capabilities](#capabilities)
- [Architecture](#architecture)
- [Measured performance](#measured-performance)
- [Quick start](#quick-start)
- [Production deployment](#production-deployment)
- [Documentation](#documentation)
- [Development](#development)
- [Supply-chain security](#supply-chain-security)
- [Contributing](#contributing)
- [License](#license)

---

## Capabilities

| Capability | What it does |
|---|---|
| **In-kernel inspection** | An XDP program parses Ethernet/VLAN/IPv4/IPv6/UDP and the GTP-U header itself (3GPP TS 29.281), rating traffic per source and **per tunnel (TEID)** — the difference between one flooding subscriber and the gNB every subscriber shares. No sidecars. |
| **Unsupervised detection** | An autoencoder served by ONNX Runtime learns the normal signaling baseline and flags deviations. The published model is trained on real GTP-U captured from a live Open5GS + UERANSIM core. |
| **Deterministic detectors** | Rule-based tunnel-flood and per-peer source-flood detectors run beside the model, covering the anomaly classes an autoencoder provably cannot separate, including floods spread across many TEIDs. |
| **Closed-loop mitigation** | Per-source or per-tunnel drops in XDP, plus optional workload isolation via Istio, Cilium or Linkerd. Mitigations de-escalate automatically. |
| **Restart-safe enforcement** | Blocklists are pinned to bpffs and reconciled against policy status, so an operator restart, upgrade or leader failover does not silently un-block traffic. |
| **Operational guardrails** | Detection-only mode, a per-policy action rate limit, a global kill switch, and Prometheus metrics for map occupancy, enforcement drift and false-positive rate. |
| **Kubernetes-native** | Everything is declared through the `TelecomSecurityPolicy` CRD and shipped as signed, multi-arch images and OCI Helm charts. |

```yaml
apiVersion: security.sentinel5g.io/v1alpha1
kind: TelecomSecurityPolicy
metadata:
  name: protect-amf-core
  namespace: telecom-core
spec:
  targetWorkloads:
    - app: amf-service
  threatDetection:
    sensitivity: "high"
    autoMitigate: false      # detection-only; see "Production deployment"
  actions:
    ebpfBlockTunnel: true    # drop only the offending GTP-U tunnel
    ebpfBlock: false         # per-source drop, a deliberate fallback on N3
    isolatePod: false        # mesh-level quarantine of the whole workload
```

---

## Architecture

Sentinel5G is built as four independently replaceable layers connected by
NATS JetStream.

```
 ┌──────────────────────────────────────────────────────────────────────┐
 │ 1. CAPTURE        XDP probe (bpf/)  ·  Cilium/Hubble  ·  Falco       │
 │                   GTP-U / SIP parsing, per-TEID rate maps            │
 └───────────────────────────────┬──────────────────────────────────────┘
                                 │ ring buffer
 ┌───────────────────────────────▼──────────────────────────────────────┐
 │ 2. INGESTION      pkg/ingestion → NormalizedEvent (batched)          │
 │                   pkg/detect: tunnel-flood & source-flood rules      │
 └───────────────────────────────┬──────────────────────────────────────┘
                                 │ NATS JetStream
 ┌───────────────────────────────▼──────────────────────────────────────┐
 │ 3. INFERENCE      cmd/ai-engine: ONNX autoencoder → ThreatScoreEvent │
 └───────────────────────────────┬──────────────────────────────────────┘
                                 │ NATS JetStream
 ┌───────────────────────────────▼──────────────────────────────────────┐
 │ 4. CONTROL        cmd/operator: TelecomSecurityPolicy reconciler     │
 │                   → XDP blocklists (pinned)  ·  mesh isolation       │
 └──────────────────────────────────────────────────────────────────────┘
```

| Path | Component |
|---|---|
| `bpf/` | C/eBPF XDP program, CO-RE, bounded maps |
| `cmd/operator/`, `pkg/` | Go operator (controller-runtime), ingestion, detectors, eBPF loader, mesh adapters |
| `cmd/ai-engine/` | Python inference service (ONNX Runtime), training and export scripts |
| `cmd/falco-bridge/` | Optional Falco → `NormalizedEvent` bridge |
| `api/v1alpha1/` | `TelecomSecurityPolicy` CRD types |
| `charts/` | Helm charts for the operator and the AI engine |
| `scripts/loadtest/` | Performance harness for the latency and CPU budgets |

Design decisions and the full event model are in
[`docs/architecture.md`](docs/architecture.md) and
[`docs/event-model.md`](docs/event-model.md).

---

## Measured performance

Every figure below comes from a reproducible harness in `scripts/`, with
method and limits documented in
[`docs/paper-data/`](docs/paper-data/README.md).

| Metric | Target | Measured |
|---|---|---|
| XDP cost per packet, full GTP-U parse | < 0.2 ms | **~195–211 ns** (hot cache), **~900 ns** under sustained load |
| CPU at 100k pkt/s | < 2% per node | **~1.1%** of an 8-core node |
| Score → mitigation, end to end | single-digit ms | **~2 ms** |
| AI engine throughput | — | **~72,000 events/s** per replica |
| Event bus with batching | 100k pkt/s per node | **~694,000 events/s** |
| False-positive rate (340k real benign packets) | — | **0 observed**, 95% upper bound **1.23 × 10⁻⁵** |

The CPU and line-rate figures were measured with native XDP on a veth pair;
physical-NIC line-rate numbers depend on the target hardware and driver.
See [`scripts/loadtest/README.md`](scripts/loadtest/README.md) to reproduce
them on your own nodes.

---

## Quick start

A disposable demo on a local cluster, using the published `v0.3.0` images —
no local build required.

**One command** (needs only `docker` and `curl`; installs nothing system-wide):

```sh
git clone https://github.com/FelipeBastosxj/Sentinel5G.git && cd Sentinel5G
./scripts/quickstart.sh                              # operator + demo workload
SENTINEL5G_AI_ENGINE=true ./scripts/quickstart.sh    # also runs the real AI engine
```

**Step by step**, on an existing cluster (Kubernetes 1.26+, `kubectl`, Helm 3):

```sh
# 1. NATS (demo only: no auth) and the operator, which also installs the CRD
kubectl apply -f deployments/quickstart/nats.yaml
helm install sentinel5g charts/sentinel5g-operator \
  --namespace sentinel5g-system --create-namespace \
  --set nats.url=nats://nats.default.svc.cluster.local:4222 \
  --set nats.allowUnauthenticated=true

# 2. A demo workload and a policy protecting it
kubectl apply -f deployments/quickstart/demo-workload.yaml
kubectl apply -f config/samples/security_v1alpha1_telecomsecuritypolicy.yaml
kubectl get telecomsecuritypolicy -n telecom-core protect-amf-core -w   # → Monitoring

# 3. Publish a threat score, as the AI engine would
kubectl run nats-box --rm -it --restart=Never --image=natsio/nats-box:latest -- \
  nats pub sentinel5g.threats.scored \
  '{"sourceEventId":"demo-1","namespace":"telecom-core","podName":"amf-0","sourceIp":"203.0.113.7","score":0.93,"model":"autoencoder-v1","detectedAt":"2026-01-05T12:00:00Z"}' \
  --server nats://nats.default.svc.cluster.local:4222
```

Within a few seconds the policy reports the mitigation:

```text
NAME               PHASE        SCORE    SCORING   AGE
protect-amf-core   Mitigating   0.9300   True      1m
```

With [Istio](https://istio.io/) installed, the operator also creates an
`AuthorizationPolicy`, and requests from the demo `attacker` pod to `amf-0`
change from `200` to `403`. To run the real scoring path instead of
publishing a score by hand, install `charts/sentinel5g-ai-engine` with the
same NATS settings; it pulls the published model artifact by default.

Clean up:

```sh
helm uninstall sentinel5g sentinel5g-ai-engine -n sentinel5g-system
kubectl delete namespace sentinel5g-system telecom-core
kubectl delete -f deployments/quickstart/nats.yaml
```

Images are published to GHCR (default) and Docker Hub as multi-arch
(`linux/amd64`, `linux/arm64`) manifests; use
`--set image.repository=docker.io/312181015/sentinel5g-operator` to pull from
Docker Hub. Charts are also available as OCI artifacts at
`oci://ghcr.io/felipebastosxj/charts/sentinel5g-operator` and
`.../sentinel5g-ai-engine`. Environment-specific issues (cloud VMs,
Codespaces, corporate proxies) are covered in
[`docs/troubleshooting.md`](docs/troubleshooting.md).

---

## Production deployment

The quick start is not a production install: it runs NATS without
authentication and enables mitigation immediately.
[`docs/production-install.md`](docs/production-install.md) is the ordered
checklist for a real cluster. In summary:

1. **Bring your own NATS JetStream** with authentication or TLS. Both
   services refuse to start against an unauthenticated bus.
2. **Install the operator and the AI engine** from the OCI charts, with a
   model calibrated on your own network's normal traffic.
3. **Run detection-only first.** Keep `autoMitigate: false` and measure the
   false-positive rate on your traffic with `scripts/fpr_large_benign.py`
   and the metrics in [`docs/observability.md`](docs/observability.md).
4. **Enable eBPF enforcement** only after its preflight passes, and size
   the tunnel maps (`MAX_TUNNEL_ENTRIES` in `bpf/packet_filter.c`, 65,536 by
   default) to the real bearer count of each UPF.
5. **Turn on `autoMitigate` per policy**, with the kill switch in reach.

> **Why detection-only first.** The 1.23 × 10⁻⁵ false-positive bound was
> measured on lab traffic. It holds for a model calibrated on traffic that
> resembles the deployment's own; a pilot on your network is what confirms
> it before automated mitigation is enabled.

Current limitations and planned work are tracked in [`ROADMAP.md`](ROADMAP.md).

---

## Documentation

| Document | Contents |
|---|---|
| [Getting started](docs/getting-started.md) | Local development setup, running every layer for real |
| [Production install](docs/production-install.md) | Step-by-step checklist for a real cluster |
| [Architecture](docs/architecture.md) | Layers, components and design decisions |
| [Event model](docs/event-model.md) | `NormalizedEvent` and `ThreatScoreEvent` schemas |
| [Integrations](docs/integrations.md) | Cilium, Falco, Hubble, Istio/Linkerd, NATS security, eBPF enforcement |
| [Observability](docs/observability.md) | Metrics, alerts, SLOs and false-positive measurement |
| [Troubleshooting](docs/troubleshooting.md) | Known environment issues and fixes |
| [Research data](docs/paper-data/README.md) | Benchmarks, datasets, training and validation records |
| [Changelog](CHANGELOG.md) · [Roadmap](ROADMAP.md) | Release history and planned work |

---

## Development

Requirements: Go 1.26+, Python 3.11+, Docker, and `clang` for building the
eBPF object outside a container.

```sh
make build              # operator binary → bin/manager
make test               # Go unit tests (pkg/, api/)
make lint               # golangci-lint
make bpf                # compile bpf/packet_filter.o
make manifests          # regenerate CRDs (CI fails on drift)
make ai-engine-test     # AI engine test suite
make ci                 # vet, tests, eBPF build, AI tests, helm lint
```

The privileged eBPF tests (real attach and map writes) need root and are not
run in CI:

```sh
sudo SENTINEL5G_BPF_OBJECT=bpf/packet_filter.o go test -tags privileged ./pkg/ebpf/
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for code style, commit conventions
and pull-request requirements.

---

## Supply-chain security

Every release image is built for `linux/amd64` and `linux/arm64` from
digest-pinned base images, scanned with Trivy and published only after a
clean scan, and shipped with a CycloneDX SBOM generated by Syft. Images,
Helm charts and the model artifact are all signed keylessly with Sigstore
Cosign.

Go dependencies are checked with `govulncheck` and the Python service with
`pip-audit` and `bandit` on every change. To report a vulnerability, follow
[`SECURITY.md`](SECURITY.md) — please do not open a public issue.

---

## Contributing

Contributions from the telecom, security and cloud-native communities are
welcome. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md), and use
[GitHub Issues](https://github.com/FelipeBastosxj/Sentinel5G/issues) for bugs
and feature requests.

## License

Licensed under the [Apache License 2.0](LICENSE).
