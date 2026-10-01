# Observability

## Operator metrics

`cmd/operator` exposes standard `controller-runtime` metrics on
`METRICS_BIND_ADDRESS` (`:8080` by default) in Prometheus format,
including out of the box:

- `controller_runtime_reconcile_total{controller="telecomsecuritypolicy"}`
- `controller_runtime_reconcile_errors_total`
- `workqueue_*` (depth, latency) for the `TelecomSecurityPolicy` controller

It also registers its own metrics on that same registry and endpoint
(`pkg/controller/metrics.go` — no second HTTP server, so nothing on the
deployment side changes to scrape them):

| Metric | Type | Labels | What it tells you |
|---|---|---|---|
| `sentinel5g_threat_scores_received_total` | counter | `source` | Every `ThreatScoreEvent` consumed from the bus. **Flat at zero means the scoring half of the system isn't running** — the AI engine was never deployed, or has no model. See "Is the scoring pipeline alive?" below. |
| `sentinel5g_threat_score` | histogram | — | Distribution of received scores in `[0,1]`, for calibrating `THREAT_SCORE_THRESHOLD` against what your traffic actually produces. |
| `sentinel5g_threshold_crossings_total` | counter | `namespace`, `policy`, `outcome` | A score crossed this policy's effective threshold. `outcome="alerting"` = withheld by `autoMitigate: false`; `outcome="mitigating"` = acted on. |
| `sentinel5g_mitigations_total` | counter | `action`, `result` | Individual mitigation actions attempted (`ebpf_block`/`mesh_quarantine` × `success`/`error`). |
| `sentinel5g_policy_phase` | gauge | `namespace`, `policy`, `phase` | `1` for each policy's current phase, `0` for its other phases — so `sum by (phase) (sentinel5g_policy_phase)` counts policies. |
| `sentinel5g_blocklist_drift_total` | counter | `kind`, `direction` | Enforcement entries the kernel-vs-status reconciliation had to correct. `direction="missing"` = status claimed a drop the kernel did not have (**traffic an operator believed was blocked was flowing**); `direction="extra"` = the kernel held a drop no policy claimed. |
| `sentinel5g_blocklist_entries` | gauge | `kind`, `state` | Size of each side of that comparison before any correction: `state="desired"` is the union of every policy's status, `state="kernel"` is what the maps actually held. |
| `sentinel5g_blocklist_capacity` | gauge | `kind` | `max_entries` of each enforcement map, so the gauge above has a denominator. |
| `sentinel5g_blocklist_reconciles_total` | counter | `result` | Reconciliation passes, `success`/`error`. |
| `sentinel5g_ebpf_enforcement_pinned` | gauge | — | `1` when this node's enforcement maps are pinned to bpffs and survive an operator restart, `0` when not. Absent entirely when eBPF isn't attached. |
| `sentinel5g_ebpf_observation_map_occupancy` | gauge | `kind` | Live entries in the per-tunnel **rate** (LRU) map on this node. Approaching its capacity means the kernel is about to evict rate counters and detection degrades silently — the only warning there is, since an LRU exposes no eviction count. |
| `sentinel5g_ebpf_observation_map_capacity` | gauge | `kind` | `max_entries` of that rate map (`MAX_TUNNEL_ENTRIES`). |
| `sentinel5g_mitigation_map_full_total` | counter | `kind` | A mitigation was **refused** because the enforcement map was at capacity. The maps are plain `HASH` and refuse rather than evict, so any non-zero rate here is drops being denied — an attacker generating distinct TEIDs is one way to get there. |

`source` on the first metric is a **closed set** (`model`, `rule`,
`unknown`), not the raw `ThreatScoreEvent.model` string. That's deliberate:
anything able to reach an unauthenticated `NATS_URL` can forge a
`ThreatScoreEvent` (see `docs/integrations.md`), and a wire-supplied label
value is an unbounded-cardinality hole in Prometheus.

### Measuring a detection-only pilot's false-positive rate

This is what `outcome="alerting"` exists for. Run the pilot with
`autoMitigate: false` (see `docs/production-install.md`), and:

