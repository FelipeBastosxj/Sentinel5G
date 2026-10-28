# Application-mix benign captures (native Linux, 2026-10-28)

The residual [`../real-dataset-v3/`](../real-dataset-v3/README.md) left open:
its benign traffic was ICMP through the tunnels, not the web / video / bulk /
voice mix a production N3 carries. These captures are that mix — real kernel
TCP/TLS and UDP application traffic through the live Open5GS + UERANSIM core
([`../test-environment.md`](../test-environment.md), kernel 6.14), with every
TEID attributable to one application. The analysis is
[`02-ai-training-inference.md`](../02-ai-training-inference.md) §2.11.

## What was generated

Four UEs, each in its own network namespace
([`lab_netns.sh`](lab_netns.sh) — needed for TCP to work at all, see
`../test-environment.md`), each playing one fixed role for the whole session
([`gen_app_mix.py`](gen_app_mix.py)). Servers run on the lab host behind the
UPF's `ogstun` (`10.45.0.1`): a TLS HTTP server, an RTP echo, and `dnsmasq`.

| UE | Application | Shape |
|---|---|---|
| `s5g-ue1` | web browsing | DNS, then an HTML page and 8–40 assets over 2–6 parallel keep-alive TLS connections, exponential think time |
| `s5g-ue2` | video streaming | DASH-style 4 s segments at 2.5 Mbit/s, 3-segment start-up burst, 2–6 min sessions |
| `s5g-ue3` | bulk download | 20–60 MB at full speed every 45–150 s, background sync in between |
| `s5g-ue4` | voice + chat | G.711-shaped RTP calls (50 pkt/s each way) of 1–4 min, 0.5–4 KB chat POSTs, occasional 1–3 MB photo upload, DNS |

| Session | Seed | Duration | GTP-U packets | Committed |
|---|---|---|---|---|
| A | 42 | 30 min | 1,974,588 | 20 s sample (below) |
| B | 7 | 15 min | 989,424 | no |

Both full captures are ~480 MB together and are not committed; they are
reproduced with:

```sh
sudo ./lab_netns.sh up
sudo python3 gen_app_mix.py --minutes 30 --seed 42 --out app_mix_a.pcap
sudo python3 gen_app_mix.py --minutes 15 --seed 7  --out app_mix_b.pcap
sudo ./lab_netns.sh down
```

| File (committed) | What |
|---|---|
| `app_mix_sample.pcap` | 36,716 packets: a contiguous 20 s slice of session A (t = 167–187 s) in which all four applications are active, including a voice call |
| `app_mix_sample.roles.json` | UE address → application, as written by the generator |
| `measurement.json` | the full output of `scripts/fpr_app_mix.py --capture app_mix_a.pcap --holdout app_mix_b.pcap` |
| `gen_app_mix.py`, `lab_netns.sh` | the generator and the lab topology |

Captured with `tcpdump -s 160`: headers are complete, payloads are truncated,
and each record keeps the original length — which is what the payload-size
feature is read from (§2.11.1).

## What it measured (§2.11)

`cmd/ai-engine/scripts/fpr_app_mix.py`, per application and direction. Uplink
(gNB → UPF) is what an XDP probe on the UPF's N3 interface sees.

| Detector | Benign packets scored | False positives | 95% upper bound |
|---|---|---|---|
| Autoencoder **as shipped** (trained on the committed real normal data) | 1,974,588 (A) | **1,735,757 (87.9%)** | — |
| … uplink only | 662,913 | 495,734 (74.8%) | — |
| Autoencoder **calibrated on A's first half**, scored on A's second half | 987,294 | **0** | 3.74 × 10⁻⁶ |
| Autoencoder **calibrated on A**, scored on independent session **B** | 989,424 | **0** | 3.73 × 10⁻⁶ (≈ 0.37 wrong mitigations/s at 100k pkt/s) |
| … uplink only | 321,344 | 0 | 1.15 × 10⁻⁵ |
| Tunnel-flood rule at its default (1,000 pkt/s per tunnel) | A, uplink | **43 alerts in 30 min**, on all four applications | — |
| Source-flood rule at its default (20,000 pkt/s per peer) | A, uplink | **fires** (peak 21,026 pkt/s from 4 UEs) | — |

And the price of local calibration, on the committed **real attack** captures:

| Attack capture | Flagged by shipped model | Flagged by locally calibrated model |
|---|---|---|
| `real_storm_udpflood` | 100% | 100% |
| `real_malformed` | 100% | 100% |
| `real_scan` | 100% | 100% |
| `multi_ue_one_flooding` (in-tunnel flood, ~3,000 pkt/s) | 95% | **0%** |

## Honest scope

- **Is:** real kernel TCP/TLS/UDP from real application patterns, through a
  real 5G core's user plane, in both directions; ~3 million packets over two
  independent sessions; every false positive attributable to an application.
- **Is not the internet.** The servers sit behind `ogstun` on the same host,
  so round-trip times are the lab's, not a WAN's, and the user plane runs in
  UERANSIM's userspace (bulk peaked at ~41 Mbit/s, ~44,000 pkt/s downlink
  per tunnel). A real 5G subscriber can be faster, which pushes per-tunnel
  rates *higher* — it makes the rule findings below conservative, not
  optimistic.
- **Is not a subscriber population.** Four UEs, one per application. The
  per-peer aggregate of a real gNB is many times this, which again makes the
  source-flood finding conservative.
