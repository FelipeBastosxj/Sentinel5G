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
// This program intentionally avoids CO-RE BTF relocations (no vmlinux.h) so
// it builds against plain UAPI kernel headers on any recent kernel. For full
// portability across differing struct layouts, generate one on the target
// host and switch the header reads below to BPF_CORE_READ():
//   bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/headers/vmlinux.h

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/in.h>
#include <linux/ip.h>
#include <linux/udp.h>

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
	if ((void *)(udph + 1) > data_end)
		return XDP_PASS;

	__u16 dest_port = bpf_ntohs(udph->dest);
	if (is_signaling_port(dest_port))
		track_signal_rate(saddr);

	return XDP_PASS;
}