```promql
# Crossings per second that WOULD have mitigated, per policy.
sum by (namespace, policy) (rate(sentinel5g_threshold_crossings_total{outcome="alerting"}[5m]))

# As a fraction of everything scored — the number to judge before flipping
# autoMitigate on.
sum(rate(sentinel5g_threshold_crossings_total{outcome="alerting"}[5m]))
  / sum(rate(sentinel5g_threat_scores_received_total[5m]))
```

Read it for what it is: every crossing during a pilot on traffic you believe
to be benign is a false positive you would have acted on. It is not a
validated detection rate — nothing here labels true positives. The
`scripts/loadtest/` harness measures latency and throughput, not detection
quality; for what the detector's recall actually is on real captures see
`docs/paper-data/02-ai-training-inference.md` §2.6.

### Is enforcement actually on?

The question this section exists to answer is narrower than it sounds, and
it is the one ROADMAP.md Phase 4 opens with: *is the kernel dropping what
the policies say it is dropping?* Until Phase 4 nothing could answer it.
The enforcement maps lived and died with the operator process, so a
rollout, an OOM kill or a crash un-blocked every active mitigation while
each policy's status went on asserting them — `Phase: Mitigating`,
`status.blockedTunnels` populated, traffic flowing.

Two mechanisms close that, and they do different jobs:

```promql
# 1. Do drops survive this process at all?
min(sentinel5g_ebpf_enforcement_pinned)
```

`0` means the enforcement maps are **not** pinned to bpffs on at least one
node, so everything that node is dropping is lost the moment the operator
restarts. That is the documented behaviour with `BPF_PIN_PATH=""`, and it
is also what you get when the Pod has no `/sys/fs/bpf` mount — in which
case the operator attached anyway (losing XDP entirely over a mount problem
would be worse) and recorded an `EBPFPinUnavailable` warning Event saying
so. `kubectl describe pod` on the operator is where that lands.

```promql
# 2. Did the kernel and the policies' status ever disagree?
sum by (direction) (rate(sentinel5g_blocklist_drift_total[15m]))
```

A burst of `direction="missing"` right after a restart *is* the replay
working — the operator re-applying from status what the kernel lost. A
sustained non-zero rate at any other time is a defect: something is
removing drops that nothing asked to remove. `direction="extra"` is the
quieter counterpart, enforcement outliving the policy that asked for it.

Removals require an entry to be unclaimed on **two consecutive passes**,
and additions do not. That asymmetry matters when reading the numbers: a
drop placed in the kernel seconds ago is briefly claimed by nothing the
reconciler can see — the mitigation path writes the map before it writes
the status, and the status is then read through a cache that lags the API
server again — so removing on sight would revert live mitigations. Erring
toward enforcing is the deliberate direction. The practical consequence is
that a genuinely stale entry disappears one interval later than you might
expect.

```promql
# 3. How close is the mitigation path to refusing new drops?
max by (kind) (
  sentinel5g_blocklist_entries{state="kernel"} / on(kind) sentinel5g_blocklist_capacity
)
```

Both enforcement maps are plain `HASH`, not `LRU_HASH`, so a full map
**refuses** the next insert rather than evicting someone else's drop (see
`CLAUDE.md`'s memory-footprint rule for why that is the security-correct
choice). The cost of that choice is that approaching the ceiling means
approaching the point where new mitigations start failing outright, which
is an attacker-reachable state for anyone who can generate distinct TEIDs.
The alerting threshold and the operational response to it are tracked as
open work in ROADMAP.md Phase 4; this ratio is the input to them.

```promql
# 4. Is the rate map about to start evicting (detection degrading silently)?
max by (kind) (
  sentinel5g_ebpf_observation_map_occupancy / on(kind) sentinel5g_ebpf_observation_map_capacity
)
# 5. Are mitigations being REFUSED because an enforcement map is full?
sum(rate(sentinel5g_mitigation_map_full_total[5m]))
```

