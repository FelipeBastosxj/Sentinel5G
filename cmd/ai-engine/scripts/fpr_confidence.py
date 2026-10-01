"""False-positive rate as a confidence interval, not a point estimate.

ROADMAP.md Phase 4's fourth item: "zero false positives" was measured on
5,000 normal packets and 192 bystander packets, and a point estimate of zero
on a few thousand samples says almost nothing about the rate that matters.
An FPR of 1e-4 -- invisible in that sample -- is ten wrong mitigations per
second at 100k pkt/s. Authorising autoMitigate on real traffic is a decision
about the UPPER BOUND of the FPR, which a point estimate does not provide.

This script provides it. For a given benign sample of n packets with k false
positives at a threshold, it reports the Clopper-Pearson exact binomial
upper confidence bound on the true FPR, and translates that bound into wrong
mitigations per second at representative packet rates -- the number an
operator actually has to accept.

Two benign universes are scored, and the difference between them is the
honest part:

- The real captures, scored OUT OF FOLD (every real normal packet scored by
  a model trained on the other real normal captures, so no packet is scored
  by a model that trained on it). This uses all the real benign data there
  is and is the bound that governs a real deployment. At a few thousand
  packets it is still loose -- that looseness is the finding.
- A large synthetic benign set, kept to show why it is NOT a usable FPR
  proxy: a model trained on real GTP-U treats the synthetic generator's
  protocol mix as unfamiliar and flags most of it, so synthetic volume
  measures distribution shift, not false positives. That is the concrete
  proof benign VOLUME has to come from real traffic.

No scipy dependency: the regularized incomplete beta function and its
inverse are implemented here (Lentz continued fraction + bisection), checked
against the k=0 closed form in tests.

Run from cmd/ai-engine/: python scripts/fpr_confidence.py
"""

from __future__ import annotations

import argparse
import json
import math

import numpy as np
import torch

from scripts.build_real_dataset import _REAL_DATASET_DIR, capture_groups
from scripts.generate_synthetic_dataset import generate as generate_synthetic
from sentinel_ai.model import Autoencoder, reconstruction_error, train

# Production sensitivity thresholds on the [0,1] score, mirroring
# pkg/controller.threat_score_watcher.go (BaseThreshold 0.85 x the
# sensitivity multiplier, clamped to [0,1]).
THRESHOLDS = {
    "high": 0.85 * 0.8,
    "medium": 0.85 * 1.0,
    "low": min(0.85 * 1.2, 1.0),
}

# Packet rates to translate an FPR bound into wrong mitigations per second.
# 100k pkt/s is the figure the roadmap item names.
RATES_PPS = [1_000, 10_000, 100_000]


def _betacf(a: float, b: float, x: float) -> float:
    """Continued fraction for the incomplete beta function (Numerical
    Recipes' betacf, Lentz's method)."""
    tiny = 1e-30
    qab, qap, qam = a + b, a + 1.0, a - 1.0
    c = 1.0
    d = 1.0 - qab * x / qap
    if abs(d) < tiny:
        d = tiny
    d = 1.0 / d
    h = d
    for m in range(1, 300):
        m2 = 2 * m
        aa = m * (b - m) * x / ((qam + m2) * (a + m2))
        d = 1.0 + aa * d
        if abs(d) < tiny:
            d = tiny
        c = 1.0 + aa / c
        if abs(c) < tiny:
            c = tiny
        d = 1.0 / d
        h *= d * c
        aa = -(a + m) * (qab + m) * x / ((a + m2) * (qap + m2))
        d = 1.0 + aa * d
        if abs(d) < tiny:
            d = tiny
        c = 1.0 + aa / c
        if abs(c) < tiny:
            c = tiny
        d = 1.0 / d
        delta = d * c
        h *= delta
        if abs(delta - 1.0) < 1e-14:
            break
    return h


def betainc(a: float, b: float, x: float) -> float:
    """Regularized incomplete beta I_x(a, b)."""
    if x <= 0.0:
        return 0.0
    if x >= 1.0:
        return 1.0
    lbeta = math.lgamma(a + b) - math.lgamma(a) - math.lgamma(b)
    front = math.exp(lbeta + a * math.log(x) + b * math.log(1.0 - x))
    if x < (a + 1.0) / (a + b + 2.0):
        return front * _betacf(a, b, x) / a
    return 1.0 - front * _betacf(b, a, 1.0 - x) / b


def betaincinv(a: float, b: float, p: float) -> float:
    """Inverse of betainc in x, by bisection. Monotone in x, so robust."""
    lo, hi = 0.0, 1.0
    for _ in range(200):
        mid = 0.5 * (lo + hi)
        if betainc(a, b, mid) < p:
            lo = mid
        else:
            hi = mid
    return 0.5 * (lo + hi)


