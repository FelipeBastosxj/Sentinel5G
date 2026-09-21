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
| `sentinel5g.events.normalized`        | `pkg/ingestion.Publisher` (Layer 2); also `pkg/hubble.Observer` and `pkg/falco.Bridge` | AI engine (`cmd/ai-engine`) | `NormalizedEvent`   |
| `sentinel5g.threats.scored`           | AI engine; also `pkg/detect` (inside the operator, via `pkg/ingestion.Publisher`) | Operator (`pkg/controller.ThreatScoreWatcher`), which also feeds `sentinel5g_threat_scores_received_total` and the `ScoringPipelineReady` condition from it | `ThreatScoreEvent`  |

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
| `protocol`        | enum        | One of `GTP-U`, `SIP`, `SMPP`, `HTTP2`, `PORT_SCAN`, `UNKNOWN`. `PORT_SCAN` is deliberately folded into the AI engine's `proto_unknown` one-hot feature (see `cmd/ai-engine/sentinel_ai/features.py`) rather than given its own feature dimension — changing `FEATURE_VECTOR_SIZE` invalidates every deployed model, so the vector's width is changed deliberately and rarely (it has been, twice, for features the model could not do without; see §2.5–2.6 of the AI document), not for a protocol that's already distinguishable from a genuinely unknown protocol via the other features (`payload_size_norm` in particular; see `payloadSize`'s note above). |
| `payloadSize`     | uint32      | Signaling payload size in bytes, except for `protocol == "PORT_SCAN"`, where this instead carries the distinct-destination-port count that triggered the scan detection (see `bpf/packet_filter.c`'s `port_scan`/`track_port_scan()`). |
| `ratePerSecond`   | float64     | eBPF-side rolling rate for this `(sourceIp, protocol)` tuple. See `tunnelRatePerSecond` below for why this one is not sufficient on its own. |
| `malformed`       | bool        | True when the eBPF parser could not validate protocol framing. |
| `vlanId`          | uint16      | 802.1Q VLAN ID the packet was tagged with, or `0` for an untagged frame. Not yet a model feature (see `cmd/ai-engine/sentinel_ai/features.py`) — ingested but unused by scoring for now. |
| `teid`            | uint32      | GTP-U Tunnel Endpoint Identifier, host byte order. **`0` is the explicit "no tunnel identity" sentinel, never tunnel number zero** — set when the capture path parsed no valid GTP-U T-PDU header: a non-GTP-U protocol, a path-management message (Echo/Error Indication/End Marker, which legitimately use TEID 0), a framing failure (which also sets `malformed`), or a capture path that cannot see tunnels at all. `pkg/hubble` and `pkg/falco` always emit `0`; see below. |
| `tunnelRatePerSecond` | float64 | eBPF-side rolling rate for this `(sourceIp, teid)` tuple, as opposed to `ratePerSecond`'s per-`sourceIp` aggregate. Always `0` when `teid` is `0`. |

### Why `teid`/`tunnelRatePerSecond` exist alongside `ratePerSecond`

They are not a refinement of the same measurement; they are a different
measurement. On a real N3 interface **every subscriber's user-plane traffic
arrives from the same source IP** — the peer gNB/UPF — so a per-source
counter can only ever report aggregate load, and one tunnel flooding is
invisible inside it. That is the measured gap in `ROADMAP.md` Phase 2.5: the
only anomaly type that is simultaneously real GTP-U and observable by
`bpf/packet_filter.c` scored *below* the held-out normal baseline (0.0679 vs
0.0811) and was missed at every production sensitivity threshold.

`bpf/packet_filter.c` now parses the GTP-U header itself (3GPP TS 29.281
§5.1 — version/PT validation, the optional sequence/N-PDU block, and a
bounded extension-header walk) rather than inferring "this is GTP-U" from
the destination port alone. The same `(source, TEID)` key addresses both
the rate map and the drop map (`tunnel_blocklist`), so the thing the
detector measured is exactly the thing an `ebpfBlockTunnel` action drops. Two consequences beyond the new fields:

- A packet on port 2152 whose GTP-U framing doesn't validate now sets
  `malformed`, which previously only ever fired for a UDP header too short
  to read.
- `tunnelRatePerSecond` is carried **in the kernel's own ring buffer record**,
  not looked up from userspace afterwards. A lookup races the 1-second window
  roll and can report `1` for the very packet the kernel counted as the
  three-thousandth — worst precisely during the flood the field exists to
  catch.

Capture paths other than eBPF cannot produce these fields *in principle*, not
merely "not yet": `pkg/hubble` reads Cilium flow summaries (L3/L4 five-tuples
and endpoint identity, never the tunnel header inside the payload), and
`pkg/falco` traces syscalls (it observes the read/write boundary, never the
payload bytes). Both always emit `0`, and the deterministic GTP-U
tunnel-flood detector requires a non-zero `teid` — so those paths cannot trip
it by construction.

## `ThreatScoreEvent`

Produced by the AI engine after scoring one or more `NormalizedEvent`s, or by a deterministic detector in `pkg/detect` (see `model`).

| Field            | Type        | Notes |
|-------------------|-------------|-------|
| `sourceEventId`   | string      | The `NormalizedEvent.eventId` that produced this score. |
| `namespace`       | string      | Copied from the scored event. |
| `podName`         | string      | Copied from the scored event. |
| `sourceIp`        | string      | Copied from the scored event. On a GTP-U N3 interface this is the peer gNB's address, shared by every subscriber behind it — which is why `teid` below exists. |
| `teid`            | uint32      | Copied from the scored event's `teid`, or `0` when it had none. What lets the mitigation be as precise as the detection: a policy with `actions.ebpfBlockTunnel` drops this one tunnel. A score arriving with `0` leaves that action a **no-op** — it deliberately does not widen into blocking the whole source. |
| `score`           | float64     | Reconstruction-error-derived anomaly score, normalized to `[0.0, 1.0]`. |
| `model`           | string      | Scoring model/version, e.g. `autoencoder-v1`. A **`rule:` prefix** means the score came from a deterministic, non-ML detector rather than the autoencoder (e.g. `rule:gtpu-tunnel-flood`). Everything else about the event — and the entire `ThreatScoreWatcher` path it drives — is identical for both, so a rule-sourced score goes through exactly the same policy/sensitivity/`autoMitigate` gating. |
| `detectedAt`      | RFC3339 time | When the AI engine, or the detector, produced this score. |

## Feature vector (AI engine internal)

`cmd/ai-engine/sentinel_ai/features.py` turns a `NormalizedEvent` into a
fixed, 12-dimensional feature vector (`FEATURE_VECTOR_SIZE`) before it
reaches the autoencoder:

| Index | Name                     | Description |
|-------|--------------------------|--------------|
| 0-4   | `proto_{gtpu,sip,smpp,http2,unknown}` | One-hot protocol encoding. |
| 5     | `payload_size_norm`      | `payloadSize` clipped to 4096 bytes, normalized to `[0,1]`. |
| 6     | `untunneled_rate_norm`   | `ratePerSecond` clipped to 5000/s, normalized to `[0,1]` — **but only for an event with `teid == 0`**; for tunneled traffic it is `0.0` and index 10 carries the rate instead. The per-source rate is identical for every subscriber behind a gNB, so letting the model see it for tunneled packets scored innocent bystanders as anomalous during someone else's flood (`docs/paper-data/02-ai-training-inference.md` §2.6.5). |
| 7     | `malformed`              | `1.0` if the eBPF parser flagged malformed framing. |
| 8     | `dest_port_signaling`    | `1.0` if `destPort` is 2152 (GTP-U) or 5060 (SIP). |
| 9     | `dest_port_norm`         | `destPort / 65535`. |
| 10    | `tunnel_rate_norm`       | `tunnelRatePerSecond` clipped to 2000/s, normalized to `[0,1]`. A much tighter clip than `untunneled_rate_norm`'s 5000 — a single tunnel carries one subscriber, so its interesting range sits orders of magnitude below an aggregate per-source rate, and clipping it the same way would squash every realistic value into the bottom couple of percent. |
| 11    | `has_teid`               | `1.0` when `teid` is non-zero. Encodes "this really is a GTP-U tunnel" as a first-class feature, rather than leaving the model to infer it from the destination port — which is what made 25,944 packets of plain UDP aimed at port 2152 indistinguishable from genuine tunneled traffic. |

This vector — not the raw event — is what the autoencoder is trained and
scored against; see `docs/architecture.md` for how the score turns into a
mitigation decision.

There is deliberately **no time-of-day feature**. `hour_sin`/`hour_cos`
used to occupy indices 10-11 and were removed after producing a 100%
false-positive rate on real traffic captured at a different hour from the
training session — see `docs/paper-data/02-ai-training-inference.md` §2.6.
`observedAt` is still on the wire — it is the kernel's monotonic capture
timestamp converted to wall-clock by `pkg/ebpf`, kept for correlation; it
just doesn't reach the model.

**Changing `FEATURE_VECTOR_SIZE` changes the shipped ONNX model's input
shape**, so a model exported against a different width cannot be used. The
AI engine refuses to start against one rather than failing per event (which
in NATS worker mode would be an exception log, a nak, five redeliveries and
a silently dropped event, forever); the error names the re-export command.
See `docs/paper-data/02-ai-training-inference.md` §2.5 and §2.6 for the
measurements that motivated the most recent changes.
