#ifndef SENTINEL5G_BPF_COMMON_H
#define SENTINEL5G_BPF_COMMON_H

/* ETH_P_IP is normally pulled in from <linux/if_ether.h>, but this project
 * builds against vmlinux.h (see bpf/Makefile) instead of UAPI kernel
 * headers -- and BTF (what vmlinux.h is generated from) only captures
 * types/enums/functions, not preprocessor #defines, so this constant isn't
 * there to pull in. It's a fixed IEEE 802.3 EtherType value, not something
 * that could vary by kernel build anyway. */
#define ETH_P_IP 0x0800

/* 3GPP GTP-U (user plane tunneling, e.g. N3/N9 interfaces). */
#define GTPU_PORT 2152

/* SIP signaling (VoIP/IMS control plane). */
#define SIP_PORT 5060

#define MAX_BLOCKLIST_ENTRIES 65536
#define MAX_RATE_ENTRIES 65536

/* Rolling window used by track_signal_rate()/track_scan_rate() to bucket
 * packet counts. */
#define SIGNALING_RATE_WINDOW_NS 1000000000ULL /* 1 second */

/* Packets from one source, within SIGNALING_RATE_WINDOW_NS, before an
 * off-signaling-port UDP burst gets escalated into a signaling_events
 * observation (see scan_rate/track_scan_rate() in packet_filter.c).
 * Previously this traffic was invisible to Layer 2/3 no matter its volume —
 * is_signaling_port() gated ALL observation, not just signal_rate tracking
 * (see ROADMAP.md Phase 1). Chosen to roughly match the synthetic dataset's
 * own "normal" signaling baseline rate (~20/s,
 * cmd/ai-engine/scripts/generate_synthetic_dataset.py) so a single stray
 * non-signaling packet (DNS, NTP, a health check) doesn't trigger this path,
 * while a sustained probe/scan pattern does. A compile-time constant
 * deliberately, not yet a runtime-tunable one (see MAX_BLOCKLIST_ENTRIES
 * above for the same convention) — not empirically validated against real
 * off-protocol traffic on a production telecom-facing interface, only
 * reasoned about; revisit once there's real data to tune it against. */
#define SCAN_EMIT_THRESHOLD 10

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
