"""Generates a synthetic telecom-signaling dataset standing in for real
GTP-U/SIP traffic captures (which Sentinel5G does not ship with, and cannot
ship with, since real telecom traffic is not available in this environment).

"Normal" samples cluster around a handful of realistic signaling baselines;
"anomalous" samples simulate signaling storms, protocol malformation, and
off-hours scanning bursts. This dataset only exists to exercise the
train -> export -> infer pipeline end to end; it is not a claim about
real-world attack traffic distributions, and must not be used to make
production sensitivity/threshold decisions without replacing it with real
captures first (see docs/architecture.md).
"""

from __future__ import annotations

import argparse
import random
from datetime import datetime, timedelta, timezone
from pathlib import Path

import numpy as np

from sentinel_ai.features import NormalizedEvent, extract_features

_PROTOCOLS_NORMAL = ["GTP-U", "SIP", "SMPP", "HTTP2"]
_PORTS_NORMAL = {"GTP-U": 2152, "SIP": 5060, "SMPP": 2775, "HTTP2": 443}

_SECONDS_PER_DAY = 24 * 60 * 60


def _random_time_of_day(rng: random.Random, base_time: datetime) -> datetime:
    """Normal traffic must span the full day, not a narrow slice of it —
    otherwise the autoencoder learns "wrong time of day" as *the* anomaly
    signal and drowns out the protocol/rate/malformed signals every other
    event type is actually meant to be flagged on.
    """
    return base_time + timedelta(seconds=rng.uniform(0, _SECONDS_PER_DAY))


def _tunnel(rng: random.Random, protocol: str, rate: float) -> tuple[int, float]:
    """Gives GTP-U events a plausible tunnel identity, and everything else
    none.

    Not cosmetic: since `has_teid` became a feature, generating GTP-U with a
    zero TEID would teach the model that "normal" GTP-U has no tunnel, which
    is the exact opposite of what the real captures show (every real GTP-U
    packet in docs/paper-data/real-dataset/ carries one) and would invert the
    feature's meaning.

    Per-tunnel rate is modelled as a share of the source's rate rather than a
    second independent draw, because that is the real relationship: one
    source IP carries many tunnels, so a tunnel's own rate is at most the
    source's and usually well below it.
    """
    if protocol != "GTP-U":
        return 0, 0.0
    teid = rng.randint(1, 0xFFFFFF)
    return teid, max(1.0, rate * rng.uniform(0.2, 1.0))


def _normal_event(rng: random.Random, base_time: datetime) -> NormalizedEvent:
    protocol = rng.choice(_PROTOCOLS_NORMAL)
    rate = max(0.0, rng.gauss(20, 8))
    teid, tunnel_rate = _tunnel(rng, protocol, rate)
    return NormalizedEvent(
        protocol=protocol,
        dest_port=_PORTS_NORMAL[protocol],
        payload_size=max(0, int(rng.gauss(256, 64))),
        rate_per_second=rate,
        malformed=False,
        observed_at=_random_time_of_day(rng, base_time),
        teid=teid,
        tunnel_rate_per_second=tunnel_rate,
    )


def _anomalous_event(rng: random.Random, base_time: datetime) -> NormalizedEvent:
    kind = rng.choice(["storm", "malformed", "scan"])

    if kind == "storm":
        protocol = rng.choice(["GTP-U", "SIP"])
        rate = rng.uniform(800, 4500)
        teid, tunnel_rate = _tunnel(rng, protocol, rate)
        return NormalizedEvent(
            protocol=protocol,
            dest_port=_PORTS_NORMAL[protocol],
            payload_size=max(0, int(rng.gauss(256, 64))),
            rate_per_second=rate,
            malformed=False,
            observed_at=_random_time_of_day(rng, base_time),
            teid=teid,
            tunnel_rate_per_second=tunnel_rate,
        )

    if kind == "malformed":
        protocol = rng.choice(_PROTOCOLS_NORMAL)
        return NormalizedEvent(
            protocol=protocol,
            dest_port=rng.randint(1, 65535),
            payload_size=max(0, int(rng.uniform(0, 8))),
            rate_per_second=max(0.0, rng.gauss(20, 8)),
            malformed=True,
            observed_at=_random_time_of_day(rng, base_time),
        )

    # kind == "scan": unknown protocol, unusual port, elevated rate — flagged
    # off-hours (02:00-04:00) specifically because a handful of normal
    # samples also naturally land in that window (see _random_time_of_day),
    # so the model has to weigh time-of-day together with the other
    # features rather than treating either signal alone as decisive.
    return NormalizedEvent(
        protocol="UNKNOWN",
        dest_port=rng.randint(1024, 65535),
        payload_size=max(0, int(rng.uniform(0, 64))),
        rate_per_second=rng.uniform(200, 1000),
        malformed=False,
        observed_at=base_time.replace(hour=2) + timedelta(seconds=rng.uniform(0, 2 * 3600)),
    )


def generate(num_normal: int, num_anomalous: int, seed: int = 42) -> tuple[np.ndarray, np.ndarray]:
    # Reproducible synthetic data generation, not a security context.
    rng = random.Random(seed)  # nosec B311
    base_time = datetime(2026, 1, 5, tzinfo=timezone.utc)

    normal = [extract_features(_normal_event(rng, base_time)) for _ in range(num_normal)]
    anomalous = [extract_features(_anomalous_event(rng, base_time)) for _ in range(num_anomalous)]

    return np.asarray(normal, dtype=np.float32), np.asarray(anomalous, dtype=np.float32)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--num-normal", type=int, default=4000)
    parser.add_argument("--num-anomalous", type=int, default=200)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--output", type=Path, default=Path("data/synthetic_dataset.npz"))
    args = parser.parse_args()

    normal, anomalous = generate(args.num_normal, args.num_anomalous, args.seed)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    np.savez(args.output, normal=normal, anomalous=anomalous)
    print(f"wrote {len(normal)} normal and {len(anomalous)} anomalous samples to {args.output}")


if __name__ == "__main__":
    main()
