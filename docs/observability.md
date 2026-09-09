# Observability

## Operator metrics

`cmd/operator` exposes standard `controller-runtime` metrics on
`METRICS_BIND_ADDRESS` (`:8080` by default) in Prometheus format,
including out of the box:

- `controller_runtime_reconcile_total{controller="telecomsecuritypolicy"}`
- `controller_runtime_reconcile_errors_total`
- `workqueue_*` (depth, latency) for the `TelecomSecurityPolicy` controller

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

These are the design targets referenced in `README.md`; they are **not**
automatically enforced by CI in this scaffold (no load-testing harness is
included yet — see `ROADMAP.md`):

| Metric | Target |
|---|---|
| Per-packet latency added by the XDP program | < 0.2ms |
| CPU overhead per worker node at 100k req/s | < 2% |
| Closed-loop mitigation latency (score → action) | single-digit milliseconds |

## Tracing threat decisions

Every automated mitigation is reflected onto the triggering
`TelecomSecurityPolicy`'s `.status`:

- `status.phase` (`Pending` → `Monitoring` → `Mitigating`/`Degraded`)
- `status.observedThreatScore` — the last score matched against this policy
- `status.lastMitigationTime` — set only when an actual mitigation fired
- `status.blockedSourceIPs` — every source IP this policy has *currently*
  pushed into the eBPF blocklist; also what the deletion finalizer unblocks
  before the policy object is actually removed. Entries are removed
  automatically once `Reconciler.tryDeEscalate`'s quiet-period timer fires
  (`DE_ESCALATION_DWELL`, default 5m since the last mitigation, not since
  each individual IP), not just on deletion (see `docs/architecture.md`)

```sh
kubectl get telecomsecuritypolicy -A -o wide
kubectl describe telecomsecuritypolicy protect-amf-core -n telecom-core
```

For a full incident trail, correlate `status.lastMitigationTime` against
the operator's structured logs (`pkg/controller.ThreatScoreWatcher` logs
every applied mitigation with `namespace`/`pod`/`sourceIp` fields) and the
AI engine's `ThreatScoreEvent.sourceEventId`, which traces back to the
originating `NormalizedEvent.eventId` from Layer 1.
