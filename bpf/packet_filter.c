// SPDX-License-Identifier: Apache-2.0
//
// bpf/packet_filter.c — Layer 1 (Capture & Kernel) of Sentinel5G.
//
// XDP program attached to a telecom-facing pod's host network interface. It
// parses Ethernet/IPv4/UDP headers looking for GTP-U (3GPP user-plane
// tunneling, port 2152) and SIP (VoIP/IMS signaling, port 5060) traffic,
// maintains a coarse per-source-IP signaling rate counter for storm
// detection, and drops packets whose source IP has been pushed into
// `blocklist` — the only way in or out of that map is pkg/ebpf.Loader,
// driven by pkg/controller.ThreatScoreWatcher in response to an AI-engine
// threat score.
//
// It also emits a `signaling_events` ring buffer record for every
// GTP-U/SIP packet observed (and for UDP packets too short to have a
// complete header, flagged malformed) — this is what pkg/ingestion turns
// into `events.NormalizedEvent` and publishes to NATS_EVENTS_SUBJECT,
// bridging this layer to Layer 2/3 (the AI engine). Deliberately not full
// packet capture: only signaling-port traffic is emitted, matching
// NormalizedEvent's "high-volume telemetry, not a packet capture" design
// (see docs/event-model.md).
//
// Builds against a generated vmlinux.h (bpf/headers/vmlinux.h, `make -C bpf`
// -- see bpf/Makefile) instead of plain UAPI kernel headers. Worth being
// precise about what this buys: `struct ethhdr`/`iphdr`/`udphdr` below are
// wire-format structs whose layout is fixed by the Ethernet/IP/UDP
// protocols themselves, not by the kernel's own build config -- unlike
// e.g. `task_struct` or `sk_buff`, their field offsets can't drift between
// kernel builds, so CO-RE's actual relocation mechanism (BPF_CORE_READ(),
// __builtin_preserve_access_index) has nothing to protect here, and this
// file doesn't use it: every `->` field access below is a plain read, same
// as before. The real, concrete win is dropping the UAPI-header build
// dependency -- vmlinux.h already declares these types itself, so this no
// longer needs linux-libc-dev/linux-headers-$(uname -r) installed or the
// <asm/types.h> multiarch include-path workaround bpf/Makefile used to
// carry (see git history for both). Struct-layout portability isn't the
// story here; a simpler, self-contained build is.

#include "headers/vmlinux.h"
#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>

#include "headers/common.h"

char LICENSE[] SEC("license") = "Dual BSD/GPL";

// blocklist maps an IPv4 source address (raw network-byte-order bytes, as
// read straight out of struct iphdr.saddr — see the byte-order note in
// pkg/ebpf/blocklist.go) to a nonzero "blocked" flag.
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, MAX_BLOCKLIST_ENTRIES);
	__type(key, __u32);
	__type(value, __u8);
} blocklist SEC(".maps");

// signal_rate_entry is a coarse, single-window packet counter per source IP.
// It is a cheap kernel-side signaling-storm signal only; the AI engine
// (cmd/ai-engine) is the source of truth for anomaly scoring.
struct signal_rate_entry {
	__u64 window_start_ns;
	__u32 count;
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_RATE_ENTRIES);
	__type(key, __u32);
	__type(value, struct signal_rate_entry);
} signal_rate SEC(".maps");

// signaling_event is a coarse, per-packet observation of GTP-U/SIP traffic,
// exported for Layer 2 normalization. Field layout is fixed and padded
// explicitly (no reliance on compiler default padding) so pkg/ebpf can
// decode it with a byte-exact matching Go struct — see rawSignalingEvent in
// pkg/ebpf/loader_linux.go.
struct signaling_event {
	__u64 timestamp_ns; // bpf_ktime_get_ns(): monotonic, NOT wall-clock —
	                     // pkg/ebpf converts this to a real time.Time.
	__u32 saddr;         // network byte order, see blocklist.go's note.
	__u32 daddr;
	__u16 dest_port;     // host byte order.
	__u16 payload_size;  // UDP payload size (bytes after the UDP header).
	__u8 protocol;       // SIGNAL_PROTO_* from headers/common.h.
	__u8 malformed;
	__u8 _pad[2];
} __attribute__((packed, aligned(8)));

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, SIGNALING_EVENTS_RINGBUF_BYTES);
} signaling_events SEC(".maps");

