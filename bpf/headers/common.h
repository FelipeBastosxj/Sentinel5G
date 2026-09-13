#ifndef SENTINEL5G_BPF_COMMON_H
#define SENTINEL5G_BPF_COMMON_H

/* ETH_P_IP is normally pulled in from <linux/if_ether.h>, but this project
 * builds against vmlinux.h (see bpf/Makefile) instead of UAPI kernel
 * headers -- and BTF (what vmlinux.h is generated from) only captures
 * types/enums/functions, not preprocessor #defines, so this constant isn't
 * there to pull in. It's a fixed IEEE 802.3 EtherType value, not something
 * that could vary by kernel build anyway. */
#define ETH_P_IP 0x0800

/* IPv6 EtherType. */
#define ETH_P_IPV6 0x86DD

/* 802.1Q VLAN tag TPID. A tagged frame's outer EtherType is this value, with
 * the real inner EtherType 4 bytes later (see struct vlan_hdr in
 * vmlinux_min.h). Single-tag only: 802.1ad/QinQ double-tagging (outer TPID
 * 0x88a8) is deliberately out of scope for this parser and falls through to
 * XDP_PASS unrecognized, same as any other unhandled EtherType. */
#define ETH_P_8021Q 0x8100

/* Low 12 bits of a VLAN tag's TCI (Tag Control Info) field are the VLAN ID;
 * the top 4 bits are priority/DEI, not part of the ID. */
#define VLAN_VID_MASK 0x0FFF

/* 3GPP GTP-U (user plane tunneling, e.g. N3/N9 interfaces). */
#define GTPU_PORT 2152

/* GTP-U flags-byte decode, 3GPP TS 29.281 §5.1. */
#define GTPU_VERSION_PT_MASK 0xF0  /* version(3) + PT(1) */
#define GTPU_VERSION_1_PT_GTP 0x30 /* version == 1 && PT == 1 (GTP, not GTP') */
#define GTPU_EXT_FLAGS_MASK 0x07   /* E|S|PN: any set => 4 optional bytes follow */

/* T-PDU is the only GTP-U message type carrying user-plane payload. Echo
 * Request/Response (1/2), Error Indication (26) and End Marker (254) are
 * path management: real GTP-U, but nothing to rate a tunnel by, and they
 * legitimately carry TEID 0. */
#define GTPU_MSG_TPDU 0xFF

/* The other message types 3GPP TS 29.281 §7.1 defines for GTP-U. Anything
 * else on port 2152 with a valid version/PT is not GTP-U at all and is
 * reported malformed, rather than waved through as "some path-management
 * message" -- random bytes hit the version/PT check 1 time in 16, and 3 of
 * the 700 packets in docs/paper-data/real-dataset/real_malformed.pcap did. */
#define GTPU_MSG_ECHO_REQUEST 1
#define GTPU_MSG_ECHO_RESPONSE 2
#define GTPU_MSG_ERROR_INDICATION 26
#define GTPU_MSG_SUPPORTED_EXT_HDR_NOTIFICATION 31
#define GTPU_MSG_END_MARKER 254

/* parse_gtpu() results. Three states, not two, because two real cases are
 * neither "a tunnel packet" nor "broken": path-management messages (Echo,
 * Error Indication, End Marker) are valid GTP-U with no tunnel to rate, and
 * must NOT be reported malformed -- a peer sends Echo Requests
 * periodically, and flagging every one of them would feed the model a
 * malformed=1 on port 2152 that only its anomaly classes ever produced. */
#define GTPU_PARSE_TPDU 1       /* valid T-PDU; *teid_out set */
#define GTPU_PARSE_OTHER 0      /* valid GTP-U, not user plane; teid 0, not malformed */
#define GTPU_PARSE_MALFORMED -1 /* framing failed; *teid_out may still be set (see parse_gtpu) */

/* How deep parse_gtpu() walks the extension-header chain. Every real N3
 * packet in docs/paper-data/real-dataset/ carries exactly ONE extension
 * header (PDU Session Container, type 0x85, 4 bytes) — measured by parsing
 * all 1,889 packets of real_normal.pcap, not assumed. 3GPP allows more (NR
 * RAN Container 0x81, Long PDCP PDU Number 0x82, UDP Port 0x40), so 4 is
 * headroom over a realistic worst case of 2-3. The loop is #pragma
 * unroll'ed into straight-line code with no BPF helper call, so each extra
 * iteration costs a handful of instructions at runtime but doubles the
 * verifier's state exploration at load time — which is why this is 4 and
 * not 16. A packet with a deeper chain is treated as unparseable
 * (malformed=1) rather than accepted with a wrong header length. */
#define GTPU_MAX_EXT_HEADERS 4

/* Hard cap on total GTP-U header bytes (mandatory 8 + optional 4 + chain).
 * Load-bearing for the verifier, not just policy: it bounds the
 * runtime-variable offset the chain walk uses, which is what lets the
 * verifier prove every dereference in parse_gtpu() is in range. 64 is far
 * above the 16 bytes every real capture in this repository uses. */
#define GTPU_MAX_HDR_BYTES 64

/* SIP signaling (VoIP/IMS control plane). */
#define SIP_PORT 5060

#define MAX_BLOCKLIST_ENTRIES 65536
#define MAX_RATE_ENTRIES 65536

/* Bounded capacity of tunnel_rate/tunnel_rate_v6 (see packet_filter.c).
 * Sized separately from MAX_RATE_ENTRIES because per-tunnel cardinality is a
 * different quantity: on a real N3 interface the number of active PDU
 * sessions can far exceed the number of distinct source IPs — that
 * asymmetry is the entire reason the map exists. LRU eviction bounds the
 * memory regardless of key cardinality; this just sets where that bound
 * sits. At 65536 entries of (8-byte key + 16-byte value) this is ~1.6MB for
 * IPv4 and ~2.6MB for the 24-byte-keyed IPv6 twin. */
#define MAX_TUNNEL_ENTRIES 65536

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
#define SIGNAL_PROTO_PORT_SCAN 3

/* Distinct destination ports a source must touch within
 * MULTIPORT_SCAN_WINDOW_NS before it's flagged as a port scan (see
 * port_scan/track_port_scan() in packet_filter.c) — a separate detector
 * from SCAN_EMIT_THRESHOLD/scan_rate above, which catches a flood against
 * ONE port and deliberately can't catch classic low-and-slow scanning
 * (many distinct ports, one or two packets each). Not yet empirically
 * tuned against real off-protocol traffic — same caveat SCAN_EMIT_THRESHOLD
 * carries. Also doubles as port_scan_entry's fixed `ports[]` array capacity
 * (see packet_filter.c), so it's bounded map-value memory, not a runtime
 * limit that could grow unbounded. */
#define MULTIPORT_SCAN_THRESHOLD 15

/* Window for multi-port scan tracking. Deliberately much longer than
 * SIGNALING_RATE_WINDOW_NS's 1 second: "low-and-slow" scanning is exactly
 * the pattern a 1-second window would miss. */
#define MULTIPORT_SCAN_WINDOW_NS 30000000000ULL /* 30 seconds */

#endif /* SENTINEL5G_BPF_COMMON_H */