Query 4 approaching 1 means the per-tunnel rate map (`MAX_TUNNEL_ENTRIES`,
65,536) is near capacity for this node's real bearer cardinality; past it the
LRU evicts the coldest counter and a rate window resets mid-flight, so a flood
can be undercounted at exactly the load the detector exists for. There is no
eviction counter to watch — occupancy is the signal, and the fix is to raise
`MAX_TUNNEL_ENTRIES` in `bpf/headers/common.h` and rebuild (note that changing
it resets the pins, see `EBPFPinsReset`). Query 5 non-zero means
`tunnel_blocklist` (or `blocklist`) is full and legitimate drops are being
refused; the operational response is in `docs/troubleshooting.md`.

The reconciliation itself runs once at startup — that is the replay — and
then every `BLOCKLIST_RECONCILE_INTERVAL` (`config.blocklistReconcileInterval`,
default `1m`). Status is treated as the desired state and the map as the
actual one, so an entry in status and not in the kernel is re-applied, and
an entry in the kernel claimed by no policy is removed.

### Is the scoring pipeline alive?

```promql
# Zero over any window means nothing has ever scored.
sum(rate(sentinel5g_threat_scores_received_total[15m]))
```

A cluster in that state has every policy parked at `Phase: Monitoring`
forever, because nothing ever publishes a score for the operator to act on.
You don't have to be scraping metrics to see it — the operator also reports
it on the policies themselves:

```sh
kubectl get tsp -A
# NAME               PHASE        SCORE   SCORING   AGE
# protect-amf-core   Monitoring           False     42m
```

`SCORING` is the `ScoringPipelineReady` condition. `False` with reason
`NoThreatScoresReceived` (see `kubectl describe tsp`) means the operator is
subscribed to the threat-score subject but no score has *ever* arrived —
almost always an AI engine that was never deployed, one running without a
model, or one crash-looping on a model of the wrong width (it refuses to
start rather than fail per event; `kubectl logs` names the re-export
command). `SCORING_PIPELINE_GRACE` (`config.scoringPipelineGrace`, default
`10m`) is how long after startup it waits before saying so; inside that
window the reason is `AwaitingFirstScore` instead.

The condition is deliberately "has *ever* scored", not a liveness rate: quiet
is the normal, healthy state of a network under no attack, so no rate
distinguishes "no threats today" from "the AI engine is gone". Continuous
liveness is what `sentinel5g_threat_scores_received_total` above is for.

Health/readiness endpoints are served on `HEALTH_PROBE_BIND_ADDRESS`
(`:8081` by default): `/healthz` and `/readyz`.

