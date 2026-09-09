#ifndef SENTINEL5G_BPF_VMLINUX_MIN_H
#define SENTINEL5G_BPF_VMLINUX_MIN_H

/* Hand-maintained stand-in for a `bpftool btf dump`-generated vmlinux.h.
 *
 * packet_filter.c doesn't use CO-RE relocations (BPF_CORE_READ() /
 * __builtin_preserve_access_index()) — every struct below is a wire-format
 * type whose layout is fixed by its protocol/UAPI, not by the kernel it was
 * built against (see packet_filter.c's top comment). The only reason a full,
 * host-BTF-derived vmlinux.h was ever needed was to get these declarations
 * conveniently — but that ties every build (including `docker build`, which
 * has no access to a host's /sys/kernel/btf/vmlinux) to a real Linux machine.
 * This header vendors just the handful of types packet_filter.c actually
 * references, so the whole bpf/ directory builds anywhere clang+llvm run,
 * container included. Same reasoning headers/common.h already applies to
 * ETH_P_IP (BTF doesn't carry #defines either way, so that one was already
 * hand-written) — this generalizes it to the few structs/enum values BTF
 * would otherwise have supplied.
 *
 * Field layouts below were verified byte-for-byte against a real
 * `bpftool btf dump file /sys/kernel/btf/vmlinux format c` output. Update by
 * hand only if packet_filter.c starts referencing a new type — that shows up
 * immediately as a clang error ("unknown type name"), not a silent gap.
 *
 * Bitfield order in `struct iphdr` (ihl before version) is the little-endian
 * layout used by every real Sentinel5G target (x86_64, arm64) — the kernel's
 * own <linux/ip.h> flips this under __BIG_ENDIAN_BITFIELD, which no
 * supported target here uses.
 */

typedef unsigned char __u8;
typedef short unsigned int __u16;
typedef unsigned int __u32;
typedef long long unsigned int __u64;
typedef int __s32;
typedef long long int __s64;
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u16 __sum16;
typedef __u32 __wsum;

/* libbpf's <bpf/bpf_helper_defs.h> declares every kernel BPF helper's
 * signature, not just the ones packet_filter.c calls -- so this header still
 * needs to satisfy types referenced only in unused helpers' signatures
 * (__s32/__s64 above included), even though nothing here ever calls them.
 * BPF_ANY is the flags argument bpf_map_update_elem() actually passes. */
enum {
	BPF_ANY = 0,
	BPF_NOEXIST = 1,
	BPF_EXIST = 2,
	BPF_F_LOCK = 4,
};

struct ethhdr {
	unsigned char h_dest[6];
	unsigned char h_source[6];
	__be16 h_proto;
};

struct iphdr {
	__u8 ihl : 4;
	__u8 version : 4;
	__u8 tos;
	__be16 tot_len;
	__be16 id;
	__be16 frag_off;
	__u8 ttl;
	__u8 protocol;
	__sum16 check;
	__be32 saddr;
	__be32 daddr;
};

struct udphdr {
	__be16 source;
	__be16 dest;
	__be16 len;
	__sum16 check;
};

/* Only the 4 bytes after the shared 12-byte dest/src MAC fields that
 * struct ethhdr already accounts for -- its h_proto field doubles as the
 * outer TPID (0x8100) when a tag is present, so this covers exactly what's
 * left: the tag control info and the inner encapsulated EtherType. Fixed
 * 802.1Q wire layout (IEEE 802.1Q), not kernel-build-dependent, same
 * reasoning as the structs above. */
struct vlan_hdr {
	__be16 h_vlan_TCI;
	__be16 h_vlan_encapsulated_proto;
};

/* Fixed IPv6 header (RFC 8200), 40 bytes, no options in the base header
 * (extension headers are separate, chained via nexthdr) -- layout is
 * protocol-fixed, not kernel-build-dependent, same reasoning as iphdr
 * above. saddr/daddr are plain 16-byte arrays here rather than the
 * kernel's `struct in6_addr` union -- packet_filter.c only ever needs the
 * raw bytes, never the union's other views.
 *
 * Bitfield order (priority before version) is the little-endian layout
 * used by every real Sentinel5G target (x86_64, arm64) -- same caveat
 * iphdr's comment above carries; the kernel's own <linux/ipv6.h> flips
 * this under __BIG_ENDIAN_BITFIELD, which no supported target here uses.
 */
struct ipv6hdr {
	__u8 priority : 4;
	__u8 version : 4;
	__u8 flow_lbl[3];
	__be16 payload_len;
	__u8 nexthdr;
	__u8 hop_limit;
	__u8 saddr[16];
	__u8 daddr[16];
};

struct xdp_md {
	__u32 data;
	__u32 data_end;
	__u32 data_meta;
	__u32 ingress_ifindex;
	__u32 rx_queue_index;
	__u32 egress_ifindex;
};

/* Only the map types packet_filter.c actually declares (see its `SEC(".maps")`
 * blocks) — values are stable UAPI identifiers, not subject to change. */
enum bpf_map_type {
	BPF_MAP_TYPE_HASH = 1,
	BPF_MAP_TYPE_LRU_HASH = 9,
	BPF_MAP_TYPE_RINGBUF = 27,
};

enum xdp_action {
	XDP_ABORTED = 0,
	XDP_DROP = 1,
	XDP_PASS = 2,
	XDP_TX = 3,
	XDP_REDIRECT = 4,
};

/* IPPROTO_UDP is the only member of the kernel's (much larger) IPPROTO enum
 * packet_filter.c references. Its value is fixed by IANA, not by kernel
 * build. */
#define IPPROTO_UDP 17

#endif /* SENTINEL5G_BPF_VMLINUX_MIN_H */
