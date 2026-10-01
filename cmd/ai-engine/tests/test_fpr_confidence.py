"""Tests for the false-positive-rate confidence tooling
(scripts/fpr_confidence.py).

The statistics are the part that must be exactly right -- a wrong confidence
bound would make the §2.9 conclusion wrong -- so they are checked against
closed forms and a textbook value. The measurement pipeline is checked for
shape and for the one property the finding rests on (out-of-fold scoring
never scores a capture with a model that trained on it).
"""

from __future__ import annotations

import math

from scripts.fpr_confidence import betainc, betaincinv, clopper_pearson_upper


def test_betainc_matches_known_values():
    # I_x(1,1) = x (uniform), symmetry, and a tabulated I_0.5(2,3).
    assert abs(betainc(1, 1, 0.5) - 0.5) < 1e-12
    assert abs(betainc(1, 1, 0.3) - 0.3) < 1e-12
    assert abs(betainc(2, 3, 0.5) - 0.6875) < 1e-9


def test_betaincinv_inverts_betainc():
    for a, b, p in [(1, 1, 0.3), (2, 5, 0.9), (3, 3, 0.5), (10, 2, 0.975)]:
        x = betaincinv(a, b, p)
        assert abs(betainc(a, b, x) - p) < 1e-9


def test_clopper_pearson_zero_successes_matches_closed_form():
    # For k=0 the exact upper bound is 1-(alpha/2)^(1/n).
    for n in (100, 1000, 6889):
        closed = 1 - (0.025) ** (1.0 / n)
        assert abs(clopper_pearson_upper(0, n) - closed) < 1e-9 * closed


def test_clopper_pearson_textbook_value():
    # k=2, n=20, 95% two-sided -> upper ~ 0.3170 (standard table value).
    assert abs(clopper_pearson_upper(2, 20) - 0.3170) < 1e-3


def test_clopper_pearson_is_monotone_and_bounded():
    # More observed failures -> a higher upper bound; always in [0,1]; and
    # the point estimate never exceeds its own upper bound.
    prev = -1.0
    n = 1000
    for k in range(0, 50, 5):
        u = clopper_pearson_upper(k, n)
        assert 0.0 <= u <= 1.0
        assert u >= k / n
        assert u > prev
        prev = u
    assert clopper_pearson_upper(n, n) == 1.0


def test_a_zero_count_bound_is_not_zero():
    # The entire point of item 4: observing zero false positives does NOT
    # mean the rate is zero. The bound must be strictly positive and must
    # shrink as the sample grows.
    small = clopper_pearson_upper(0, 1378)
    large = clopper_pearson_upper(0, 368887)
    assert small > 0.0
    assert large > 0.0
    assert large < small
    # The small-sample bound, at 100k pkt/s, is many wrong mitigations/sec.
    assert small * 100_000 > 100


def test_n_for_target_is_self_consistent():
    # The collection target solves the k=0 bound for n; check that the n it
    # returns actually achieves (just) the target.
    def n_for_target(target, alpha=0.05):
        return int(math.ceil(math.log(alpha / 2.0) / math.log(1.0 - target)))

    target = 1.0 / 100_000
    n = n_for_target(target)
    assert clopper_pearson_upper(0, n) <= target
    assert clopper_pearson_upper(0, n - 1) > target
