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
// threat score. IPv6 traffic is inspected the same way through a parallel
// set of maps/struct/ringbuf (the `*_v6` declarations below) rather than a
// unified 128-bit-capable scheme, so none of the IPv4 behavior above is
// touched by IPv6 support existing — see struct signaling_event_v6's
// comment for why that extends to a separate ring buffer too. IPv6
// extension headers between the fixed header and UDP are not walked; a
// packet with one present falls through unobserved, a known, deliberate
// gap (see ROADMAP.md).
//
// A single 802.1Q VLAN tag is transparently unwrapped before the EtherType
// dispatch below: a tagged frame's real EtherType/IPv4 header is parsed the
// same as an untagged one, and the tag's VLAN ID is carried through to
// `signaling_events` (see struct signaling_event's vlan_id field). QinQ
// double-tagging is not unwrapped — see ETH_P_8021Q's comment in
// headers/common.h.
//
// It also emits a `signaling_events` ring buffer record for every
// GTP-U/SIP packet observed, for UDP packets too short to have a complete
// header (flagged malformed), once per window for a source sending a
// sustained burst of UDP to ports outside 2152/5060 (see
// SCAN_EMIT_THRESHOLD in headers/common.h; a single stray off-port packet
// does not emit — this catches a flood against ONE port), and once per
// window for a source touching MULTIPORT_SCAN_THRESHOLD *distinct*
// off-signaling ports (see port_scan/track_port_scan() below — this catches
// classic low-and-slow scanning, which the flood check above cannot, since
// the two are deliberately separate detectors keyed differently). This is
// what pkg/ingestion turns into `events.NormalizedEvent` and publishes to
// NATS_EVENTS_SUBJECT, bridging this layer to Layer 2/3 (the AI engine).
// Still deliberately not full packet capture: every signaling-port packet
// is emitted, but off-port UDP only once a burst/scan crosses its
// respective threshold, matching NormalizedEvent's "high-volume telemetry,
// not a packet capture" design (see docs/event-model.md).
//
// Builds against headers/vmlinux_min.h, a small hand-maintained stand-in for
// a `bpftool btf dump`-generated vmlinux.h (see that file's own top comment
// for why). Worth being precise about what this buys: `struct
// ethhdr`/`iphdr`/`udphdr` below are wire-format structs whose layout is
// fixed by the Ethernet/IP/UDP protocols themselves, not by the kernel's own
// build config -- unlike e.g. `task_struct` or `sk_buff`, their field
// offsets can't drift between kernel builds, so CO-RE's actual relocation
// mechanism (BPF_CORE_READ(), __builtin_preserve_access_index) has nothing
// to protect here, and this file doesn't use it: every `->` field access
// below is a plain read, same as before. The real, concrete win is dropping
// the UAPI-header build dependency (and, with vmlinux_min.h, the host-BTF
// dependency too) -- this no longer needs linux-libc-dev/linux-headers-
// $(uname -r) installed, the <asm/types.h> multiarch include-path
// workaround bpf/Makefile used to carry, or a real Linux kernel's
// /sys/kernel/btf/vmlinux available at build time (see git history).
// Struct-layout portability isn't the story here; a simpler, self-contained
// build is.

#include "headers/vmlinux_min.h"
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

// scan_key deliberately keys scan_rate by (source, dest_port), NOT just
// source the way signal_rate does. Tried source-only first; a real test
// against the live WSL2 core caught why that's wrong here specifically:
// signal_rate can get away with coarse per-source-only aggregation because
// is_signaling_port() traffic is *never* threshold-gated before emitting —
// every signaling packet gets its own accurate event regardless of the
// shared rate counter, so the counter being coarse never corrupts an
// individual event. scan_rate's emission, below, IS threshold-gated (has to
// be — off-signaling volume isn't bounded the way signaling volume is), so
// which packet happens to be "the one that crosses the line" is now
// semantically significant: a source-only key let unrelated background
// loopback UDP (e.g. a local resolver reply on an ephemeral port) share one
// counter with genuine probe traffic on a different port, and the emitted
// event got attributed to whichever happened to cross the threshold —
// observed directly via `bpftool map dump` during testing, not inferred.
// Per-(source, port) keying makes each emitted event self-consistent: the
// dest_port it reports is always the same port whose own rate crossed
// SCAN_EMIT_THRESHOLD. LRU eviction still bounds total map memory
// regardless of key cardinality (MAX_RATE_ENTRIES), so this doesn't
// reintroduce the "unbounded eBPF map" problem CLAUDE.md rules out.
struct scan_key {
	__u32 saddr;
	__u16 dest_port; // host byte order, matches emit_signaling_event's arg.
	__u16 _pad;
} __attribute__((packed, aligned(4)));

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_RATE_ENTRIES);
	__type(key, struct scan_key);
	__type(value, struct signal_rate_entry);
} scan_rate SEC(".maps");

