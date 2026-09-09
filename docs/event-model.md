# Event model

This is the canonical schema for the two events that cross the Go/Python
boundary. It is defined twice — once per language — and the two
definitions **must** be kept in lockstep by hand:

- Go: `pkg/events/types.go`
- Python: `cmd/ai-engine/sentinel_ai/features.py` (`NormalizedEvent`) and
  `cmd/ai-engine/sentinel_ai/server.py` (`ThreatScoreEvent` construction)

## NATS subjects

| Subject (default)                    | Producer      | Consumer            | Payload            |
|---------------------------------------|---------------|----------------------|---------------------|
| `sentinel5g.events.normalized`        | `pkg/ingestion.Publisher` (Layer 2) | AI engine (`cmd/ai-engine`) | `NormalizedEvent`   |
| `sentinel5g.threats.scored`           | AI engine     | Operator (`pkg/controller.ThreatScoreWatcher`) | `ThreatScoreEvent`  |

Both subjects live on a single JetStream stream (`SENTINEL5G` by default,
`NATS_STREAM_NAME`), file-backed with a 24h retention limit — see
`pkg/events/nats.go`'s `ensureStream`.

## `NormalizedEvent`

Produced by the Layer 2 normalization step from a raw capture-layer
observation. High-volume telemetry, not a packet capture — a small, fixed
set of fields.

| Field            | Type        | Notes |
|-------------------|-------------|-------|
| `eventId`         | string      | Assigned by the ingestion layer. |
| `observedAt`      | RFC3339 time | When the underlying packet/message was captured. |
| `namespace`       | string      | Kubernetes namespace of the workload observed. |
| `podName`         | string      | Pod name. |
| `nodeName`        | string      | Node the capture happened on. |
| `sourceIp`        | string      | Source IPv4 or IPv6 address (see `bpf/packet_filter.c`'s parallel `*_v6` maps/ringbuf — IPv6 is a separate capture path, not a unified scheme, but both render into this same string field). |
| `destIp`          | string      | Destination IPv4 or IPv6 address. |
| `destPort`        | uint16      | Destination port. |
| `protocol`        | enum        | One of `GTP-U`, `SIP`, `SMPP`, `HTTP2`, `PORT_SCAN`, `UNKNOWN`. `PORT_SCAN` is deliberately folded into the AI engine's `proto_unknown` one-hot feature (see `cmd/ai-engine/sentinel_ai/features.py`) rather than given its own feature dimension, to avoid changing `FEATURE_VECTOR_SIZE` and breaking the shipped ONNX model's input shape — it's still distinguishable from a genuinely unknown protocol via the other features (`payload_size_norm` in particular; see `payloadSize`'s note above). |
| `payloadSize`     | uint32      | Signaling payload size in bytes, except for `protocol == "PORT_SCAN"`, where this instead carries the distinct-destination-port count that triggered the scan detection (see `bpf/packet_filter.c`'s `port_scan`/`track_port_scan()`). |
| `ratePerSecond`   | float64     | eBPF-side rolling rate for this `(sourceIp, protocol)` tuple. |
| `malformed`       | bool        | True when the eBPF parser could not validate protocol framing. |
| `vlanId`          | uint16      | 802.1Q VLAN ID the packet was tagged with, or `0` for an untagged frame. Not yet a model feature (see `cmd/ai-engine/sentinel_ai/features.py`) — ingested but unused by scoring for now. |

## `ThreatScoreEvent`

Produced by the AI engine after scoring one or more `NormalizedEvent`s.

| Field            | Type        | Notes |
|-------------------|-------------|-------|
| `sourceEventId`   | string      | The `NormalizedEvent.eventId` that produced this score. |
| `namespace`       | string      | Copied from the scored event. |
| `podName`         | string      | Copied from the scored event. |
| `sourceIp`        | string      | Copied from the scored event. |
| `score`           | float64     | Reconstruction-error-derived anomaly score, normalized to `[0.0, 1.0]`. |
| `model`           | string      | Scoring model/version, e.g. `autoencoder-v1`. |
| `detectedAt`      | RFC3339 time | When the AI engine produced this score. |

## Feature vector (AI engine internal)

`cmd/ai-engine/sentinel_ai/features.py` turns a `NormalizedEvent` into a
fixed, 12-dimensional feature vector (`FEATURE_VECTOR_SIZE`) before it
reaches the autoencoder:

| Index | Name                     | Description |
|-------|--------------------------|--------------|
| 0-4   | `proto_{gtpu,sip,smpp,http2,unknown}` | One-hot protocol encoding. |
| 5     | `payload_size_norm`      | `payloadSize` clipped to 4096 bytes, normalized to `[0,1]`. |
| 6     | `rate_per_second_norm`   | `ratePerSecond` clipped to 5000/s, normalized to `[0,1]`. |
| 7     | `malformed`              | `1.0` if the eBPF parser flagged malformed framing. |
| 8     | `dest_port_signaling`    | `1.0` if `destPort` is 2152 (GTP-U) or 5060 (SIP). |
| 9     | `dest_port_norm`         | `destPort / 65535`. |
| 10-11 | `hour_sin`, `hour_cos`   | Cyclical encoding of time-of-day, for off-hours anomaly signal. |

This vector — not the raw event — is what the autoencoder is trained and
scored against; see `docs/architecture.md` for how the score turns into a
mitigation decision.
