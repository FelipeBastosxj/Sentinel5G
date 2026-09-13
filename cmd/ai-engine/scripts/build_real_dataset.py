"""Builds a real-traffic training/eval dataset from the packet captures under
docs/paper-data/real-dataset/ (see that directory's README for how each
capture was produced — a live Open5GS+UERANSIM core in WSL2, per
docs/paper-data/test-environment.md), replacing
scripts/generate_synthetic_dataset.py's synthetic distributions with real,
on-wire GTP-U traffic wherever the current pipeline can actually produce it.

Read this before trusting the "anomalous" half of the output: not every
synthetic anomaly type has a real production equivalent today.
bpf/packet_filter.c only tracks rate and emits `signaling_events` for
GTP-U/SIP (port 2152/5060) traffic — a real "storm" is exactly what that
code path does today, at real scale, and this script's normal/storm samples
are genuinely on-wire captures scored through the same
feature-extraction/rate-window logic production uses (see
scripts/score_real_capture.py, whose window-reconstruction this reuses).
"Malformed" and "scan" are different:

- `malformed` now means more than it used to, and this script's labels moved
  with it. It once fired only for a UDP packet too short to have a complete
  UDP header — one constant degenerate feature vector, reproduced exactly by
  `_kernel_malformed_samples` below rather than pretending a distribution
  exists where the kernel provides none. Since bpf/packet_filter.c gained a
  real GTP-U parser it ALSO fires for a packet on port 2152 whose GTP-U
  framing fails to validate, and two captures here are exactly that. This
  script therefore no longer asserts a label the wire didn't support:
  real_storm_udpflood.pcap (25,944 packets of plain UDP at the N3 port, no
  GTP header at all) and real_malformed.pcap (payloads shorter than the
  8-byte mandatory header) are now parsed as malformed with no tunnel
  identity, instead of being labelled well-formed GTP-U because they
  happened to be addressed to port 2152.
- "Scan" (unknown-protocol/off-signaling-port probing) was once structurally
  invisible to bpf/packet_filter.c, since `is_signaling_port()` gated
  observation as well as rate tracking. ROADMAP.md Phase 1's `scan_rate` and
  Phase 2's `port_scan` detectors closed that. Read
  docs/paper-data/02-ai-training-inference.md §2.4's own update before
  assuming real_scan.pcap is representative of what those emit, because it
  isn't: it sends each packet to an independently random port, which
  essentially never repeats one enough times to cross either threshold.

Everything here is derived from the committed `.pcap` files, not the
`*_raw.txt` dumps beside them. That changed with per-TEID rates: the text
dumps are `tcpdump -tt -n` output, carrying timestamps, ports and lengths
but no payload bytes — and therefore no TEID. The packets were always
there; they were simply unreadable through the dumps. See
scripts/pcap_gtpu.py.

Run from cmd/ai-engine/: python scripts/build_real_dataset.py
"""

from __future__ import annotations

import argparse
import random
from datetime import datetime, timedelta, timezone
from pathlib import Path

import numpy as np

from sentinel_ai.features import NormalizedEvent, extract_features

try:  # Running as `python scripts/build_real_dataset.py` from cmd/ai-engine/.
    from pcap_gtpu import Packet, read_udp_packets
except ImportError:  # Imported as scripts.build_real_dataset (pytest).
    from scripts.pcap_gtpu import Packet, read_udp_packets

SIGNALING_RATE_WINDOW_S = 1.0  # bpf/headers/common.h SIGNALING_RATE_WINDOW_NS

_REAL_DATASET_DIR = (
    Path(__file__).resolve().parent.parent.parent.parent / "docs" / "paper-data" / "real-dataset"
)


def signal_rate_series(packets: list[Packet]) -> list[int]:
    """Reimplements bpf/packet_filter.c's track_signal_rate: per-capture,
    single-window packet counter, reset when the window (1s) elapses. All
    these captures are single-source (one UE / one generator process), so a
    single running window rather than per-source-IP keying is equivalent to
    what the kernel map would show for that one source.
    """
    counts = []
    window_start = None
    count = 0
    for packet in packets:
        if window_start is None or (packet.timestamp - window_start) > SIGNALING_RATE_WINDOW_S:
            window_start = packet.timestamp
            count = 1
        else:
            count += 1
        counts.append(count)
    return counts


