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
