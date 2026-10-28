"""Tests for the application-mix false-positive tooling
(scripts/fpr_app_mix.py).

What the §2.11 finding rests on is checked directly: every tunnel is
attributed to the right application from the inner IP header in both
directions, and the rule mirror fires exactly where the Go detectors would --
at the threshold, once per cooldown -- and not one packet earlier.
"""

from __future__ import annotations

import socket
import struct
from pathlib import Path

from scripts.build_real_dataset import tunnel_rate_series
import re

from scripts.fpr_app_mix import (
    SOURCE_FLOOD_DISTINCT_TUNNELS,
    SOURCE_FLOOD_PPS,
    TUNNEL_FLOOD_PPS,
    _direction,
    _rules,
    _tunnel_owners,
)
from scripts.pcap_gtpu import read_udp_packets

GNB, UPF = "127.0.0.1", "127.0.0.7"


def _ipv4(src: str, dst: str, proto: int, payload: bytes) -> bytes:
    return (
        struct.pack(">BBHHHBBH", 0x45, 0, 20 + len(payload), 0, 0, 64, proto, 0)
        + socket.inet_aton(src)
        + socket.inet_aton(dst)
        + payload
    )


def _gtpu_frame(outer_src: str, outer_dst: str, teid: int, inner_src: str, inner_dst: str):
    inner = _ipv4(inner_src, inner_dst, 17, b"\x00" * 8 + b"x" * 32)
    gtp = struct.pack(">BBHI", 0x30, 0xFF, len(inner), teid) + inner
    udp = struct.pack(">HHHH", 2152, 2152, 8 + len(gtp), 0) + gtp
    return b"\x00" * 12 + b"\x08\x00" + _ipv4(outer_src, outer_dst, 17, udp)


def _write_pcap(path: Path, frames: list[tuple[float, bytes]]) -> Path:
    out = struct.pack("<IHHiIII", 0xA1B2C3D4, 2, 4, 0, 0, 65535, 1)
    for ts, frame in frames:
        sec = int(ts)
        out += struct.pack("<IIII", sec, int((ts - sec) * 1e6), len(frame), len(frame)) + frame
    path.write_bytes(out)
    return path


def test_tunnels_are_attributed_by_inner_ip_in_both_directions(tmp_path):
    pcap = _write_pcap(
        tmp_path / "c.pcap",
        [
            (1.0, _gtpu_frame(GNB, UPF, 0x10, "10.45.0.6", "10.45.0.1")),  # web uplink
            (1.1, _gtpu_frame(UPF, GNB, 0x20, "10.45.0.1", "10.45.0.6")),  # web downlink
            (1.2, _gtpu_frame(GNB, UPF, 0x30, "10.45.0.7", "10.45.0.1")),  # video uplink
            (1.3, _gtpu_frame(GNB, UPF, 0x40, "10.45.0.99", "10.45.0.1")),  # unknown UE
        ],
    )
    owners = _tunnel_owners(pcap, {"10.45.0.6": "web", "10.45.0.7": "video"})
    assert owners == {(GNB, 0x10): "web", (UPF, 0x20): "web", (GNB, 0x30): "video"}


def test_direction_is_uplink_only_from_the_gnb(tmp_path):
    pcap = _write_pcap(
        tmp_path / "c.pcap",
        [
            (1.0, _gtpu_frame(GNB, UPF, 1, "10.45.0.6", "10.45.0.1")),
            (1.1, _gtpu_frame(UPF, GNB, 2, "10.45.0.1", "10.45.0.6")),
        ],
    )
    assert [_direction(p) for p in read_udp_packets(pcap)] == ["uplink", "downlink"]


def _burst(n: int, start: float, teid: int = 0x10) -> list[tuple[float, bytes]]:
    frame = _gtpu_frame(GNB, UPF, teid, "10.45.0.6", "10.45.0.1")
    return [(start + i * (0.9 / n), frame) for i in range(n)]