The Helm chart exposes both ports through an always-on `Service`
(`<release>-sentinel5g-operator`, ports named `metrics`/`health`) — reach
`/metrics` at `http://<service>:8080/metrics` from inside the cluster
regardless of how you scrape it. For clusters running
[Prometheus Operator](https://prometheus-operator.dev/), set
`serviceMonitor.enabled: true` to get a matching `ServiceMonitor` instead of
wiring a `scrape_config` by hand (needs the `monitoring.coreos.com/v1`
`ServiceMonitor` CRD already installed). `podDisruptionBudget.enabled: true`
bounds how many replicas a node drain/cluster upgrade can evict at once —
see `values.yaml`'s comment for why it defaults to `maxUnavailable: 1`
rather than `minAvailable`.

A `helm upgrade` that changes any `config.*`/`nats.*` value now rolls the
operator Pod (a ConfigMap checksum annotation on the Deployment — before
it, the new values landed in the ConfigMap and never took effect). One
consequence: a rollout restarts the `SCORING_PIPELINE_GRACE` clock, so the
condition briefly reads `AwaitingFirstScore` again.

`charts/sentinel5g-ai-engine` mirrors all of this for the engine: an
always-on `Service` (`<release>-sentinel5g-ai-engine`, port `metrics` →
`config.metricsAddr`, `:9090` in NATS mode; plus `http` in HTTP mode),
`serviceMonitor.enabled` with the same CRD guard, and
`podDisruptionBudget.enabled` with the same `maxUnavailable` reasoning.

## AI engine metrics

`cmd/ai-engine` exposes Prometheus metrics in both run modes
(`AI_ENGINE_MODE`), sharing the same metric definitions
(`sentinel_ai/metrics.py`) regardless of which one is running:

- `sentinel5g_ai_score_latency_seconds` — time spent in
  `ScoringEngine.score_event`, recorded synchronously around every scoring
  call from either mode.
- `sentinel5g_ai_nats_events_total{outcome="scored"|"malformed"|"publish_failed"}`
  — NATS-worker-mode only: how `run_nats_worker`'s handler resolved each
  event (mirrors its three `msg.ack()`/`msg.term()`/`msg.nak()` branches
  1:1).

**HTTP mode** (`AI_ENGINE_MODE=http`, the default): `/metrics` is exposed on
the same port as `/v1/score`/`/healthz` (`AI_ENGINE_HTTP_ADDR`) via
[`prometheus-fastapi-instrumentator`](https://github.com/trallnag/prometheus-fastapi-instrumentator),
which also adds automatic per-endpoint request count/latency/size metrics
(`http_requests_total`, `http_request_duration_seconds`, ...).

**NATS worker mode** (`AI_ENGINE_MODE=nats`): there's no HTTP app in this
mode at all, so `/metrics` is served by a small dedicated
`prometheus_client` HTTP server on `AI_ENGINE_METRICS_ADDR` (`:9090` by
default) instead.

## Target SLOs

These are the design targets referenced in `README.md`. Two of the three
are now measured by the harness in `scripts/loadtest/`
(`docs/paper-data/01-performance-benchmarks.md` §1.4); the CPU-per-node
target is not. **None of them is enforced by CI** — the measurements need
root, a real kernel and a cluster, so they are run by hand and recorded
with a date and a host:

| Metric | Target |
|---|---|
| Per-packet latency added by the XDP program | < 0.2ms |
| CPU overhead per worker node at 100k req/s | < 2% |
| Closed-loop mitigation latency (score → action) | single-digit milliseconds |

## Tracing threat decisions

Every automated mitigation is reflected onto the triggering
`TelecomSecurityPolicy`'s `.status`:

- `status.phase` (`Pending` → `Monitoring` → `Alerting`/`Mitigating`/
  `Degraded`). `Alerting` means the threshold was crossed but
  `autoMitigate: false` withheld the action — a working detection-only
  pilot, not a fault; `Degraded` is reserved for a genuine operator-side
  failure (a mesh or eBPF action erroring)
- `status.observedThreatScore` — the last score matched against this policy
- `status.lastMitigationTime` — set only when an actual mitigation fired
- `status.blockedTunnels` — every GTP-U tunnel this policy has *currently*
  dropped, as `<source ip>/<teid hex>` (e.g. `10.0.0.1/0x4d84`). The
  precise mitigation: one subscriber, not the peer address every subscriber
  behind a gNB shares. Reversed by the same finalizer and quiet-period
  timer as the list below
- `status.blockedSourceIPs` — every source IP this policy has *currently*
  pushed into the eBPF blocklist; also what the deletion finalizer unblocks
  before the policy object is actually removed. Entries are removed
  automatically once `Reconciler.tryDeEscalate`'s quiet-period timer fires
  (`DE_ESCALATION_DWELL`, default 5m since the last mitigation, not since
  each individual IP), not just on deletion (see `docs/architecture.md`)
- `status.conditions[ScoringPipelineReady]` — whether any score has ever
  arrived (see "Is the scoring pipeline alive?" above); surfaced as the
  `SCORING` column in `kubectl get tsp`

Which detector produced a given score is in the `ThreatScoreEvent` itself:
`model: "autoencoder-v1"` for the ML path, `model: "rule:gtpu-tunnel-flood"`
for the deterministic detector (`docs/integrations.md`). The operator's
structured log for the mitigation carries it, and
`sentinel5g_threat_scores_received_total{source="model"|"rule"}` splits the
two rates.

```sh
kubectl get telecomsecuritypolicy -A -o wide
kubectl describe telecomsecuritypolicy protect-amf-core -n telecom-core
```

For a full incident trail, correlate `status.lastMitigationTime` against
the operator's structured logs (`pkg/controller.ThreatScoreWatcher` logs
every applied mitigation with `namespace`/`pod`/`sourceIp` fields) and the
AI engine's `ThreatScoreEvent.sourceEventId`, which traces back to the
originating `NormalizedEvent.eventId` from Layer 1.
