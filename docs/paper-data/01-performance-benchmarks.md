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

**Measured (idle only) on 2026-09-06**, the first time the operator has ever
run in-cluster from its published image (`ghcr.io/felipebastosxj/sentinel5g-operator:v0.1.0`,
via `helm install`, verified as part of testing the README Quickstart —
see `docs/getting-started.md` for the ownership gotcha hit along the way).
`kubectl top pod -n sentinel5g-system`: **1m CPU / 11Mi memory** at idle
(one reconciled policy, no sustained event traffic).

**Under stress: still not measured.** The `nats bench` run in 1.3 below used
a disposable throwaway subject, not `sentinel5g.threats.scored` — it
exercises NATS itself, not the operator's reconcile loop, so it doesn't
answer this question. A real measurement needs a burst of actual
`ThreatScoreEvent`s (or `NormalizedEvent`s through a running AI engine) on
the real subjects while sampling `kubectl top pod`.

AI engine: still not deployed in-cluster at all (the Quickstart's step 3
hand-publishes a `ThreatScoreEvent` directly, bypassing it entirely). No
request-level latency/in-flight instrumentation exists yet either
(`docs/observability.md`'s tracked `prometheus-fastapi-instrumentator` gap).

**Pending — next step:** deploy the AI engine in-cluster too (no Helm
template for it yet — only the operator has one; would need a plain
Deployment/Service or a chart addition), drive a real burst of
`NormalizedEvent`s (e.g. via `pkg/ingestion` against the Open5GS+UERANSIM
core's real traffic — see `02-ai-training-inference.md` §2.2/2.3 for that
same core already up), and sample `kubectl top pod` on both workloads
during it.

## 1.3 NATS JetStream metrics: pub/sub latency and throughput

**Measured** on 2026-09-06, via the `nats bench` subcommand against the
live in-cluster NATS (a disposable stream/subject created for the bench and
cleaned up after — not `sentinel5g.threats.scored` itself, so this is
NATS's own throughput ceiling, not this project's actual event rate):

```sh
nats bench js pub async <disposable-subject> --clients 2 --msgs 50000
```

| Metric | Value |
|---|---|
| Aggregate throughput | **270,821 msgs/sec** (~33 MiB/sec) |
| Per-op latency, P50 | 3.56ms |
| Per-op latency, P90 | 4.52ms |
| Per-op latency, P99 | 5.56ms |

At idle, `kubectl top pod` showed the NATS pod at `1m CPU / 4Mi memory` —
negligible, as expected with no load; a CPU/memory sample *during* the bench
run above wasn't captured.

**Read honestly:** this measures generic JetStream pub/sub throughput, not
this project's actual `sentinel5g.events.normalized`/`sentinel5g.threats.scored`
traffic shape (small, infrequent JSON events, not a 50k-message flood).
**Pending — next step:** repeat concurrently with a real signaling-storm
burst from the Open5GS+UERANSIM core (§2.2/2.3 in the AI-training file) on
the actual project subjects, so the measurement reflects this system's real
event shape/rate instead of synthetic bench traffic alone.