def tunnel_rate_series(packets: list[Packet]) -> list[int]:
    """Reimplements bpf/packet_filter.c's track_tunnel_rate: a 1-second
    window per (source IP, TEID), counting INCLUDING the current packet.

    Unlike signal_rate_series above, this genuinely needs per-key state --
    the entire reason the kernel map exists is that one source IP carries
    many tunnels. A packet with no tunnel identity (teid == 0) contributes
    nothing and receives 0, matching what the kernel emits.
    """
    state: dict[tuple[str, int], tuple[float, int]] = {}
    counts = []
    for packet in packets:
        if packet.teid == 0:
            counts.append(0)
            continue
        key = (packet.source_ip, packet.teid)
        window_start, count = state.get(key, (None, 0))
        if window_start is None or (packet.timestamp - window_start) > SIGNALING_RATE_WINDOW_S:
            state[key] = (packet.timestamp, 1)
            counts.append(1)
        else:
            state[key] = (window_start, count + 1)
            counts.append(count + 1)
    return counts


_SECONDS_PER_DAY = 24 * 60 * 60


def events_from_capture(
    path: Path,
    protocol: str,
    dest_port_override: int | None,
    spread_time_of_day: random.Random | None = None,
) -> list[NormalizedEvent]:
    """Turns one committed capture into NormalizedEvents the way the
    production path would.

    `malformed` is no longer a caller-supplied label: it is whatever
    pcap_gtpu's mirror of the kernel's own GTP-U validation decided, so this
    script can no longer assert framing the wire doesn't support.

    `spread_time_of_day`, when given, re-stamps each event's absolute
    time-of-day uniformly across 24h AFTER the rate windows have been
    computed from the real timestamps. This is the same treatment
    generate_synthetic_dataset.py's _random_time_of_day has always applied
    to synthetic normal traffic, and for exactly the reason its docstring
    gives: a capture session spans one hour of wall-clock, so trained on it
    raw, the autoencoder learns "wrong time of day" as THE anomaly signal.
    That is not hypothetical -- see docs/paper-data/02-ai-training-inference.md
    §2.6: the model trained without this scored 1.0000 on 5,000/5,000
    packets of well-behaved traffic from a second capture, purely because it
    was taken eleven hours later. The relative timing (which drives every
    rate feature) is untouched; only the hour_sin/hour_cos inputs change.
    Pass None to keep the capture's real timestamps, e.g. for scoring.
    """
    packets = read_udp_packets(path)
    if not packets:
        raise RuntimeError(f"{path} contained no parseable UDP packets")
    rates = signal_rate_series(packets)
    tunnel_rates = tunnel_rate_series(packets)

    events = []
    for packet, rate, tunnel_rate in zip(packets, rates, tunnel_rates):
        observed_at = datetime.fromtimestamp(packet.timestamp, tz=timezone.utc)
        if spread_time_of_day is not None:
            observed_at = observed_at.replace(
                hour=0, minute=0, second=0, microsecond=0
            ) + timedelta(seconds=spread_time_of_day.uniform(0, _SECONDS_PER_DAY))
        events.append(
            NormalizedEvent(
                protocol=protocol,
                dest_port=(
                    dest_port_override if dest_port_override is not None else packet.dest_port
                ),
                payload_size=packet.payload_size,
                rate_per_second=float(rate),
                malformed=packet.malformed,
                observed_at=observed_at,
                teid=packet.teid,
                tunnel_rate_per_second=float(tunnel_rate),
            )
        )
    return events


def _kernel_malformed_samples(
    rng: random.Random, count: int, time_span: tuple[datetime, datetime]
) -> list[NormalizedEvent]:
    """Exact reproduction of the one and only feature vector
    bpf/packet_filter.c:172 ever emits for a UDP-header-truncated packet:
    protocol=UNKNOWN, dest_port=0, payload_size=0, malformed=1. Not a
    captured distribution -- the kernel code provides no other varying
    dimension for this branch -- so `count` copies with jittered timestamps
    (spread across the real captures' time span, for a plausible
    hour-of-day spread) is a faithful, honest reproduction rather than an
    invented one.
    """
    start, end = time_span
    span_s = (end - start).total_seconds()
    return [
        NormalizedEvent(
            protocol="UNKNOWN",
            dest_port=0,
            payload_size=0,
            rate_per_second=1.0,
            malformed=True,
            observed_at=start + timedelta(seconds=rng.uniform(0, max(span_s, 1.0))),
        )
        for _ in range(count)
    ]