// tunnel_key deliberately keys tunnel_rate by (source, TEID), and that
// combination is the whole point of this map. On a real N3 interface every
// subscriber's user-plane traffic arrives from the SAME source IP -- the
// peer gNB/UPF -- so signal_rate's per-source counter aggregates every UE
// together and cannot separate one tunnel flooding from ordinary combined
// load. That is the measured gap ROADMAP.md Phase 2.5 records: the only
// anomaly type that is simultaneously real GTP-U and observable here scored
// BELOW the normal baseline (0.0679 vs 0.0811) and was missed at every
// production threshold.
//
// saddr stays in the key alongside the TEID because a TEID is only unique
// per GTP-U peer, never globally -- two gNBs can legitimately allocate the
// same value, and merging them would attribute one peer's traffic to
// another.
struct tunnel_key {
	__u32 saddr; // Network byte order, same treatment as blocklist's key.
	__u32 teid;  // HOST byte order (bpf_ntohl'd once in parse_gtpu) --
	              // see tunnelRateKey in pkg/ebpf/loader_linux.go.
} __attribute__((packed, aligned(4))); // 8 bytes, confirmed via
                                        // `bpftool map show` (key 8B).


struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_TUNNEL_ENTRIES);
	__type(key, struct tunnel_key);
	__type(value, struct signal_rate_entry); // Reused as-is; no tunnel-specific fields.
} tunnel_rate SEC(".maps");

// port_scan_entry tracks a bounded, deduplicated set of the distinct
// destination ports a source has touched within MULTIPORT_SCAN_WINDOW_NS.
// Deliberately separate from scan_key/scan_rate above (which is keyed by
// (source, port) and only catches a flood against ONE port, see that
// struct's comment) — this is a distinct-port-COUNT structure per source,
// catching the shape scan_rate documents it cannot: low-and-slow probing
// across many ports. `ports` is sized exactly MULTIPORT_SCAN_THRESHOLD: once
// full, the threshold has already been crossed by definition, so there's
// never a need for more slots or an eviction policy inside the array itself
// (LRU at the map level already bounds total *sources* tracked, same as
// signal_rate/scan_rate).
struct port_scan_entry {
	__u64 window_start_ns;
	__u16 distinct_count;
	__u8 emitted; // 1 once this window's event has fired; skip rescanning.
	__u8 _pad;
	__u16 ports[MULTIPORT_SCAN_THRESHOLD];
} __attribute__((packed, aligned(8)));

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_RATE_ENTRIES);
	__type(key, __u32); // saddr, same key shape as signal_rate.
	__type(value, struct port_scan_entry);
} port_scan SEC(".maps");

// in6_key is the raw 16-byte IPv6 address key shared by the *_v6 maps
// below — always network byte order, straight from struct ipv6hdr.saddr,
// the same "opaque bytes, not an integer" treatment blocklist's comment
// (and pkg/ebpf/blocklist.go's ipv4Key) documents for IPv4.
struct in6_key {
	__u8 addr[16];
} __attribute__((packed, aligned(8)));

// blocklist_v6 is blocklist's IPv6 counterpart. A separate map (not a
// unified 128-bit-capable key scheme on the existing blocklist) so the
// existing IPv4 map/keys/callers are untouched by IPv6 support existing —
// see this file's top comment.
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, MAX_BLOCKLIST_ENTRIES);
	__type(key, struct in6_key);
	__type(value, __u8);
} blocklist_v6 SEC(".maps");

// signal_rate_v6 is signal_rate's IPv6 counterpart; reuses
// signal_rate_entry as-is (no IPv4-specific fields in that struct).
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_RATE_ENTRIES);
	__type(key, struct in6_key);
	__type(value, struct signal_rate_entry);
} signal_rate_v6 SEC(".maps");

// scan_key_v6 mirrors scan_key at 128 bits — same (source, port) keying
// rationale, see scan_key's comment above.
struct scan_key_v6 {
	__u8 saddr[16];
	__u16 dest_port; // host byte order, matches emit_signaling_event_v6's arg.
	__u16 _pad;
} __attribute__((packed, aligned(8)));

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_RATE_ENTRIES);
	__type(key, struct scan_key_v6);
	__type(value, struct signal_rate_entry);
} scan_rate_v6 SEC(".maps");

