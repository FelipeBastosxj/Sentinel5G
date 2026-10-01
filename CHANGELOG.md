# Changelog

Notable changes to Sentinel5G, per release. See `ROADMAP.md` for what's
planned next.

## [Unreleased]

Phases 2.5 and 3 are complete. `ROADMAP.md` was restructured on the
strength of a gap review rather than a plan: the production-readiness work
that review turned up is now **Phase 4** (validation, scale, robustness --
17 items, each naming the file or measurement it came from), and the
scale/multi-cluster items that used to sit in Phase 3 moved to **Phase 5**,
deliberately gated behind it. The headline from that review, and the reason
for the gating, was that an operator restart un-blocked everything it had
blocked, silently. **That one is now closed** -- the first Phase 4 item --
and the remaining three in its gate are validation work that no amount of
code closes.

### Added (Phase 4)
- **NATS event batching** (`pkg/ingestion.Publisher` + a new
  `Bus.PublishNormalizedEventBatch`), closing ROADMAP.md Phase 4's bus-ceiling
  item -- the design's first throughput limit, reached before eBPF's. The
  Publisher coalesces up to NATS_EVENT_BATCH_SIZE events into one JetStream
  message (a JSON array), flushed by size or NATS_EVENT_FLUSH_INTERVAL,
  cutting the message rate by the batch factor (proven: 100 events -> 10
  messages at batch 10, against live JetStream). The inline flood detectors
  are not batched -- they act per packet, so only ML telemetry is delayed.
  The AI-engine consumer (parse_event_batch) accepts both the array and the
  single-object form, so batching/non-batching/older Publishers all
  interoperate. Off by default (a wire-behaviour change); see
  docs/event-model.md. Batching was chosen over sampling because it drops
  nothing -- a sampled-away packet could be the attack.
- **A reactor rate limit** bounding apiserver pressure under a score storm,
  closing ROADMAP.md Phase 4's "nothing bounds the reactor" item. Status
  writes are now change-aware: a phase transition or a blocklist delta is
  meaningful and always persisted (the finalizer and de-escalation read it),
  but an ObservedThreatScore-only refresh -- the dominant churn when the AI
  engine scores one event per packet under at-least-once delivery -- is gated
  by a per-policy token bucket (REACTOR_STATUS_WRITES_PER_SECOND, default 10,
  burst 20). Throttled refreshes are skipped and counted
  (sentinel5g_reactor_status_writes_throttled_total); no mitigation decision
  is ever dropped, and one noisy policy can't starve another's refreshes.
- **A global mitigation kill switch** (`pkg/controller.KillSwitch`), closing
  ROADMAP.md Phase 4's "no stop-everything-now" item. While a ConfigMap
  (`sentinel5g-killswitch`, default) exists in the operator's namespace with
  `engaged=true`, every mitigation on every policy is withheld -- detection
  continues, nothing is blocked or quarantined. Engaging it is one `kubectl
  create configmap`, effective within a 2s poll TTL with no operator restart;
  `sentinel5g_kill_switch_engaged` and `sentinel5g_mitigations_suppressed_total`
  make it visible. Read via a tight get-by-name namespaced Role (not
  cluster-wide ConfigMap access), cached for the TTL so a score storm can't
  become an API storm, and fail-safe (a transient read error keeps the last
  known value). See docs/production-install.md §8.
- **eBPF map-occupancy metrics and a mitigation-refused signal**, closing
  ROADMAP.md Phase 4's fifth and sixth scale items. The per-tunnel rate map
  is an LRU that evicts silently when full -- a rate window resetting
  mid-flight, detection degrading under exactly the bearer cardinality it
  exists for -- so its live occupancy is now sampled every reconcile and
  exported as `sentinel5g_ebpf_observation_map_occupancy` against
  `..._capacity`, the only warning an LRU can give. And a mitigation refused
  because an enforcement map is at capacity (plain HASH, returns E2BIG rather
  than evicting -- recognised by the new `ebpf.IsMapFull`) now increments
  `sentinel5g_mitigation_map_full_total{kind}`, with a defined operational
  response in docs/troubleshooting.md whose first branch is that the per-peer
  cardinality detector IS the defence against a TEID-exhaustion DoS -- the
  two Phase 4 items compose.
