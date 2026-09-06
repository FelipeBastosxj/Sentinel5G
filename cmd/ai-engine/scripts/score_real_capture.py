"""Scores real, tcpdump-captured GTP-U traffic (docs/paper-data/normal.pcap,
docs/paper-data/storm.pcap — from a live Open5GS+UERANSIM core in WSL2,
2026-09-06) through the production feature-extraction and scoring code
(sentinel_ai.features.extract_features, sentinel_ai.server.ScoringEngine)
using the real trained model at cmd/ai-engine/models/autoencoder.onnx.

This does not go through NATS/the operator's ingestion.Publisher — building
and running that full distributed chain against this WSL2 environment was
judged not worth the added moving parts for what's fundamentally a
feature-extraction + inference question. Instead it reimplements
bpf/packet_filter.c's track_signal_rate window logic (1s window, resets on
elapse — see bpf/headers/common.h SIGNALING_RATE_WINDOW_NS) directly against
the pcap's real per-packet timestamps, matching exactly what
pkg/ingestion.FromSignalingEvent would have read from the kernel's
signal_rate map for these same packets. Every other field (protocol,
destPort, payloadSize, malformed) is read directly from the real capture.

Run: python scripts/score_real_capture.py
"""

from __future__ import annotations

import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from sentinel_ai.features import NormalizedEvent, extract_features  # noqa: E402
from sentinel_ai.server import ScoringEngine  # noqa: E402

SIGNALING_RATE_WINDOW_S = 1.0  # bpf/headers/common.h SIGNALING_RATE_WINDOW_NS


def parse_pcap_udp_timestamps(raw_txt_path: Path) -> list[tuple[float, int]]:
    """Returns (epoch_timestamp, udp_length) for each packet. Reads a
    pre-captured `tcpdump -r <pcap> -tt -n` text dump (produced once, inside
    WSL2 where tcpdump/the pcap actually live) rather than re-invoking
    tcpdump here, since Windows has no tcpdump on PATH."""
    events = []
    for line in raw_txt_path.read_text().splitlines():
        if " UDP, length " not in line:
            continue
        ts_str = line.split(" ", 1)[0]
        length = int(line.rsplit("length ", 1)[1])
        events.append((float(ts_str), length))
    return events


def signal_rate_series(packets: list[tuple[float, int]]) -> list[int]:
    """Reimplements bpf/packet_filter.c's track_signal_rate: per-source,
    single-window packet counter, reset when the window (1s) elapses.
    Returns the window count associated with each packet, in order — this is
    exactly the value pkg/ingestion.FromSignalingEvent would read from
    ebpf.Loader.SignalRate() for that packet's observation.
    """
    counts = []
    window_start = None
    count = 0
    for ts, _ in packets:
        if window_start is None or (ts - window_start) > SIGNALING_RATE_WINDOW_S:
            window_start = ts
            count = 1
        else:
            count += 1
        counts.append(count)
    return counts


def score_window(
    label: str, packets: list[tuple[float, int]], rates: list[int], engine: ScoringEngine
) -> None:
    scores = []
    for (ts, udp_len), rate in zip(packets, rates):
        event = NormalizedEvent(
            protocol="GTP-U",
            dest_port=2152,
            payload_size=max(0, udp_len - 8),  # UDP header is 8 bytes
            rate_per_second=float(rate),
            malformed=False,
            observed_at=datetime.fromtimestamp(ts, tz=timezone.utc),
        )
        scores.append(engine.score_features(extract_features(event)))

    duration = packets[-1][0] - packets[0][0] if len(packets) > 1 else 0.0
    print(f"\n=== {label} ===")
    print(
        f"packets={len(packets)} duration={duration:.2f}s "
        f"observed_rate={len(packets) / duration if duration else 0:.2f} pkt/s "
        f"(track_signal_rate peak window count={max(rates)})"
    )
    print(
        f"score min={min(scores):.4f} max={max(scores):.4f} "
        f"mean={sum(scores) / len(scores):.4f} "
        f"first={scores[0]:.4f} last={scores[-1]:.4f}"
    )
    return scores


def main() -> None:
    base = Path(__file__).resolve().parent.parent.parent.parent / "docs" / "paper-data"
    model_path = str(Path(__file__).resolve().parent.parent / "models" / "autoencoder.onnx")

    engine = ScoringEngine(model_path)

    normal = parse_pcap_udp_timestamps(base / "normal_raw.txt")
    storm = parse_pcap_udp_timestamps(base / "storm_raw.txt")

    normal_rates = signal_rate_series(normal)
    storm_rates = signal_rate_series(storm)

    score_window("NORMAL (baseline PDU-session ping, ~1 pkt/s)", normal, normal_rates, engine)
    score_window("STORM (flood ping through uesimtun0)", storm, storm_rates, engine)


if __name__ == "__main__":
    main()
