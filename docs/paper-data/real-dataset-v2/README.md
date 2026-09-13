# Multi-UE captures (native Linux, 2026-09-11)

Captured against the live Open5GS + UERANSIM core described in
[`../test-environment.md`](../test-environment.md), with **four attached
UEs**. That is the property `../real-dataset/` could not provide and the
reason this directory exists: every capture there is single-UE and therefore
single-TEID, so per-tunnel rate is numerically identical to per-source rate
on it, and nothing in it can show that keying by TEID buys anything.

| File | Packets | Duration | Tunnels (TEIDs) | Source IP | How |
|---|---|---|---|---|---|
| `multi_ue_normal.pcap` | 5,000 | 129.9 s | 4, exactly 1,250 packets each | `127.0.0.1` | `ping -I uesimtunN -i 0.1` from all four UEs concurrently |
| `multi_ue_one_flooding.pcap` | 20,000 | 6.6 s | 4 — one at 19,808 packets, the other three at 64 each | `127.0.0.1` | three UEs as above; the fourth flooding at 3,000 pkt/s via `gen_tunnel_flood.py` |

Both were produced by `capture.sh`, and every packet in both carries the
**same source IP** — the gNB's N3 address. Per-tunnel and per-source rates,
reconstructed from the pcaps with the same 1-second window the kernel uses:

| | per-source rate (peak) | per-tunnel rate (peak, per TEID) |
|---|---|---|
| `multi_ue_normal` | 40 | 10 / 10 / 10 / 10 |
| `multi_ue_one_flooding` | 3,031 | **3,001** / 10 / 10 / 10 |

That second row is the measurement the whole per-TEID design rests on. A
per-source counter reports 3,031 for *all four* subscribers — it cannot say
which one is flooding, and a mitigation keyed on it blocklists the gNB, i.e.
everyone. A per-tunnel counter reports 3,001 for one TEID and 10 for the
other three.

**Snaplen is 128 bytes.** Enough for Ethernet + IPv4 + UDP + the full GTP-U
header with its optional block and one extension header (58 bytes), with
headroom; the user-plane payload is irrelevant to every measurement here and
keeping it would have made the flood capture several times larger.
Consequence: `payload_size` derived from these files is the *captured*
UDP payload length (≤ 86 bytes), not the on-wire one. Scoring pipelines
that use it should know that.

## Reproducing

```sh
# with the lab up and four UEs attached (see ../test-environment.md)
sudo ./capture.sh
```

`gen_tunnel_flood.py` is the flood generator. Read its docstring before
substituting anything else: two obvious alternatives (`iperf3 -B` and
UERANSIM's own `nr-binder`) both fail **silently** on this topology,
reporting a successful transfer while not one packet enters the GTP-U
tunnel. The first attempt at this capture used one of them and produced a
"flood" in which the flooding UE's TEID was absent entirely.

## Scope limits, stated

- Four subscribers on one gNB is enough to demonstrate the per-TEID
  argument; it is not a production subscriber population.
- Both captures are short (2 min / 7 s) and were taken within a few minutes
  of each other. They carry no diurnal or weekly structure.
- The "normal" traffic is ICMP echo at a fixed 10 pkt/s per UE — a
  controlled baseline, not a model of real subscriber behaviour.
- The flood is 3,000 pkt/s by choice, three times the shipped
  `GTPU_TUNNEL_FLOOD_PPS` default. The tunnel sustains ~125,000 pkt/s
  (`../test-environment.md`), so this is nowhere near what an attacker could
  push; it is what was needed to make the measurement without committing a
  multi-gigabyte file.