def clopper_pearson_upper(k: int, n: int, alpha: float = 0.05) -> float:
    """Exact (Clopper-Pearson) upper confidence bound on a binomial rate:
    the largest p for which observing <= k successes in n trials still has
    probability >= alpha/2. For k == n the upper bound is 1.0.
    """
    if n == 0:
        return 1.0
    if k >= n:
        return 1.0
    return betaincinv(k + 1, n - k, 1.0 - alpha / 2.0)


def _score(model: Autoencoder, rows: np.ndarray, reference_error: float) -> np.ndarray:
    """The production [0,1] score: reconstruction error / (reference * 4),
    clamped -- sentinel_ai/server.py's formula, replicated so no ONNX export
    is needed for a threshold count."""
    err = reconstruction_error(model, torch.from_numpy(rows)).numpy()
    return np.clip(err / (reference_error * 4.0), 0.0, 1.0)


def _fpr_block(scores: np.ndarray, n: int, alpha: float) -> dict:
    out = {"n": int(n)}
    for name, t in THRESHOLDS.items():
        k = int(np.sum(scores >= t))
        point = k / n if n else 0.0
        upper = clopper_pearson_upper(k, n, alpha)
        out[name] = {
            "false_positives": k,
            "point_estimate": point,
            "upper_bound_95": upper,
            "wrong_mitigations_per_second_at_upper_bound": {
                str(rate): upper * rate for rate in RATES_PPS
            },
        }
    return out


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--synthetic-n", type=int, default=1_000_000)
    parser.add_argument("--alpha", type=float, default=0.05)
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()

    groups = capture_groups(_REAL_DATASET_DIR)
    normal_caps = [g for g in groups if g.label == 0 and not g.synthetic]

    # Out-of-fold scoring: every real normal packet is scored by a model
    # trained on the OTHER real normal captures. This uses all the real
    # benign data (no 20% thrown away to a holdout) and no packet is ever
    # scored by a model that trained on it -- both properties an FPR
    # measurement needs. reference_error is taken per fold from that fold's
    # own training errors, exactly as production derives it.
    real_scores_parts = []
    last_model = None
    last_reference = 0.0
    for held in normal_caps:
        train_rows = np.concatenate([g.features() for g in normal_caps if g.name != held.name])
        model = Autoencoder(input_dim=train_rows.shape[1])
        torch.manual_seed(args.seed)
        train(model, torch.from_numpy(train_rows), epochs=200, lr=1e-2)
        train_err = reconstruction_error(model, torch.from_numpy(train_rows)).numpy()
        reference_error = float(np.percentile(train_err, 99))
        last_model, last_reference = model, reference_error
        real_scores_parts.append(_score(model, held.features(), reference_error))
    real_scores = np.concatenate(real_scores_parts)

    # A large synthetic benign universe, scored by the last fold's model.
    # NOT an FPR proxy -- see the module docstring and the caveat below.
    synth_normal, _ = generate_synthetic(
        num_normal=args.synthetic_n, num_anomalous=1, seed=args.seed
    )
    synth_scores = _score(last_model, synth_normal, last_reference)

    # The real benign packet count an acceptable bound would need: solve the
    # k=0 Clopper-Pearson bound 1-(alpha/2)^(1/n) <= target for n.
    def n_for_target(target_fpr: float) -> int:
        return int(math.ceil(math.log(args.alpha / 2.0) / math.log(1.0 - target_fpr)))

    result = {
        "alpha": args.alpha,
        "confidence": 1.0 - args.alpha,
        "real_benign_out_of_fold": {
            "what": "every real normal packet, scored out of fold (model never trained on it)",
            "caveat": "the right distribution and all the real data there is -- still only a few "
            "thousand packets, so the bound is loose, and that looseness is the finding.",
            **_fpr_block(real_scores, len(real_scores), args.alpha),
        },
        "synthetic_not_an_fpr_proxy": {
            "what": f"{args.synthetic_n} synthetic normal events",
            "caveat": "NOT a false-positive rate. A model trained on real GTP-U flags the "
            "synthetic generator's protocol mix as unfamiliar, so this measures distribution "
            "shift. It is kept as proof that benign VOLUME must come from real traffic.",
            **_fpr_block(synth_scores, len(synth_scores), args.alpha),
        },
        "real_packets_needed_for": {
            "1_wrong_mitigation_per_sec_at_100k_pps": n_for_target(1.0 / 100_000),
            "0.1_wrong_mitigation_per_sec_at_100k_pps": n_for_target(0.1 / 100_000),
            "note": "assuming zero false positives in that sample; any observed FP raises the "
            "required n. This is the concrete benign-traffic collection target.",
        },
    }
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
