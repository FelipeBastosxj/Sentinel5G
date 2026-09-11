# Changelog

Notable changes to Sentinel5G, per release. See `ROADMAP.md` for what's
planned next.

## [Unreleased]

Phase 2.5 (production readiness) work-in-progress -- see `ROADMAP.md`.

### Added
- `SECURITY.md`: vulnerability disclosure policy (GitHub private
  vulnerability reporting), response targets, and severity guidance.
- `docs/production-install.md`: ordered checklist for a real install,
  distinct from `scripts/quickstart.sh`'s kind-only demo.
- The Helm chart is now also published as a signed OCI artifact
  (`oci://ghcr.io/felipebastosxj/charts/sentinel5g-operator`) on every
  release, alongside the container images -- `helm install` no longer
  requires cloning the repo first.
- `EBPFAttachFailed` Kubernetes Event, recorded against the operator's own
  Pod (via new `POD_NAME`/`POD_NAMESPACE` Downward API env vars) whenever
  the eBPF attach fails, so `kubectl describe pod`/`kubectl get events`
  surface it directly instead of only the equivalent log line.
- `TelecomSecurityPolicy.status.phase` has a new `Alerting` value: set when
  the effective threat score threshold is crossed but `autoMitigate: false`
  withholds action, instead of the previous `Degraded` (which reads as "the
  operator is broken" and is now reserved for a genuine operator-side
  failure) — makes a detection-only pilot's false-positive rate legible.
- Prometheus metrics for the operator itself (`pkg/controller/metrics.go`),
  on the manager's existing `/metrics` endpoint -- it previously exposed only
  controller-runtime's built-ins. `sentinel5g_threshold_crossings_total`'s
  `outcome="alerting"` is the shadow-mode counter `ROADMAP.md` Phase 2.5
  listed as still open inside the otherwise-closed `Alerting` item: it counts
  exactly the crossings a detection-only pilot would have mitigated, so the
  false-positive rate is measurable before turning `autoMitigate` on. Also
  `sentinel5g_threat_scores_received_total` (flat at zero when the scoring
  half of the system was never deployed), `sentinel5g_threat_score`,
  `sentinel5g_mitigations_total` and `sentinel5g_policy_phase`. Label values
  derived from the NATS wire go through a closed-set mapping, since anything
  that can reach an unauthenticated bus could otherwise drive unbounded
  Prometheus series growth.
- A `ScoringPipelineReady` condition on every `TelecomSecurityPolicy`, plus a
  `Scoring` column in `kubectl get tsp` and a `ScoringPipelineNotReady`
  warning Event. The AI engine is deployed separately from the operator and
  needs a trained model; skip that and nothing ever publishes a
  `ThreatScoreEvent`, so every policy sits at `Phase: Monitoring` forever --
  which is also exactly what a healthy, quiet cluster looks like. The
  condition is the signal that tells the two apart. `SCORING_PIPELINE_GRACE`
  (`config.scoringPipelineGrace`, default `10m`) covers an install where the
  AI engine legitimately starts after the operator.
- GTP-U header parsing in `bpf/packet_filter.c` (3GPP TS 29.281 §5.1) and a
  per-tunnel rate counter: new `tunnel_rate`/`tunnel_rate_v6` LRU maps keyed
  by `(source IP, TEID)`, bounded by `MAX_TUNNEL_ENTRIES`. Until now nothing
  in the project ever parsed a GTP-U header -- traffic was classified as
  GTP-U purely because it was UDP to port 2152 -- and the only rate signal
  was per source IP, which on a real N3 interface aggregates every
  subscriber together because they all arrive from the peer gNB's address.
  `NormalizedEvent` gains `teid` and `tunnelRatePerSecond` to carry it, and
  `pkg/ebpf.Loader` gains a `TunnelRate()` lookup.
- `pkg/detect`: a deterministic, non-ML GTP-U tunnel-flood detector, on by
  default (`GTPU_TUNNEL_FLOOD_ENABLED`, `config.gtpuTunnelFlood` in the
  chart). It exists because the autoencoder provably cannot catch this
  class -- a real in-tunnel flood reconstructs *better* than normal traffic
  (0.0679 vs an 0.0811 baseline), so no threshold on its score separates
  them. It publishes an ordinary `ThreatScoreEvent` with score 1.0 and
  `model: "rule:gtpu-tunnel-flood"`, which goes through exactly the same
  policy/sensitivity/`autoMitigate` gating as an ML score -- a detection-only
  pilot stays detection-only. The threshold
  (`GTPU_TUNNEL_FLOOD_PPS`, default 1000 packets/s on a single tunnel) is
  reasoned rather than validated against a production N3 interface; the same
  caveat `SCAN_EMIT_THRESHOLD` carries.