static __always_inline void emit_signaling_event(__u32 saddr, __u32 daddr, __u16 dest_port,
						  __u16 payload_size, __u8 protocol, __u8 malformed)
{
	struct signaling_event *evt = bpf_ringbuf_reserve(&signaling_events, sizeof(*evt), 0);
	if (!evt)
		return; // Ring buffer full: drop the observation, never the packet.

	evt->timestamp_ns = bpf_ktime_get_ns();
	evt->saddr = saddr;
	evt->daddr = daddr;
	evt->dest_port = dest_port;
	evt->payload_size = payload_size;
	evt->protocol = protocol;
	evt->malformed = malformed;
	bpf_ringbuf_submit(evt, 0);
}

static __always_inline int is_signaling_port(__u16 dest_port_host)
{
	return dest_port_host == GTPU_PORT || dest_port_host == SIP_PORT;
}

static __always_inline void track_signal_rate(__u32 saddr)
{
	struct signal_rate_entry *entry = bpf_map_lookup_elem(&signal_rate, &saddr);
	__u64 now = bpf_ktime_get_ns();

	if (!entry) {
		struct signal_rate_entry fresh = {.window_start_ns = now, .count = 1};
		bpf_map_update_elem(&signal_rate, &saddr, &fresh, BPF_ANY);
		return;
	}

	if (now - entry->window_start_ns > SIGNALING_RATE_WINDOW_NS) {
		entry->window_start_ns = now;
		entry->count = 1;
	} else {
		entry->count += 1;
	}
}

SEC("xdp")
int xdp_packet_filter(struct xdp_md *ctx)
{
	void *data_end = (void *)(long)ctx->data_end;
	void *data = (void *)(long)ctx->data;

	struct ethhdr *eth = data;
	if ((void *)(eth + 1) > data_end)
		return XDP_PASS;

	if (eth->h_proto != bpf_htons(ETH_P_IP))
		return XDP_PASS;

	struct iphdr *iph = (void *)(eth + 1);
	if ((void *)(iph + 1) > data_end)
		return XDP_PASS;

	// A below-minimum IHL is itself malformed-protocol evidence, but at the
	// XDP layer we let it fall through to the normal Linux stack rather
	// than risk an out-of-bounds read chasing IP options.
	if (iph->ihl < 5)
		return XDP_PASS;

	__u32 saddr = iph->saddr;

	__u8 *blocked = bpf_map_lookup_elem(&blocklist, &saddr);
	if (blocked && *blocked)
		return XDP_DROP;

	if (iph->protocol != IPPROTO_UDP)
		return XDP_PASS;

	struct udphdr *udph = (void *)iph + (iph->ihl * 4);
	if ((void *)(udph + 1) > data_end) {
		// Too short to have a complete UDP header on an otherwise
		// plausible signaling flow — a strong anomaly signal on its own,
		// worth reporting even though we can't safely read dest_port.
		emit_signaling_event(saddr, iph->daddr, 0, 0, SIGNAL_PROTO_UNKNOWN, 1);
		return XDP_PASS;
	}

	__u16 dest_port = bpf_ntohs(udph->dest);
	if (is_signaling_port(dest_port)) {
		track_signal_rate(saddr);

		__u8 protocol = (dest_port == GTPU_PORT) ? SIGNAL_PROTO_GTPU : SIGNAL_PROTO_SIP;
		__u16 payload_size = (__u16)((void *)data_end - (void *)(udph + 1));
		emit_signaling_event(saddr, iph->daddr, dest_port, payload_size, protocol, 0);
	}

	return XDP_PASS;
}
