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

### Prerequisites

* Kubernetes cluster (v1.26+)
* Kernel 5.4+ with eBPF support enabled
* `kubectl` and `helm` v3 installed

### 1. Install via Helm

No hosted chart repository is published yet — install straight from a clone of
this repo (see [`docs/getting-started.md`](docs/getting-started.md) for the
full local walkthrough):

git clone https://github.com/FelipeBastosxj/Sentinel5G.git
cd Sentinel5G

helm lint charts/sentinel5g-operator
helm install sentinel5g charts/sentinel5g-operator --namespace sentinel5g-system --create-namespace

### 2. Apply a Security Policy

Create a file named `telecom-policy.yaml`:

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
    autoMitigate: true
  actions:
    ebpfBlock: true
    isolatePod: true

Apply the policy:

kubectl apply -f telecom-policy.yaml

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