// tunnel_key_v6 mirrors tunnel_key at 128 bits. _pad is declared explicitly
// rather than left to aligned(8)'s implicit round-up, so the struct's real
// size is readable from this declaration alone -- scan_key_v6 above relies
// on the implicit rounding, and its Go mirror needed two separate padding
// fields to match (see scanRateKeyV6 in pkg/ebpf/loader_linux.go).
struct tunnel_key_v6 {
	__u8 saddr[16];
	__u32 teid; // Host byte order, same as tunnel_key.
	__u32 _pad;
} __attribute__((packed, aligned(8))); // 24 bytes, confirmed via
                                        // `bpftool map show` (key 24B).


struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_TUNNEL_ENTRIES);
	__type(key, struct tunnel_key_v6);
	__type(value, struct signal_rate_entry);
} tunnel_rate_v6 SEC(".maps");

// port_scan_v6 is port_scan's IPv6 counterpart; reuses port_scan_entry
// as-is, keyed by in6_key instead of a raw __u32 saddr.
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, MAX_RATE_ENTRIES);
	__type(key, struct in6_key);
	__type(value, struct port_scan_entry);
} port_scan_v6 SEC(".maps");

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
	__u16 vlan_id;        // 0 == untagged. Was two bytes of alignment
	                       // padding before VLAN support existed.
	__u32 teid;           // GTP-U Tunnel Endpoint Identifier, host byte
	                       // order. 0 means no valid GTP-U T-PDU header was
	                       // parsed: a non-GTP-U protocol, a path-management
	                       // message (which legitimately uses TEID 0), or a
	                       // framing failure -- which also sets malformed.
	__u32 tunnel_rate;    // Packets counted for (saddr, teid) in the current
	                       // SIGNALING_RATE_WINDOW_NS, INCLUDING this one.
	                       // Carried in-band rather than left for userspace
	                       // to look up afterwards, because the 1-second
	                       // window can roll between the packet and the
	                       // lookup -- a userspace read can legitimately
	                       // return 1 for the packet the kernel counted as
	                       // the three-thousandth. 0 whenever teid is 0.
} __attribute__((packed, aligned(8))); // 32 bytes; a multiple of 8, so
                                        // aligned(8) adds no trailing
                                        // padding. Mirrored byte-for-byte by
                                        // rawSignalingEvent in
                                        // pkg/ebpf/loader_linux.go.

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, SIGNALING_EVENTS_RINGBUF_BYTES);
} signaling_events SEC(".maps");

static __always_inline void emit_signaling_event(__u32 saddr, __u32 daddr, __u16 dest_port,
						  __u16 payload_size, __u8 protocol, __u8 malformed,
						  __u16 vlan_id, __u32 teid, __u32 tunnel_rate)
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
	evt->vlan_id = vlan_id;
	evt->teid = teid;
	evt->tunnel_rate = tunnel_rate;
	bpf_ringbuf_submit(evt, 0);
}

// signaling_event_v6 is signaling_event's IPv6 counterpart — a separate
// wire struct (not a variant of signaling_event), 48 bytes fixed by 2×
// 16-byte addresses replacing the 2× 4-byte ones. Kept on its own ringbuf
// below rather than sharing signaling_events behind a discriminator tag,
// so every existing IPv4 decode path in pkg/ebpf stays byte-for-byte
// unchanged — see pkg/ebpf/loader_linux.go's rawSignalingEventV6 for the Go
// mirror.
struct signaling_event_v6 {
	__u64 timestamp_ns;
	__u8 saddr[16];
	__u8 daddr[16];
	__u16 dest_port;
	__u16 payload_size;
	__u8 protocol;
	__u8 malformed;
	__u16 vlan_id;
	__u32 teid;        // See struct signaling_event's teid/tunnel_rate
	__u32 tunnel_rate; // comments; identical semantics at 128 bits.
} __attribute__((packed, aligned(8))); // 56 bytes, no trailing padding.

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, SIGNALING_EVENTS_RINGBUF_BYTES);
} signaling_events_v6 SEC(".maps");

static __always_inline void emit_signaling_event_v6(const __u8 *saddr, const __u8 *daddr,
						      __u16 dest_port, __u16 payload_size,
						      __u8 protocol, __u8 malformed, __u16 vlan_id,
						      __u32 teid, __u32 tunnel_rate)
{
	struct signaling_event_v6 *evt = bpf_ringbuf_reserve(&signaling_events_v6, sizeof(*evt), 0);
	if (!evt)
		return; // Ring buffer full: drop the observation, never the packet.

	evt->timestamp_ns = bpf_ktime_get_ns();
	__builtin_memcpy(evt->saddr, saddr, 16);
	__builtin_memcpy(evt->daddr, daddr, 16);
	evt->dest_port = dest_port;
	evt->payload_size = payload_size;
	evt->protocol = protocol;
	evt->malformed = malformed;
	evt->vlan_id = vlan_id;
	evt->teid = teid;
	evt->tunnel_rate = tunnel_rate;
	bpf_ringbuf_submit(evt, 0);
}

