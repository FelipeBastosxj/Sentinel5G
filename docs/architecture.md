# Architecture

Sentinel5G is a closed-loop detection-and-remediation mesh built from four
layers. Each layer is independently replaceable behind the interfaces
described below, which is what lets the reference implementation stay small
while the architecture scales to real telecom deployments.

```
+-----------------------------------------------------------------------------------+
|                            LAYER 1: CAPTURE & KERNEL                              |
|       Pod/Worker Node Telecom (3GPP/SIP/SMPP) ---> eBPF Probe (Cilium/Falco)       |
+-----------------------------------------------------------------------------------+
                                          |
                                          v (Event Stream)
+-----------------------------------------------------------------------------------+
|                          LAYER 2: INGESTION & PIPELINE                            |
|              Fluent Bit / NATS JetStream ---> Normalization & Extraction          |
+-----------------------------------------------------------------------------------+
                                          |
                                          v (Feature Vector)
+-----------------------------------------------------------------------------------+
|                        LAYER 3: AI ENGINE / ANOMALY DETECTION                      |
|              ONNX Runtime (Python) ---> Inference Engine (Autoencoder)            |
+-----------------------------------------------------------------------------------+
                                          |
                                          v (Threat Score)
+-----------------------------------------------------------------------------------+
|                       LAYER 4: CONTROLLER & AUTOMATION                            |
|           K8s Operator (Go) ---> eBPF Reconfiguration / Service Mesh / CNI         |
+-----------------------------------------------------------------------------------+
```

## Technology stack

| Layer                | Technologies                                        | Role |
|-----------------------|------------------------------------------------------|------|
| Kernel & Capture      | C/eBPF, Falco, Cilium                                | High-performance in-kernel capture of GTP-U/SIP/HTTP2 (5G SBA) traffic with no meaningful per-packet latency penalty. |
| Inference & AI Engine | Python, PyTorch, ONNX Runtime                       | Lightweight, low-latency inference. The model trains in PyTorch and is exported to ONNX for production inference. |
| Controller/Operator   | Go, controller-runtime (Kubebuilder patterns)       | Native Kubernetes reconciliation of `TelecomSecurityPolicy` CRDs, managed via `kubectl`. |
| Pipeline & Messaging  | NATS JetStream                                      | Real-time telemetry/threat-score transport between the capture and inference layers. |
| Security & CI/CD      | Cosign, Syft (SBOM), GitHub Actions, Trivy           | SAST, SBOM generation, and container signing for every published image. |

## Component responsibilities

### Layer 1 — Non-invasive capture (`bpf/`)

`bpf/packet_filter.c` is an XDP program attached at a telecom-facing pod's
host network interface. It inspects Ethernet/IPv4/UDP headers looking for
GTP-U (port 2152) and SIP (port 5060) traffic without requiring a sidecar in
every pod. It maintains a coarse per-source-IP signaling-rate counter
(storm detection) and consults a `blocklist` BPF map populated exclusively
by the operator (`pkg/ebpf`), giving Layer 4 a way to drop malicious traffic
at the kernel/NIC level.

This reference implementation uses plain UAPI kernel headers rather than a
generated `vmlinux.h`, so it builds against any recent kernel without first
extracting BTF from the target host. For full CO-RE portability across
differing kernel struct layouts, generate one and switch to
`BPF_CORE_READ()`:

```sh
bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/headers/vmlinux.h
```

### Layer 2 — Ingestion & pipeline (`pkg/events`, `pkg/ingestion`)

`pkg/events` defines the wire schema (`NormalizedEvent`, `ThreatScoreEvent`)
shared between the capture side, the AI engine, and the operator, and wraps
a NATS JetStream connection for both. See [event-model.md](event-model.md)
for the full schema and subject list.

`pkg/ingestion` is the actual bridge from Layer 1 to this schema: its
`Publisher` (a `manager.Runnable`, started by `cmd/operator` alongside
`ThreatScoreWatcher`) reads `bpf/packet_filter.c`'s `signaling_events` ring
buffer via `pkg/ebpf.EventSource`, resolves each observation's source IP to
the Pod that owns it via `pkg/controller.PodIPIndex` (kept in sync by a
cluster-wide `PodIPIndexer` watching all Pods, not just ones a policy
targets — attribution has to work before a policy exists to match against),
and publishes the result on `NATS_EVENTS_SUBJECT`. Traffic from outside the
cluster still produces an event with an empty namespace/Pod name rather than
being dropped — an external probe against a telecom-facing Service is
exactly the traffic this project exists to catch. Only runs when eBPF is
actually attached, the same "degrade gracefully" rule `EbpfBlock` already
follows.

### Layer 3 — AI engine (`cmd/ai-engine`)

An unsupervised autoencoder (`cmd/ai-engine/sentinel_ai/model.py`) learns the
normal signaling baseline for a workload; deviations above a configurable
threshold produce a `ThreatScoreEvent`. Training happens in PyTorch; the
exported ONNX graph is what actually serves inference
(`sentinel_ai/server.py`, via `onnxruntime`), keeping the production
dependency surface — and the Python GIL — out of the hot path.

### Layer 4 — Orchestration & automation (`pkg/controller`, `pkg/ebpf`, `pkg/mesh`)

The `TelecomSecurityPolicy` CRD (`api/v1alpha1`) declares which workloads a
policy protects, how sensitive its threat detection is, and which
mitigations are permitted. `pkg/controller.ThreatScoreWatcher` consumes
scored threats, matches them against active policies, and — when a policy's
`autoMitigate` is enabled — drives:

- `pkg/ebpf`: pushes the offending source IP into the XDP blocklist map.
- `pkg/mesh`: quarantines the workload via an Istio `AuthorizationPolicy`
  (or a no-op adapter in detection-only deployments).

Both actions are independent: a cluster without a service mesh can still run
with `ebpfBlock: true, isolatePod: false`, and vice versa.

Mitigation is currently one-directional by design: `ThreatScoreWatcher` calls
`Block`/`Quarantine` but never `Unblock`/`Release` on its own. A later low
score only moves `Status.Phase` back to `Monitoring` — it does not restore
traffic. This is a deliberate fail-safe (an automated system should not be
the one deciding an active threat has stopped being one), not an oversight;
reversing a mitigation today is a manual `kubectl delete
authorizationpolicy` / operator restart. See `ROADMAP.md` Phase 1 for the
plan to make de-escalation itself a considered, hysteresis-based decision
rather than removing the fail-safe outright.

## Design principles

- **Zero-Trust by default.** The operator's default `securityContext` drops
  all Linux capabilities; eBPF blocklist enforcement is an explicit opt-in
  (`ebpf.enabled` in the Helm chart) because it needs `CAP_BPF`/`CAP_NET_ADMIN`.
- **Closed-loop, but reversible.** Every automated action lives behind a
  named adapter (`pkg/ebpf.BlocklistUpdater`, `pkg/mesh.Adapter`) so it can
  be swapped for a no-op in detection-only deployments, or a different mesh
  implementation entirely.
- **Latency budget.** The target is <0.2ms added latency per packet at the
  XDP layer and single-digit-millisecond closed-loop mitigation once a
  threat score crosses threshold — see `docs/observability.md` for how this
  is measured.
