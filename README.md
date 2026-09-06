# Sentinel5G: AI-Driven Threat Detection & Automated Mitigation for Cloud-Native Telecom

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![eBPF Powered](https://img.shields.io/badge/eBPF-Enabled-orange)](https://ebpf.io/)
[![Status: reference implementation](https://img.shields.io/badge/status-reference%20implementation-lightgrey)](ROADMAP.md)

**Sentinel5G** is an open-source, cloud-native security operator designed to secure critical 5G/LTE and telecom containerized workloads. By combining kernel-level inspection via **eBPF**, real-time **unsupervised AI threat detection**, and **automated closed-loop remediation**, Sentinel5G detects signaling storms, protocol anomalies, and zero-day threats in microseconds without sidecar performance penalties.

---

## 💡 Key Features

* **Kernel-Level Visibility (eBPF):** Non-intrusive packet and event inspection for 3GPP/SIP/SMPP protocols at the XDP/TC layer.
* **AI-Driven Anomaly Detection:** Machine learning inference engine (ONNX Runtime) that learns a per-workload signaling baseline and flags deviations as zero-day candidates. The shipped model trains on a synthetic dataset (see [`docs/getting-started.md`](docs/getting-started.md)); validating against real GTP-U/SIP/SMPP traffic is tracked in [`ROADMAP.md`](ROADMAP.md).
* **Closed-Loop Automation:** Automated network isolation and eBPF-level packet dropping triggered instantly upon anomaly detection.
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
already-published `ghcr.io/felipebastosxj/*:v0.1.0` images — no local Go/Python
build required. Verified end to end against a real k3s cluster; see
[`docs/getting-started.md`](docs/getting-started.md) for the deeper walkthrough
that runs every layer (real eBPF capture + the AI engine) instead of hand-
publishing the AI engine's output like step 3 below does.

### Prerequisites

* A local Kubernetes cluster (`kind`, `minikube`, or `k3s`) — v1.26+
* `kubectl` and `helm` v3
* *(optional, for a real deny instead of just watching the policy's phase
  change)* [Istio](https://istio.io/) on that cluster — `istioctl install
  --set profile=demo -y`. Without it, step 3 still reaches `Phase:
  Mitigating`, it just won't produce a real 403.

No hosted chart repository is published yet, so clone the repo first — every
command below is run from its root:

```sh
git clone https://github.com/FelipeBastosxj/Sentinel5G.git
cd Sentinel5G
```

### 1. Bring up NATS JetStream and the operator

```sh
kubectl apply -f deployments/quickstart/nats.yaml
helm install sentinel5g charts/sentinel5g-operator --namespace sentinel5g-system --create-namespace \
  --set nats.url=nats://nats.default.svc.cluster.local:4222
```

This also installs the `TelecomSecurityPolicy` CRD — no separate `kubectl
apply` needed for it. (`deployments/quickstart/nats.yaml` is a minimal
single-node JetStream instance for this demo only — see
[`docs/integrations.md`](docs/integrations.md) for a production-appropriate
NATS setup with auth/TLS.)

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
# Phase: Mitigating   Score: 0.9300
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
helm uninstall sentinel5g -n sentinel5g-system
kubectl delete namespace sentinel5g-system telecom-core
kubectl delete -f deployments/quickstart/nats.yaml
```

---

## 📊 Design Targets

These are the latency/overhead budgets the architecture is designed around,
**not** measured benchmarks — no load-testing harness is wired into CI yet.
See [`docs/observability.md`](docs/observability.md) for how they're intended
to be measured and tracked, and [`ROADMAP.md`](ROADMAP.md) for the harness
that will validate them under sustained load.

* **Latency Impact:** < 0.2ms added per packet at the XDP layer.
* **CPU Overhead:** < 2% per worker node at 100k req/sec (target).
* **Mitigation Speed:** single-digit-millisecond closed-loop response once a threat score crosses threshold (target).

---

## 🤝 Contributing

We welcome contributions from the telecom, cybersecurity, and cloud-native communities! 

* Read our [Contribution Guidelines](CONTRIBUTING.md) to get started.
* Report bugs or request features via [GitHub Issues](https://github.com/FelipeBastosxj/Sentinel5G/issues).

---

## 📄 License

This project is licensed under the **Apache License 2.0** - see the [LICENSE](LICENSE) file for details.