- `cmd/ai-engine/scripts/pcap_gtpu.py`: a stdlib-only pcap reader and a
  Python mirror of the kernel's GTP-U parser, so the committed captures can
  be read the same way production reads the wire. The dataset scripts now
  derive per-TEID rates from the `.pcap` files instead of the `tcpdump -tt
  -n` text dumps beside them -- the dumps carry no payload bytes, so no TEID.

### Changed
- **Breaking for a mixed deployment:** `struct signaling_event` grew from 24
  to 32 bytes and `signaling_event_v6` from 48 to 56, so the compiled
  `packet_filter.o` and the operator binary must be upgraded together. The
  published operator image bakes the object in at build time, so this only
  affects a deployment overriding `--bpf-object` with its own copy;
  `pkg/ebpf.Attach` now refuses an object without the `tunnel_rate` map
  rather than silently decoding every ring buffer record at the wrong length.
- `malformed` now also fires for a packet on port 2152 whose GTP-U framing
  fails to validate, where it previously only fired for a UDP header too
  short to read. Traffic that is not protocol-conformant GTP-U but aimed at
  the GTP-U port -- a plain UDP flood against the N3 socket, for instance --
  is reclassified accordingly.
- **Breaking for any deployed model:** `FEATURE_VECTOR_SIZE` went 12 -> 14
  (`tunnel_rate_norm`, `has_teid`), which changes the ONNX graph's input
  shape. Every existing `autoencoder.onnx` must be re-exported. The AI engine
  now refuses to start against a mismatched model instead of failing per
  event -- in NATS worker mode that was an exception log, a nak, five
  redeliveries and a silently dropped event, repeated forever, with nothing
  saying the model was simply the wrong shape.
- **Breaking-ish default:** the operator and the AI engine
  (`AI_ENGINE_MODE=nats`) now refuse to start against a NATS bus with no
  auth/TLS configured, unless `NATS_ALLOW_UNAUTHENTICATED=true`
  (`nats.allowUnauthenticated` in both Helm charts) is set explicitly.
  `scripts/quickstart.sh`, `deployments/docker-compose.yml`, and
  `deployments/quickstart/ai-engine.yaml` all now set this for their own
  (intentionally unauthenticated, throwaway) NATS instances; a real install
  should configure real credentials instead -- see
  `docs/production-install.md`.
- The operator no longer hard-exits when it can't reach NATS at startup: it
  retries with backoff in the background (`pkg/events.Connector`), and
  `/readyz` reports not-ready in the meantime instead of the Pod
  crash-looping. `ThreatScoreWatcher`/`pkg/ingestion.Publisher`/
  `pkg/hubble.Observer` all tolerate NATS not being connected yet.
