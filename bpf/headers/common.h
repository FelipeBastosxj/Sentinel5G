#ifndef SENTINEL5G_BPF_COMMON_H
#define SENTINEL5G_BPF_COMMON_H

/* 3GPP GTP-U (user plane tunneling, e.g. N3/N9 interfaces). */
#define GTPU_PORT 2152

/* SIP signaling (VoIP/IMS control plane). */
#define SIP_PORT 5060

#define MAX_BLOCKLIST_ENTRIES 65536
#define MAX_RATE_ENTRIES 65536

/* Rolling window used by track_signal_rate() to bucket packet counts. */
#define SIGNALING_RATE_WINDOW_NS 1000000000ULL /* 1 second */

/* Ring buffer capacity for signaling_events (see struct signaling_event in
 * packet_filter.c) — must be a power of 2. 256KB comfortably holds bursts of
 * per-packet signaling telemetry between userspace reads without needing to
 * size it for sustained bulk-data-plane throughput, since only GTP-U/SIP
 * control traffic (not every packet) is emitted. */
#define SIGNALING_EVENTS_RINGBUF_BYTES (256 * 1024)

/* signal_event_protocol values, shared between packet_filter.c's emitter and
 * pkg/ebpf's decoder (see SignalProtocol in pkg/ebpf/blocklist.go — keep
 * both in sync by hand, same as every other cross-language constant in this
 * project). */
#define SIGNAL_PROTO_UNKNOWN 0
#define SIGNAL_PROTO_GTPU 1
#define SIGNAL_PROTO_SIP 2

#endif /* SENTINEL5G_BPF_COMMON_H */
