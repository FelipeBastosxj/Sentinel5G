"""Deterministic feature extraction from a NormalizedEvent.

This is the Python mirror of pkg/events/types.go's NormalizedEvent and MUST
stay in lockstep with docs/event-model.md, the single source of truth for
the wire schema shared between the Go operator/eBPF side and this service.
"""

from __future__ import annotations

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
    "untunneled_rate_norm",
    "malformed",
    "dest_port_signaling",
    "dest_port_norm",
    "tunnel_rate_norm",
    "has_teid",
]

# hour_sin/hour_cos -- a cyclical time-of-day encoding, meant as an
# "off-hours activity" signal -- used to sit between dest_port_norm and
# tunnel_rate_norm. They were removed on the strength of a measurement, not
# a preference (docs/paper-data/02-ai-training-inference.md §2.6):
#
# Every real capture this project has is a single session spanning about an
# hour of wall-clock. Trained on that raw, the autoencoder learned "was this
# packet captured during my training hour" as THE dominant signal and scored
# 1.0000 on 5,000/5,000 packets of well-behaved traffic from a second
# capture, purely because it was taken eleven hours later -- a 100%
# false-positive rate on any deployment whose traffic happens at a different
# time of day, which is all of them. Spreading the training timestamps
# across 24h (what the synthetic generator already did) fixed that but turned
# the two features into pure noise the model cannot reconstruct, which
# inflated the normalization's reference error ~17x and compressed every
# real anomaly's score below the production thresholds. No real capture
# carries any time-of-day information to learn, and the "off-hours scan"
# shape only ever existed in the synthetic generator's imagination. A
# feature with no supporting data and two demonstrated failure modes is a
# liability, so it is gone; a future capture with a real diurnal baseline
# would be the evidence to bring it back on.


FEATURE_VECTOR_SIZE = len(FEATURE_NAMES)

_PROTOCOLS = ("GTP-U", "SIP", "SMPP", "HTTP2")
_SIGNALING_PORTS = {2152, 5060}

# "PORT_SCAN" (pkg/events.ProtocolPortScan, see docs/event-model.md) is
# deliberately NOT in _PROTOCOLS: any protocol not listed here already falls
# into the proto_unknown one-hot bucket below, and adding a 5th protocol
# dimension would grow FEATURE_VECTOR_SIZE, changing the shipped ONNX
# model's input shape and requiring a retrain. (tunnel_rate_norm/has_teid
# below DID pay that cost, deliberately and once, for features the model
# cannot do without -- see their comment. It is a reason to be deliberate
# about the vector's width, not a rule that it may never change.) A port-scan event is still
# distinguishable from a genuinely unknown protocol via the other features
# (payload_size_norm in particular carries the distinct-port count for this
# protocol, not a byte size — see NormalizedEvent.payload_size's docstring
# reference in docs/event-model.md).

# Clipping bounds keep raw counters in a well-behaved range before they reach
# the autoencoder. Chosen generously relative to
# scripts/generate_synthetic_dataset.py's generation parameters.
_MAX_PAYLOAD_BYTES = 4096
_MAX_RATE_PER_SECOND = 5000.0

# Per-tunnel rate gets a much tighter clip than _MAX_RATE_PER_SECOND, and
# that is the point rather than an oversight. A single GTP-U tunnel carries
# ONE subscriber's traffic, so its interesting range sits orders of magnitude
# below an aggregate per-source-IP rate. Clipping it at 5000 the way
# rate_per_second does would squash every realistic per-tunnel value into the
# bottom couple of percent of the feature's range -- which is precisely the
# resolution loss that leaves rate_per_second_norm unable to separate a real
# in-tunnel flood today (0.0679 vs an 0.0811 normal baseline, see
# docs/paper-data/02-ai-training-inference.md §2.4).
_MAX_TUNNEL_RATE_PER_SECOND = 2000.0


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
    # Defaulted, and appended rather than inserted, for two reasons: a frozen
    # dataclass requires defaulted fields last, and every existing
    # construction site (the dataset scripts, the tests) keeps working
    # unchanged while meaning exactly what it did before -- "no tunnel
    # identity".
    teid: int = 0
    tunnel_rate_per_second: float = 0.0

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
            # .get defaults, not required keys: an older Go operator
            # publishing without these still scores cleanly, as has_teid=0.
            teid=int(data.get("teid", 0)),
            tunnel_rate_per_second=float(data.get("tunnelRatePerSecond", 0.0)),
        )