- **The false-positive rate is now a confidence interval, not a point
  estimate** (`cmd/ai-engine/scripts/fpr_confidence.py`), closing ROADMAP.md
  Phase 4's fourth item and completing the four-item correctness gate. A
  Clopper-Pearson exact binomial upper bound (implemented without scipy,
  checked against closed forms) translated into wrong mitigations per second
  at a given packet rate. Two findings, both worse than assumed: even taking
  "zero false positives" at face value, 0 in 1,378 packets bounds the FPR at
  only 2.7e-3 -- up to 267 wrong mitigations/sec at 100k pkt/s -- and scored
  out of fold (capture-independent, per §2.8) the real FPR at the production
  thresholds is ~23%, because the normalization calibration does not transfer
  to an unseen capture. Synthetic benign traffic can't substitute (a
  real-GTP-U model flags ~75% of it as unfamiliar). Verdict: autoMitigate is
  not authorized by anything measured; the detection-only pilot stays the
  only defensible path, and is now the instrument for making the measurement
  that would change that. See docs/paper-data/02-ai-training-inference.md §2.9.
- **A capture-independent holdout and a model-vs-rule baseline**
  (`cmd/ai-engine/scripts/baseline_comparison.py`), closing ROADMAP.md
  Phase 4's third item. The accuracy numbers to date came from a within-pool
  train/holdout split -- unseen rows from captures the model also trained
  on, which measures reconstruction, not generalization. Leave-one-capture-
  out (train on every normal capture but one, score the held-out one) gives
  AUC 0.935-0.936 against the ~0.98 within-pool figure: the gap is the
  memorization the within-pool split hid. 5-fold x 3-seed variance is
  std 0.00033, so the number is not seed-luck. The baseline comparison
  reverses this item's own suspicion -- the autoencoder (0.935 AUC,
  0.908 recall at <=1% FPR) clearly beats the rule and a trivial tunnel-rate
  threshold (0.696 / 0.413), but because those are blind by construction to
  every TEID-less anomaly, not because the model is cleverer; on the
  in-tunnel-flood class the rule is still what works, so the two are
  complementary. `build_real_dataset` was refactored so `capture_groups`
  is the one definition of the dataset and `build` pools it. See
  docs/paper-data/02-ai-training-inference.md §2.8.
- **A per-peer GTP-U source-flood detector** (`pkg/detect.GTPUSourceFloodDetector`),
  closing ROADMAP.md Phase 4's second item: a flood spread thinly across
  many TEIDs evaded the per-tunnel rule entirely. 200 tunnels at 999 pkt/s
  is ~200,000 pkt/s from one peer and crossed the 1000 pkt/s per-tunnel
  threshold zero times, by construction -- the per-tunnel detector's whole
  purpose was to stop aggregating subscribers together, so it cannot also
  be the thing that catches an aggregate. The new detector is scoped to the
  source and fires on either an aggregate rate (`GTPU_SOURCE_FLOOD_PPS`,
  default 20,000) or distinct-TEID cardinality per window
  (`GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS`, default 256, which catches TEID
  rotation at rates no aggregate threshold would see). On by default, same
  policy/sensitivity/autoMitigate gating as any other score. Its verdict
  names no tunnel -- there is no single tunnel whose removal fixes an
  aggregate flood -- so it carries TEID 0 and drives the source-wide
  `actions.ebpfBlock`. The evasion is closed by test
  (`TestSourceFlood_CatchesTheEvasionThePerTunnelRuleMisses` runs both
  detectors over the identical stream; per-tunnel fires 0, per-source fires
  1) and the false-positive side is held by another (ordinary four-UE
  traffic → nothing). See docs/paper-data/02-ai-training-inference.md §2.7.
