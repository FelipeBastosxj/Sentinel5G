# Roadmap

Sentinel5G is at an early, foundational stage: the four-layer architecture
in [`docs/architecture.md`](docs/architecture.md) works end to end, but
several pieces are intentionally scoped down for a first release. Read
alongside `docs/getting-started.md`, `docs/integrations.md`, and
`CONTRIBUTING.md`.

## Phase 0 — Foundations

- [x] `TelecomSecurityPolicy` CRD, reconciler, in-memory policy index.
- [x] `bpf/packet_filter.c`: XDP capture, blocklist map, per-source
      signaling-rate counter.
- [x] Autoencoder AI engine: synthetic dataset, training, ONNX export,
      HTTP + NATS-worker inference server.
- [x] Closed-loop mitigation: eBPF blocklist push + Istio quarantine.
- [x] Helm chart, kustomize manifests, CI (lint/test/SBOM/scan/sign).
- [x] Optional NATS auth/TLS, security scanners in CI.
- [x] Layer 1 → Layer 2 bridge (`pkg/ingestion`): real kernel-observed
      packets now become `NormalizedEvent`s, scored by the real model —
      verified against a live Open5GS+UERANSIM core.

## Phase 1 — Real-world signal

- [x] **Real training dataset.** Real GTP-U captures from a live
      Open5GS+UERANSIM core replace the synthetic-only dataset (1,889
      normal / 32,235 anomalous samples). Found a real gap: the only
      fully-real, production-observable anomaly type (an in-tunnel flood)
      scores *below* normal traffic and isn't caught at any threshold.
      Details: `docs/paper-data/02-ai-training-inference.md` §2.4.
- [x] **Off-signaling-port visibility.** Layer 1 now detects a sustained
      flood against one off-signaling port (`scan_rate` map in
      `bpf/packet_filter.c`). Doesn't catch classic multi-port scanning —
      see Phase 2. Also fixed a real, pre-existing bug found while testing
      this: `pkg/ebpf.Loader.SignalRate()` was silently broken and always
      returned 0 for every real event.
- [x] `controller-gen` wired into `make manifests`; CI fails on drift
      between the Go types and generated CRD/deepcopy output.
- [x] `bpf/packet_filter.c` builds against a small hand-maintained header
      (`bpf/headers/vmlinux_min.h`) instead of UAPI kernel headers or a
      host-BTF-generated `vmlinux.h` — no `bpftool`/kernel-headers
      dependency, and it now builds inside a plain `docker build` too (see
      the next item).
- [x] Finalizer-based cleanup: deleting a policy now releases its mesh
      quarantine and unblocks its eBPF-blocked IPs automatically.
- [x] Automatic de-escalation: mitigations reverse on their own after a
      quiet period (`DE_ESCALATION_DWELL`, default 5m) instead of needing
      manual cleanup.
- [x] Fixed the operator getting stuck at `Phase: Monitoring` forever when
      `MESH_ADAPTER=istio` (the default) but Istio isn't installed — it now
      falls back to a no-op mesh adapter, same as eBPF already does when
      not attached.
- [x] `scripts/quickstart.sh`: one command from a fresh clone to a real
      mitigation firing, using published images. Installs `kind`/`helm` if
      missing and works around a real DNS-resolution failure common to
      `kind` on Docker (any host, not just one cloud sandbox).
- [x] The compiled eBPF object is now baked into the published operator
      image at build time (`Dockerfile`'s `bpf-builder` stage) — previously
      nothing put it there, so `ebpf.enabled: true` silently no-op'd on
      every real deployment. `ebpf.enabled: true` now also wires the
      required Linux capabilities automatically, and attach failures are
      classified (missing object / insufficient privilege / unknown
      interface / incompatible kernel) instead of a bare error string.
- [x] Real e2e test in CI (`.github/workflows/e2e.yml`): builds each PR's
      own images and runs `scripts/quickstart.sh` against a real `kind`
      cluster — the class of bug the Istio-CRD fallback fix above was only
      found by running by hand is now caught automatically.
- [x] `scripts/quickstart.sh` preflight checks (network egress, RBAC) with
      root-caused error messages, plus `docs/troubleshooting.md`
      consolidating the DNS/RBAC/egress/ARM64/eBPF causes reported from
      cloud VMs and Codespaces that don't show up on a local dev machine.
- [x] Images published to Docker Hub alongside GHCR, both as multi-arch
      (`linux/amd64`+`linux/arm64`) manifests, signed with cosign.

## Phase 2 — Deeper integrations

- [ ] General multi-port scan detection. `scan_rate` only catches a flood
      against *one* port; needs a distinct-port-count structure per source
      to catch classic low-and-slow scanning.
- [ ] Cilium-native capture path (Hubble or a custom BPF program) as an
      alternative to standalone XDP.
- [ ] Falco output bridging into `NormalizedEvent`.
- [ ] Additional `pkg/mesh.Adapter` implementations (Linkerd, Cilium mesh).
- [ ] Prometheus instrumentation for the AI engine.
- [ ] VLAN (802.1Q) support in `bpf/packet_filter.c` — tagged frames
      bypass inspection entirely today.
- [ ] IPv6 support — `pkg/ebpf` and the XDP program are IPv4-only.
- [ ] Real multi-node eBPF coverage: the operator is a `Deployment`, not a
      `DaemonSet`, so `ebpf.enabled: true` only protects whichever node(s)
      it lands on. `docs/integrations.md` documents a `podAntiAffinity`
      workaround; a real `DaemonSet` option is still open.
- [ ] `NetworkPolicy` for the NATS bus (optional/opt-in).
- [ ] `PodDisruptionBudget` and a `Service`/`ServiceMonitor` for the
      operator's metrics port.
- [ ] `pkg/controller.PodIPIndex` grows unbounded — needs eviction on Pod
      deletion.
- [ ] `k8s.io/*`/`controller-runtime` dependency bump — currently pinned
      to the Kubernetes 1.30 line, needs its own regression pass.
- [ ] Path-based CI job filtering (skip unrelated jobs on single-toolchain
      PRs).
- [ ] Dockerfile hardening: pin base images to a digest, add
      `HEALTHCHECK`.

## Phase 3 — Scale & multi-cluster

- [ ] Multi-cluster policy propagation.
- [ ] Load-testing harness for the <0.2ms/packet and mitigation-latency
      targets in `docs/observability.md`. Also where to settle two open
      questions: whether the `blocklist` eBPF map needs LRU semantics, and
      whether the `signaling_events` ring buffer holds up under real
      telecom-scale volume.
- [ ] Poetry-based lockfile for `cmd/ai-engine`, if wanted.

Have an idea that isn't here? Open an issue — see
[`CONTRIBUTING.md`](CONTRIBUTING.md).