static __always_inline int is_signaling_port(__u16 dest_port_host)
{
	return dest_port_host == GTPU_PORT || dest_port_host == SIP_PORT;
}

// parse_gtpu extracts the TEID from a GTP-U packet and validates its framing.
// Returns GTPU_PARSE_TPDU for a well-formed user-plane packet,
// GTPU_PARSE_OTHER for valid GTP-U that carries no user plane (path
// management: Echo Request/Response, Error Indication, End Marker -- real
// protocol traffic with a legitimate TEID of 0, and NOT a framing error),
// and GTPU_PARSE_MALFORMED when the framing fails.
//
// Until this existed, nothing in the project ever parsed a GTP-U header:
// traffic was classified as GTP-U purely because it was UDP to port 2152.
// That is why docs/paper-data/real-dataset/real_storm_udpflood.pcap -- 25,944
// packets of plain zero-filled UDP aimed at the real N3 port, with no GTP
// header at all -- was labelled "GTP-U" throughout, and why the kernel's
// malformed flag never fired for it.
//
// *teid_out is written as soon as the mandatory header validates, BEFORE the
// extension-header walk, and is left set even when that walk then fails.
// That is deliberate and the caller relies on it: the TEID sits at a fixed
// offset (bytes 4..7) and needs no chain walk, so a packet whose chain is
// truncated or too deep still has a correct tunnel identity, and is still
// rate-tracked under it -- merely also flagged malformed. Dropping tunnel
// attribution for malformed packets would hand an attacker a trivial
// bypass: append a bad extension header to every flood packet and the
// per-tunnel counter never moves. (A first version of this function got
// exactly that wrong, and its own comment claimed otherwise; review caught
// it, not a test -- hence the test that now pins it in scripts/pcap_gtpu's
// mirror.)
static __always_inline int parse_gtpu(void *gtp, void *data_end, __u32 *teid_out)
{
	struct gtpuhdr *gh = gtp;
	if ((void *)(gh + 1) > data_end)
		return GTPU_PARSE_MALFORMED; // Fewer than 8 payload bytes: not a GTP-U header.

	// Version must be 1 and PT must be 1 (GTP, not the GTP' charging
	// protocol). This check is what keeps a plain non-GTP datagram aimed at
	// port 2152 from being read as a tunnel with a garbage TEID.
	if ((gh->flags & GTPU_VERSION_PT_MASK) != GTPU_VERSION_1_PT_GTP)
		return GTPU_PARSE_MALFORMED;

	switch (gh->msg_type) {
	case GTPU_MSG_TPDU:
		break;
	case GTPU_MSG_ECHO_REQUEST:
	case GTPU_MSG_ECHO_RESPONSE:
	case GTPU_MSG_ERROR_INDICATION:
	case GTPU_MSG_SUPPORTED_EXT_HDR_NOTIFICATION:
	case GTPU_MSG_END_MARKER:
		return GTPU_PARSE_OTHER; // Valid GTP-U, no user plane to rate.
	default:
		return GTPU_PARSE_MALFORMED; // Not a message type GTP-U defines.
	}

	*teid_out = bpf_ntohl(gh->teid);

	if (!(gh->flags & GTPU_EXT_FLAGS_MASK))
		return GTPU_PARSE_TPDU; // No optional block: 8-byte header, nothing left to walk.

	// Any of E/S/PN set means the sequence(2) + N-PDU(1) + next-extension-
	// type(1) block is ALL present, including the fields whose own flag is
	// clear (3GPP TS 29.281 §5.2.1). Every real capture in this repository
	// has flags 0x34 -- E set, S and PN clear -- and therefore carries this
	// block, so a parser that skipped it would mis-frame 100% of the real
	// GTP-U here.
	if ((void *)((__u8 *)gtp + 12) > data_end)
		return GTPU_PARSE_MALFORMED;
	__u8 next = *((__u8 *)gtp + 11);
	__u32 off = 12;

	#pragma unroll
	for (int i = 0; i < GTPU_MAX_EXT_HEADERS; i++) {
		if (next == 0)
			break; // End of the chain.

		// Bounds `off` to a compile-time-known range. Load-bearing for the
		// verifier, not a sanity check: without it the offset below is an
		// unbounded runtime value and the program is rejected outright.
		if (off > GTPU_MAX_HDR_BYTES - 4)
			return GTPU_PARSE_MALFORMED;

		__u8 *ext = (__u8 *)gtp + off;
		if ((void *)(ext + 1) > data_end)
			return GTPU_PARSE_MALFORMED;

		__u32 elen = (__u32)ext[0] * 4; // Length is in 4-octet units.
		if (elen == 0)
			return GTPU_PARSE_MALFORMED; // A zero-length extension would never terminate.
		if (off + elen > GTPU_MAX_HDR_BYTES)
			return GTPU_PARSE_MALFORMED;

		__u8 *tail = (__u8 *)gtp + off + elen - 1;
		if ((void *)(tail + 1) > data_end)
			return GTPU_PARSE_MALFORMED;

		next = *tail; // An extension header's last octet is the next type.
		off += elen;
	}

	if (next != 0)
		return GTPU_PARSE_MALFORMED; // Chain deeper than GTPU_MAX_EXT_HEADERS: unparseable.

	return GTPU_PARSE_TPDU;
}

