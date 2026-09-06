# 1. Performance & Latency Metrics (Benchmarks)

Status up front, because it governs how to read everything below: **no
load-testing harness exists yet.** `ROADMAP.md` Phase 3 lists it as future
work ("Load-testing harness validating the <0.2ms/packet and single-digit-ms
mitigation-latency targets... under sustained throughput, not just unit
tests"). `README.md` and `docs/observability.md` both explicitly label the
`<0.2ms` / `<2%` CPU / single-digit-ms figures as **design targets**, not
measured benchmarks. This file keeps that distinction — it does not restate
the targets as if they were measurements.

## 1.1 Processing delay: eBPF (XDP) runtime inspecting GTP-U/SIP/SMPP

**Design target:** < 0.2ms added latency per packet at the XDP layer
(`docs/observability.md`).

**What is actually proven today (functional correctness, not latency):**
the WSL2 real-world validation round (2026-09-06, see
`memory/wsl2_real_test_environment.md`) attached `bpf/packet_filter.o` for
real to the host's `eth0` and sent a genuine 500+500 UDP packet burst on
port 2152 (GTP-U) and port 5060 (SIP) from the Windows host across the
WSL2 boundary. `bpftool map dump` on the `signal_rate` LRU map confirmed
the per-source sliding-window counter reset and counted exactly 1000 —
i.e., the classification and rate-limiting logic is correct under load, but
no wall-clock per-packet timestamp was captured in that run.

The Layer 1 → Layer 2/3 bridge (commit `3746a89`) separately proved the
full pipeline is functionally correct end to end against a live
Open5GS+UERANSIM core: 6 real ICMP packets through a real PDU session
produced 6 `NormalizedEvent`s and 6 `ThreatScoreEvent`s, correlated by
`sourceEventId`. Again: correctness, not a latency measurement.

**Pending — how to actually measure this:** `bpf/packet_filter.c` does not
currently timestamp packet entry/exit. The lowest-effort real measurement
path, once wanted:
```c
// at program entry
u64 t0 = bpf_ktime_get_ns();
// ... existing parse/classify logic ...
u64 dt = bpf_ktime_get_ns() - t0;
// write dt into a new BPF_MAP_TYPE_HISTOGRAM or PERCPU_ARRAY bucket
```
or, without touching the program at all, `bpftool prog profile` /
`xdp-bench` against the already-compiled `bin/bpf/packet_filter.o` can
sample cycles-per-packet directly. Neither has been run yet.

## 1.2 Resource consumption: Operator (Go) and AI Engine (Python/ONNX) under stress

**Status: not measured.** Checked directly against the live k3s cluster
(`kubectl get ns`) on 2026-09-06: no `sentinel5g-system` namespace exists —
the operator and AI engine are not currently deployed in-cluster (only the
`telecom-core` test namespace with the `amf-0`/`attacker` Istio-enforcement
test pods, plus the base k3s/NATS/Istio stack). There is nothing running to
put load on yet.

What instrumentation already exists to capture this once deployed:
- Operator: standard `controller-runtime` metrics on `:8080` (Prometheus
  format) — `controller_runtime_reconcile_total`, `workqueue_*` (depth,
  latency) — real, in `cmd/operator/main.go`. No AI-engine-specific
  instrumentation yet: `docs/observability.md` flags request-level
  latency/in-flight count on the FastAPI app as **not yet instrumented**,
  tracked in `ROADMAP.md` as a `prometheus-fastapi-instrumentator`
  integration.
- The k3s cluster already has `metrics-server` running (confirmed via
  `kubectl top nodes` returning real numbers: `felipe-pc 185m CPU (0%),
  3734Mi memory (23%)` at idle on 2026-09-06), so `kubectl top pod -n
  sentinel5g-system` will work immediately once the Helm chart is
  installed.

**Pending — next step:** `helm install sentinel5g charts/sentinel5g-operator
-n sentinel5g-system --create-namespace` into the existing WSL2 k3s
cluster, drive load (e.g. the `send_5g_traffic.py` burst script already
used for the XDP validation, or the new Open5GS+UERANSIM core once it is
up), and capture `kubectl top pod -n sentinel5g-system` samples over the
run.

## 1.3 NATS JetStream metrics: pub/sub latency and throughput

**Status: not measured under storm conditions.** What is real: NATS
JetStream is running in-cluster (`default/nats-*` pod, confirmed `Running`
throughout this session) and is exercised by real integration tests (CI
starts a real `nats:2.10-alpine -js` container — `.github/workflows/ci.yml`
— not a mock). At idle, `kubectl top pod` shows the NATS pod at `1m CPU /
4Mi memory` — negligible, as expected with no load.

No throughput/latency benchmark (events/sec, pub-to-sub latency
distribution) has been run against it. The `nats` CLI is already installed
in the WSL2 environment (used to hand-publish `ThreatScoreEvent`s during
earlier validation) and ships a bench subcommand suited to this:
```sh
nats bench sentinel5g.events.normalized --js --pub 4 --sub 2 --msgs 100000
```

**Pending — next step:** run `nats bench` against the live
`sentinel5g.events.normalized` / `sentinel5g.threats.scored` subjects,
ideally concurrently with a real signaling-storm burst from the
Open5GS+UERANSIM core (once up) so the measurement reflects the actual
event shape/rate this system produces, not synthetic bench traffic alone.
