from datetime import datetime, timezone

from sentinel_ai.features import FEATURE_VECTOR_SIZE, NormalizedEvent, extract_features


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
