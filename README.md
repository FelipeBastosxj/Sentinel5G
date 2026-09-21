# Sentinel5G: AI-Driven Threat Detection & Automated Mitigation for Cloud-Native Telecom

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![eBPF Powered](https://img.shields.io/badge/eBPF-Enabled-orange)](https://ebpf.io/)
[![Status: reference implementation](https://img.shields.io/badge/status-reference%20implementation-lightgrey)](ROADMAP.md)

**Sentinel5G** is an open-source, cloud-native security operator designed to secure critical 5G/LTE and telecom containerized workloads. By combining kernel-level inspection via **eBPF**, real-time **unsupervised AI threat detection**, and **automated closed-loop remediation**, Sentinel5G detects signaling storms, protocol anomalies, and zero-day threats in microseconds without sidecar performance penalties.

---

## 💡 Key Features

* **Kernel-Level Visibility (eBPF):** Non-intrusive packet and event inspection for 3GPP/SIP/SMPP protocols at the XDP/TC layer — including parsing the GTP-U header itself (3GPP TS 29.281) to rate traffic **per tunnel (TEID)**, which is what tells one flooding subscriber apart from the gNB every subscriber shares.
* **AI-Driven Anomaly Detection:** Machine learning inference engine (ONNX Runtime) that learns a per-workload signaling baseline and flags deviations as zero-day candidates. The published model trains on **real** GTP-U captured from a live Open5GS+UERANSIM core ([`docs/paper-data/real-dataset/`](docs/paper-data/real-dataset/README.md)) — read that directory's stated scope limits before treating it as a model of your own network. Paired with an explicit non-ML detector for the one anomaly class the autoencoder provably cannot catch; the measurement is in [`docs/paper-data/02-ai-training-inference.md`](docs/paper-data/02-ai-training-inference.md) §2.5–§2.6.
* **Closed-Loop Automation:** Automated network isolation and eBPF-level packet dropping triggered instantly upon anomaly detection — droppable **per GTP-U tunnel**, so a flooding subscriber is cut off without taking down every other subscriber behind the same gNB.
* **Kubernetes-Native:** Full declarative control via custom CRDs (`TelecomSecurityPolicy`).
* **Zero-Trust Telecom Architecture:** Aligned with CISA and NIST guidelines for U.S. Critical Infrastructure Security.

---

## 🏛️ System Architecture

+-------------------------------------------------------------------+
|                       Telecom Pod (GTP/SIP)                       |
+-------------------------------------------------------------------+
                                  | (eBPF Probe)
                                  v
+------------------+     +------------------+     +-----------------+
| High-Performance | --> |  ONNX AI Engine  | --> | K8s Security    |
| Kernel Capture   |     | (Autoencoders)   |     | Operator (Go)   |
+------------------+     +------------------+     +-----------------+
                                                           |
                                    (Instant XDP Drop / Mesh Isolation)
                                                           v
                                                 [Mitigated Threat]

---

## 🚀 Quick Start

Go from zero to watching a real automated mitigation fire, using only the
already-published `ghcr.io/felipebastosxj/*:v0.2.2` images — no local Go/Python
build required. Verified end to end against a real k3s cluster; see
[`docs/getting-started.md`](docs/getting-started.md) for the deeper walkthrough
that runs every layer (real eBPF capture + the AI engine) instead of hand-
publishing the AI engine's output like step 3 below does. `scripts/quickstart.sh`
can do the real thing too (`SENTINEL5G_AI_ENGINE=true`, which CI's e2e runs)
from the first release that publishes the model artifact.

Images are published to [GHCR](https://github.com/FelipeBastosxj?tab=packages)
and, from the next tagged release on, also to [Docker
Hub](https://hub.docker.com/r/312181015/sentinel5g-operator) — both as
multi-arch (`linux/amd64`+`linux/arm64`) manifests. Pull whichever registry
your network allows; pass `--set
image.repository=docker.io/312181015/sentinel5g-operator` to the Helm
install below to use Docker Hub instead of the GHCR default.

**Prefer one command?** `./scripts/quickstart.sh` runs every step below
automatically (installing `kubectl`/`kind`/`helm` for its own use if you
don't have them, and working around a real `kind`-on-Docker DNS-resolution
failure mode found the hard way — not specific to any one machine or cloud
sandbox). All it needs is `docker` (running, and usable by your user) and
`curl`; everything else it handles, without installing anything into your
system: the CLIs it downloads, its kubeconfig and helm's cache all live
beside the clone — or in a per-user directory when the clone itself isn't
writable, e.g. a checkout unpacked with `sudo` or a shared path on a VM.

The steps below are what it's actually running, for anyone who wants to
follow along or adapt them. Hitting something environment-specific (a
cloud VM, a Codespace, a corporate network)? Check
[`docs/troubleshooting.md`](docs/troubleshooting.md) before assuming it's a
Sentinel5G bug — most of what's been found there so far is generic
kind/network/RBAC friction, not code.

### Prerequisites

* A local Kubernetes cluster (`kind`, `minikube`, or `k3s`) — v1.26+
* `kubectl` and `helm` v3
* *(optional, for a real deny instead of just watching the policy's phase
  change)* [Istio](https://istio.io/) on that cluster — `istioctl install
  --set profile=demo -y`. Without it, step 3 still reaches `Phase:
  Mitigating`, it just won't produce a real 403.

Both charts are published as OCI artifacts (`helm install sentinel5g
oci://ghcr.io/felipebastosxj/charts/sentinel5g-operator --version 0.2.2`,
likewise `.../charts/sentinel5g-ai-engine`; no `helm repo add` needed) but this walkthrough also uses other files from
the repo (the demo NATS/workload manifests, the sample policy) — clone it
first; every command below is run from its root:

```sh
git clone https://github.com/FelipeBastosxj/Sentinel5G.git
cd Sentinel5G
```

### 1. Bring up NATS JetStream and the operator

```sh
kubectl apply -f deployments/quickstart/nats.yaml
helm install sentinel5g charts/sentinel5g-operator --namespace sentinel5g-system --create-namespace \
  --set nats.url=nats://nats.default.svc.cluster.local:4222 \
  --set nats.allowUnauthenticated=true
```

This also installs the `TelecomSecurityPolicy` CRD — no separate `kubectl
apply` needed for it. (`deployments/quickstart/nats.yaml` is a minimal
single-node JetStream instance with no auth, for this demo only — the
operator otherwise refuses to start against an unauthenticated bus, see
[`docs/production-install.md`](docs/production-install.md) for a
production-appropriate NATS setup with auth/TLS and the rest of a real
install, not just this demo.)

### 2. Protect a demo workload

```sh
kubectl apply -f deployments/quickstart/demo-workload.yaml
kubectl apply -f config/samples/security_v1alpha1_telecomsecuritypolicy.yaml
```

Confirm the demo `amf-0` pod is reachable before any mitigation fires:

```sh
kubectl exec -n telecom-core attacker -- curl -s -o /dev/null -w '%{http_code}\n' http://amf-0
# 200
```

The policy reaches `Phase: Monitoring` within a few seconds:

```sh
kubectl get telecomsecuritypolicy -n telecom-core protect-amf-core -w
```

### 3. Simulate an attack and watch it get mitigated

In production this is published by the AI engine after scoring real
signaling traffic (see `docs/getting-started.md`); publish one by hand here
to see Layer 4 close the loop on its own:

```sh
kubectl run nats-box --rm -it --restart=Never --image=natsio/nats-box:latest -- \
  nats pub sentinel5g.threats.scored '{"sourceEventId":"demo-1","namespace":"telecom-core","podName":"amf-0","sourceIp":"203.0.113.7","score":0.93,"model":"autoencoder-v1","detectedAt":"2026-01-05T12:00:00Z"}' \
  --server nats://nats.default.svc.cluster.local:4222
```

Within a few seconds, the policy shows real automated mitigation:

```sh
kubectl get telecomsecuritypolicy -n telecom-core protect-amf-core
# NAME               PHASE        SCORE    SCORING   AGE
# protect-amf-core   Mitigating   0.9300   True      1m
```

`SCORING` is whether a score has *ever* arrived — with the AI engine
missing it reads `False` after a grace period, which is the difference
between "quiet cluster" and "half the system was never deployed". To run
the real scoring half instead of publishing its output by hand, install
the AI engine chart (it pulls a published model artifact by default and
refuses to install without a model source) and publish a
`NormalizedEvent` for it to score — `docs/getting-started.md` step 3, or
`SENTINEL5G_AI_ENGINE=true ./scripts/quickstart.sh` once a release has
published `sentinel5g-model`:

```sh
helm install sentinel5g-ai-engine charts/sentinel5g-ai-engine --namespace sentinel5g-system \
  --set nats.url=nats://nats.default.svc.cluster.local:4222 \
  --set nats.allowUnauthenticated=true
```

and — if Istio is installed — the same `curl` that returned `200` a moment
ago now gets refused by a real `AuthorizationPolicy` the operator just
created:

```sh
kubectl get authorizationpolicy -n telecom-core
kubectl exec -n telecom-core attacker -- curl -s -o /dev/null -w '%{http_code}\n' http://amf-0
# 403
```

### Cleanup

```sh
helm uninstall sentinel5g sentinel5g-ai-engine -n sentinel5g-system
kubectl delete namespace sentinel5g-system telecom-core
kubectl delete -f deployments/quickstart/nats.yaml
```

---

## 📊 Design Targets

Two of the three are now **measured**, with a reproducible harness
(`scripts/loadtest/`, results in
[`docs/paper-data/01-performance-benchmarks.md`](docs/paper-data/01-performance-benchmarks.md)
§1.4). None is enforced by CI, which would need real hardware rather than a
synthetic replay — read that directory's README for what the methods can
and cannot see before quoting the numbers.
See [`docs/observability.md`](docs/observability.md) for how they're
measured and tracked in a running deployment, and
[`scripts/loadtest/README.md`](scripts/loadtest/README.md) for how to
reproduce the numbers yourself.

* **Latency Impact:** < 0.2ms added per packet at the XDP layer — measured at **~195–211 ns** for a full GTP-U parse.
* **Mitigation Speed:** single-digit-millisecond closed-loop response once a threat score crosses threshold — measured at **~2 ms** end to end.
* **CPU Overhead:** < 2% per worker node at 100k req/sec — still a target, not measured.

---

## 🤝 Contributing

We welcome contributions from the telecom, cybersecurity, and cloud-native communities! 

* Read our [Contribution Guidelines](CONTRIBUTING.md) to get started.
* Report bugs or request features via [GitHub Issues](https://github.com/FelipeBastosxj/Sentinel5G/issues).

---

## 📄 License

This project is licensed under the **Apache License 2.0** - see the [LICENSE](LICENSE) file for details.