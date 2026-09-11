"""Scores real, tcpdump-captured GTP-U traffic through the production
feature-extraction and scoring code (sentinel_ai.features.extract_features,
sentinel_ai.server.ScoringEngine) using the real trained model at
cmd/ai-engine/models/autoencoder.onnx, and reports what the deterministic
detector (pkg/detect) would have done with the same packets.

Reads the committed `.pcap` files directly rather than the `*_raw.txt`
`tcpdump -tt -n` dumps beside them. That is not a cosmetic change: the text
dumps carry timestamps, ports and lengths but no payload bytes, so no TEID —
and the per-tunnel rate is the entire point of this comparison. The window
reconstruction and GTP-U parsing are shared with
scripts/build_real_dataset.py and scripts/pcap_gtpu.py rather than
reimplemented a third time.

This does not go through NATS or the operator's ingestion.Publisher:
building that full distributed chain is not worth the moving parts for what
is fundamentally a feature-extraction and inference question. Every field
(protocol, destPort, payloadSize, malformed, teid) comes from the real
capture, and the rate windows mirror bpf/packet_filter.c's
track_signal_rate/track_tunnel_rate against the pcap's real per-packet
timestamps.

Run: python scripts/score_real_capture.py
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from sentinel_ai.features import extract_features  # noqa: E402
from sentinel_ai.server import ScoringEngine  # noqa: E402

try:  # Running as `python scripts/score_real_capture.py` from cmd/ai-engine/.
    from build_real_dataset import events_from_capture
except ImportError:  # Imported as scripts.score_real_capture.
    from scripts.build_real_dataset import events_from_capture

# Mirrors pkg/detect's defaults closely enough to be comparable, but NOT the
# shipped GTPU_TUNNEL_FLOOD_PPS default of 1000. These captures were taken
# through a WSL2 tunnel that caps around 63 pkt/s, so the shipped default
# fires on none of them -- which is the finding, not a bug. 25 sits above
# this normal capture's observed maximum (18 pkt/s) and well below the
# flood's median (31), and is what docs/paper-data/02-ai-training-inference.md
# §2.5 reports against.
DETECTOR_PPS = 25
DETECTOR_COOLDOWN_S = 30.0


def detector_events(events, packets_per_second: int, cooldown_s: float) -> int:
    """Mirrors pkg/detect.GTPUFloodDetector.Evaluate: fires when the protocol
    is GTP-U, the TEID is non-zero, and the per-tunnel rate crosses the
    threshold -- at most once per (source, TEID) per cooldown.
    """
    last_fired: dict[int, float] = {}
    fired = 0
    for event in events:
        if event.protocol != "GTP-U" or event.teid == 0:
            continue
        if event.tunnel_rate_per_second < packets_per_second:
            continue
        timestamp = event.observed_at.timestamp()
        previous = last_fired.get(event.teid)
        if previous is not None and timestamp - previous < cooldown_s:
            continue
        last_fired[event.teid] = timestamp
        fired += 1
    return fired


def score_capture(label: str, events, engine: ScoringEngine) -> None:
    scores = [engine.score_features(extract_features(e)) for e in events]
    rates = [e.rate_per_second for e in events]
    tunnel_rates = [e.tunnel_rate_per_second for e in events]
    teids = {e.teid for e in events if e.teid}

    duration = events[-1].observed_at.timestamp() - events[0].observed_at.timestamp()

    print(f"\n=== {label} ===")
    print(
        f"packets={len(events)} duration={duration:.2f}s "
        f"observed_rate={len(events) / duration if duration else 0:.2f} pkt/s"
    )
    print(f"tunnels (TEIDs)={sorted(hex(t) for t in teids) or 'none parsed'}")
    print(
        f"per-source rate  peak={max(rates):.0f}  |  "
        f"per-tunnel rate  peak={max(tunnel_rates):.0f}"
    )
    print(
        f"model score min={min(scores):.4f} max={max(scores):.4f} "
        f"mean={sum(scores) / len(scores):.4f}"
    )
    print(
        f"deterministic detector at {DETECTOR_PPS} pkt/s per tunnel: "
        f"{detector_events(events, DETECTOR_PPS, DETECTOR_COOLDOWN_S)} event(s)"
    )


def main() -> None:
    dataset_dir = (
        Path(__file__).resolve().parent.parent.parent.parent
        / "docs"
        / "paper-data"
        / "real-dataset"
    )
    model_path = str(Path(__file__).resolve().parent.parent / "models" / "autoencoder.onnx")
    engine = ScoringEngine(model_path)

    normal = events_from_capture(
        dataset_dir / "real_normal.pcap", protocol="GTP-U", dest_port_override=2152
    )
    flood = events_from_capture(
        dataset_dir / "real_storm_pingflood.pcap", protocol="GTP-U", dest_port_override=2152
    )

    score_capture("NORMAL (baseline PDU-session traffic)", normal, engine)
    score_capture("IN-TUNNEL FLOOD (flood ping through uesimtun0)", flood, engine)


if __name__ == "__main__":
    main()
