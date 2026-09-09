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

- [x] General multi-port scan detection. A second detector (`port_scan`/
      `track_port_scan()` in `bpf/packet_filter.c`) tracks a bounded,
      deduplicated set of distinct destination ports per source over a
      30s window, separate from `scan_rate`'s single-port flood check.
- [ ] Cilium-native capture path (Hubble or a custom BPF program) as an
      alternative to standalone XDP.
- [ ] Falco output bridging into `NormalizedEvent`.
- [ ] Additional `pkg/mesh.Adapter` implementations (Linkerd, Cilium mesh).
- [x] Prometheus instrumentation for the AI engine. HTTP mode gets
      `/metrics` via `prometheus-fastapi-instrumentator` on its existing
      app; NATS worker mode (no app of its own) gets a dedicated
      `prometheus_client` server on `AI_ENGINE_METRICS_ADDR`.
- [x] VLAN (802.1Q) support in `bpf/packet_filter.c` — a single 802.1Q tag
      is transparently unwrapped before inspection, with the VLAN ID
      carried through to `signaling_events`. QinQ (double-tagged) frames
      remain out of scope.
- [x] IPv6 support. A parallel set of maps/ringbuf in `bpf/packet_filter.c`
      (`*_v6`) and a second ring-buffer reader in `pkg/ebpf.Loader` — not a
      unified 128-bit-capable scheme — so IPv4 support is untouched. IPv6
      extension headers between the fixed header and UDP are not walked.
- [x] Real multi-node eBPF coverage: `daemonset.enabled` in the Helm chart
      renders the operator as a `DaemonSet` (guaranteeing one Pod per node)
      instead of a `Deployment`, with the `hostNetwork: true` +
      `dnsPolicy: ClusterFirstWithHostNet` this needs for `--bpf-interface`
      to actually see the node's real traffic rather than the Pod's own
      veth. A real, explicit security trade-off — see
      `docs/integrations.md` — so it's its own opt-in, not folded into
      `ebpf.enabled`. The `podAntiAffinity` workaround remains documented
      for anyone who can't accept `hostNetwork`.
- [x] `NetworkPolicy` for the NATS bus (optional/opt-in via
      `networkPolicy.nats.enabled` in the Helm chart).
- [x] `PodDisruptionBudget` and a `Service`/`ServiceMonitor` for the
      operator's metrics port. The `Service` is always rendered;
      `podDisruptionBudget.enabled`/`serviceMonitor.enabled` are opt-in.
- [x] `pkg/controller.PodIPIndex` grows unbounded — needs eviction on Pod
      deletion. Fixed via a reverse index (`byPod`) so `Remove` can find a
      deleted Pod's last known IP from a bare `NotFound` response, which
      carries none.
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
