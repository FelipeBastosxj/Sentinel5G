"""Minimal pcap reader plus a Python mirror of bpf/packet_filter.c's
parse_gtpu(), for deriving per-TEID rates offline from the committed
captures in docs/paper-data/real-dataset/.

Why this exists at all: the `*_raw.txt` dumps that scripts/build_real_dataset.py
originally read are `tcpdump -tt -n` output, which carries timestamps, ports
and lengths but **no payload bytes** — so no TEID. The `.pcap` files beside
them do carry the full packets, and they are already committed, so the tunnel
identity this project now needs was there all along and simply unreadable
through the text dumps.

Deliberately stdlib-only (`struct`), no scapy or dpkt. These are classic pcap
files with an Ethernet (or Linux cooked / raw IP) link layer, which is under
a hundred lines of parsing — adding a capture-library dependency to the AI
engine's dev extras to avoid that would be a worse trade, and it would put a
second, differently-behaved GTP-U parser between us and the kernel's.

The GTP-U validation below mirrors parse_gtpu() in bpf/packet_filter.c
decision for decision (version/PT, T-PDU-only, the optional
sequence/N-PDU block, the bounded extension-header walk). Keep the two in
sync by hand, the same convention sentinel_ai/features.py has with
pkg/events/types.go.
"""

from __future__ import annotations

import struct
from dataclasses import dataclass
from pathlib import Path

# Mirrors bpf/headers/common.h. Keep in sync by hand.
GTPU_PORT = 2152
GTPU_VERSION_PT_MASK = 0xF0
GTPU_VERSION_1_PT_GTP = 0x30
GTPU_EXT_FLAGS_MASK = 0x07
GTPU_MSG_TPDU = 0xFF
GTPU_MAX_EXT_HEADERS = 4
GTPU_MAX_HDR_BYTES = 64

_PCAP_MAGIC_LE = 0xA1B2C3D4
_PCAP_MAGIC_LE_NANO = 0xA1B23C4D

# Link types we can walk to the IP header (pcap "network" field).
_LINKTYPE_ETHERNET = 1
_LINKTYPE_RAW = 101
_LINKTYPE_LINUX_SLL = 113


@dataclass(frozen=True)
class Packet:
    """One UDP packet from a capture, with its GTP-U tunnel identity when the
    payload actually parses as a GTP-U T-PDU.
    """

    timestamp: float
    source_ip: str
    dest_port: int
    #: UDP payload size in bytes (what bpf/packet_filter.c reports).
    payload_size: int
    #: 0 when no valid GTP-U T-PDU header was parsed — the same "no tunnel
    #: identity" sentinel pkg/events.NormalizedEvent documents.
    teid: int
    #: True when this is GTP-U-port traffic whose framing failed to validate,
    #: matching what the kernel now flags.
    malformed: bool


def parse_gtpu(payload: bytes) -> tuple[int, bool]:
    """Mirror of parse_gtpu() in bpf/packet_filter.c.

    Returns (teid, ok). ok is False when the payload is not a well-formed
    GTP-U T-PDU; teid is 0 in that case.
    """
    if len(payload) < 8:
        return 0, False

    flags = payload[0]
    msg_type = payload[1]

    if (flags & GTPU_VERSION_PT_MASK) != GTPU_VERSION_1_PT_GTP:
        return 0, False
    if msg_type != GTPU_MSG_TPDU:
        # Path management (Echo, Error Indication, End Marker): real GTP-U,
        # but no user-plane payload and a legitimate TEID of 0.
        return 0, False

    teid = struct.unpack_from(">I", payload, 4)[0]

    if not (flags & GTPU_EXT_FLAGS_MASK):
        return teid, True

    # Any of E/S/PN set means all four optional bytes are present.
    if len(payload) < 12:
        return 0, False
    next_type = payload[11]
    off = 12

    for _ in range(GTPU_MAX_EXT_HEADERS):
        if next_type == 0:
            break
        if off > GTPU_MAX_HDR_BYTES - 4 or off >= len(payload):
            return 0, False
        ext_len = payload[off] * 4
        if ext_len == 0 or off + ext_len > GTPU_MAX_HDR_BYTES:
            return 0, False
        if off + ext_len > len(payload):
            return 0, False
        next_type = payload[off + ext_len - 1]
        off += ext_len

    if next_type != 0:
        return 0, False

    return teid, True