- **The eBPF enforcement maps now survive the operator process**, closing
  `ROADMAP.md` Phase 4's first item. Up to now `Loader.Close()` detached
  the XDP program and the whole collection went with it, so a `helm
  upgrade`, an OOM kill or a crash un-blocked every active mitigation while
  each policy's status went on asserting `Phase: Mitigating` and a
  populated `status.blockedTunnels`. That is the same fail-open the
  HASH-not-LRU map choice exists to prevent, reached through the back door:
  a control reporting itself as on while being off.

  Two mechanisms, doing different jobs. **Pinning** (`BPF_PIN_PATH`,
  default `/sys/fs/bpf/sentinel5g`; `ebpf.pinPath` in the chart, which also
  adds the `hostPath` mount for it) keeps the drops in force across the
  gap: the next attach reuses the same kernel maps with their contents
  intact. **`pkg/controller.BlocklistReconciler`** makes them correct
  afterwards -- once at startup, then every `BLOCKLIST_RECONCILE_INTERVAL`
  (default `1m`) -- treating policy status as the desired state and the map
  as the actual one, re-applying what is missing and removing what no
  policy claims. A removal needs the entry to be unclaimed on two
  consecutive passes; an addition is immediate. That asymmetry is load
  bearing — the mitigation path writes the kernel *before* the status that
  claims the write, and the reconciler reads that status through a lagging
  cache, so removing on sight would revert mitigations placed seconds
  earlier. A failed `List` aborts the pass outright rather than being read
  as "nothing should be blocked".

  Only the *enforcement* maps are pinned. The `*_rate`/`port_scan`
  observation maps deliberately are not: carrying a 1-second rate window
  across a restart hands the new process a reading from a period nobody was
  watching. The split is asserted in both directions by a test, so adding a
  map to the pinned list by reflex fails rather than quietly changing what
  the system means.

  Proven on kernel 6.14 rather than argued: block, `Close()`, re-`Attach`,
  read the entry back (`TestPinnedEnforcementSurvivesALoaderRestart`),
  against its own control case showing an unpinned load does *not* survive.

  One consequence worth knowing before upgrading: in DaemonSet mode every
  node reconciles against the same cluster-wide policy status, so a source
  blocked because one node observed it ends up blocked on every node. That
  is the reading `status.blockedSourceIPs` is built on -- a record of what
  should not be allowed, not a note about where it was seen.
