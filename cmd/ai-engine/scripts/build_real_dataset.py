"""Builds a real-traffic training/eval dataset from the packet captures under
docs/paper-data/real-dataset/ (see that directory's README for how each
capture was produced — a live Open5GS+UERANSIM core in WSL2, per
memory/wsl2_real_test_environment.md), replacing
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

- The kernel's actual `malformed` flag (packet_filter.c:172) only fires when
  a UDP packet is too short to have a *complete UDP header* at all, and
  always emits the exact same degenerate feature vector regardless of what
  the truncated bytes contained (protocol=UNKNOWN, dest_port=0,
  payload_size=0). A live capture of *that* case is a single constant point,
  not a distribution — see `_kernel_malformed_samples` below, which
  reproduces it exactly (jittered only in timestamp) rather than pretending
  volume exists where the kernel provides none. The real_malformed.pcap
  capture in this directory is a different, real phenomenon instead
  (valid-header, near-zero-payload packets on the real signaling port) and
  is folded in as its own labeled anomaly shape, not mislabeled as
  kernel-malformed.
- "Scan" (unknown-protocol/off-signaling-port probing) is structurally
  invisible to bpf/packet_filter.c as it exists today: `is_signaling_port()`
  gates both `track_signal_rate()` and `emit_signaling_event()`, so
  non-2152/5060 UDP traffic is XDP_PASSed with no observation emitted at
  all. real_scan.pcap is real, on-wire traffic, but no
  currently-shipping code path would ever turn it into a NormalizedEvent in
  production — included here for forward-looking model coverage only. See
  docs/paper-data/02-ai-training-inference.md and ROADMAP.md for the
  write-up of this as a real architecture gap, not a dataset footnote.

Run from cmd/ai-engine/: python scripts/build_real_dataset.py
"""

from __future__ import annotations

import argparse
import random
from datetime import datetime, timedelta, timezone
from pathlib import Path

import numpy as np

from sentinel_ai.features import NormalizedEvent, extract_features

SIGNALING_RATE_WINDOW_S = 1.0  # bpf/headers/common.h SIGNALING_RATE_WINDOW_NS

_REAL_DATASET_DIR = (
    Path(__file__).resolve().parent.parent.parent.parent / "docs" / "paper-data" / "real-dataset"
)


def _parse_raw_txt(path: Path) -> list[tuple[float, int, int]]:
    """Parses a `tcpdump -r <pcap> -tt -n` text dump into
    (epoch_timestamp, dest_port, udp_length) tuples, one per UDP packet line
    (format: "<ts> IP <src>.<sport> > <dst>.<dport>: UDP, length <N>" —
    same convention as docs/paper-data/normal_raw.txt).
    """
    events = []
    for line in path.read_text().splitlines():
        if " UDP, length " not in line:
            continue
        ts_str, rest = line.split(" ", 1)
        dest_part = rest.split(" > ", 1)[1].split(":", 1)[0]
        dest_port = int(dest_part.rsplit(".", 1)[1])
        length = int(rest.rsplit("length ", 1)[1])
        events.append((float(ts_str), dest_port, length))
    return events


def _signal_rate_series(packets: list[tuple[float, int, int]]) -> list[int]:
    """Reimplements bpf/packet_filter.c's track_signal_rate: per-capture,
    single-window packet counter, reset when the window (1s) elapses —
    identical logic to scripts/score_real_capture.py's version. All these
    captures are single-source (one UE / one generator process), so a
    single running window (rather than per-source-IP keying) is equivalent
    to what the kernel map would show for that one source.
    """
    counts = []
    window_start = None
    count = 0
    for ts, _dport, _length in packets:
        if window_start is None or (ts - window_start) > SIGNALING_RATE_WINDOW_S:
            window_start = ts
            count = 1
        else:
            count += 1
        counts.append(count)
    return counts


def _events_from_capture(
    path: Path, protocol: str, malformed: bool, dest_port_override: int | None
) -> list[NormalizedEvent]:
    packets = _parse_raw_txt(path)
    if not packets:
        raise RuntimeError(f"{path} contained no parseable UDP lines")
    rates = _signal_rate_series(packets)

    events = []
    for (ts, dest_port, length), rate in zip(packets, rates):
        events.append(
            NormalizedEvent(
                protocol=protocol,
                dest_port=dest_port_override if dest_port_override is not None else dest_port,
                payload_size=max(0, length - 8),  # UDP header is 8 bytes
                rate_per_second=float(rate),
                malformed=malformed,
                observed_at=datetime.fromtimestamp(ts, tz=timezone.utc),
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

    normal_events = _events_from_capture(
        real_dataset_dir / "real_normal_raw.txt",
        protocol="GTP-U",
        malformed=False,
        dest_port_override=2152,
    )

    anomalous_events: list[NormalizedEvent] = []
    anomalous_events += _events_from_capture(
        real_dataset_dir / "real_storm_pingflood_raw.txt",
        protocol="GTP-U",
        malformed=False,
        dest_port_override=2152,
    )
    anomalous_events += _events_from_capture(
        real_dataset_dir / "real_storm_udpflood_raw.txt",
        protocol="GTP-U",
        malformed=False,
        dest_port_override=2152,
    )
    anomalous_events += _events_from_capture(
        real_dataset_dir / "real_malformed_raw.txt",
        protocol="GTP-U",
        malformed=False,  # real, complete UDP header -- see module docstring
        dest_port_override=2152,
    )
    anomalous_events += _events_from_capture(
        real_dataset_dir / "real_scan_raw.txt",
        protocol="UNKNOWN",
        malformed=False,
        dest_port_override=None,  # keep each packet's real (random) dest port
    )

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