def _ip_offset(link_type: int, frame: bytes) -> int | None:
    """Returns the offset of the IPv4 header within frame, or None."""
    if link_type == _LINKTYPE_ETHERNET:
        if len(frame) < 14:
            return None
        ethertype = struct.unpack_from(">H", frame, 12)[0]
        if ethertype == 0x8100:  # Single 802.1Q tag, as the kernel unwraps.
            if len(frame) < 18:
                return None
            ethertype = struct.unpack_from(">H", frame, 16)[0]
            return 18 if ethertype == 0x0800 else None
        return 14 if ethertype == 0x0800 else None
    if link_type == _LINKTYPE_RAW:
        return 0
    if link_type == _LINKTYPE_LINUX_SLL:
        if len(frame) < 16:
            return None
        return 16 if struct.unpack_from(">H", frame, 14)[0] == 0x0800 else None
    return None


def read_udp_packets(path: Path) -> list[Packet]:
    """Reads every UDP packet from a classic pcap file."""
    raw = Path(path).read_bytes()
    if len(raw) < 24:
        raise RuntimeError(f"{path}: too short to be a pcap file")

    magic = struct.unpack_from("<I", raw, 0)[0]
    if magic in (_PCAP_MAGIC_LE, _PCAP_MAGIC_LE_NANO):
        endian = "<"
    else:
        magic = struct.unpack_from(">I", raw, 0)[0]
        if magic not in (_PCAP_MAGIC_LE, _PCAP_MAGIC_LE_NANO):
            raise RuntimeError(f"{path}: not a classic pcap file (magic {magic:#x})")
        endian = ">"
    # The nanosecond-resolution variant only changes the ts_usec unit.
    ts_divisor = 1_000_000_000.0 if magic == _PCAP_MAGIC_LE_NANO else 1_000_000.0

    link_type = struct.unpack_from(endian + "I", raw, 20)[0]

    packets: list[Packet] = []
    offset = 24
    while offset + 16 <= len(raw):
        ts_sec, ts_frac, caplen, _origlen = struct.unpack_from(endian + "IIII", raw, offset)
        offset += 16
        frame = raw[offset : offset + caplen]
        offset += caplen
        if len(frame) < caplen:
            break  # Truncated final record.

        ip_off = _ip_offset(link_type, frame)
        if ip_off is None or len(frame) < ip_off + 20:
            continue

        version_ihl = frame[ip_off]
        if version_ihl >> 4 != 4:
            continue
        ihl = (version_ihl & 0x0F) * 4
        if ihl < 20 or frame[ip_off + 9] != 17:  # IPPROTO_UDP
            continue
        # A non-initial fragment has no UDP header here, same bail-out the
        # kernel takes (see the IP_OFFMASK check in packet_filter.c).
        if struct.unpack_from(">H", frame, ip_off + 6)[0] & 0x1FFF:
            continue

        source_ip = ".".join(str(b) for b in frame[ip_off + 12 : ip_off + 16])

        udp_off = ip_off + ihl
        if len(frame) < udp_off + 8:
            # Too short for a complete UDP header: exactly the case the
            # kernel reports as protocol=UNKNOWN, dest_port=0, malformed=1.
            packets.append(
                Packet(
                    timestamp=ts_sec + ts_frac / ts_divisor,
                    source_ip=source_ip,
                    dest_port=0,
                    payload_size=0,
                    teid=0,
                    malformed=True,
                )
            )
            continue

        dest_port = struct.unpack_from(">H", frame, udp_off + 2)[0]
        payload = frame[udp_off + 8 :]

        teid = 0
        malformed = False
        if dest_port == GTPU_PORT:
            teid, ok = parse_gtpu(payload)
            malformed = not ok

        packets.append(
            Packet(
                timestamp=ts_sec + ts_frac / ts_divisor,
                source_ip=source_ip,
                dest_port=dest_port,
                payload_size=len(payload),
                teid=teid,
                malformed=malformed,
            )
        )

    return packets
