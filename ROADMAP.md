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
- [x] Cilium-native capture path as an alternative to standalone XDP.
      `pkg/hubble.Observer` (opt-in via `HUBBLE_ADDR`, wired into
      `cmd/operator`) streams flows from Cilium's own Hubble Observer gRPC
      API (typically Hubble Relay) instead of attaching `packet_filter.c`
      to the same interface a second time, and republishes matching
      telecom-signaling flows as `NormalizedEvent`s on the same NATS
      subject the eBPF path uses. Verified via an in-process gRPC server
      (bufconn) exercising the real generated client/server wire path
      against a live NATS server; not verified against a live Hubble/
      Cilium deployment (this project's real test cluster runs flannel,
      not Cilium) — see `docs/integrations.md` for the full, explicit
      caveat.
- [x] Falco output bridging into `NormalizedEvent`. A new, separate binary
      (`cmd/falco-bridge`, backed by `pkg/falco.Bridge`) receives Falco's
      own `http_output` JSON alerts and publishes them onto the same NATS
      subject the eBPF path uses — the two Layer 1 sources are independent
      and not mutually exclusive. Verified end to end against a real NATS
      server and inside a real built container (`HEALTHCHECK` reports
      healthy); a live Falco daemon's own alerts were not exercised (this
      WSL2 environment's custom kernel doesn't reliably support Falco's
      kernel probe) — see `docs/integrations.md` for the full, explicit
      caveat.
- [x] Additional `pkg/mesh.Adapter` implementations: `CiliumAdapter`
      (`MESH_ADAPTER=cilium`, `cilium.io/v2` `CiliumNetworkPolicy` with
      `ingressDeny`/`egressDeny: [{from,to}Entities: ["all"]]`) and
      `LinkerdAdapter` (`MESH_ADAPTER=linkerd`, `policy.linkerd.io/v1beta3`
      `Server` per declared container port, `accessPolicy: deny`) — both
      verified against a real API server with the real CRD schema
      installed. Linkerd's guarantee is deliberately weaker than the other
      two (port-scoped, declared-ports-only best-effort, not a true
      workload-wide deny) since `Server` has no wildcard-all-ports form and
      `Adapter.Quarantine`'s signature carries no port information — see
      `docs/integrations.md` for the full trade-off and what a stronger
      guarantee would need (a port-aware `Adapter` signature, affecting
      every adapter and call site).
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
- [x] `k8s.io/*`/`controller-runtime` dependency bump — from the
      Kubernetes 1.30 line to 1.37 (`k8s.io/api`/`apimachinery`/`client-go`
      v0.37.0, `controller-runtime` v0.25.0 — the exact pairing
      `controller-runtime` v0.25.0 itself declares). Found and fixed a real
      regression along the way: `api/v1alpha1/groupversion_info.go`'s
      `scheme.Builder` is now deprecated in favor of plain
      `runtime.SchemeBuilder`, but the replacement needs an explicit
      `metav1.AddToGroupVersion` call `scheme.Builder` used to do
      automatically — missing it doesn't fail to compile, only at runtime
      against a real API server (`CreateOptions is not suitable for
      converting...`), caught by `pkg/controller/envtest_test.go`, not the
      fake-client unit tests. `setup-envtest`'s pinned version bumped to
      1.36.2 (`envtest` binaries lag client-library releases slightly; this
      is the newest one available) to match.
- [x] Path-based CI job filtering (skip unrelated jobs on single-toolchain
      PRs). A `changes` job (`dorny/paths-filter`) gates `ci.yml`'s four
      jobs; editing the workflow file itself always runs all of them.
- [x] Dockerfile hardening: base images pinned to their multi-arch
      manifest-list digest (`.github/dependabot.yml` keeps them current).
      `HEALTHCHECK` added to both images — the operator's distroless final
      stage has no shell, so `manager healthcheck` is a real subcommand
      hitting its own `/healthz` over HTTP; the AI engine's
      `sentinel_ai.healthcheck` module picks `/healthz` or the metrics
      server's `/metrics` depending on `AI_ENGINE_MODE`, since NATS-worker
      mode has no HTTP app of its own. Verified end-to-end with real
      `docker build`/`docker run` against the live k3s+NATS environment —
      both images report `"Status":"healthy"`.

## Phase 2.5 — Production readiness

Everything above is about making each layer *work*. This section is the
honest gap between that and installing Sentinel5G on a cluster that
carries real traffic — raised by the first person to ask "can I put this
in production?", and answered here rather than in a thread. Nothing in
this section is a bug in what's built; it's what's missing *around* it.
The two load-bearing items are the detection gap and the load-testing
harness (Phase 3 below): until both move, automated mitigation on real
traffic is a false-positive risk without the compensating benefit.

