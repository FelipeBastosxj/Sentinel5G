"""Deterministic feature extraction from a NormalizedEvent.

This is the Python mirror of pkg/events/types.go's NormalizedEvent and MUST
stay in lockstep with docs/event-model.md, the single source of truth for
the wire schema shared between the Go operator/eBPF side and this service.
"""

from __future__ import annotations

import math
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Mapping

FEATURE_NAMES = [
    "proto_gtpu",
    "proto_sip",
    "proto_smpp",
    "proto_http2",
    "proto_unknown",
    "payload_size_norm",
    "rate_per_second_norm",
    "malformed",
    "dest_port_signaling",
    "dest_port_norm",
    "hour_sin",
    "hour_cos",
]

FEATURE_VECTOR_SIZE = len(FEATURE_NAMES)

_PROTOCOLS = ("GTP-U", "SIP", "SMPP", "HTTP2")
_SIGNALING_PORTS = {2152, 5060}

# "PORT_SCAN" (pkg/events.ProtocolPortScan, see docs/event-model.md) is
# deliberately NOT in _PROTOCOLS: any protocol not listed here already falls
# into the proto_unknown one-hot bucket below, and adding a 5th protocol
# dimension would grow FEATURE_VECTOR_SIZE, changing the shipped ONNX
# model's input shape and requiring a retrain. A port-scan event is still
# distinguishable from a genuinely unknown protocol via the other features
# (payload_size_norm in particular carries the distinct-port count for this
# protocol, not a byte size — see NormalizedEvent.payload_size's docstring
# reference in docs/event-model.md).

# Clipping bounds keep raw counters in a well-behaved range before they reach
# the autoencoder. Chosen generously relative to
# scripts/generate_synthetic_dataset.py's generation parameters.
_MAX_PAYLOAD_BYTES = 4096
_MAX_RATE_PER_SECOND = 5000.0


@dataclass(frozen=True)
class NormalizedEvent:
    """Python mirror of pkg/events/types.go's NormalizedEvent.

    Only carries the fields extract_features() actually consumes, not every
    wire field — e.g. vlanId (see docs/event-model.md) is ingested by the Go
    side but deliberately not yet a model feature here, so it's omitted
    rather than mirrored-and-unused.
    """

    protocol: str
    dest_port: int
    payload_size: int
    rate_per_second: float
    malformed: bool
    observed_at: datetime

    @classmethod
    def from_dict(cls, data: Mapping[str, object]) -> "NormalizedEvent":
        observed_at = data.get("observedAt")
        if isinstance(observed_at, str):
            observed_at = datetime.fromisoformat(observed_at.replace("Z", "+00:00"))
        elif not isinstance(observed_at, datetime):
            observed_at = datetime.now(timezone.utc)

        return cls(
            protocol=str(data.get("protocol", "UNKNOWN")),
            dest_port=int(data.get("destPort", 0)),
            payload_size=int(data.get("payloadSize", 0)),
            rate_per_second=float(data.get("ratePerSecond", 0.0)),
            malformed=bool(data.get("malformed", False)),
            observed_at=observed_at,
        )


def extract_features(event: NormalizedEvent) -> list[float]:
    """Extracts the fixed-size, FEATURE_VECTOR_SIZE-long feature vector used
    by both training (scripts/train.py) and serving (sentinel_ai/server.py).
    """
    protocol_one_hot = [1.0 if event.protocol == proto else 0.0 for proto in _PROTOCOLS]
    protocol_one_hot.append(1.0 if event.protocol not in _PROTOCOLS else 0.0)

    payload_norm = min(max(event.payload_size, 0), _MAX_PAYLOAD_BYTES) / _MAX_PAYLOAD_BYTES
    rate_norm = min(max(event.rate_per_second, 0.0), _MAX_RATE_PER_SECOND) / _MAX_RATE_PER_SECOND

    hour = event.observed_at.hour + event.observed_at.minute / 60.0
    hour_angle = 2 * math.pi * hour / 24.0

    features = protocol_one_hot + [
        payload_norm,
        rate_norm,
        1.0 if event.malformed else 0.0,
        1.0 if event.dest_port in _SIGNALING_PORTS else 0.0,
        min(max(event.dest_port, 0), 65535) / 65535.0,
        math.sin(hour_angle),
        math.cos(hour_angle),
    ]

    if len(features) != FEATURE_VECTOR_SIZE:
        # A plain check, not `assert`: assertions are stripped under
        # `python -O`/PYTHONOPTIMIZE, which would silently drop this
        # invariant check in an optimized production run.
        raise RuntimeError(
            f"feature extraction produced {len(features)} values, expected {FEATURE_VECTOR_SIZE}"
        )
    return features
