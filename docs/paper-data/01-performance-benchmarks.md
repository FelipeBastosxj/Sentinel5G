# 1. Performance & Latency Metrics (Benchmarks)

Status up front, because it governs how to read everything below: **two of
the three design targets are now measured, one is not.** The load-testing
harness asked for by `ROADMAP.md` Phase 3 exists (`scripts/loadtest/`) and
§1.4 records what it found for the `<0.2ms`/packet and single-digit-ms
mitigation-latency targets, together with the limits of each method. The
`<2%` CPU-per-node target has **no** sustained-load measurement behind it
and remains a design target, labelled as one everywhere it appears. Sections
1.1–1.3 predate the harness and keep their own, narrower provenance notes;
they are not retro-fitted with §1.4's numbers.

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

**Dated:** this was the program *before* Phase 2.5 added `parse_gtpu()`
(a `#pragma unroll`ed extension-header walk), the `tunnel_rate` map update
on every GTP-U packet, and grew the ring-buffer record from 24 to 32 bytes
— xlated size went 6936 → 9896 bytes.

**Re-measured, 2026-09-13**, on the native-Linux host (Linux 6.14,
`test-environment.md`), by a different method: `bpftool prog run`
(`BPF_PROG_TEST_RUN`) executes the loaded program in the kernel against a
synthetic packet N times and reports the mean. Reproduce from the repo root
with the program loaded at `/sys/fs/bpf/x` and the packets from
`docs/paper-data/01-performance-benchmarks.md`'s companion snippet below:

| Packet | repeat | Ring buffer state | ns/packet |
|---|---|---|---|
| GTP-U T-PDU, flags `0x34`, one ext header, 84-byte inner | 1,000 | live (every packet `submit`s) | 240 |
| same | 4,000 | live | 188 |
| same | 8,000 | filling (256 KB / 32 B = 8,192 records) | 236 |
| same | 50,000 | full (observation dropped, cheap path) | 158 |
| same | 1,000,000 | full | 141 |
| SIP, port 5060 | 1,000,000 | full | 126 |
| zero-filled UDP at port 2152 (fails GTP-U validation) | 1,000,000 | full | 128 |
| UDP at an off-signaling port | 1,000,000 | full | 113 |

**~190–240 ns per GTP-U packet with the ring buffer live, roughly 800× under
the <0.2 ms design target.** The GTP-U parser plus the `tunnel_rate` map
update cost about **15 ns** over the SIP path (141 vs 126 ns on the same
cheap path), which is the number this section was left open to find.

Read the two methods against each other honestly: the 2026-09-06 figure
(735–821 ns) was `run_time_ns/run_cnt` over *live* traffic in WSL2 — cold
maps, a real NIC driver path, and a userspace reader draining the ring —
while `prog run` replays one packet with hot caches and no consumer, so it
measures the program's own instructions rather than a deployment. Both
answer the CLAUDE.md question the same way; neither is a sustained-load
benchmark — that is §1.4, which re-measures the same program under a
repeat sweep up to a million iterations and against a saturated map.

```sh
# packets: Ethernet + IPv4 127.0.0.1 -> 127.0.0.7 + UDP; see the git history
# of this file for the Python that built /tmp/pkt_*.bin
sudo bpftool prog load bpf/packet_filter.o /sys/fs/bpf/x
sudo bpftool prog run pinned /sys/fs/bpf/x data_in /tmp/pkt_gtpu.bin data_out /dev/null repeat 4000
```
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
higher-throughput load driver on the *bus* is still pending: §1.4's harness
drives the kernel path and the closed loop, not NATS itself. Note, though,
that the ~60-70 pkt/s figure this environment could push through a GTP-U
tunnel turned out not to be an environment limit at all: the same tunnel
on the native-Linux lab carries ~125,000 pkt/s from a real UDP generator
(`test-environment.md`), which is what made §1.4 measurable at real rates
rather than around WSL2.

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

**Read honestly:** the 270k figure is `nats bench`'s *async* pub (fire many,
reconcile acks in bulk) — NATS's own ceiling, not this project's publish
path. The operator's `pkg/ingestion.Publisher` publishes **synchronously**
(it waits for each JetStream ack), which is the number that actually bounds a
node, so it was measured directly (2026-10-01,
`pkg/events/throughput_bench_test.go` against a live server, real
`NormalizedEvent` payloads):

