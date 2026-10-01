# Large benign captures (native Linux, 2026-10-01)

The one thing `02-ai-training-inference.md` §2.9 named as able to move the
false-positive-rate bound: **orders of magnitude more real benign traffic.**
Captured against the live Open5GS + UERANSIM core
([`../test-environment.md`](../test-environment.md), kernel 6.14), four UEs
on the same N3 source IP, reproducible with [`gen_benign.sh`](gen_benign.sh).

| File (committed) | Packets | What |
|---|---|---|
| `benign_steady_sample.pcap` | 15,000 | first slice of a 300,000-packet steady session (100 pkt/s/UE, 80-byte payload) |
| `benign_diverse_sample.pcap` | 15,000 | first slice of a 40,000-packet session with **varied payloads (200/512/900/1,200 B) and rates** across the four UEs |

The committed files are 15k-packet samples (~6 MB total, in line with
`../real-dataset-v2/`). The **full** captures — 300,000 steady + 40,000
diverse = **340,000 benign packets** — are not committed (they are ~50 MB);
`gen_benign.sh` reproduces them by raising its `-c` counts, and the
measurement below was run on the full set.

## What it measured (§2.9 / §2.10)

The autoencoder trained on the committed real normal data (`../real-dataset/`
+ `../real-dataset-v2/`, 6,889 packets) was used to score these captures,
which it never trained on. `cmd/ai-engine/scripts/fpr_large_benign.py`:

| Benign set | Packets | False positives | 95% upper bound on FPR | ≤ wrong mitigations/s at 100k pkt/s |
|---|---|---|---|---|
| full steady | 300,000 | **0** | 1.23 × 10⁻⁵ | **1.23** |
| full diverse | 40,000 | **0** | 9.22 × 10⁻⁵ | 9.22 |
| both full combined | 340,000 | **0** | ≈ 8.8 × 10⁻⁶ | **≈ 0.88** |
| committed diverse sample | 15,000 | 0 | 2.46 × 10⁻⁴ | 24.6 |

Zero false positives across 340,000 real benign packets — including varied
payload sizes and rates — takes the bound from the 5.3 × 10⁻⁴ of §2.9 (6,889
packets) to **1.23 × 10⁻⁵**, i.e. from "up to 53 wrong mitigations/second at
100k pkt/s" to **about one**.

## Honest scope (what this is and is not)

- **Is:** real on-wire GTP-U from a real 5G core, two orders of magnitude
  more benign traffic than before, scored by a model that never saw it, with
  an exact binomial bound — not a point estimate.
- **Is not production traffic.** This is lab user-plane (ICMP through the
  GTP-U tunnels), single host, four UEs on loopback N3. Real application
  traffic (video, web, signaling mixes) is more varied than ping, even with
  the diverse-payload session. The §2.8 finding still stands: a deployment
  must **calibrate the model's reference error on its own normal traffic**,
  because calibration does not transfer across structurally different
  captures — the tight bound here holds *because* the scored traffic
  resembles the (locally representative) training baseline, which is exactly
  the condition a local calibration establishes.