def build(real_dataset_dir: Path, seed: int = 42) -> tuple[np.ndarray, np.ndarray]:
    # Reproducible sampling (kernel-malformed timestamp jitter), not a security context.
    rng = random.Random(seed)  # nosec B311

    # Every real capture gets its time-of-day spread -- see
    # events_from_capture's docstring. Applied to the anomalous captures too,
    # not just normal: there is no evidence about time-of-day for any real
    # category here, so leaving hour_sin/hour_cos as pure noise for all of
    # them is the honest encoding, and it is what lets the model learn to
    # ignore those two features rather than learning a capture schedule.
    normal_events = events_from_capture(
        real_dataset_dir / "real_normal.pcap",
        protocol="GTP-U",
        dest_port_override=2152,
        spread_time_of_day=rng,
    )
    # The multi-UE baseline from real-dataset-v2/ (four subscribers, four
    # TEIDs, one gNB source IP) joins the normal set so "normal" is no longer
    # defined by a single tunnel's shape. See real-dataset-v2/README.md.
    v2_dir = real_dataset_dir.parent / "real-dataset-v2"
    if (v2_dir / "multi_ue_normal.pcap").exists():
        normal_events += events_from_capture(
            v2_dir / "multi_ue_normal.pcap",
            protocol="GTP-U",
            dest_port_override=2152,
            spread_time_of_day=rng,
        )

    anomalous_events: list[NormalizedEvent] = []
    anomalous_events += events_from_capture(
        real_dataset_dir / "real_storm_pingflood.pcap",
        protocol="GTP-U",
        dest_port_override=2152,
        spread_time_of_day=rng,
    )
    anomalous_events += events_from_capture(
        real_dataset_dir / "real_storm_udpflood.pcap",
        protocol="GTP-U",
        dest_port_override=2152,
        spread_time_of_day=rng,
    )
    anomalous_events += events_from_capture(
        real_dataset_dir / "real_malformed.pcap",
        protocol="GTP-U",
        dest_port_override=2152,
        spread_time_of_day=rng,
    )
    anomalous_events += events_from_capture(
        real_dataset_dir / "real_scan.pcap",
        protocol="UNKNOWN",
        dest_port_override=None,  # keep each packet's real (random) dest port
        spread_time_of_day=rng,
    )
    if (v2_dir / "multi_ue_one_flooding.pcap").exists():
        # Three of the four tunnels in this capture are behaving normally --
        # that is the entire point of the scenario -- so only the flooding
        # tunnel's packets are anomalous. Labelling the bystanders as
        # anomalous would teach the model that being on the same gNB as an
        # attacker is itself an anomaly, which is exactly the per-source
        # aggregation error the per-TEID work exists to undo.
        flooding = events_from_capture(
            v2_dir / "multi_ue_one_flooding.pcap",
            protocol="GTP-U",
            dest_port_override=2152,
            spread_time_of_day=rng,
        )
        counts: dict[int, int] = {}
        for e in flooding:
            counts[e.teid] = counts.get(e.teid, 0) + 1
        flooding_teid = max(counts, key=counts.get)
        anomalous_events += [e for e in flooding if e.teid == flooding_teid]

    all_ts = [e.observed_at for e in normal_events + anomalous_events]
    kernel_malformed_count = max(
        1, len(anomalous_events) // 20
    )  # order-of-magnitude-matched, not dominant
    anomalous_events += _kernel_malformed_samples(
        rng, kernel_malformed_count, (min(all_ts), max(all_ts))
    )

    normal = np.asarray([extract_features(e) for e in normal_events], dtype=np.float32)
    anomalous = np.asarray([extract_features(e) for e in anomalous_events], dtype=np.float32)
    return normal, anomalous


def main() -> None:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--real-dataset-dir", type=Path, default=_REAL_DATASET_DIR)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--output", type=Path, default=Path("data/real_dataset.npz"))
    args = parser.parse_args()

    normal, anomalous = build(args.real_dataset_dir, args.seed)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    np.savez(args.output, normal=normal, anomalous=anomalous)
    print(f"wrote {len(normal)} normal and {len(anomalous)} anomalous samples to {args.output}")


if __name__ == "__main__":
    main()