- **Enforcement-integrity metrics** (`docs/observability.md`'s new "Is
  enforcement actually on?" section):
  `sentinel5g_blocklist_drift_total{kind,direction}` -- `missing` means
  status claimed a drop the kernel did not have, i.e. traffic an operator
  believed was blocked was flowing; `extra` means enforcement outliving the
  policy that asked for it. Plus `sentinel5g_blocklist_entries{kind,state}`
  for both sides of the comparison, `sentinel5g_blocklist_capacity` so that
  gauge has a denominator, and `sentinel5g_ebpf_enforcement_pinned`, which
  answers "does a drop on this node survive a restart at all?" as a number.
- Two new warning Events against the operator's own Pod, alongside the
  existing `EBPFAttachFailed`: `EBPFPinUnavailable` (the pin path is not on
  a bpffs -- almost always a missing `/sys/fs/bpf` mount) and
  `EBPFPinsReset` (a rebuilt object changed a map's shape, so the existing
  pins could not be reused and the drops were lost). Both are recoverable
  states that used to be invisible.

### Fixed (Phase 4)
- **A transient apiserver error during a status write silently undid a
  mitigation.** The watcher blocked in the kernel before writing status, so
  a failed status write left the kernel blocking and status not recording it
  -- and the BlocklistReconciler (status is desired state) then reconciled
  that orphan away a pass or two later. Fixed with write-ahead ordering: the
  intent is recorded in status and persisted before the kernel/mesh is
  touched, so a failed write means nothing was blocked (clean retry) and an
  action failing after the write leaves status claiming the block for the
  reconciler to re-apply. Found by the new chaos/fault-injection tests
  (pkg/controller/chaos_test.go), which close ROADMAP.md Phase 4's "no chaos
  testing" item.
- **`Unblock`/`UnblockTunnel` treated an already-absent entry as an
  error.** De-escalation and finalization both walk a policy's status
  unblocking every entry, and a single phantom -- a restart that lost the
  maps, a reconcile that already pruned it -- aborted the loop and left
  every real entry behind it in the kernel. Already-gone is the outcome
  being asked for, so it is now success. Found by writing the restart
  test, not in production.
- **A changed BPF map key would have gone undetected.** The ring buffer's
  record size has been pinned by test since Phase 2.5, but nothing checked
  the enforcement maps' *key* sizes against the loaded object. A change to
  `struct tunnel_key` that was not mirrored in `pkg/ebpf` would have
  marshalled silently and dropped the wrong tunnels; `Attach` now refuses
  the object instead.
- `BPF_PIN_PATH=""` -- the documented way to turn pinning off -- would have
  been read as "unset" by the config loader's `getEnv` and silently turned
  pinning *on*. Empty is now a meaningful value for that one variable.

### Added (Phase 3)
- **A per-tunnel (TEID-keyed) eBPF drop path**, closing `ROADMAP.md`
  Phase 3's largest gap: detection has been per subscriber since Phase 2.5
  while mitigation was still per peer address. `bpf/packet_filter.c` gained
  `tunnel_blocklist` (and its IPv6 twin), keyed by the *same*
  `(source, TEID)` struct `tunnel_rate` is measured by, so what the
  detector identified is exactly what gets dropped. `ThreatScoreEvent`
  carries `teid`, `actions.ebpfBlockTunnel` is the new opt-in action, and
  `status.blockedTunnels` is what the finalizer and the de-escalation timer
  replay to undo it. On a real N3 interface this is the difference between
  cutting off one flooding subscriber and cutting off every subscriber
  behind that gNB.

  A score without a TEID (every Hubble- and Falco-sourced one, by
  construction) leaves the action a **no-op** rather than silently widening
  to the whole source, counted as
  `sentinel5g_mitigations_total{action="ebpf_block_tunnel",result="no_teid"}`.
  And a dropped packet is deliberately not rate-tracked: counting traffic
  the kernel is discarding would pin the tunnel's rate at the flood level
  and de-escalation's quiet period would never fire.
- **A load-testing harness** (`scripts/loadtest/`), closing the other
  Phase 3 item, with results in
  `docs/paper-data/01-performance-benchmarks.md` §1.4. Both measurable SLOs
  are met with room: **~195-211 ns/packet** for a full GTP-U parse (~1,000x
  under the 0.2 ms target) and **~2 ms** from a published score to
  `Phase: Mitigating`, of which the eBPF map write is ~760 ns. The CPU
  target remains unmeasured and is now labelled as such rather than
  implied.

  Both questions the roadmap attached to the item are settled with data.
  `blocklist` does **not** need LRU semantics -- neither does
  `tunnel_blocklist` -- because an LRU evicting an operator-owned
  mitigation is a control failing open under load; both are plain HASH and
  a full map refuses the insert (pinned at 16,384 by a privileged test).
  And `signaling_events` holds up as designed: 8,192 in-flight records,
  and when it fills the observation is dropped while the packet still
  passes -- ~1.6 ms of drain headroom at line rate, which is the real
  constraint on anything added to `Publisher`'s hot path.
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
- `charts/sentinel5g-ai-engine`: a Helm chart for the AI engine, which had
  none -- it was a plain manifest needing a hand-created Secret, and skipping
  that step produced a deployment that came up healthy and silently never
  published a `ThreatScoreEvent`. The chart **refuses to install** without a
  model source rather than installing something that cannot work, and CI
  asserts that guard rather than trusting it.
- A published model artifact per release: `publish-model` in `release.yml`
  trains from the committed real dataset and pushes a signed, multi-arch
  `sentinel5g-model` image, which the new chart consumes via an
  initContainer. `make ai-engine-train-real` is the same path locally; the
  release summary records the model's `reference_error` and the dataset hash
  so a published model is traceable to the data that produced it.
- Both Helm charts are now published as OCI artifacts on release (the
  existing job became a matrix), and `make helm-lint`/`helm-template` cover
  both.
- `docs/paper-data/real-dataset-v2/`: multi-UE captures from a native-Linux
  Open5GS + UERANSIM lab (`docs/paper-data/test-environment.md`, which also
  replaces the dangling `memory/wsl2_real_test_environment.md` references
  six files carried). Four subscribers on one gNB, one flooding at
  3,000 pkt/s -- the first data on which per-TEID keying can be shown to
  name the flooding subscriber where per-source keying names the gNB.
  Includes `gen_tunnel_flood.py`, a `SO_BINDTODEVICE` flood generator,
  because both `iperf3 -B` and UERANSIM's `nr-binder` silently bypass the
  tunnel on this topology.
- `scripts/quickstart.sh` can now deploy the real AI engine
  (`SENTINEL5G_AI_ENGINE=true`) and drive the mitigation from a real score
  rather than a forged `ThreatScoreEvent`; `.github/workflows/e2e.yml` does,
  building and training everything from the PR, so CI's end-to-end test
  exercises Layer 3 for the first time. Off by default until a release
  publishes the model artifact.

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
- **Breaking for any deployed model:** the feature vector changed twice in
  this cycle and is back at 12 wide, but a *different* 12: `tunnel_rate_norm`
  and `has_teid` are in, and `hour_sin`/`hour_cos` are out. The removal is
  a measured decision, not a cleanup -- trained on single-session real
  captures, the two time-of-day features made the model score 1.0 on
  every packet of traffic captured at a different hour, a 100%
  false-positive rate on any real deployment
  (`docs/paper-data/02-ai-training-inference.md` §2.6). Every existing
  `autoencoder.onnx` must be re-exported; the AI engine now refuses to start
  against a mismatched model instead of failing per event -- in NATS worker
  mode that was an exception log, a nak, five redeliveries and a silently
  dropped event, repeated forever, with nothing saying the model was simply
  the wrong shape.
- `build_real_dataset.py` spreads each real capture's time-of-day across
  24h before feature extraction (the treatment the synthetic generator
  always applied), and now folds in the multi-UE captures. Retrained: ROC
  AUC 0.9449 -> 0.9829, recall 0.69-0.73 -> 0.91-0.92 at every tier, zero
  false positives on a capture from a different day, and the ML path
  catches a real 3,000 pkt/s in-tunnel flood for the first time.
- `rate_per_second_norm` became `untunneled_rate_norm`: same index and
  width, but zero for any event carrying a TEID. The per-source rate is the
  same number for every subscriber behind a gNB, so the model was scoring
  innocent bystanders as anomalous during someone else's flood -- 150/192
  packets above the mitigation threshold in one training run, 0/192 after
  (§2.6.5). Kept for untunneled traffic, where it is the only rate signal.
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
- `prometheus-fastapi-instrumentator` 7.x -> 8.x in the AI engine. Not a
  feature: 7.x pins `starlette <1.0`, and `pip-audit` (which gates CI) now
  flags ten known vulnerabilities in every `starlette` 0.x release, fixed
  only in 1.3.1+. 8.x is the first line that allows `starlette` 1.x.
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
- **Kubernetes Events from the operator were never created.** The RBAC
  granted `events` in the core API group, but controller-runtime's
  `mgr.GetEventRecorder` writes through `events.k8s.io/v1` -- so every Event
  was rejected with "events.events.k8s.io is forbidden" and surfaced only as
  an error in the operator's own log. That silently disabled the
  `EBPFAttachFailed` Event from the moment it was added. Found by watching a
  real Event fail to land on a real cluster.
- **`helm upgrade` left the operator running on its old configuration.** The
  Deployment carried no ConfigMap checksum, and the operator reads its
  settings once at startup -- so changing any `config.*` or `nats.*` value
  updated the ConfigMap and nothing else, with the new values visible in
  `kubectl get cm` and not in effect.
- The AI engine image declared `USER sentinel5g` by name. Kubernetes cannot
  verify a named user is non-root, so any pod spec with
  `runAsNonRoot: true` refused to start it with
  `CreateContainerConfigError`. Now numeric (`65532:65532`), matching every
  other image here. It went unnoticed because
  `deployments/quickstart/ai-engine.yaml` sets no securityContext at all.
- **A benign score ended a mitigation and orphaned its eBPF block.**
  `ThreatScoreWatcher` moved any policy to `Monitoring` on a sub-threshold
  score, but reversal (`tryDeEscalate`) only runs in `Mitigating` -- so
  `blockedSourceIPs` and the kernel blocklist entry stayed forever. Latent
  while scores were rare; with the AI engine scoring every event from every
  source on the Pod, the benign score arrived milliseconds after the block,
  every time. A `Mitigating` policy now keeps its phase on a benign score
  (`Alerting`, which has no side effects, still returns to `Monitoring`).
- **`parse_gtpu()` dropped the tunnel on a bad extension header, and flagged
  Echo Requests as malformed.** Both caught in review. A T-PDU whose
  extension chain failed to parse was emitted with its TEID but never
  rate-tracked -- append one broken extension header to each flood packet
  and the per-tunnel counter never moved, a trivial detector bypass. And
  path-management messages (Echo, Error Indication, End Marker) were
  reported malformed, contrary to the documented contract, which would have
  fed the model a `malformed=1` on every routine keepalive. The parser now
  returns three states, rate-tracks whenever a TEID was read, validates the
  message type against the ones TS 29.281 defines, and the Python mirror
  matches; fuzz and property tests pin all of it.
- `ScoringPipelineReady`'s grace period is anchored where the process can
  actually receive a score (`ThreatScoreWatcher.Start`), not at
  construction -- a standby replica winning the lease after sitting idle
  reported `NoThreatScoresReceived` on every policy the instant it did. And a
  policy already reporting `False` is now rechecked every minute instead of
  waiting for the 10h resync.
- `charts/sentinel5g-ai-engine`'s `metrics` port and ServiceMonitor scraped a
  dead port in `config.mode: http`, where `/metrics` is served by the FastAPI
  app on `httpAddr` rather than the dedicated server on `metricsAddr`.
- `pkg/ebpf`'s ring-buffer decode went through reflection (`binary.Read`),
  costing ~176ns and four allocations per packet -- as much as the XDP
  program itself. Direct field reads: 25ns, two allocations (the two
  `net.IP`s). The per-packet XDP cost was also finally measured on the
  current program: ~190-240ns with the ring buffer live
  (`docs/paper-data/01-performance-benchmarks.md`).
- `scripts/quickstart.sh` re-run with a re-built image under the same local
  tag left the old Pod running; it now forces a rollout whenever images were
  kind-loaded.
- **Scores buffered during an operator restart were dropped.** JetStream
  redelivers everything the durable missed the instant `ThreatScoreWatcher`
  resubscribes, but `PolicyIndex` is only populated as `Reconciler` visits
  each policy -- so on a fresh process every buffered score was matched
  against an empty index, acked, and lost with a debug-level log line.
  `Start` now seeds the index from the cache before subscribing. Found by
  the e2e above: the very first real score after a rollout vanished.
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