| Publish path (one goroutine) | Throughput |
|---|---|
| synchronous, one event per message | **~15,000 msg/s = ~15,000 events/s** |
| batched (batch 16) | 11,490 msg/s → **183,848 events/s** |
| batched (batch 64) | 6,867 msg/s → **439,481 events/s** |
| batched (batch 256) | 2,711 msg/s → **693,964 events/s** |

This is the measurement that makes §1.5's batching item load-bearing rather
than nice-to-have: a single synchronous publisher sustains only ~15k events/s,
so **100k pkt/s on one node cannot be published unbatched** — it is below the
per-publisher ceiling. Batching lifts the *event* rate ~46× (to ~694k/s at
batch 256) even as the message rate falls, which is exactly the trade that
keeps the bus from being the bottleneck. Multiple per-node publishers
aggregate above this; the point is the per-publisher floor, and that batching
clears it.

## 1.4 Load-testing harness: both SLOs measured, both open questions settled

**Measured** on 2026-09-21 on the native-Linux host
([`test-environment.md`](test-environment.md), Linux 6.14), against the
current program — the one with GTP-U parsing, per-TEID rate tracking and
the per-tunnel drop map. Reproduce with `scripts/loadtest/`; that
directory's README states what each method can and cannot see, and those
caveats are load-bearing for how these numbers should be read.

```sh
make -C bpf
sudo scripts/loadtest/xdp_bench.sh
sudo KUBECONFIG=... scripts/loadtest/mitigation_latency.sh
```

### Per-packet XDP cost (`< 0.2 ms` target)

`bpftool prog run` replays one real frame N times through the real loaded
program. The repeat sweep matters: the ring buffer holds 8,192 records
(256 KB ÷ 32 B) and this method has no consumer draining it, so past that
point every submit fails fast and the cost *drops*. The `live` rows are the
honest figure.

| Packet | ring live (1k / 4k / 8,192) | ring saturated (50k / 1M) |
|---|---|---|
| GTP-U T-PDU, flags `0x34`, one ext header | 204 / 195 / 211 ns | 182 / 170 ns |
| SIP (port 5060) | 194 / 151 / 144 ns | 137 / 137 ns |
| Zero-filled UDP at port 2152 (fails validation) | 149 / 151 / 208 ns | 123 / 125 ns |
| UDP at an off-signaling port | 144 / 112 / 107 ns | 119 / 127 ns |

**~195–211 ns for a full GTP-U parse — roughly 1,000× under the 0.2 ms
target.** The parse, the tunnel-rate update and the tunnel-blocklist lookup
together cost about 60 ns over the off-port path.

### Closed-loop mitigation latency (single-digit-ms target)

Two halves, because they fail for different reasons:

| Half | Measured |
|---|---|
| `Block(ip)` — the eBPF map write | 733 ns/op, 2 allocs |
| `BlockTunnel(ip, teid)` | 762 ns/op, 2 allocs |
| `BlockTunnel`, distinct TEIDs (a mitigation storm: every write a new insert) | 865 ns/op |
| Published score → `Phase: Mitigating` (kind cluster) | **2 ms** mean over 6 samples |

Every eBPF map write is visible to the XDP program on the very next packet,
so the first three rows *are* the time from decision to enforcement — there
is no propagation delay to add.

The 2 ms figure needed method work to be worth anything: polling
`kubectl get` cannot resolve it, because one round trip to this API server
costs **32 ms** — more than the thing being measured. An earlier run using
polling reported 33 ms and was measuring `kubectl`. The harness now opens a
`--watch` before publishing and times the first `Mitigating` the API server
streams, and prints the round-trip cost first so the floor is visible
rather than implied.

### The two questions `ROADMAP.md` Phase 3 attached to this item

**Does `blocklist` need LRU semantics?** No, and the reasoning is a
security one rather than a performance one. An entry is a mitigation the
operator owns the lifetime of — removed by de-escalation or a policy's
finalizer — so an LRU silently evicting one would un-block traffic nobody
asked to un-block: a control failing *open* under exactly the load that
created the entries. `blocklist` and `tunnel_blocklist` are both plain
`HASH`; a full map refuses the insert, and the refusal surfaces as
`sentinel5g_mitigations_total{result="error"}` and a failed reconcile.
Pinned by `TestBlocklistIsBoundedAndFailsLoudlyWhenFull`
(`go test -tags privileged`): at 16,384 entries the next insert is refused
with `key too big for map`, not swallowed. The rate maps are LRU precisely
because they are the opposite case — an evicted counter costs one missed
observation, never a wrong enforcement decision.