// budget is the one CLAUDE.md constraint that is measured per packet.
static __always_inline void track_signal_rate(__u32 saddr, __u64 now)
{
	struct signal_rate_entry *entry = bpf_map_lookup_elem(&signal_rate, &saddr);

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

// Returns the window's packet count *before* this packet was counted, so the
// caller can detect the exact packet that crosses SCAN_EMIT_THRESHOLD and
// emit once per window rather than once per packet — off-signaling-port UDP
// has no assumed bound on volume the way signaling traffic does, so
// unconditional per-packet emission here risks both the <0.2ms/packet
// latency budget and overrunning signaling_events with non-signaling noise.
static __always_inline __u32 track_scan_rate(__u32 saddr, __u16 dest_port, __u64 now)
{
	struct scan_key key = {.saddr = saddr, .dest_port = dest_port};
	struct signal_rate_entry *entry = bpf_map_lookup_elem(&scan_rate, &key);

	if (!entry) {
		struct signal_rate_entry fresh = {.window_start_ns = now, .count = 1};
		bpf_map_update_elem(&scan_rate, &key, &fresh, BPF_ANY);
		return 0;
	}

	__u32 prior_count = entry->count;
	if (now - entry->window_start_ns > SIGNALING_RATE_WINDOW_NS) {
		prior_count = 0;
		entry->window_start_ns = now;
		entry->count = 1;
	} else {
		entry->count += 1;
	}
	return prior_count;
}

// track_tunnel_rate returns the window count INCLUDING this packet, unlike
// track_scan_rate which returns the PRIOR count because its caller needs an
// edge trigger. The difference is deliberate: this value is written straight
// into the emitted event, so it has to describe the packet being reported.
static __always_inline __u32 track_tunnel_rate(__u32 saddr, __u32 teid, __u64 now)
{
	struct tunnel_key key = {.saddr = saddr, .teid = teid};
	struct signal_rate_entry *entry = bpf_map_lookup_elem(&tunnel_rate, &key);

	if (!entry) {
		struct signal_rate_entry fresh = {.window_start_ns = now, .count = 1};
		bpf_map_update_elem(&tunnel_rate, &key, &fresh, BPF_ANY);
		return 1;
	}

	if (now - entry->window_start_ns > SIGNALING_RATE_WINDOW_NS) {
		entry->window_start_ns = now;
		entry->count = 1;
		return 1;
	}

	entry->count += 1;
	return entry->count;
}

// track_port_scan returns 1 exactly once — the packet whose port is the
// MULTIPORT_SCAN_THRESHOLD-th *new distinct* port this source has touched
// within the current window. Complements (does not replace)
// track_scan_rate(): the two catch different attack shapes (see
// port_scan_entry's comment above). Takes now (rather than calling
// bpf_ktime_get_ns() itself) so the caller can share one timestamp between
// this and track_scan_rate() for the same packet instead of paying for two
// BPF helper calls on the highest-volume traffic class the <0.2ms/packet
// budget most needs to watch.
static __always_inline __u32 track_port_scan(__u32 saddr, __u16 dest_port, __u64 now)
{
	struct port_scan_entry *entry = bpf_map_lookup_elem(&port_scan, &saddr);

	if (!entry || now - entry->window_start_ns > MULTIPORT_SCAN_WINDOW_NS) {
		struct port_scan_entry fresh = {.window_start_ns = now, .distinct_count = 1};
		fresh.ports[0] = dest_port;
		bpf_map_update_elem(&port_scan, &saddr, &fresh, BPF_ANY);
		return 0;
	}

	if (entry->emitted)
		return 0; // Already reported this window; avoid rescanning every packet.

	#pragma unroll
	for (__u32 i = 0; i < MULTIPORT_SCAN_THRESHOLD; i++) {
		if (i >= entry->distinct_count)
			break;
		if (entry->ports[i] == dest_port)
			return 0; // Already-seen port: not a new distinct touch.
	}

	if (entry->distinct_count >= MULTIPORT_SCAN_THRESHOLD)
		return 0; // Defensive; shouldn't happen once emitted is set.

	entry->ports[entry->distinct_count] = dest_port;
	entry->distinct_count += 1;

	if (entry->distinct_count == MULTIPORT_SCAN_THRESHOLD) {
		entry->emitted = 1;
		return 1; // Edge trigger.
	}
	return 0;
}

// track_signal_rate_v6/track_scan_rate_v6/track_port_scan_v6 mirror
// track_signal_rate/track_scan_rate/track_port_scan above at 128 bits.
// Kept as near-duplicates rather than a shared generic-key helper —
// obscuring the verifier-relevant pointer arithmetic for marginal LOC
// savings isn't a good trade in a program the verifier has to prove safe.
static __always_inline void track_signal_rate_v6(const struct in6_key *saddr6, __u64 now)
{
	struct signal_rate_entry *entry = bpf_map_lookup_elem(&signal_rate_v6, saddr6);

	if (!entry) {
		struct signal_rate_entry fresh = {.window_start_ns = now, .count = 1};
		bpf_map_update_elem(&signal_rate_v6, saddr6, &fresh, BPF_ANY);
		return;
	}

	if (now - entry->window_start_ns > SIGNALING_RATE_WINDOW_NS) {
		entry->window_start_ns = now;
		entry->count = 1;
	} else {
		entry->count += 1;
	}
}

static __always_inline __u32 track_scan_rate_v6(const struct in6_key *saddr6, __u16 dest_port, __u64 now)
{
	struct scan_key_v6 key = {.dest_port = dest_port};
	__builtin_memcpy(key.saddr, saddr6->addr, 16);

	struct signal_rate_entry *entry = bpf_map_lookup_elem(&scan_rate_v6, &key);

	if (!entry) {
		struct signal_rate_entry fresh = {.window_start_ns = now, .count = 1};
		bpf_map_update_elem(&scan_rate_v6, &key, &fresh, BPF_ANY);
		return 0;
	}

	__u32 prior_count = entry->count;
	if (now - entry->window_start_ns > SIGNALING_RATE_WINDOW_NS) {
		prior_count = 0;
		entry->window_start_ns = now;
		entry->count = 1;
	} else {
		entry->count += 1;
	}
	return prior_count;
}

static __always_inline __u32 track_tunnel_rate_v6(const struct in6_key *saddr6, __u32 teid, __u64 now)
{
	struct tunnel_key_v6 key = {.teid = teid};
	__builtin_memcpy(key.saddr, saddr6->addr, 16);

	struct signal_rate_entry *entry = bpf_map_lookup_elem(&tunnel_rate_v6, &key);

	if (!entry) {
		struct signal_rate_entry fresh = {.window_start_ns = now, .count = 1};
		bpf_map_update_elem(&tunnel_rate_v6, &key, &fresh, BPF_ANY);
		return 1;
	}

	if (now - entry->window_start_ns > SIGNALING_RATE_WINDOW_NS) {
		entry->window_start_ns = now;
		entry->count = 1;
		return 1;
	}

	entry->count += 1;
	return entry->count;
}

static __always_inline __u32 track_port_scan_v6(const struct in6_key *saddr6, __u16 dest_port, __u64 now)
{
	struct port_scan_entry *entry = bpf_map_lookup_elem(&port_scan_v6, saddr6);

	if (!entry || now - entry->window_start_ns > MULTIPORT_SCAN_WINDOW_NS) {
		struct port_scan_entry fresh = {.window_start_ns = now, .distinct_count = 1};
		fresh.ports[0] = dest_port;
		bpf_map_update_elem(&port_scan_v6, saddr6, &fresh, BPF_ANY);
		return 0;
	}

	if (entry->emitted)
		return 0;

	#pragma unroll
	for (__u32 i = 0; i < MULTIPORT_SCAN_THRESHOLD; i++) {
		if (i >= entry->distinct_count)
			break;
		if (entry->ports[i] == dest_port)
			return 0;
	}

	if (entry->distinct_count >= MULTIPORT_SCAN_THRESHOLD)
		return 0;

	entry->ports[entry->distinct_count] = dest_port;
	entry->distinct_count += 1;

	if (entry->distinct_count == MULTIPORT_SCAN_THRESHOLD) {
		entry->emitted = 1;
		return 1;
	}
	return 0;
}

// Verified on Linux 6.14 with `bpftool prog load` + `bpftool prog show`:
// xlated 9896B, jited 6050B, max stack depth 152 bytes -- comfortably under
// the verifier's hard 512-byte limit (CLAUDE.md). The GTP-U parsing added
// here cost ~2.9KB of xlated instructions and ~60 bytes of stack over the
// previous revision (6936B / 4200B), almost all of it the #pragma unroll'ed
// extension-header walk. Per-packet cost via `bpftool prog run`: ~190-240ns
// for a real-shaped GTP-U packet with the ring buffer live, ~15ns of which
// is the parser + tunnel_rate update (docs/paper-data/
// 01-performance-benchmarks.md). Re-measure and update these numbers if
// GTPU_MAX_EXT_HEADERS ever changes.
SEC("xdp")
int xdp_packet_filter(struct xdp_md *ctx)
{
	void *data_end = (void *)(long)ctx->data_end;
	void *data = (void *)(long)ctx->data;

	struct ethhdr *eth = data;
	if ((void *)(eth + 1) > data_end)
		return XDP_PASS;

	__u16 h_proto = eth->h_proto;
	void *l3 = (void *)(eth + 1);
	__u16 vlan_id = 0; // 0 == untagged; VLAN ID 0 is reserved (priority-tag
			    // only) in 802.1Q, so this doubles safely as the
			    // "no tag" sentinel.

	// Single 802.1Q tag only — see ETH_P_8021Q's comment in
	// headers/common.h for why QinQ isn't unwrapped here.
	if (h_proto == bpf_htons(ETH_P_8021Q)) {
		struct vlan_hdr *vlan = l3;
		if ((void *)(vlan + 1) > data_end)
			return XDP_PASS;
		vlan_id = bpf_ntohs(vlan->h_vlan_TCI) & VLAN_VID_MASK;
		h_proto = vlan->h_vlan_encapsulated_proto;
		l3 = (void *)(vlan + 1);
	}

	if (h_proto == bpf_htons(ETH_P_IPV6)) {
		struct ipv6hdr *ip6h = l3;
		if ((void *)(ip6h + 1) > data_end)
			return XDP_PASS;

		struct in6_key saddr6 = {};
		__builtin_memcpy(saddr6.addr, ip6h->saddr, 16);

		__u8 *blocked6 = bpf_map_lookup_elem(&blocklist_v6, &saddr6);
		if (blocked6 && *blocked6)
			return XDP_DROP;

		// nexthdr must be UDP directly -- IPv6 extension headers between
		// the fixed header and UDP are not walked in this first pass, see
		// this file's top comment.
		if (ip6h->nexthdr != IPPROTO_UDP)
			return XDP_PASS;

		struct udphdr *udph6 = (void *)(ip6h + 1);
		if ((void *)(udph6 + 1) > data_end) {
			emit_signaling_event_v6(saddr6.addr, ip6h->daddr, 0, 0,
						 SIGNAL_PROTO_UNKNOWN, 1, vlan_id, 0, 0);
			return XDP_PASS;
		}

		__u16 dest_port6 = bpf_ntohs(udph6->dest);
		__u16 payload_size6 = (__u16)((void *)data_end - (void *)(udph6 + 1));

		if (is_signaling_port(dest_port6)) {
			__u64 now6 = bpf_ktime_get_ns(); // Shared by both trackers below.
			track_signal_rate_v6(&saddr6, now6);

			__u8 protocol6 = (dest_port6 == GTPU_PORT) ? SIGNAL_PROTO_GTPU : SIGNAL_PROTO_SIP;
			__u32 teid6 = 0, tunnel_rate6 = 0;
			__u8 malformed6 = 0;

			if (dest_port6 == GTPU_PORT) {
				int rc6 = parse_gtpu((void *)(udph6 + 1), data_end, &teid6);
				malformed6 = (rc6 == GTPU_PARSE_MALFORMED);
				// Tracked whenever a TEID was read, malformed or not -- see
				// parse_gtpu's comment on why the malformed case matters.
				if (teid6 != 0)
					tunnel_rate6 = track_tunnel_rate_v6(&saddr6, teid6, now6);
			}

			emit_signaling_event_v6(saddr6.addr, ip6h->daddr, dest_port6, payload_size6,
						 protocol6, malformed6, vlan_id, teid6, tunnel_rate6);
		} else {
			// Shared between both trackers below: they're always called
			// together for this packet, so one bpf_ktime_get_ns() call
			// serves both instead of each fetching it independently.
			__u64 now6 = bpf_ktime_get_ns();

			__u32 prior6 = track_scan_rate_v6(&saddr6, dest_port6, now6);
			if (prior6 + 1 == SCAN_EMIT_THRESHOLD)
				emit_signaling_event_v6(saddr6.addr, ip6h->daddr, dest_port6, payload_size6,
							 SIGNAL_PROTO_UNKNOWN, 0, vlan_id, 0, 0);

			if (track_port_scan_v6(&saddr6, dest_port6, now6))
				emit_signaling_event_v6(saddr6.addr, ip6h->daddr, dest_port6,
							 MULTIPORT_SCAN_THRESHOLD, SIGNAL_PROTO_PORT_SCAN,
							 0, vlan_id, 0, 0);
		}

		return XDP_PASS;
	}

	if (h_proto != bpf_htons(ETH_P_IP))
		return XDP_PASS;

	struct iphdr *iph = l3;
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

	// A non-initial IP fragment (nonzero fragment offset) has no UDP header
	// at this computed offset at all -- these are arbitrary fragment payload
	// bytes, not dest_port/payload_size. Reading them as if they were a real
	// UDP header would feed garbage into track_signal_rate/track_scan_rate/
	// emit_signaling_event. Deliberately NOT also bailing on the MF (more
	// fragments) flag alone: a *first* fragment (MF=1, offset=0) still has a
	// real, intact UDP header right here -- skipping it too would blind
	// signal/scan detection to fragmentation-based evasion, a well-known
	// IDS-bypass technique, which is a worse gap than the one being fixed.
	// iph->frag_off is network byte order; IP_OFFMASK (0x1FFF) is the
	// low-13-bit fragment-offset field per RFC 791.
	if (bpf_ntohs(iph->frag_off) & 0x1FFF)
		return XDP_PASS;

	struct udphdr *udph = (void *)iph + (iph->ihl * 4);
	if ((void *)(udph + 1) > data_end) {
		// Too short to have a complete UDP header on an otherwise
		// plausible signaling flow — a strong anomaly signal on its own,
		// worth reporting even though we can't safely read dest_port.
		emit_signaling_event(saddr, iph->daddr, 0, 0, SIGNAL_PROTO_UNKNOWN, 1, vlan_id, 0, 0);
		return XDP_PASS;
	}

	__u16 dest_port = bpf_ntohs(udph->dest);
	__u16 payload_size = (__u16)((void *)data_end - (void *)(udph + 1));

	if (is_signaling_port(dest_port)) {
		__u64 now = bpf_ktime_get_ns(); // Shared by both trackers below.
		track_signal_rate(saddr, now);

		__u8 protocol = (dest_port == GTPU_PORT) ? SIGNAL_PROTO_GTPU : SIGNAL_PROTO_SIP;
		__u32 teid = 0, tunnel_rate = 0;
		__u8 malformed = 0;

		// Per-tunnel rate, the signal signal_rate structurally cannot
		// provide: on a real N3 every UE shares one source IP, so the
		// per-source counter above sees aggregate load and nothing else.
		// See struct tunnel_key's comment.
		if (dest_port == GTPU_PORT) {
			int rc = parse_gtpu((void *)(udph + 1), data_end, &teid);
			malformed = (rc == GTPU_PARSE_MALFORMED);
			// Tracked whenever a TEID was read, malformed or not -- see
			// parse_gtpu's comment on why the malformed case matters.
			if (teid != 0)
				tunnel_rate = track_tunnel_rate(saddr, teid, now);
		}

		emit_signaling_event(saddr, iph->daddr, dest_port, payload_size, protocol,
				      malformed, vlan_id, teid, tunnel_rate);
	} else {
		// Off-signaling-port UDP: previously invisible here no matter its
		// volume (is_signaling_port() gated all observation, not just
		// signal_rate tracking) — a real off-protocol probe/scan had no way
		// to reach Layer 2/3 (see ROADMAP.md Phase 1). Escalate to an
		// observation once a sustained burst from this source crosses
		// SCAN_EMIT_THRESHOLD within the window, not on every packet.
		//
		// now is shared between both trackers below (always called together
		// for this packet) rather than each fetching bpf_ktime_get_ns()
		// independently.
		__u64 now = bpf_ktime_get_ns();

		__u32 prior_count = track_scan_rate(saddr, dest_port, now);
		if (prior_count + 1 == SCAN_EMIT_THRESHOLD)
			emit_signaling_event(saddr, iph->daddr, dest_port, payload_size,
					      SIGNAL_PROTO_UNKNOWN, 0, vlan_id, 0, 0);

		// Distinct-port-count detector, separate from the flood check above
		// — see track_port_scan()'s comment for what it catches that
		// track_scan_rate() can't. payload_size is repurposed here to carry
		// the distinct-port count (MULTIPORT_SCAN_THRESHOLD, by definition
		// of the edge trigger), not a byte size — see
		// pkg/ebpf/blocklist.go's SignalingEvent.PayloadSize doc.
		if (track_port_scan(saddr, dest_port, now))
			emit_signaling_event(saddr, iph->daddr, dest_port, MULTIPORT_SCAN_THRESHOLD,
					      SIGNAL_PROTO_PORT_SCAN, 0, vlan_id, 0, 0);
	}

	return XDP_PASS;
}