def extract_features(event: NormalizedEvent) -> list[float]:
    """Extracts the fixed-size, FEATURE_VECTOR_SIZE-long feature vector used
    by both training (scripts/train.py) and serving (sentinel_ai/server.py).
    """
    protocol_one_hot = [1.0 if event.protocol == proto else 0.0 for proto in _PROTOCOLS]
    protocol_one_hot.append(1.0 if event.protocol not in _PROTOCOLS else 0.0)

    payload_norm = min(max(event.payload_size, 0), _MAX_PAYLOAD_BYTES) / _MAX_PAYLOAD_BYTES

    # The per-source rate is only allowed to speak for traffic that has no
    # tunnel identity (SIP, SMPP, an off-port probe, a raw UDP flood at the
    # GTP-U port). For a tunneled packet it is zeroed and tunnel_rate_norm
    # below carries the rate instead. Measured, not stylistic
    # (docs/paper-data/02-ai-training-inference.md §2.6.5): on a gNB where
    # one subscriber floods, the per-source rate is identical for every
    # innocent subscriber behind that gNB, and a model that saw it scored
    # the bystanders' packets above the mitigation threshold -- 150 of 192
    # in one training run. Zeroing it for tunneled traffic took that to 0 of
    # 192 while keeping 91% of the flooding tunnel's own packets flagged;
    # dropping the feature outright did the same but blinded the model to
    # every untunneled storm. This is the per-source cross-attribution the
    # per-TEID work removed from the detector, removed from the model too.
    if event.teid != 0:
        rate_norm = 0.0
    else:
        rate_norm = (
            min(max(event.rate_per_second, 0.0), _MAX_RATE_PER_SECOND) / _MAX_RATE_PER_SECOND
        )

    tunnel_norm = (
        min(max(event.tunnel_rate_per_second, 0.0), _MAX_TUNNEL_RATE_PER_SECOND)
        / _MAX_TUNNEL_RATE_PER_SECOND
    )

    features = protocol_one_hot + [
        payload_norm,
        rate_norm,
        1.0 if event.malformed else 0.0,
        1.0 if event.dest_port in _SIGNALING_PORTS else 0.0,
        min(max(event.dest_port, 0), 65535) / 65535.0,
        tunnel_norm,
        # has_teid earns its own dimension independently of tunnel_rate_norm.
        # Until the kernel parsed GTP-U headers, "protocol" was asserted from
        # the destination port alone, so a plain UDP flood aimed at port 2152
        # with no GTP header at all (25,944 packets of it in
        # docs/paper-data/real-dataset/) was indistinguishable from genuine
        # tunneled traffic. This gives the autoencoder a first-class encoding
        # of "this really is a GTP-U tunnel", so normal becomes "GTP-U WITH a
        # valid TEID" and the non-conformant class is anomalous for a reason
        # rather than incidentally via its rate.
        1.0 if event.teid != 0 else 0.0,
    ]

    if len(features) != FEATURE_VECTOR_SIZE:
        # A plain check, not `assert`: assertions are stripped under
        # `python -O`/PYTHONOPTIMIZE, which would silently drop this
        # invariant check in an optimized production run.
        raise RuntimeError(
            f"feature extraction produced {len(features)} values, expected {FEATURE_VECTOR_SIZE}"
        )
    return features