**Does `signaling_events` hold up under telecom-scale volume?** It holds up
in the way it was designed to, and the design's trade is worth stating
plainly. The ring holds 8,192 in-flight records; when it fills,
`emit_signaling_event` drops *the observation* and the packet still passes
— Layer 1 never drops traffic because Layer 2 fell behind. The saturated
column above is that path, and it is *cheaper* than the live one, so
overload does not compound. At ~200 ns/packet, 8,192 records is ~1.6 ms of
drain headroom at line rate. That is comfortable for a consumer doing a
channel send and would not be for one doing blocking I/O — which is the
real constraint this establishes on anything added to
`pkg/ingestion.Publisher`'s hot path, and why it drains the ring even while
NATS is disconnected.

## 1.5 AI-engine throughput per replica

Score *latency* has been instrumented since Phase 2 (`SCORE_LATENCY_SECONDS`),
but `ROADMAP.md` Phase 4 noted the engine scores one event per ONNX call with
no batching and nothing had measured how many events per second a single
replica sustains — the number that sizes a deployment. `scripts/throughput_bench.py`
measures it (reproducible: `python scripts/throughput_bench.py`), on the host
in `test-environment.md`, single process, single thread, `CPUExecutionProvider`:

| Path | Events/sec (one replica) |
|---|---|
| Inference only (`score_features`) | ~106,000 |
| **Production call (`score_event`: extract + infer + latency timer)** | **~72,000** |
| Full wire path (`from_dict` → extract → infer) | ~84,000 |
| Batched inference, batch 256 (one ONNX run) | ~32,800,000 |

The sizing number is **~72,000 events/sec per replica** — the NATS worker
calls `score_event`. That comfortably exceeds one node's plausible signaling
rate, so the replica count is set by the aggregate across nodes and by the
NATS ceiling (§1.3), not by the model: scoring is not the bottleneck this
project worried it might be.

The batched row is the striking one and it is the finding, not a flourish:
the same vectors run **~300× faster** per event in one ONNX call than one at a
time. The engine does not batch today (one call per NATS message), so that
300× is headroom a batching consumer would unlock — which is exactly why the
NATS-side batching item (Phase 4) is where the real throughput ceiling lives,
not in the model. One replica is nowhere near its own inference limit; the
message bus gets there first.

## 1.6 Single-active consumer, and the indexes it leans on at scale