- `charts/sentinel5g-operator`'s CRD now carries `helm.sh/resource-policy:
  keep` by default (`crds.keep: true`) -- `helm uninstall` no longer deletes
  every `TelecomSecurityPolicy` along with the release.
- `serviceMonitor.enabled: true` without the `monitoring.coreos.com/v1` CRD
  installed now renders nothing (with a warning in the post-install NOTES)
  instead of failing the `helm install`/`upgrade` outright.
- `pkg/ebpf.Attach` now checks `--bpf-interface` resolves to a real
  interface *before* loading the BPF collection into the kernel, instead of
  after -- a typo'd interface name fails faster and without the wasted load.
- CI's CRD drift check (`config/crd/bases` vs. the Helm chart's hand-copied
  `templates/crd.yaml`) compared the two documents for raw equality, but the
  chart legitimately adds `helm.sh/resource-policy: keep` (`crds.keep`, on by
  default since the same unreleased cycle). The check therefore failed on
  every run that touched either file, for a reason that was never drift. It
  now ignores that one chart-added annotation and still compares everything
  controller-gen actually emits.
- Two pre-existing `flake8` violations (`sentinel_ai/config.py:95`,
  `tests/test_config.py:35`) that failed `make ai-engine-lint` and CI's
  python job on every run.
- `scripts/quickstart.sh` keeps helm's cache/config alongside its own
  kubeconfig instead of in `$HOME`, so it also works where `$HOME` isn't
  writable (a service account, a container running as an arbitrary uid).
- `scripts/quickstart.sh` now prints the `export KUBECONFIG=...` line (and
  the real path of any CLI it installed) with its closing hints -- the
  commands it suggested previously couldn't work as pasted, since the
  script's kubeconfig is deliberately isolated from the user's shell.

### Fixed
- `scripts/quickstart.sh` failed outright on a clone the running user
  doesn't own or can't write to -- a checkout unpacked with `sudo`, a
  shared `/opt`/`/srv` path on a VM, an NFS mount with `root_squash` --
  because both its kubeconfig and the CLIs it downloads were hard-coded
  under the repo root. Everything it writes now goes to one directory,
  still the repo root when writable, otherwise `$XDG_STATE_HOME`/`$HOME`
  or `$TMPDIR` (which it reports).
- `scripts/quickstart.sh` hard-failed when `kubectl` wasn't already
  installed -- the single most common way a first run died on a fresh
  machine or VM -- despite already installing `kind` and `helm` itself.
  It now installs `kubectl` the same way, so `docker` and `curl` are the
  only prerequisites.
- `scripts/quickstart.sh`'s CLI downloads used `curl -sLo` without
  `--fail`, which writes a 404/503 error *page* to the target file and
  exits 0: a bad URL or registry blip produced an "installed" binary that
  failed much later as `cannot execute binary file`. All downloads now
  fail loudly (and retry transient errors), and each CLI is checked to
  actually run before use.
- Re-running `scripts/quickstart.sh` against an existing cluster whose
  kubeconfig was gone (deleted, swept from `/tmp`, or simply written
  elsewhere) died on `no context exists with the name:
  "kind-sentinel5g-quickstart"`. The reuse path now re-exports it.
- `scripts/quickstart.sh` reported an unreachable Docker daemon as only
  "is it running?", which misdiagnoses the common case on a fresh Linux
  host or VM: the daemon is up and the user simply isn't in the `docker`
  group. Both causes are now named, with the fix for each.


## [0.2.2] - 2026-09-08

### Added
- Images now also published to Docker Hub, and every published image
  (GHCR and Docker Hub alike) is now multi-arch (`linux/amd64` +
  `linux/arm64`) instead of `amd64`-only.
- `.github/workflows/e2e.yml`: builds this ref's own images and runs
  `scripts/quickstart.sh` against a real `kind` cluster on every PR --
  catches cluster-environment bugs (like the Istio-CRD one fixed in 0.2.1)
  in CI instead of by hand.
- `docs/troubleshooting.md`: consolidated guide for the install issues
  reported from cloud VMs and GitHub Codespaces that don't show up on a
  local dev machine (network egress, RBAC, DNS-in-kind, ARM64, eBPF).
- `scripts/quickstart.sh`: fast, root-caused preflight checks for network
  egress and RBAC permissions, instead of failing as a generic timeout
  deep inside `kind`/`helm`.

### Fixed
- The compiled eBPF object (`bpf/packet_filter.o`) was never actually
  included in the published operator image, and nothing documented how to
  get it there -- `ebpf.enabled: true` silently no-op'd on every real
  deployment. Now baked into the image at build time.
- `ebpf.enabled: true` didn't grant the Linux capabilities it needs
  (`CAP_BPF`/`CAP_NET_ADMIN`) -- now wired automatically.
- eBPF attach failures logged a bare error string; now classified (missing
  object, insufficient privilege, unknown interface, incompatible kernel).

### Changed
- The eBPF build no longer depends on `bpftool` or the host kernel's BTF
  (`bpf/headers/vmlinux_min.h` replaces the generated `vmlinux.h`), so it
  now also builds inside a plain `docker build`, not just on a real Linux
  machine.

## [0.2.1] - 2026-09-07

### Fixed
- Operator got stuck at `Phase: Monitoring` forever when Istio wasn't
  installed (the default `MESH_ADAPTER=istio` built a real Istio adapter
  unconditionally). Now falls back to a no-op mesh adapter, same as eBPF
  already does when not attached.

### Added
- `scripts/quickstart.sh`: one command from a fresh clone to a real
  mitigation firing, using published images.

## [0.2.0] - 2026-09-07

### Added
- Real training dataset from a live Open5GS+UERANSIM core, replacing the
  synthetic-only dataset.
- Off-signaling-port scan detection in `bpf/packet_filter.c`.
- `controller-gen` wired into `make manifests`, CI drift check.
- CO-RE (`vmlinux.h`) build for the eBPF program.
- Finalizer-based cleanup on policy deletion.
- Automatic mitigation de-escalation after a quiet period.
- `LOG_LEVEL` now actually controls the operator's log output.
- `pkg/mesh` and the AI engine's HTTP/NATS layer gained test coverage.

### Fixed
- eBPF: non-initial IP fragments were parsed as if they had a real UDP
  header, corrupting telemetry.
- Per-node signaling events were silently dropped on multi-replica
  operator deployments.
- `pkg/ebpf.Loader.SignalRate()` always returned 0 for real events (a
  struct-padding bug, present since it shipped).
- Release pipeline now scans images with Trivy before publishing, not
  after.
- CI now catches drift between the CRD and the Helm chart's copy of it.

### Changed
- **Breaking:** the AI engine's NATS durable consumer was renamed to add
  queue-group support (enables horizontal scaling). Existing deployments
  need to redeploy.

## [0.1.0] - 2026-09-06

Initial public release: `TelecomSecurityPolicy` CRD, closed-loop
mitigation (eBPF blocklist + Istio quarantine), autoencoder-based AI
engine, Helm chart, and CI with SBOM/scan/sign.