# Small thresholds keep the synthetic pcaps small; the semantics are the
# same at any value.
_PPS, _SRC_PPS, _DISTINCT = 1000, 20000, 256


def _run_rules(tmp_path, frames):
    pcap = _write_pcap(tmp_path / "c.pcap", frames)
    packets = read_udp_packets(pcap)
    owners = _tunnel_owners(pcap, {"10.45.0.6": "web"})
    return _rules(
        packets,
        tunnel_rate_series(packets),
        owners,
        _direction,
        tunnel_pps=_PPS,
        source_pps=_SRC_PPS,
        distinct_tunnels=_DISTINCT,
    )


def test_tunnel_flood_needs_the_full_threshold_inside_one_window(tmp_path):
    below = _run_rules(tmp_path, _burst(_PPS - 1, 10.0))
    assert below["tunnel_flood"]["alerts"] == {}
    assert below["tunnel_flood"]["peak_tunnel_pps"]["web/uplink"] == _PPS - 1

    at = _run_rules(tmp_path, _burst(_PPS, 10.0))
    assert at["tunnel_flood"]["alerts"] == {"web/uplink": 1}


def test_tunnel_flood_fires_once_per_cooldown(tmp_path):
    # Two bursts 5 s apart: inside the 30 s cooldown, one alert. A third
    # burst 40 s after the first is past it, a second alert.
    frames = _burst(1500, 10.0) + _burst(1500, 15.0) + _burst(1500, 50.0)
    result = _run_rules(tmp_path, frames)
    assert result["tunnel_flood"]["alerts"] == {"web/uplink": 2}
    assert result["tunnel_flood"]["tunnels_alerted"] == {"web/uplink": 1}


def test_source_flood_counts_packets_and_distinct_tunnels_per_peer(tmp_path):
    frames = [
        (10.0 + i * 0.001, _gtpu_frame(GNB, UPF, 100 + i, "10.45.0.6", "10.45.0.1"))
        for i in range(300)
    ]
    result = _run_rules(tmp_path, frames)["source_flood"]
    assert result["peak_pps"] == {GNB: 300}
    assert result["peak_distinct_tunnels"] == {GNB: 300}
    assert result["alerts"] == 1  # 300 distinct TEIDs >= 256


def test_payload_size_survives_a_truncating_snaplen(tmp_path):
    # tcpdump -s N keeps the original length in each record. The kernel
    # (XDP sees the whole frame) reports the real UDP payload; the mirror
    # must too, not the snaplen-truncated byte count it happens to hold.
    frame = _gtpu_frame(GNB, UPF, 0x10, "10.45.0.6", "10.45.0.1")
    real_payload = len(frame) - 14 - 20 - 8
    snap = 60
    rec = struct.pack("<IIII", 1, 0, snap, len(frame)) + frame[:snap]
    pcap = tmp_path / "t.pcap"
    pcap.write_bytes(struct.pack("<IHHiIII", 0xA1B2C3D4, 2, 4, 0, 0, snap, 1) + rec)
    (packet,) = read_udp_packets(pcap)
    assert packet.payload_size == real_payload
    assert packet.teid == 0x10


def test_rule_defaults_match_the_operator_config():
    # The mirror is only a measurement of the shipped rules if it uses the
    # shipped numbers; pkg/config/config.go is where they are defined.
    go = (Path(__file__).resolve().parents[3] / "pkg" / "config" / "config.go").read_text()

    def default(env: str) -> int:
        return int(re.search(rf'"{env}", (\d+)\)', go).group(1))

    assert default("GTPU_TUNNEL_FLOOD_PPS") == TUNNEL_FLOOD_PPS
    assert default("GTPU_SOURCE_FLOOD_PPS") == SOURCE_FLOOD_PPS
    assert default("GTPU_SOURCE_FLOOD_DISTINCT_TUNNELS") == SOURCE_FLOOD_DISTINCT_TUNNELS
