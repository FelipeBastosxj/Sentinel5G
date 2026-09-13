# Real-dataset-at-scale captures (ROADMAP.md Phase 1)

Captured 2026-09-07 against the live Open5GS+UERANSIM core in
`../test-environment.md`'s WSL2 environment, superseding the
original `docs/paper-data/normal.pcap`/`storm.pcap` pair (30/618 packets,
one shape each) with materially more volume and traffic-shape variety.

| File | Packets | How | Real GTP-U framing? |
|---|---|---|---|
| `real_normal.pcap` | 1,888 | `gen_normal.py`: `ping -I uesimtun0` through the live PDU session, cycling interval (0.05-1s) and payload size (8-256B) | Yes — genuine in-tunnel traffic |
| `real_storm_pingflood.pcap` | 3,347 | `ping -f -I uesimtun0` for ~55s (~61 pkt/s) | Yes — genuine in-tunnel traffic |
| `real_storm_udpflood.pcap` | 25,943 | `gen_udp_storm.py`: direct UDP to `127.0.0.7:2152`, 3 rate tiers (measured ~477/~1710/~3000 pkt/s) | **No** — see caveat below |
| `real_malformed.pcap` | 700 | `gen_malformed.py`: direct UDP to `127.0.0.7:2152`, payload 0-8 bytes | No (deliberately malformed) |
| `real_scan.pcap` | 708 | `gen_scan.py`: direct UDP to `127.0.0.7:<random high port>`, port 2152/5060 excluded | No (off-protocol by design) |

**Caveat on `real_storm_udpflood.pcap`:** the in-tunnel ping flood tops out
around ~62 pkt/s on this kernel (consistent with the original `storm.pcap`
measurement) — nowhere near the synthetic training distribution's storm
range (800-4500/s, `generate_synthetic_dataset.py`). To get real on-wire
packets at rates that actually exercise that range, this capture sends
plain UDP datagrams straight at the UPF's real N3 socket
(`127.0.0.7:2152`), bypassing the UE tunnel entirely — no TEID, no real GTP
header, just a payload sized to match the ~100-byte real GTP-U packets seen
in the other captures. These are real, on-wire packets hitting the real
production port, at rates a real flood/DDoS against that port would produce
— just not protocol-conformant GTP-U. Treat `rate_per_second` derived from
this capture as real; treat "GTP-U protocol" as an approximation, not a
verified-by-parsing claim (at the time this file was written, nothing in
this project parsed GTP-U headers at all — `protocol="GTP-U"` was asserted
from the capture filter).

**Update, 2026-09-11 — now verified by parsing.** `bpf/packet_filter.c`
parses GTP-U headers (3GPP TS 29.281 §5.1),
`cmd/ai-engine/scripts/pcap_gtpu.py` mirrors that parser, and every capture
here has been read through it; see
`docs/paper-data/02-ai-training-inference.md` §2.5.1 for the per-capture
table. Two statements above changed as a result: this capture carries no GTP
header at all (GTP version 0, zero-filled payload), and `real_malformed.pcap`
likewise fails validation — **both are now flagged `malformed`** instead of
being carried through the pipeline as well-formed GTP-U.

**Known scope limits, stated honestly rather than glossed over:**
- Single UE, single session (source IP `127.0.0.1` for the whole PDU
  session's traffic) — not a multi-subscriber capture. **Measured
  consequence:** every packet in `real_normal.pcap` and
  `real_storm_pingflood.pcap` carries the same TEID (`0x00004d84`), so
  per-tunnel rate is numerically identical to per-source rate on this
  dataset. Per-TEID features can be shown correct here but not
  *discriminative*; that needs a multi-UE capture. Pinned by
  `cmd/ai-engine/tests/test_gtpu_parse.py`.
- No SIP/SMPP traffic: this Open5GS deployment does not run IMS (no SIP
  signaling ever traverses this core), and no SMPP infrastructure exists in
  this environment at all. Those two protocols remain synthetic-only in the
  training data — real captures could not be produced for them here.
- All captures happened within roughly one hour of wall-clock time, so the
  `hour_sin`/`hour_cos` time-of-day features will show almost no real
  variance across this dataset (unlike the synthetic generator, which
  deliberately spans the full 24h day). This is an accurate reflection of
  what a single real capture session produces, not a bug to paper over.
  **Update, 2026-09-13:** it was, however, a bug in the resulting model —
  trained on this, it learned the capture hour as *the* anomaly signal and
  scored 1.0 on every packet of a later capture. Those two features no
  longer exist; see `../02-ai-training-inference.md` §2.6.

Raw `tcpdump -r <pcap> -tt -n` text dumps (`<name>_raw.txt`) accompany each
pcap, same convention as the original `normal_raw.txt`/`storm_raw.txt`.

Live core confirmed healthy after all captures (see fork report).
