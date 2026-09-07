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

## AI engine metrics

`cmd/ai-engine`'s FastAPI app exposes `/healthz`. Request-level metrics
(latency, in-flight count) are not yet instrumented — tracked in
`ROADMAP.md` as a `prometheus-fastapi-instrumentator` integration.

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
