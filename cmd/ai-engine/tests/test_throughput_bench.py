"""Fast guards for scripts/throughput_bench.py -- not the throughput numbers
(those are a measurement the script prints, host-dependent), but the helpers
the measurement is built from, so a refactor can't silently make the
reported rates meaningless.
"""

from __future__ import annotations

from scripts.throughput_bench import _rate, _sample_event_dict
from sentinel_ai.features import FEATURE_VECTOR_SIZE, NormalizedEvent, extract_features


def test_sample_event_is_a_valid_scorable_gtpu_event():
    # The benchmark's realism rests on this: the sample dict must parse into
    # a real tunneled event and extract a full feature vector, or the numbers
    # measure scoring of a degenerate input.
    event = NormalizedEvent.from_dict(_sample_event_dict())
    assert event.protocol == "GTP-U"
    assert event.teid != 0
    assert event.tunnel_rate_per_second > 0
    feats = extract_features(event)
    assert len(feats) == FEATURE_VECTOR_SIZE


def test_rate_counts_iterations_over_elapsed():
    calls = {"n": 0}

    def fn():
        calls["n"] += 1

    r = _rate(fn, n=1000, warmup=10)
    # 1000 timed calls + 10 warmup.
    assert calls["n"] == 1010
    assert r > 0  # a positive events/sec
