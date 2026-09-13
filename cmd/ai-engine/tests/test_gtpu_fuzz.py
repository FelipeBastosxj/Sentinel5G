"""Heavier tests for scripts/pcap_gtpu.parse_gtpu, the Python mirror of the
kernel's parse_gtpu(). No hypothesis dependency -- a seeded PRNG over a few
hundred thousand inputs is enough to cover the header space that matters,
and the seed makes a failure reproducible.
"""

import random
import struct

import pytest

from scripts.pcap_gtpu import GTPU_MAX_EXT_HEADERS, GTPU_MAX_HDR_BYTES, parse_gtpu


def _tpdu(teid: int, flags: int = 0x30, ext_chain: bytes = b"", payload: bytes = b"\xa5" * 40):
    head = bytes([flags, 0xFF]) + struct.pack(">H", 0) + struct.pack(">I", teid)
    if flags & 0x07:
        head += bytes([0, 0, 0]) + (bytes([ext_chain[0]]) if ext_chain else bytes([0]))
        head += ext_chain[1:]
    return head + payload


def test_random_bytes_never_raise():
    rng = random.Random(20260913)
    for _ in range(200_000):
        n = rng.randint(0, 96)
        payload = bytes(rng.getrandbits(8) for _ in range(n))
        teid, malformed = parse_gtpu(payload)
        assert isinstance(malformed, bool)
        # A TEID is only ever reported when the mandatory header validated
        # (version 1, PT 1, T-PDU); anything else is 0.
        if len(payload) >= 8 and teid != 0:
            assert (payload[0] & 0xF0) == 0x30 and payload[1] == 0xFF


def test_every_valid_minimal_header_is_accepted_with_its_teid():
    rng = random.Random(1)
    for _ in range(50_000):
        teid = rng.getrandbits(32)
        got, malformed = parse_gtpu(_tpdu(teid))
        assert not malformed and got == teid


@pytest.mark.parametrize("depth", range(0, GTPU_MAX_EXT_HEADERS + 2))
def test_extension_chains_up_to_the_cap_and_one_past_it(depth):
    """A chain of `depth` 4-byte extension headers. Up to the cap it parses;
    one past the cap is reported unparseable rather than accepted with a
    wrong length, matching the kernel.
    """
    # Each ext header: [len=1 (4 octets)][2 bytes][next_type]; the chain's
    # first next_type lives in the optional block's 4th byte.
    types = [0x85] * depth
    chain = bytes([types[0]]) if depth else bytes([0])
    for i in range(depth):
        nxt = types[i + 1] if i + 1 < depth else 0
        chain += bytes([1, 0, 0, nxt])
    teid, malformed = parse_gtpu(_tpdu(0x4D84, flags=0x34, ext_chain=chain))
    assert teid == 0x4D84  # read before the walk, kept either way
    assert malformed == (depth > GTPU_MAX_EXT_HEADERS)


def test_a_chain_that_walks_past_the_byte_cap_is_rejected():
    # One huge extension header claiming 63*4 bytes: off + elen > 64.
    chain = bytes([0x85]) + bytes([63]) + b"\x00" * 250 + bytes([0])
    assert parse_gtpu(_tpdu(1, flags=0x34, ext_chain=chain)) == (1, True)
    assert GTPU_MAX_HDR_BYTES == 64


def test_path_management_messages_are_not_tunnels():
    for msg_type in (1, 2, 26, 31, 254):
        payload = bytes([0x30, msg_type, 0, 0]) + struct.pack(">I", 0x1234) + b"\x00" * 8
        assert parse_gtpu(payload) == (0, False)


def test_only_gtp_version_1_pt_1_is_accepted():
    accepted = set()
    for flags in range(256):
        _, malformed = parse_gtpu(_tpdu(7, flags=flags))
        if not malformed:
            accepted.add(flags & 0xF0)
    assert accepted == {0x30}
