#!/usr/bin/env python3
"""Builds the synthetic frames the XDP benchmark replays.

Each file is one complete Ethernet frame, which is what
`bpftool prog run ... data_in` expects. They are generated rather than
committed because they are a few hundred bytes of pure structure -- and
because the GTP-U one has to stay in lockstep with what
bpf/packet_filter.c's parse_gtpu() accepts, which a checked-in binary
would silently stop doing.

The GTP-U frame deliberately mirrors what the real captures contain
(docs/paper-data/real-dataset-v2/): flags 0x34 (E set), one PDU Session
Container extension header, TEID 0x4d84 -- so the benchmark exercises the
same parse path production does, extension-header walk included, not a
minimal 8-byte header that would skip it.
"""

from __future__ import annotations

import argparse
import ipaddress
import struct
from pathlib import Path

GTPU_PORT = 2152
SIP_PORT = 5060
DEFAULT_TEID = 0x4D84


def _checksum(data: bytes) -> int:
    if len(data) % 2:
        data += b"\0"
    total = sum(struct.unpack("!%dH" % (len(data) // 2), data))
    total = (total >> 16) + (total & 0xFFFF)
    total += total >> 16
    return (~total) & 0xFFFF


def frame(
    payload: bytes, dport: int, sip: str = "127.0.0.1", dip: str = "127.0.0.7"
) -> bytes:
    """Ethernet + IPv4 + UDP around payload. Source port is always 2152, as
    a real gNB's is."""
    udp = struct.pack("!HHHH", GTPU_PORT, dport, 8 + len(payload), 0) + payload
    ip = struct.pack(
        "!BBHHHBBH4s4s",
        0x45,
        0,
        20 + len(udp),
        0,
        0,
        64,
        17,
        0,
        ipaddress.ip_address(sip).packed,
        ipaddress.ip_address(dip).packed,
    )
    ip = ip[:10] + struct.pack("!H", _checksum(ip)) + ip[12:]
    return b"\x00" * 12 + b"\x08\x00" + ip + udp


def gtpu_tpdu(teid: int = DEFAULT_TEID, inner: int = 84) -> bytes:
    """A T-PDU shaped like the real captures: E set, one 4-byte PDU Session
    Container extension header, then the inner packet."""
    return (
        bytes([0x34, 0xFF])
        + struct.pack("!H", 4 + 4 + inner)  # length: optional block + ext + inner
        + struct.pack("!I", teid)
        + bytes([0x00, 0x00, 0x00, 0x85])  # seq(2) + N-PDU(1) + next ext type
        + bytes([0x01, 0x00, 0x00, 0x00])  # ext: 1*4 bytes, next type 0 (end)
        + b"\xa5" * inner
    )


PACKETS = {
    # The path the <0.2ms budget is really about: full GTP-U parse, tunnel
    # map update, ring buffer submit.
    "gtpu": lambda: frame(gtpu_tpdu(), GTPU_PORT),
    # Same, second tunnel -- used by the blocked/unblocked comparison so one
    # can be blocked while the other stays live.
    "gtpu_other_teid": lambda: frame(gtpu_tpdu(teid=0xC9C7), GTPU_PORT),
    # Signaling, but no GTP-U header to parse: isolates the parser's cost.
    "sip": lambda: frame(b"INVITE sip:x@y SIP/2.0\r\n" + b"\x00" * 60, SIP_PORT),
    # Aimed at the GTP-U port but not GTP-U: the malformed path.
    "junk_on_gtpu_port": lambda: frame(b"\x00" * 100, GTPU_PORT),
    # Off-signaling port: the cheapest path through the program.
    "off_port": lambda: frame(b"\x00" * 100, 4444),
}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--out-dir", type=Path, default=Path("/tmp/sentinel5g-loadtest")
    )
    args = parser.parse_args()
    args.out_dir.mkdir(parents=True, exist_ok=True)
    for name, build in PACKETS.items():
        path = args.out_dir / f"{name}.bin"
        path.write_bytes(build())
        print(f"{path} ({len(build())} bytes)")


if __name__ == "__main__":
    main()