- [x] **The operator hard-exits when NATS is unreachable.** Fixed:
      `pkg/events.Connector` connects in the background with retry/backoff
      instead of `os.Exit(1)`-ing `cmd/operator/main.go`; `/readyz` reports
      not-ready until the first successful connection instead of the Pod
      crash-looping, and `ThreatScoreWatcher`/`pkg/ingestion.Publisher`/
      `pkg/hubble.Observer` all tolerate the bus not being connected yet.
- [x] **No production install path, documented separately from
      `scripts/quickstart.sh`.** Fixed: `docs/production-install.md` is the
      ordered checklist (NATS auth/TLS first, `crds.keep`, a real model for
      the AI engine, detection-only rollout via the new `Alerting` phase,
      eBPF preflight, ServiceMonitor).
- [ ] **The AI engine has no chart, and no way to obtain a model.**
      `deployments/quickstart/ai-engine.yaml` is a plain manifest, and it
      needs `autoencoder.onnx` + `.onnx.data` + `.norm.json` delivered
      through a hand-created Secret; those artifacts are deliberately not
      committed. Skip that and nothing ever publishes a
      `ThreatScoreEvent`: every policy sits at `Phase: Monitoring`
      forever, with no signal anywhere that the scoring half of the
      system was never deployed. Options, roughly in order of usefulness:
      a published model artifact per release, a chart that takes it as a
      required value, or at minimum a readiness condition on the operator
      meaning "subscribed, but no scores have ever arrived".
- [x] **`helm uninstall` deletes every `TelecomSecurityPolicy`.** Fixed:
      the CRD template now carries `helm.sh/resource-policy: keep` by
      default (new `crds.keep: true` value) — `helm uninstall` leaves the
      CRD and every existing policy in place. `crds.keep: false` restores
      the old behavior for anyone who wants it.
- [x] **Enabling eBPF enforcement is really four settings, and getting
      the combination wrong still ends in a no-op.** Preflight fixed:
      `pkg/ebpf.Attach` now resolves `--bpf-interface` *before* loading
      anything into the kernel, and a failed attach is now recorded as an
      `EBPFAttachFailed` Kubernetes Event against the operator's own Pod
      (`POD_NAME`/`POD_NAMESPACE` Downward API env vars), not just logged.
      Kernel-capability support still can't be introspected reliably ahead
      of a real attach attempt, so that half stays a real-attempt failure
      classified via `pkg/ebpf.ClassifyAttachError`, same as before.
- [x] **`serviceMonitor.enabled: true` fails the apply** when the
      `monitoring.coreos.com/v1` CRD isn't installed. Fixed: guarded with
      `.Capabilities.APIVersions.Has` (not `lookup`, which doesn't work
      under `helm template --dry-run`) — renders nothing, with a warning
      in the post-install NOTES, instead of failing.
- [x] **Nothing stops a deployment shipping with an unauthenticated
      bus.** Fixed: both `pkg/config.Load()` (operator) and the AI engine's
      `AI_ENGINE_MODE=nats` startup check now refuse to run with none of
      the NATS auth/TLS knobs set, unless `NATS_ALLOW_UNAUTHENTICATED=true`
      is set explicitly. `scripts/quickstart.sh`/`docker-compose.yml`/the
      quickstart AI engine manifest all opt into it for their own
      throwaway, unauthenticated demo NATS.
- [x] **A detection-only pilot works, but is labelled as a
      malfunction.** Fixed: a new `PolicyPhaseAlerting` phase
      (`api/v1alpha1/telecomsecuritypolicy_types.go`) is set instead of
      `Degraded` when the threshold is crossed but `autoMitigate: false`
      withholds action — `Degraded` is now reserved for a genuine
      operator-side failure. A Prometheus counter for shadow-mode
      false-positive-rate measurement is still open, not yet added.
- [ ] **Close the in-tunnel-flood detection gap.** Restating Phase 1's
      §2.4 finding as open work rather than a closed measurement: the
      only anomaly type that is simultaneously real GTP-U and observable
      by `bpf/packet_filter.c` today scores *below* the held-out normal
      baseline (0.0679 vs 0.0811) and is missed at every production
      sensitivity threshold — recall 0/3,348. This is the single biggest
      reason not to trust automated mitigation on real traffic yet.
      Likely needs different features (per-tunnel/per-TEID rate rather
      than per-source only), a different model, or an explicit non-ML
      detector for this class.

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
