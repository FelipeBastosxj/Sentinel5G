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

It also tracks UDP traffic on ports *other* than 2152/5060, per
(source IP, destination port), and escalates a source into a
`signaling_events` observation once a sustained burst to one such port
crosses `SCAN_EMIT_THRESHOLD` (`bpf/headers/common.h`) within the same
1-second window `track_signal_rate()` uses — previously this traffic was
completely invisible to Layer 2/3 no matter its volume. Read the honest
limit of what this catches before assuming it: keying by
`(source, port)` — deliberately, to avoid one source's unrelated,
low-volume traffic on *different* ports getting misattributed to whichever
port happened to cross a shared counter first, a cross-attribution bug
this project's own test traffic caught during development — means it
detects a sustained flood against *one* off-signaling port, not classic
low-and-slow multi-port scanning (many distinct ports, one or two packets
each never lets any single port's counter reach the threshold). Real,
useful visibility into probe traffic aimed at a single unexpected port; not
general port-scan detection.

Builds against a generated `vmlinux.h` (`make -C bpf`; see `bpf/Makefile`)
rather than plain UAPI kernel headers. Worth being precise about what this
actually buys, since it's easy to overstate: `struct ethhdr`/`iphdr`/`udphdr`
are wire-format structs whose layout is fixed by the Ethernet/IP/UDP
protocols themselves, not by kernel build config — unlike kernel-internal
structs such as `task_struct` or `sk_buff`, their field offsets can't drift
between kernel builds. So CO-RE's actual portability mechanism (BTF
relocations via `BPF_CORE_READ()`/`preserve_access_index`) has nothing to
protect here, and `bpf/packet_filter.c` doesn't use it — every packet-header
field read is a plain access, same as before. The real, concrete win is
dropping the UAPI-header build dependency: `vmlinux.h` already declares
these types itself, so the build no longer needs
`linux-libc-dev`/`linux-headers-$(uname -r)` installed, or the
`<asm/types.h>` multiarch include-path workaround the old UAPI approach
carried. `vmlinux.h` itself is generated at build time from whichever
machine is compiling it (gitignored, not committed — a 3.6MB/172k-line file
generated from one specific host's kernel isn't obviously more portable
committed than not), consistent with CO-RE's actual model: the compiled
object's BTF relocation records — where they exist — get resolved against
the real *target* kernel's BTF at load time, not the build machine's.

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

`ThreatScoreWatcher` calls `Block`/`Quarantine` but never `Unblock`/`Release`
itself — a later low score only moves `Status.Phase` back to `Monitoring`,
it does not restore traffic. Reversal instead happens automatically,
separately, on a timer: `Reconciler.tryDeEscalate` unblocks/releases once a
policy has gone `DeEscalationDwell` (`DE_ESCALATION_DWELL`, default 5m)
without a *new* mitigation. This is a quiet-period timer on
`Status.LastMitigationTime`, not "wait for that source's score to sustain
low" — once a source is blocked, `bpf/packet_filter.c`'s XDP program drops
every subsequent packet from it before any protocol parsing, so a blocked
source produces zero further `ThreatScoreEvent`s and there is nothing
further to sample. A new mitigation for *any* source under a policy resets
the quiet-period clock for *every* currently mitigated source under it, not
just the new one — deliberately conservative (don't start unwinding
anything while the policy is still actively seeing new threats), and
resistant to a trivial "send one benign packet to get unblocked" evasion,
since nothing in the event stream can make time move faster. See
`ROADMAP.md` Phase 1's "Automatic de-escalation" entry for the full
reasoning, including the mesh-only case
(`isolatePod: true, ebpfBlock: false`, where the source *isn't* silenced by
XDP) sharing this same timer rather than a separate per-source one.

Deleting the `TelecomSecurityPolicy` itself is different: a finalizer
(`security.sentinel5g.io/finalizer`, `pkg/controller.Reconciler.finalize`)
releases any mesh quarantine and unblocks every source IP the policy pushed
into the eBPF blocklist (tracked in `Status.BlockedSourceIPs`) before the
object is actually removed — so retiring a policy no longer requires a
manual `kubectl delete authorizationpolicy` / operator restart to clean up
after it. `Mesh.Release`/`Blocklist.Unblock` are called unconditionally on
deletion (both documented idempotent), not gated on whether mitigation ever
actually fired.

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