`ThreatScoreWatcher` is leader-gated: one process consumes every score for
the whole cluster. That is correct for idempotency (one writer of each
policy's status, see its doc comment) and a potential throughput ceiling, and
`ROADMAP.md` Phase 4 noted the tradeoff was never measured.
`throughput_bench_test.go` measures it (`go test ./pkg/controller/ -bench
'Watcher|Index' -run x -benchmem`), on the host in `test-environment.md`:

| Benchmark | Result | Reading |
|---|---|---|
| `WatcherHandle` sub-threshold steady state | ~17 µs/op → **~58,000 scores/sec** | the single-consumer drain rate for scores that need no API write (the dominant case, since meaningful writes are bounded by distinct threats and the reactor rate limit throttles the rest). Fake-client-bound — the real informer cache is faster — so this is a floor. |
| `PodIPIndex.Lookup`, 1,000 pods | ~9 ns/op, 0 allocs | O(1) map lookup |
| `PodIPIndex.Lookup`, 50,000 pods | ~12 ns/op, 0 allocs | **flat** — the "never exercised at cluster scale" concern answered: pod resolution does not degrade with cluster size |
| `PolicyIndex.MatchingPolicies`, 10 policies | ~0.3 µs/op | — |
| `PolicyIndex.MatchingPolicies`, 1,000 policies | ~23 µs/op | linear in policy count, but sub-25 µs even at 1,000 policies per namespace, far above any realistic count |

The picture: one consumer sustains tens of thousands of scores per second,
the AI engine produces ~72,000 per replica (§1.5), and neither in-memory index
is the limit — `PodIPIndex` is flat O(1), `PolicyIndex` is linear but tiny at
realistic policy counts. So the single-active consumer is a real ceiling only
when the *aggregate* score rate across all nodes approaches ~50k/s, and the
lever there is sampling upstream (the NATS item), **not** sharding the
consumer — which would reintroduce the multi-writer races the leader gate
exists to prevent. The tradeoff is deliberate and now quantified.

## 1.7 CPU and ring saturation under a sustained real load

§1.4's per-packet figure (195-211 ns) is `BPF_PROG_TEST_RUN` — hot caches,
no consumer, no attach point — and `ROADMAP.md` Phase 4 was right that the
ring-buffer headroom behind it was *arithmetic*, not an observation.
`scripts/loadtest/cpu_saturation.sh` replaces the arithmetic with a
measurement: a veth pair (a real driver RX path, unlike loopback), the XDP
program attached to the RX end, the `SignalingEvents` consumer draining the
ring, and valid GTP-U frames blasted at it from the TX end. Stable across
runs on the host in `test-environment.md`:

| Quantity | Measured |
|---|---|
| Offered load (one generator thread) | ~386,000 pkt/s |
| Program executions | = frames sent (every frame hit the XDP program) |
| **Ring observations dropped** | **~14 out of ~2.3 million (<0.001%)** |
| Program CPU per packet (BPF `run_time/run_count`) | ~900 ns |
| Program CPU share | ~34.8% of one core at 386k pkt/s |

Two findings, both replacing an assumption with a number:

- **The ring buffer does not saturate.** At 386k pkt/s with a real consumer
  attached, fourteen observations in 2.3 million were dropped. The "~1.6 ms
  of drain headroom" §1.4 computed is now observed to hold under a load
  nearly four times the roadmap's 100k pkt/s/node concern — the consumer
  keeps up, and the packet is never dropped regardless (a full ring costs an
  observation, not a packet).
- **The honest per-packet CPU is ~900 ns, not 211 ns.** Under a real load
  with a live consumer and cold-ish caches the program costs about 4× the
  hot-cache microbenchmark — still 200× under `CLAUDE.md`'s 0.2 ms budget.
  At the roadmap's 100k pkt/s that is ~9% of **one** core, or ~1.1% of an
  eight-core node — under the `<2%`-per-node target, over it as a per-core
  figure.

### Still measured only in part

The caveats are stated rather than buried. This is **generic (SKB) XDP on a
veth**, not native XDP on a physical NIC: the program's `run_time` is
faithful either way (it is the BPF code's own on-CPU time), but native XDP on
real hardware would *lower* the program's per-packet cost (no SKB) while
*adding* the driver/IRQ cost the veth path lacks, and the single-thread
generator caps the offered load at ~386k pkt/s — the program kept up with
zero backpressure, so that ceiling is the generator's, not the program's. A
true **physical line-rate** figure (10/40/100 GbE) and the node CPU that
comes with a real NIC's driver still need that NIC; what is now measured
rather than assumed is the program's own cost under real load and the ring's
behaviour under it.

The per-replica scoring throughput in §1.5 is likewise a scoring rate on
pre-built events, not an end-to-end node measurement — it does not include
the kernel capture or the NATS round trip, and does not claim to.

## 1.8 Per-tunnel rate map: the cardinality ceiling, measured

`MAX_TUNNEL_ENTRIES` is 65,536 and `tunnel_rate` is an `LRU_HASH`, so
`ROADMAP.md` Phase 4 asked what actually happens when a real UPF's bearer
count exceeds it. `TestTunnelRateMapEvictsWhenFullAndLosesTrackedRate`
(`-tags privileged`) answers it against the real map: seed one tunnel with a
high rate, insert distinct tunnels past capacity, and the seeded tunnel's
entry is **gone** — the LRU evicted the coldest to make room, and its rate
window (and therefore the flood it was about to flag) is silently lost.
Confirmed at `>65,536` distinct tunnels.

So the degradation the item feared is real and now demonstrated, and the
guardrail is already shipped: `sentinel5g_ebpf_observation_map_occupancy`
against `..._capacity` (§observability) rises toward 1 *before* eviction
bites, which is the only warning an LRU can give. The sizing rule follows
directly — `MAX_TUNNEL_ENTRIES` must exceed the deployment's peak concurrent
bearer count, and a production UPF serves far more than 65,536 (commercial
and Open5GS deployments run from hundreds of thousands into the millions), so
the 65,536 default is a lab value: raise it (and rebuild — note that changing
`max_entries` resets the pins, `EBPFPinsReset`) to the node's real bearer
ceiling, sized from the occupancy gauge. The exact figure a given UPF needs
is the one thing here that genuinely requires that UPF; the *behaviour* at
the ceiling, and the metric that sees it coming, are measured.
