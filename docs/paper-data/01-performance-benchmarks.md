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

**Measured** on 2026-09-06, the first time both workloads have run
in-cluster from their published images
(`ghcr.io/felipebastosxj/sentinel5g-operator:v0.1.0` via the Helm chart;
`sentinel5g-ai-engine:v0.1.0` via a new plain manifest,
[`deployments/quickstart/ai-engine.yaml`](../../deployments/quickstart/ai-engine.yaml)
— no Helm template for the AI engine exists yet, this fills that gap with
the real model already trained locally delivered via a Secret, created
imperatively per that file's own header comment rather than committing
model bytes to the repo).

Idle baseline: operator **1m CPU / 11Mi memory**; AI engine **~1m CPU / low
double-digit Mi** (both negligible, as expected with no traffic).

Under real load (both bursts driven by a loop of `nats pub` calls — a
crude harness whose own per-process spawn overhead was the actual
bottleneck, not either pod; NATS itself separately benchmarked at 270k
msgs/sec in §1.3, so treat these as a floor, not a ceiling):

| Workload | Load | Duration / rate | Peak CPU | Peak memory |
|---|---|---|---|---|
| AI engine | 500 real `NormalizedEvent`s on `sentinel5g.events.normalized` | 7s (~71 msgs/s) | 48m | 49Mi |
| Operator | 300 real `ThreatScoreEvent`s on `sentinel5g.threats.scored` | 5s (~60 msgs/s) | 12m | 14Mi |

Both bursts were confirmed actually processed, not just received: the AI
engine burst produced exactly 500 corresponding `ThreatScoreEvent`s on the
output subject (`nats stream info`: 1,000 total messages = 500 in + 500
out); the operator burst moved `protect-amf-core`'s
`status.observedThreatScore` to `0.6648` with `lastMitigationTime` matching
the burst window, i.e. real reconciliation on every message, not a
saturated queue silently dropping load.

**Caveat, found while capturing this:** both pods restarted repeatedly
during this session (13 restarts on the operator, 3 on the AI engine
within 5 minutes) — but every one traces to `kubectl describe pod` /
`kubectl get events` showing `SandboxChanged: Pod sandbox changed, it will
be killed and re-created` hitting **both** pods simultaneously on a
roughly 75-second cadence, a containerd/CNI-level event, not an
application crash (exit code 0, no panic/error in the logs each time). This
is WSL2 network-stack instability (the same `cni0` bridge already noted
going link-down elsewhere this session — see
`memory/wsl2_real_test_environment.md`), not a Sentinel5G bug. The CPU/mem
numbers above are still real measurements taken during genuine load, just
worth knowing this environment's restarts aren't evidence of an app-level
resource leak or crash.

**Still pending:** a higher-throughput load driver (the current harness's
~60-70 msgs/s is far below NATS's own measured ceiling) and AI-engine
request-level latency instrumentation
(`docs/observability.md`'s tracked `prometheus-fastapi-instrumentator`
gap) to get a real p50/p99 scoring latency, not just aggregate CPU/mem.

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
