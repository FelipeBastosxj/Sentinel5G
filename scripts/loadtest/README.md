# Load-testing harness

`ROADMAP.md` Phase 3 asked for a harness against the SLOs in
[`docs/observability.md`](../../docs/observability.md), and for two specific
questions to be settled with it rather than reasoned about. This directory
is that harness, and
[`docs/paper-data/01-performance-benchmarks.md`](../../docs/paper-data/01-performance-benchmarks.md)
§1.4 records what it measured.

| Script | Answers |
|---|---|
| `gen_packets.py` | Builds the frames the benchmark replays. Generated, not committed, so the GTP-U one cannot silently drift out of what `parse_gtpu()` accepts. |
| `xdp_bench.sh` | Per-packet XDP cost (`< 0.2 ms` target), across four packet classes and a repeat sweep that walks the ring buffer from empty to saturated. Also prints every map's real type and capacity. |
| `mitigation_latency.sh` | Closed-loop mitigation latency (single-digit-ms target), in two halves: the eBPF map write, and NATS publish → `Phase: Mitigating`. |

```sh
make -C bpf                                   # the object both scripts load
sudo scripts/loadtest/xdp_bench.sh
sudo KUBECONFIG=... scripts/loadtest/mitigation_latency.sh
```

Each half degrades to a clear skip rather than a wrong number: no root
skips the kernel measurements, no reachable cluster skips the end-to-end
one.

## What the methods can and cannot see

**`bpftool prog run`** (`BPF_PROG_TEST_RUN`) runs the real loaded program
against a real frame N times *in the kernel*, and reports the mean. It is
not a deployment: caches are hot, there is no NIC driver path, and — the
one that actually changes the number — **there is no userspace consumer
draining the ring buffer**. Past 8,192 records (256 KB ÷ a 32-byte record)
every `bpf_ringbuf_reserve` fails fast and the reported cost *drops*. That
is why `xdp_bench.sh` sweeps repeat counts and labels each row `live` or
`saturated` instead of printing one figure: the `live` rows are the honest
per-packet cost, the `saturated` ones are what the program costs once it
has given up emitting.

**The cluster half** cannot resolve below one API-server round trip, and on
a local kind cluster that round trip costs ~32 ms — more than the thing
being measured. So it opens a `--watch` *before* publishing and times the
first `Mitigating` the API server streams, and it prints the round-trip
cost first so the floor is visible rather than implied.

## The two questions Phase 3 attached to this item

**Does `blocklist` need LRU semantics?** No — and the same answer covers
`tunnel_blocklist`. Both are plain `BPF_MAP_TYPE_HASH`, and that is the
right choice for the same reason in both: an entry is a mitigation the
operator owns the lifetime of (removed by de-escalation or a policy's
finalizer), so an LRU silently evicting one would un-block traffic nobody
asked to un-block — a security control failing open under load, which is
the worst way for it to fail. A full map refuses the insert instead, and
the refusal surfaces as a `Mitigations{result="error"}` metric and a failed
reconcile. `TestBlocklistIsBoundedAndFailsLoudlyWhenFull` (run under
`-tags privileged`) pins that: at 16,384 entries the next insert is
refused, not swallowed. The rate-tracking maps are LRU precisely because
they are the opposite case — an evicted rate counter costs one missed
observation, never a wrong enforcement decision.

**Does `signaling_events` hold up under telecom-scale volume?** It holds up
in the way it was designed to, which is worth stating precisely because the
design trades one thing for another. The ring buffer holds 8,192 in-flight
32-byte records. When it fills, `emit_signaling_event` drops *the
observation* and the packet still passes — Layer 1 never drops traffic
because Layer 2 fell behind. At the ~200 ns/packet this harness measures,
8,192 records is roughly 1.6 ms of headroom at line rate for
`pkg/ingestion.Publisher` to drain. That is comfortable for a consumer
doing a channel send, and it is not comfortable if that consumer is ever
made to do blocking I/O on the hot path — which is the real constraint this
measurement establishes, and why `Publisher` drains the ring even while
NATS is disconnected (see its `Start` doc comment).

What this harness still does not measure: sustained line-rate traffic
through a real NIC, and CPU overhead per worker node (the `< 2%` at 100k
req/s target). Both need a load generator on real hardware rather than a
synthetic replay, and remain open in `ROADMAP.md`.
