"""Pins what the committed captures actually contain.

These assertions are deliberately specific. They guard the pcap reader and
the GTP-U parser (scripts/pcap_gtpu.py, the Python mirror of
bpf/packet_filter.c's parse_gtpu), and they also record a measured property
of this project's own dataset that every conclusion drawn from it depends
on -- the single-TEID limitation. If a future capture set lands here and
these fail, that is the signal to re-read
docs/paper-data/02-ai-training-inference.md §2.5 rather than to relax the
test.
"""

from pathlib import Path

import pytest

from scripts.pcap_gtpu import parse_gtpu, read_udp_packets

_REAL_DATASET_DIR = (
    Path(__file__).resolve().parent.parent.parent.parent / "docs" / "paper-data" / "real-dataset"
)


def test_real_normal_capture_is_one_tunnel_of_well_formed_gtpu():
    packets = read_udp_packets(_REAL_DATASET_DIR / "real_normal.pcap")

    assert len(packets) == 1889
    assert {p.teid for p in packets} == {0x00004D84}
    assert not any(p.malformed for p in packets)


def test_the_in_tunnel_flood_shares_the_normal_capture_s_tunnel():
    """The single most consequential property of this dataset.

    Normal traffic and the in-tunnel flood were captured from the same UE, so
    they carry the SAME TEID -- which makes per-TEID rate numerically
    identical to per-source rate here. Per-tunnel features therefore cannot
    be shown to be *discriminative* on this data, only correct; closing the
    gap on it is the deterministic detector's doing, not the model's. Getting
    real tunnel diversity needed a multi-UE capture, which now exists as
    docs/paper-data/real-dataset-v2/ (see 02-ai-training-inference.md §2.6);
    this test still pins the v1 dataset's single-TEID property so the
    limitation stays stated rather than assumed away.
    """
    normal = read_udp_packets(_REAL_DATASET_DIR / "real_normal.pcap")
    flood = read_udp_packets(_REAL_DATASET_DIR / "real_storm_pingflood.pcap")

    assert {p.teid for p in normal} == {p.teid for p in flood} == {0x00004D84}


def test_the_udp_flood_capture_has_no_gtpu_header_at_all():
    """25,944 packets aimed at the real N3 port with no GTP framing. Before
    the kernel parsed GTP-U headers these were indistinguishable from genuine
    tunneled traffic, because the protocol was asserted from the destination
    port alone.
    """
    packets = read_udp_packets(_REAL_DATASET_DIR / "real_storm_udpflood.pcap")

    assert len(packets) == 25944
    assert all(p.teid == 0 for p in packets)
    assert all(p.malformed for p in packets)


def test_the_malformed_capture_fails_gtpu_validation_for_two_reasons():
    """Payloads are 0-8 bytes. Most are shorter than the 8-byte mandatory
    GTP-U header; the 70 that are exactly 8 bytes long are still rejected,
    because gen_malformed.py fills them with zeros and a zero flags byte
    means GTP version 0. Both paths land on malformed, which is the point --
    before the kernel parsed GTP-U these were all labelled well-formed.
    """
    packets = read_udp_packets(_REAL_DATASET_DIR / "real_malformed.pcap")

    assert len(packets) == 700
    assert all(p.payload_size <= 8 for p in packets)
    assert any(p.payload_size == 8 for p in packets), "expected the boundary case to be present"
    assert all(p.malformed for p in packets)
    assert all(p.teid == 0 for p in packets)


def test_the_scan_capture_is_not_gtpu_port_traffic_so_is_not_parsed():
    packets = read_udp_packets(_REAL_DATASET_DIR / "real_scan.pcap")

    assert len(packets) == 708
    assert all(p.dest_port != 2152 for p in packets)
    # Not malformed: nothing claimed it was GTP-U in the first place.
    assert not any(p.malformed for p in packets)


@pytest.mark.parametrize(
    "payload, expected",
    [
        # Version 1, PT 1, no optional block: the 8-byte minimal header.
        (bytes([0x30, 0xFF, 0x00, 0x00]) + (0x1234).to_bytes(4, "big"), (0x1234, False)),
        # Version 0 (GTPv0) is rejected.
        (bytes([0x00, 0xFF, 0x00, 0x00]) + (0x1234).to_bytes(4, "big"), (0, True)),
        # PT 0 is GTP', a different protocol on the same header shape.
        (bytes([0x20, 0xFF, 0x00, 0x00]) + (0x1234).to_bytes(4, "big"), (0, True)),
        # Echo Request: real GTP-U, but path management -- no user-plane
        # payload and a legitimately zero TEID. Valid, NOT malformed.
        (bytes([0x30, 0x01, 0x00, 0x00]) + (0).to_bytes(4, "big"), (0, False)),
        # Shorter than the mandatory header.
        (bytes([0x30, 0xFF, 0x00]), (0, True)),
    ],
)
def test_parse_gtpu_validation_matches_the_kernel_s_rules(payload, expected):
    assert parse_gtpu(payload) == expected


def test_parse_gtpu_walks_the_pdu_session_container_every_real_packet_carries():
    """flags 0x34 means E is set, so the four optional bytes AND an extension
    header chain are present. Every real GTP-U packet in this repository
    looks like this, so a parser that ignored the E bit would mis-frame all
    of them.
    """
    payload = (
        bytes([0x34, 0xFF, 0x00, 0x00])
        + (0x4D84).to_bytes(4, "big")
        + bytes([0x00, 0x00])  # sequence number
        + bytes([0x00])  # N-PDU number
        + bytes([0x85])  # next extension: PDU Session Container
        + bytes([0x01, 0x00, 0x00, 0x00])  # 1 * 4 bytes, next type 0 (end)
    )

    assert parse_gtpu(payload) == (0x4D84, False)


def test_parse_gtpu_rejects_a_zero_length_extension_header():
    """A zero length would never advance the offset -- the kernel's own loop
    bails for exactly this reason, and so must the mirror.
    """
    payload = (
        bytes([0x34, 0xFF, 0x00, 0x00])
        + (0x4D84).to_bytes(4, "big")
        + bytes([0x00, 0x00, 0x00, 0x85])
        + bytes([0x00, 0x00, 0x00, 0x00])  # length 0
    )

    assert parse_gtpu(payload) == (0x4D84, True)
