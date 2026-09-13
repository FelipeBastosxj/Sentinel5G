from datetime import datetime, timezone

from sentinel_ai.features import (
    FEATURE_NAMES,
    FEATURE_VECTOR_SIZE,
    NormalizedEvent,
    extract_features,
)


def _event(**overrides):
    defaults = dict(
        protocol="GTP-U",
        dest_port=2152,
        payload_size=256,
        rate_per_second=20.0,
        malformed=False,
        observed_at=datetime(2026, 1, 5, 12, 0, tzinfo=timezone.utc),
    )
    defaults.update(overrides)
    return NormalizedEvent(**defaults)


def test_feature_vector_has_expected_size():
    features = extract_features(_event())
    assert len(features) == FEATURE_VECTOR_SIZE


def test_protocol_one_hot_is_exclusive():
    features = extract_features(_event(protocol="SIP"))
    assert features[:5] == [0.0, 1.0, 0.0, 0.0, 0.0]


def test_unknown_protocol_sets_unknown_flag():
    features = extract_features(_event(protocol="RTP"))
    assert features[:5] == [0.0, 0.0, 0.0, 0.0, 1.0]


def test_port_scan_protocol_deliberately_falls_into_unknown_bucket():
    # PORT_SCAN (pkg/events.ProtocolPortScan) is deliberately not its own
    # one-hot dimension — see the _PROTOCOLS comment in features.py for why
    # (avoids growing FEATURE_VECTOR_SIZE and breaking the shipped ONNX
    # model's input shape). This locks that decision in as a test, not just
    # a comment.
    features = extract_features(_event(protocol="PORT_SCAN"))
    assert features[:5] == [0.0, 0.0, 0.0, 0.0, 1.0]
    assert len(features) == FEATURE_VECTOR_SIZE


def test_malformed_flag_is_reflected():
    features = extract_features(_event(malformed=True))
    assert features[7] == 1.0


def test_signaling_port_flag_true_for_gtpu_and_sip():
    assert extract_features(_event(dest_port=2152))[8] == 1.0
    assert extract_features(_event(dest_port=5060))[8] == 1.0
    assert extract_features(_event(dest_port=443))[8] == 0.0


def test_extreme_values_are_clipped_into_unit_range():
    features = extract_features(_event(payload_size=10_000_000, rate_per_second=1_000_000))
    assert 0.0 <= features[5] <= 1.0
    assert 0.0 <= features[6] <= 1.0


def test_from_dict_round_trip_matches_direct_construction():
    payload = {
        "protocol": "SIP",
        "destPort": 5060,
        "payloadSize": 512,
        "ratePerSecond": 42.0,
        "malformed": False,
        "observedAt": "2026-01-05T12:00:00Z",
    }
    from_dict_event = NormalizedEvent.from_dict(payload)
    direct_event = _event(protocol="SIP", dest_port=5060, payload_size=512, rate_per_second=42.0)

    assert extract_features(from_dict_event) == extract_features(direct_event)


def test_tunnel_features_are_the_last_two_dimensions():
    """Index-pinned, because the ONNX model is trained against positions, not
    names: reordering FEATURE_NAMES without retraining would silently feed
    the model the wrong columns.
    """
    assert FEATURE_NAMES[-2:] == ["tunnel_rate_norm", "has_teid"]
    assert FEATURE_VECTOR_SIZE == 12


def test_time_of_day_is_not_a_feature():
    """Pinned deliberately. hour_sin/hour_cos were removed after they produced
    a 100% false-positive rate on real traffic captured at a different hour
    from the training session (docs/paper-data/02-ai-training-inference.md
    §2.6). Reintroducing them needs a real diurnal baseline as evidence, not
    a one-line edit -- this test is the speed bump.
    """
    assert not any("hour" in name for name in FEATURE_NAMES)

    morning = _event(observed_at=datetime(2026, 9, 7, 11, 0, tzinfo=timezone.utc))
    night = _event(observed_at=datetime(2026, 9, 11, 22, 0, tzinfo=timezone.utc))
    assert extract_features(morning) == extract_features(night)


def test_has_teid_distinguishes_a_real_tunnel_from_port_2152_traffic():
    """The feature that earns its dimension on the committed captures.

    real_storm_udpflood.pcap is 25,944 packets of plain UDP aimed at the real
    N3 port with no GTP header at all. Before the kernel parsed GTP-U, it was
    indistinguishable from genuine tunneled traffic because "protocol" was
    asserted from the destination port alone.
    """
    observed_at = datetime(2026, 9, 7, 12, 0, tzinfo=timezone.utc)
    tunneled = NormalizedEvent(
        protocol="GTP-U",
        dest_port=2152,
        payload_size=300,
        rate_per_second=20.0,
        malformed=False,
        observed_at=observed_at,
        teid=0x4D84,
        tunnel_rate_per_second=20.0,
    )
    not_tunneled = NormalizedEvent(
        protocol="GTP-U",
        dest_port=2152,
        payload_size=300,
        rate_per_second=20.0,
        malformed=False,
        observed_at=observed_at,
    )

    assert extract_features(tunneled)[-1] == 1.0
    assert extract_features(not_tunneled)[-1] == 0.0


def test_tunnel_rate_is_clipped_into_the_unit_interval():
    event = NormalizedEvent(
        protocol="GTP-U",
        dest_port=2152,
        payload_size=300,
        rate_per_second=0.0,
        malformed=False,
        observed_at=datetime(2026, 9, 7, 12, 0, tzinfo=timezone.utc),
        teid=1,
        tunnel_rate_per_second=10_000_000.0,
    )
    assert extract_features(event)[-2] == 1.0


def test_from_dict_defaults_the_tunnel_fields():
    """One-directional compatibility that matters during a rollout: an older
    Go operator publishing without these keys still scores cleanly, as
    "no tunnel identity", rather than raising.
    """
    event = NormalizedEvent.from_dict({"protocol": "GTP-U", "destPort": 2152})

    assert event.teid == 0
    assert event.tunnel_rate_per_second == 0.0
    assert len(extract_features(event)) == FEATURE_VECTOR_SIZE
