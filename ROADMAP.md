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
The two load-bearing items were the detection gap and the load-testing
harness (Phase 3 below): until both moved, automated mitigation on real
traffic was a false-positive risk without the compensating benefit. Both
have now moved: the detection gap is closed (see its entry for what did and
did not close it, and for what remains unvalidated), and the load-testing
harness exists as `scripts/loadtest/`, with its results in
`docs/paper-data/01-performance-benchmarks.md` §1.4.

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
- [x] **The AI engine has no chart, and no way to obtain a model.** Fixed,
      with all three of the options this item listed rather than the
      cheapest, because they cover different failures:
      `charts/sentinel5g-ai-engine` is the chart, and it *refuses to
      install* without a model source instead of installing something that
      cannot work; `release.yml`'s `publish-model` job trains from the
      committed real dataset and publishes a signed, multi-arch artifact the
      chart pulls by default (`make ai-engine-train-real` locally); and the
      operator now reports a `ScoringPipelineReady` condition — visible as
      the `Scoring` column in `kubectl get tsp`, plus a
      `ScoringPipelineNotReady` warning Event — so a deployment that got a
      model some other way and still isn't scoring says so too. The
      condition is deliberately "has *ever* scored", not a liveness rate:
      quiet is the normal state of a network under no attack, so no rate
      separates "no threats today" from "the AI engine is gone".
      Verified end to end on a real cluster: install → initContainer
      delivers the model → a published `NormalizedEvent` is scored →
      `Phase: Mitigating`. That exercise also surfaced three real defects
      nothing else had caught — see CHANGELOG's Fixed section.
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
      operator-side failure. The Prometheus counter for shadow-mode
      false-positive-rate measurement this item left open is now added:
      `sentinel5g_threshold_crossings_total{outcome="alerting"}` counts
      exactly the mitigations a detection-only pilot withheld, so its rate
      over traffic believed benign is the false-positive rate an operator is
      being asked to accept. Four more operator metrics landed with it (the
      operator previously exposed only controller-runtime's built-ins) — see
      `docs/observability.md`, including the queries.
- [x] **Close the in-tunnel-flood detection gap.** Closed, and which of
      the three candidate fixes actually did it is the finding — full
      write-up in `docs/paper-data/02-ai-training-inference.md` §2.5.

      The root cause turned out to be structural, not a tuning problem.
      `bpf/packet_filter.c` had never parsed a GTP-U header at all: traffic
      was GTP-U because it was UDP to port 2152, and the only rate signal
      was per source IP — which on a real N3 interface aggregates every
      subscriber together, since they all arrive from the peer gNB's
      address. It now parses the header (3GPP TS 29.281 §5.1) and tracks a
      per-`(source, TEID)` rate, carried in-band on every event.

      Per-tunnel features plus a retrain moved the model but did not close
      the gap. ROC AUC went 0.9449 → 0.9680 and recall 0.693-0.728 → 0.896,
      almost entirely from the new `has_teid` feature finally separating
      traffic that merely *targets* port 2152 from real tunnels. The
      in-tunnel flood itself went from scoring *below* normal (0.0679 vs
      0.0811) to just above it (0.2469 vs 0.2403) — the right direction,
      but a margin of 0.0066 inside normal's own range, so recall is still
      0/3,348. Those are now the only false negatives left in the matrix.

      What closed it is the third option: `pkg/detect`, an explicit non-ML
      GTP-U tunnel-flood rule, on by default and subject to the same
      policy/sensitivity/`autoMitigate` gating as an ML score. Across the
      same two real captures at 25 pkt/s per tunnel: **0 events over 1,889
      packets of normal traffic, and a real mitigation on the flood the
      model misses entirely.**

      Two limits are recorded rather than smoothed over, and both are open
      work below: the committed captures contain a **single TEID**, so
      per-tunnel rate is numerically identical to per-source rate on them
      (correctness shown, discriminative value not); and they cap at 63
      pkt/s because of the WSL2 tunnel they came from, so the shipped
      1000 pkt/s default fires on none of them.

## Phase 3 — Per-subscriber precision & measured performance

Complete. Detection and mitigation are now the same granularity (one GTP-U
tunnel, not the gNB every subscriber shares), and the two latency SLOs have
numbers behind them instead of intentions. What this phase deliberately did
*not* do is make any of it survive a restart, a real NIC, or an adversary
who has read this file — that is Phase 4, and it is the gate before
Phase 5's scale work.


- [x] **A multi-UE capture, and a non-WSL2 throughput ceiling.** Both
      done on a native-Linux Open5GS + UERANSIM lab
      (`docs/paper-data/test-environment.md`, captures in
      `docs/paper-data/real-dataset-v2/`), write-up in
      `docs/paper-data/02-ai-training-inference.md` §2.6. Four subscribers
      on one gNB, all from the same source IP: with one flooding at
      3,000 pkt/s, per-source keying attributes the flood to the gNB (i.e.
      all four), per-TEID keying names the one tunnel — the argument the
      per-TEID work rested on, now measured. The ~63 pkt/s ceiling turned
      out to be `ping -f`'s, not WSL2's; the same tunnel carries ~125,000
      pkt/s from a real generator, so the 1,000 pkt/s default is
      conservative rather than unreachable.

      The finding nobody was looking for: the model trained on the
      single-session captures scored **1.0 on every packet** of the new
      normal traffic, because its two time-of-day features had learned the
      training capture's hour. That is a 100% false-positive rate on any
      real deployment. `hour_sin`/`hour_cos` are removed; retrained without
      them, AUC 0.9829, zero false positives on the new capture, and the ML
      path catches a real 3,000 pkt/s in-tunnel flood for the first time
      (70% of its packets above the high threshold).
- [x] **Drop or down-weight `rate_per_second` now that
      `tunnel_rate_per_second` exists.** Measured by ablation (§2.6.5) and
      resolved as neither: the feature is kept for untunneled traffic (SIP,
      SMPP, off-port probes — it is their only rate signal) and zeroed for
      any event with a TEID, where the tunnel's own rate speaks instead.
      Bystander tunnels during a flood went from 150/192 packets above the
      mitigation threshold to 0/192, with 94% of the flooding tunnel's
      packets still flagged. Same 12-wide vector, no ONNX shape change.
- [x] **A TEID-keyed drop path in eBPF.** Done: detection and mitigation
      are now the same granularity. `bpf/packet_filter.c` gained a
      `tunnel_blocklist` map keyed by the *same* `(source, TEID)` struct
      `tunnel_rate` is addressed by — so what the detector measured is
      exactly what gets dropped — plus its IPv6 twin. `ThreatScoreEvent`
      carries the `teid`, `Actions.EbpfBlockTunnel` is the new opt-in
      action, `Status.BlockedTunnels` is what the finalizer and the
      de-escalation timer replay to undo it.

      Two decisions worth recording. A score arriving without a TEID (every
      Hubble- and Falco-sourced one, by construction) does **not** fall
      back to blocking the whole source: the operator asked for one
      subscriber and would silently get the gNB, so the action is a no-op
      and says so in a metric. And a dropped packet is deliberately not
      rate-tracked — counting traffic the kernel is discarding would pin
      the tunnel's rate at the flood level and de-escalation's quiet period
      would never fire.

      Verified against the real verifier: blocking `(127.0.0.1, 0x4d84)`
      drops that tunnel while a second tunnel *from the same source IP*,
      SIP, and off-port traffic all still pass, and unblocking restores it.
- [x] **Wire the AI engine into `scripts/quickstart.sh`'s e2e.** Done:
      `SENTINEL5G_AI_ENGINE=true` installs `charts/sentinel5g-ai-engine`
      and publishes a *NormalizedEvent* (a 3,000 pkt/s tunnel-flood shape)
      instead of a forged score, so the AI engine has to score it for the
      loop to close; the script then asserts `ScoringPipelineReady=True`.
      `.github/workflows/e2e.yml` builds all three images from the PR --
      training the model from the committed dataset the same way
      `release.yml` does -- so CI's end-to-end test now runs Layer 3 for
      real. Off by default for a plain `./scripts/quickstart.sh` until the
      first release publishes `sentinel5g-model`; the forged-event path
      stays as the fallback. Running it found a real operator bug: after a
      restart, `ThreatScoreWatcher` consumed the scores JetStream had
      buffered before `PolicyIndex` was populated, and dropped every one of
      them silently. `Start` now seeds the index from the cache first.
- [x] **Load-testing harness for the `<0.2ms`/packet and
      mitigation-latency targets in `docs/observability.md`.** Done:
      `scripts/loadtest/` (its README explains what each method can and
      cannot see), results in
      `docs/paper-data/01-performance-benchmarks.md` §1.4. Both SLOs are
      met with room: **~195-211 ns/packet** for full GTP-U parsing with the
      ring buffer live (~1,000x under the 0.2 ms target) and **~2 ms** from
      a published score to `Phase: Mitigating`, of which the eBPF map write
      is ~760 ns.

      Both attached questions are settled. **`blocklist` does not need LRU
      semantics** — neither does `tunnel_blocklist` — and that is a
      security property, not a performance one: an entry is a mitigation
      the operator owns the lifetime of, so an LRU silently evicting one
      would un-block traffic nobody asked to un-block, a control failing
      open under load. Both are plain HASH; a full map refuses the insert
      (pinned at 16,384 by a privileged test) and the refusal surfaces as a
      metric and a failed reconcile. **The `signaling_events` ring buffer
      holds up as designed**: 8,192 in-flight records, and when it fills
      the observation is dropped while the packet still passes — Layer 1
      never drops traffic because Layer 2 fell behind. At the measured
      per-packet cost that is ~1.6 ms of drain headroom at line rate, which
      is the real constraint on anything ever added to `Publisher`'s hot
      path.

      Still open, and now precisely bounded rather than unmeasured:
      sustained line-rate traffic through a real NIC, and the `<2%` CPU per
      worker node at 100k req/s. Both need a load generator on real
      hardware rather than a synthetic replay, and both moved to Phase 4
      below — where they belong, because until they are measured the
      ring-buffer headroom above is arithmetic, not observation.

## Phase 4 — Production hardening: validation, scale, robustness

Phase 2.5 asked "can I install this?" and answered it. This phase asks the
harder question — **"can I turn `autoMitigate: true` on, on a network that
matters, and be right?"** — and the honest answer today is not yet. Every
item below was found by reading the code and the measurements that exist,
not by imagining failures; each names the file or the number it came from.

Ordered by what breaks first, not by effort. Items 1-4 are the gate: until
they move, automated mitigation on a real network is imprudent regardless
of how good the detector is, and scaling in Phase 5 only multiplies each of
them by the number of clusters.

**The four-item gate is closed.** The restart fail-open is fixed with bpffs
pinning and a kernel-vs-status reconcile; the distributed-TEID evasion is
caught by a second, per-peer detector; the accuracy numbers now have a
capture-independent holdout and a measured baseline; and the false-positive
rate is a confidence interval whose honest verdict is that `autoMitigate` is
not yet authorizable on real traffic — the detection-only pilot remains the
only defensible way to turn the system on, and is now also the instrument
for making the measurement that would change that. What remains below is
Scale and Robustness: real ceilings and untested failure modes, not
correctness holes in what is built.

### Correctness — the system can currently be wrong and not know it

- [x] **An operator restart silently drops every active mitigation.**
      Closed, with both halves this item asked for, and the pin half is
      closed by measurement rather than by assertion.

      The failure was the fail-open the HASH-not-LRU decision exists to
      prevent, arriving through the back door: `Loader.Close()` closed the
      link, the XDP program detached and the whole collection —
      `blocklist` and `tunnel_blocklist` included — died with it, while
      `status.blockedTunnels` went on asserting blocks that no longer
      existed.

      **Pinning.** The four *enforcement* maps (`blocklist`,
      `blocklist_v6`, `tunnel_blocklist`, `tunnel_blocklist_v6`) are now
      pinned to bpffs at `BPF_PIN_PATH` (default `/sys/fs/bpf/sentinel5g`,
      `ebpf.pinPath` in the chart, which also adds the `hostPath` mount),
      so the next attach reuses the same kernel objects with their contents
      intact. The *observation* maps (`*_rate`, `port_scan`) deliberately
      are not: carrying a 1-second rate window across a restart would hand
      the new process a reading from a period nobody was watching. That is
      the same enforcement-versus-observation split that decides HASH
      versus LRU, applied to a second question, and it is asserted in both
      directions by `TestPinnedMapsAreTheEnforcementMapsOnly`. Proven on
      kernel 6.14 by `TestPinnedEnforcementSurvivesALoaderRestart` (block,
      `Close()`, re-`Attach`, read the entry back) against its own control
      case, `TestUnpinnedEnforcementDoesNotSurviveALoaderRestart`.

      **Reconcile.** `pkg/controller.BlocklistReconciler` runs once at
      startup — that is the replay — and then every
      `BLOCKLIST_RECONCILE_INTERVAL` (default 1m). Status is the desired
      state, the map is the actual one: an entry in status and not in the
      kernel is re-applied, an entry in the kernel claimed by no policy is
      removed. Two things guard it against becoming the outage it prevents:
      a failed `List` aborts the pass rather than being read as "nothing
      should be blocked", and a removal requires the entry to have been
      unclaimed on *two* consecutive passes — because the mitigation path
      writes the kernel before it writes the status claiming the write, so
      a drop placed seconds ago is briefly claimed by nothing the
      reconciler can see. Additions stay immediate. The asymmetry errs
      toward enforcing, deliberately.

      **Drift metric.** `sentinel5g_blocklist_drift_total{kind,direction}`,
      plus `sentinel5g_blocklist_entries{kind,state}` for both sides of the
      comparison, `sentinel5g_blocklist_capacity` for its denominator, and
      `sentinel5g_ebpf_enforcement_pinned` — which answers "does a drop on
      this node survive the process at all?" as a number rather than an
      assumption. Queries in `docs/observability.md`.

      Two things this surfaced and fixed on the way: `Unblock` /
      `UnblockTunnel` returned an error on a key that was already gone, so
      de-escalation and finalization aborted on the first phantom entry and
      never reached the real ones behind it; and the map key sizes are now
      checked against the loaded object at attach, so a change to
      `struct tunnel_key` that was not mirrored in Go fails loudly instead
      of dropping the wrong tunnels.

      Two residual gaps, stated rather than smoothed over. Between
      `Close()` and the next `Attach` the XDP program is detached, so
      nothing is filtered during the restart itself — pinning preserves the
      decisions, it does not keep the program running. And a pin set that a
      rebuilt object cannot reuse (a changed `max_entries`, say) is
      discarded rather than reconciled: enforcement continues from empty,
      an `EBPFPinsReset` Event says so, and the next sync re-applies from
      status. Upgrade and rollback with active blocklists is still its own
      open item below.
- [x] **A one-line evasion: spread the flood across many TEIDs.** Closed
      with a second detector, `pkg/detect.GTPUSourceFloodDetector`, rather
      than by touching the model.

      The evasion was real and is exactly as described: `features.py` zeroes
      `rate_norm` for any tunneled event (the §2.6.5 fix that stopped one
      subscriber's flood being cross-attributed to its innocent neighbours),
      and `GTPU_TUNNEL_FLOOD_PPS` is per tunnel, so 200 tunnels at 999 pkt/s
      — ~200,000 pkt/s from one peer — crossed nothing. The two scopes are
      genuinely irreconcilable in one threshold: a per-tunnel one must sit
      below a single subscriber's legitimate rate, a per-source one must sit
      above the whole gNB's combined load, and the gap between those is where
      this attack lives.

      So there are now two detectors, not one tunable. The new one keys on
      the **source** and carries two signals, either of which fires: the
      aggregate GTP-U rate from that peer across every tunnel
      (`GTPU_SOURCE_FLOOD_PPS`, default 20,000), and the count of **distinct
      TEIDs** the peer uses per window (`GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS`,
      default 256) — which catches TEID rotation at rates no aggregate
      threshold would notice, since 300 distinct TEIDs at one packet each is
      only 300 pkt/s but is not a shape any real gNB produces. Both are
      measured over the same 1-second window the kernel uses.

      The evasion is closed by test, not assertion:
      `TestSourceFlood_CatchesTheEvasionThePerTunnelRuleMisses` runs **both**
      detectors over one 200-tunnel × 999 pkt/s stream and asserts the
      per-tunnel rule fires zero times while the per-source rule fires once;
      `TestSourceFlood_SilentOnOrdinaryMultiSubscriberTraffic` holds the
      false-positive side (four UEs at 25 pkt/s for 60 s → nothing). The
      per-peer verdict is emitted with **no TEID** — there is no single
      tunnel whose removal fixes an aggregate flood — so it drives the
      source-wide `actions.ebpfBlock`, and a policy set only for the
      per-tunnel action counts a `no_teid` no-op rather than silently
      blocking one arbitrary tunnel of the many.

      One blind spot is stated rather than claimed closed: an attacker who
      forges a TEID that genuinely belongs to **another live subscriber** on
      the same peer is indistinguishable from that subscriber's own traffic
      without UPF session state this component does not have. The cardinality
      signal catches rotation *into unused* TEIDs; it cannot catch
      impersonation of a valid one. Closing that needs the UPF's bearer
      table and is Phase 5 territory.

      This detector was also deliberately **not** given to the autoencoder
      as a feature, and that is the same lesson as §2.6.5 from the other
      side: a per-source quantity is identical for every subscriber behind a
      gNB, so a per-packet model fed one cross-attributes the flood to all of
      them. A rule can hold a per-source signal safely because its verdict is
      itself per-source and the operator acts on it with a per-source action.
- [x] **No capture-independent holdout, and no baseline to beat.** Closed
      by `cmd/ai-engine/scripts/baseline_comparison.py`, written so the
      capture list has one definition (`build_real_dataset.capture_groups`,
      which `build` now pools) and reported in full in §2.8 — including the
      part that came out against expectation.

      **Capture-independent holdout (leave-one-capture-out).** Train on every
      normal capture but one, score the held-out capture against all
      anomalous: AUC **0.935–0.936**, versus the ~0.98 the within-pool split
      reports. That gap is exactly the memorization this item suspected —
      the old number *was* flattered by testing on captures the model
      trained on. With only two real normal captures this is two folds, and
      the write-up now says plainly that more normal captures from different
      sessions are the single biggest thing that would strengthen the
      numbers.

      **Variance.** 5-fold × 3 seeds = 15 measurements: mean AUC 0.9353,
      **std 0.00033**. The single number was not seed-luck; that is now
      measured rather than asserted.

      **The baseline comparison, and it reverses the suspicion.** Autoencoder
      vs `rule:gtpu-tunnel-flood` vs a trivial `tunnel_rate` threshold, same
      holdout, by AUC and by recall at a ≤1% false-positive budget:
      autoencoder **0.935 AUC / 0.908 recall**, the rule and the trivial
      threshold **0.696 / 0.413**. So the ML *does* add something over the
      rule — but the honest reason is that the rule is blind by construction
      to every TEID-less anomaly (the UDP storm, the scans, the malformed
      frames, most of the set), not that it is cleverer. On the specific
      in-tunnel-flood class the model still cannot separate it (§2.6) and the
      rule is still what catches it. The two are complementary, which is why
      both ship; the comparison shows the model is not a dressed-up version
      of the rule, and it shows where it is not the answer.
- [x] **False-positive rate measured on a universe too small to authorize
      `autoMitigate`.** Closed by making the FPR a confidence interval with
      an operational translation (`cmd/ai-engine/scripts/fpr_confidence.py`,
      Clopper-Pearson exact bound, no scipy — the stats are checked against
      closed forms in `test_fpr_confidence.py`), and the finding is that the
      answer is "not yet", stated as a number. Full write-up in §2.9.

      Two things fell out, both worse than the item assumed. First, even
      taking "zero false positives" at face value, the sample only bounds
      the true FPR: 0 in 1,378 packets gives a 95% upper bound of 2.7e-3,
      which at 100k pkt/s is **up to 267 wrong mitigations per second**. The
      point estimate of zero is three orders of magnitude short of ruling
      that out; the tool computes the concrete target (~370,000 benign
      packets at zero FP to bound it at one wrong mitigation/sec). Second,
      "zero" was itself a within-pool artifact: scored out of fold (the §2.8
      capture-independence), the real FPR at the production thresholds is
      **~23%**, because the normalization reference error is calibrated on
      training reconstruction error and does not transfer to a capture the
      model never saw. And the obvious shortcut is closed too — a model
      trained on real GTP-U flags ~75% of synthetic benign traffic, so
      synthetic volume measures distribution shift, not FPR; the benign
      data has to be real.

      The deliverable is the instrument and the honest verdict:
      `autoMitigate: true` is authorized by nothing measured here, the
      detection-only pilot remains the only defensible way to turn the
      system on, and the pilot is now also how the missing measurement gets
      made — count benign packets and false alarms, feed them to the bound,
      decide per deployment. This is the last of the four gate items.

### Scale — the architectural ceilings, not the implementation's

- [x] **NATS carries one event per signaling packet, with no sampling,
      batching or aggregation in `pkg/ingestion.Publisher`.** Closed with
      batching, the lossless answer. `pkg/ingestion.Publisher` now coalesces
      up to `NATS_EVENT_BATCH_SIZE` events into one JetStream message (a JSON
      array), flushed by size or `NATS_EVENT_FLUSH_INTERVAL`, cutting the
      message rate by the batch factor — proven end to end: 100 events →
      10 messages at batch 10 (`pkg/ingestion/batch_test.go` against live
      JetStream). The inline flood detectors are **not** batched; they act
      per packet, so only the ML telemetry is delayed, bounded by the flush
      interval. The AI-engine consumer (`parse_event_batch`) accepts both the
      array and the single-object form, so batching, non-batching and older
      Publishers all interoperate with one consumer. It composes with §1.5's
      finding that batched inference is ~300× faster per event — the same
      batch that relieves the bus also unlocks the engine's batched-scoring
      headroom. Off by default (a wire-behaviour change; enable once the AI
      engine is current), documented in `docs/event-model.md`. Sampling and
      aggregation were the alternatives; batching was chosen because it drops
      nothing — a sampled-away packet could be the attack.
- [x] **`MAX_TUNNEL_ENTRIES` is 65,536; a real UPF serves far more
      bearers.** The metric half is closed; the sizing half is honestly
      still open and now *visible* rather than invisible. The per-tunnel
      rate map's live occupancy is sampled every reconcile and exported as
      `sentinel5g_ebpf_observation_map_occupancy` against
      `..._capacity`, per node — so the silent degradation (an LRU evicting
      the coldest rate counter, a window resetting mid-flight) now shows as
      a gauge approaching 1 before it bites, which is the only warning an
      LRU can give since the kernel exposes no eviction count. The alert and
      the fix (raise `MAX_TUNNEL_ENTRIES`, rebuild, expect an
      `EBPFPinsReset`) are in `docs/observability.md`. What remains is a
      *measured* right-size against a real UPF's bearer cardinality, which
      needs a real UPF — folded into the line-rate measurement item below,
      not a separate unknown.
- [x] **A full `tunnel_blocklist` is a denial of service against the
      mitigation path.** Closed. A refused insert (the map is plain HASH and
      returns `E2BIG` rather than evicting, recognised by `ebpf.IsMapFull`)
      now increments `sentinel5g_mitigation_map_full_total{kind}`, so "drops
      are being denied" is a distinct, alertable signal instead of a generic
      error. `docs/troubleshooting.md` carries the defined response, and its
      first branch is the one this exposed as the real answer: an attacker
      generating distinct TEIDs to exhaust the map is caught by the per-peer
      cardinality detector (Phase 4 item 2), and a single source-wide block
      on that peer reclaims every slot its forged tunnels took. The two
      Phase 4 items compose: the evasion detector *is* the DoS defence.
- [x] **The `<2%` CPU-per-node target has never been measured, and neither
      has sustained line rate.** Measured, with the residual stated honestly.
      `scripts/loadtest/cpu_saturation.sh` runs real GTP-U frames through the
      real XDP program on a veth (a real driver RX path) with the
      `SignalingEvents` consumer draining the ring, and BPF run-time stats
      on. Findings (`docs/paper-data/01-performance-benchmarks.md` §1.7):
      the ring buffer **does not saturate** — ~14 dropped observations in
      ~2.3M at 386k pkt/s, retiring the "1.6 ms headroom is arithmetic"
      concern with an observation; and the honest per-packet program cost
      under load with a consumer is **~900 ns** (4× the 211 ns hot-cache
      microbenchmark, still 200× under the 0.2 ms budget), which at 100k
      pkt/s is ~9% of one core / ~1.1% of an eight-core node. What remains is
      only the narrow part the item named literally — a **physical NIC** at
      true line rate (10/40/100 GbE) and its driver CPU — because this is
      generic XDP on a veth; the program's own cost and the ring's behaviour
      under real load, which were the substance, are now observations, not
      arithmetic.
- [x] **AI engine throughput per replica is unknown.** Measured.
      `scripts/throughput_bench.py` puts one replica at **~72,000 events/sec**
      on the production `score_event` path (single process, single thread,
      CPU), full write-up in `docs/paper-data/01-performance-benchmarks.md`
      §1.5. That comfortably exceeds one node's plausible signaling rate, so
      the model is not the sizing bottleneck — the replica count is set by the
      aggregate across nodes and the NATS ceiling (§1.3), not by scoring. The
      benchmark also measured that batching the same vectors in one ONNX call
      is **~300×** faster per event, headroom the engine's one-call-per-message
      design leaves on the table — which is why the throughput ceiling lives
      at the bus (item above), not the model.
- [x] **`ThreatScoreWatcher` is single-active by design, at
      `replicaCount: 1`.** Measured (`throughput_bench_test.go`,
      `docs/paper-data/01-performance-benchmarks.md` §1.6). One consumer
      drains ~58,000 no-write scores/sec (a fake-client floor; the real
      informer cache is faster), against an AI engine producing ~72,000/sec
      per replica — so the single-active design is a real ceiling only when
      the *aggregate* rate across nodes nears ~50k/s. The two in-memory
      indexes it leans on were the specific worry, and both are fine at
      scale: `PodIPIndex.Lookup` is **flat O(1)** (~9–12 ns, 0 allocs, from
      1k to 50k pods), and `PolicyIndex.MatchingPolicies` is linear but
      sub-25 µs even at 1,000 policies per namespace. The lever when the
      ceiling is reached is sampling upstream (the NATS item), not sharding
      the consumer — which would reintroduce the multi-writer races the
      leader gate exists to prevent. The tradeoff is deliberate and now
      quantified.

### Robustness — the failure modes nothing currently tests

- [x] **No chaos or fault-injection testing at all.** Closed, and it earned
      its keep by finding a real defect. `pkg/controller/chaos_test.go` now
      injects each named fault: the apiserver unavailable when the status
      write is due, an action failing after status has recorded it, a node
      rebooting with active blocks, and the bus dropping mid-mitigation.

      The apiserver case surfaced a genuine bug: the watcher blocked in the
      kernel *before* writing status, so a failed status write left the
      kernel blocking and status not — and the `BlocklistReconciler` (status
      is desired state) would then reconcile that orphan away, silently
      undoing the mitigation a pass or two later. Fixed by **write-ahead
      ordering**: the intent is recorded in status and persisted before the
      kernel or mesh is touched. A failed status write now means nothing was
      blocked (clean retry); an action failing after the write leaves status
      claiming the block and the reconciler re-applies it. The tests tie the
      watcher's write-ahead and the reconciler's re-apply into one
      convergence proof rather than two comments.
- [x] **No rate limit on the action path.** Closed. `pkg/controller`'s
      status writes are now change-aware (`writeStatus`): a write that
      changes the phase or a blocklist set is *meaningful* and always goes
      through — those are bounded by the number of distinct threats and the
      finalizer/de-escalation read them — while a write that only refreshes
      `ObservedThreatScore` (the dominant churn under a per-packet ML score
      stream with at-least-once delivery) is gated by a per-policy token
      bucket (`ReactorLimiter`, `REACTOR_STATUS_WRITES_PER_SECOND` default
      10, burst 20). Throttled refreshes are skipped and counted
      (`sentinel5g_reactor_status_writes_throttled_total`), so apiserver
      pressure is bounded without ever dropping a mitigation decision. The
      per-policy keying means one noisy policy can't starve another's
      refreshes. The detector cooldown bounds the detector; this bounds the
      reactor, as the item asked.
- [x] **No global kill switch.** Closed. `pkg/controller.KillSwitch` is a
      ConfigMap the operator polls: while `sentinel5g-killswitch` exists with
      `engaged=true`, every mitigation on every policy is withheld —
      detection continues (scores, crossings, `Alerting`), nothing is blocked
      or quarantined. Engaging it is one `kubectl create configmap`, takes
      effect within the poll TTL (2s) with no operator restart, and is
      visible as `sentinel5g_kill_switch_engaged` plus a
      `mitigations_suppressed_total` count of what was withheld. Read through
      a direct get-by-name (a tight namespaced Role, not cluster-wide
      ConfigMap access), cached for the TTL so a score storm can't turn it
      into an API storm, and it fails safe: a transient read error keeps the
      last known value rather than flipping the switch. See
      `docs/production-install.md` §8.
- [x] **Leader failover with in-flight scores is untested.** Closed.
      `pkg/controller/failover_test.go` exercises the idempotency through the
      two things a real failover does: a score redelivered after the previous
      leader already acted on it (the at-least-once case JetStream produces on
      resubscribe) leaves the blocked set and phase singular, not doubled;
      and a freshly-elected leader with an empty index rebuilds it from the
      API (`warmIndex`) before processing, so a redelivered score for a
      policy the new leader never saw created still matches — rather than
      matching nothing, being acked, and vanishing, which is the failure the
      warm-before-subscribe ordering was written to prevent. The deleting-
      policy guard is tested too: a failover must not resurrect a policy the
      finalizer is removing.
- [x] **The IPv6 path compiles, has full parity, and has never run end to
      end.** Now it has. `TestIPv6DataPathEndToEnd` (`-tags privileged`)
      feeds a real v6 GTP-U frame to the real loaded XDP program via
      `BPF_PROG_TEST_RUN` and asserts the whole v6 data path: the packet
      parses (XDP_PASS), the v6 per-tunnel rate map is populated from the
      outer v6 `(source, TEID)`, and once that tunnel is blocked the
      identical frame is dropped (XDP_DROP). Real v6 packets, real map side
      effects, real verdicts — the caveat being that the frame is injected
      via PROG_TEST_RUN rather than arriving on a NIC, so the v6 *logic*
      (parse/rate/enforce/drop) runs for real but the driver delivery does
      not. See `docs/paper-data/04-validation-testing-logs.md` §4.10.
- [x] **Upgrade and rollback with active blocklists is untested.** Closed.
      The "today an upgrade *is* a silent unblock" premise was already
      retired by the pinning + reconcile of this phase's first item; this
      adds the tests that prove the upgrade paths specifically.
      `pkg/controller/upgrade_test.go`: an upgrade that changes a BPF map's
      shape (so the pins can't be reused) is **loud and recovered** — an
      `EBPFPinsReset` Event plus a reconcile that re-applies from status,
      not a silent loss; the only cross-version mitigation state
      (`BlockedSourceIPs`/`BlockedTunnels`, both `[]string`) round-trips
      through its wire format and an unknown future entry is skipped rather
      than wedging deletion; and a cluster mid-rolling-upgrade, with
      old-style (IP-only) and new-style (tunnel) policies side by side,
      unions into the desired enforcement set cleanly. The CRD staying
      `v1alpha1` is noted, not blocking: the mitigation state is two string
      slices, the most rollback-stable shape available.
- [ ] **`cmd/falco-bridge` and `pkg/hubble` have still never run against
      real daemons** — the caveat that was carried from Phase 2 and that
      native Linux finally makes cheap to remove.

## Phase 5 — Scale & multi-cluster

Deliberately gated behind Phase 4. Every unmeasured ceiling and every
silent failure above gets multiplied by the number of clusters here, so
moving these first would mean scaling something that cannot yet say
whether it is working.

- [ ] **Mesh quarantine is still per workload, not per subscriber.** The
      eBPF drop is now per tunnel; `IsolatePod` still quarantines the whole
      Pod, because `pkg/mesh.Adapter.Quarantine`'s signature carries a
      label selector and nothing finer. A policy running
      `ebpfBlockTunnel: true, isolatePod: true` therefore has one precise
      action and one blunt one — which is the right default (the mesh layer
      cannot see a GTP-U tunnel at all) but worth knowing before enabling
      both.
- [ ] Multi-cluster policy propagation.
- [ ] Poetry-based lockfile for `cmd/ai-engine`, if wanted.

Have an idea that isn't here? Open an issue — see
[`CONTRIBUTING.md`](CONTRIBUTING.md).
