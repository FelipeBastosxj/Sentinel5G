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
`test-environment.md`) attached `bpf/packet_filter.o` for
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

**Measured** on 2026-09-06, via the kernel's own BPF stats accounting
(`kernel.bpf_stats_enabled=1`), which makes `bpftool prog show` report real
cumulative `run_time_ns`/`run_cnt` for a running program — no code changes
to `bpf/packet_filter.c` needed:

```sh
ip link set dev lo xdp obj bpf/packet_filter.o sec xdp   # attach (generic mode -- lo has no native XDP)
sysctl -w kernel.bpf_stats_enabled=1
bpftool prog show id <id>                                  # baseline run_time_ns/run_cnt
ping -I uesimtun0 -f -c 2000 -q 8.8.8.8                     # real GTP-U burst through the live PDU session
bpftool prog show id <id>                                   # delta / delta run_cnt = ns/invocation
```

Attached to `lo`, the same interface the live Open5GS+UERANSIM core's real
GTP-U traffic already flows on (confirmed via `/etc/open5gs/upf.yaml`'s N3
address) — safe to do because the `blocklist` map was confirmed empty
before attaching (nothing to drop) and the PDU session/`uesimtun0` tunnel
were confirmed still up both before and after.

| Sample | Invocations | ns/packet |
|---|---|---|
| Burst 1 | ~5,990 | 821.2 |
| Burst 2 | ~2,243 | 734.6 |

**~735–821ns per packet (0.00074–0.00082ms) — roughly 240–270× under the
<0.2ms design target.** Two independent samples landed within 11% of each
other, not a single fluke reading.

**Dated, and pending re-measurement (2026-09-13):** this was the program
*before* Phase 2.5 added `parse_gtpu()` (a `#pragma unroll`ed extension-
header walk), the `tunnel_rate` map update on every GTP-U packet, and grew
the ring-buffer record from 24 to 32 bytes — xlated size went 6936 → 9880
bytes. The load path is intact and verified (verifier accepts it, max stack
152 B), but per-packet cost has not been re-measured. The native-Linux lab
(`test-environment.md`) now sustains ~125,000 pkt/s through one tunnel, so
the next measurement can be taken at real rates rather than the ~60 pkt/s
this one was, with `bpftool map dump` on `tunnel_rate` as the per-TEID
counterpart of the `signal_rate` dump used above.
**Read honestly:** `lo` has no native XDP driver, so this ran in
`xdpgeneric` mode. `run_time_ns` measures time *inside the BPF program
itself* (parse + classify + map lookups) — real and reproducible — but
does **not** include the generic-XDP softirq/skb-allocation dispatch
overhead that precedes the program in this mode, which native XDP on a
real NIC (the actual production target `docs/architecture.md` describes)
wouldn't have at all. So this measures the classification logic's own
cost accurately; it is not a substitute for a native-XDP NIC benchmark,
which would need a real (non-loopback) interface to attach to.

Cleanup: `ip link set dev lo xdp off` (confirmed fully unloaded —
`bpftool prog show id <id>` afterward returns "No such file or
directory"), `kernel.bpf_stats_enabled` reset to `0`.

## 1.2 Resource consumption: Operator (Go) and AI Engine (Python/ONNX) under stress

**Measured** on 2026-09-06, the first time both workloads have run
in-cluster from their published images
(`ghcr.io/felipebastosxj/sentinel5g-operator:v0.1.0` via the Helm chart;
`sentinel5g-ai-engine:v0.1.0` via a new plain manifest,
[`deployments/quickstart/ai-engine.yaml`](../../deployments/quickstart/ai-engine.yaml)
— at the time no Helm template for the AI engine existed and this manifest
filled that gap, with a locally trained model delivered via a hand-created
Secret. Since Phase 2.5 the supported path is `charts/sentinel5g-ai-engine`
with a published model artifact; the manifest is kept for the
bring-your-own-Secret case only).

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
`test-environment.md`), not a Sentinel5G bug. The CPU/mem
numbers above are still real measurements taken during genuine load, just
worth knowing this environment's restarts aren't evidence of an app-level
resource leak or crash.

**Partly resolved since:** the AI-engine request-level latency gap is
closed — `sentinel5g_ai_score_latency_seconds` (ROADMAP.md Phase 2) records
every `ScoringEngine.score_event`, in both run modes — and the operator now
has its own decision-path metrics too (`docs/observability.md`). The
higher-throughput load driver on the *bus* is still pending. Note, though,
that the ~60-70 pkt/s figure this environment could push through a GTP-U
tunnel turned out not to be an environment limit at all: the same tunnel
on the native-Linux lab carries ~125,000 pkt/s from a real UDP generator
(`test-environment.md`), so the harness in ROADMAP.md Phase 3 can be built
against real rates rather than around WSL2.

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
burst on the actual project subjects, so the measurement reflects this
system's real event shape/rate instead of synthetic bench traffic alone.
The burst now exists as a committed, reproducible artifact:
`real-dataset-v2/multi_ue_one_flooding.pcap` (3,000 pkt/s through one
tunnel) and the `gen_tunnel_flood.py` that produces it at any rate the lab
supports.
