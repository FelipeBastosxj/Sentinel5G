"""Tests for the capture-independent holdout and baseline comparison
(scripts/baseline_comparison.py) and the capture_groups provenance it rests
on (scripts/build_real_dataset.py).

These guard the properties the numbers in
docs/paper-data/02-ai-training-inference.md §2.8 depend on -- not the exact
AUC values (those are a training run's output, reproduced by the script),
but the invariants that make them mean what the section says.
"""

from __future__ import annotations

import numpy as np

from scripts.baseline_comparison import (
    _rule_scores,
    _tunnel_rate_scores,
    head_to_head,
    leave_one_capture_out,
    pooled_kfold_variance,
)
from scripts.build_real_dataset import _REAL_DATASET_DIR, build, capture_groups
from sentinel_ai.features import FEATURE_NAMES, FEATURE_VECTOR_SIZE


def test_capture_groups_pool_to_exactly_what_build_returns():
    """The refactor's load-bearing invariant: capture_groups is now the one
    definition of the dataset, and build() just pools it. If these ever
    diverge, the baseline comparison is evaluating a different dataset than
    training uses, silently.
    """
    groups = capture_groups(_REAL_DATASET_DIR)
    normal_from_groups = sum(len(g.events) for g in groups if g.label == 0)
    anomalous_from_groups = sum(len(g.events) for g in groups if g.label == 1)

    normal, anomalous = build(_REAL_DATASET_DIR)
    assert len(normal) == normal_from_groups
    assert len(anomalous) == anomalous_from_groups


def test_capture_groups_have_both_labels_and_a_real_normal_holdout_candidate():
    groups = capture_groups(_REAL_DATASET_DIR)
    assert any(g.label == 0 for g in groups), "no normal captures"
    assert any(g.label == 1 for g in groups), "no anomalous captures"
    # Leave-one-capture-out needs at least two NON-synthetic normal captures,
    # or there is nothing to hold out. This is the property that makes §2.8's
    # holdout capture-independent rather than within-pool.
    real_normal = [g for g in groups if g.label == 0 and not g.synthetic]
    assert (
        len(real_normal) >= 2
    ), "fewer than two real normal captures: no capture-independent holdout"


def test_only_the_kernel_malformed_group_is_synthetic():
    groups = capture_groups(_REAL_DATASET_DIR)
    synthetic = [g.name for g in groups if g.synthetic]
    assert synthetic == ["kernel_malformed"], synthetic


def test_group_features_are_well_formed():
    for g in capture_groups(_REAL_DATASET_DIR):
        feats = g.features()
        assert feats.shape[1] == FEATURE_VECTOR_SIZE
        assert np.isfinite(feats).all()


def test_rule_score_is_zero_without_a_tunnel_identity():
    """The rule baseline must score a TEID-less row at 0 -- that is the
    has_teid gate, and it is why the rule is blind to the UDP-storm and scan
    anomalies (the finding §2.8 rests on). A row with has_teid=0 scoring
    non-zero would quietly inflate the rule's measured AUC.
    """
    teid_idx = FEATURE_NAMES.index("has_teid")
    rate_idx = FEATURE_NAMES.index("tunnel_rate_norm")

    row = np.zeros((1, FEATURE_VECTOR_SIZE), dtype=np.float32)
    row[0, rate_idx] = 0.9  # a high rate...
    row[0, teid_idx] = 0.0  # ...but no tunnel identity
    assert _rule_scores(row)[0] == 0.0

    row[0, teid_idx] = 1.0
    assert _rule_scores(row)[0] == 0.9
    # The trivial baseline, by contrast, ignores the gate entirely.
    assert _tunnel_rate_scores(row)[0] == 0.9


def test_leave_one_capture_out_holds_each_normal_capture_out_of_training():
    """Every fold must train ONLY on captures other than the one it scores --
    the whole point. A fold that trained on its own holdout would report
    memorization as generalization.
    """
    groups = capture_groups(_REAL_DATASET_DIR)
    result = leave_one_capture_out(groups, seed=42)
    assert result["folds"], "no folds produced"
    for fold in result["folds"]:
        assert fold["held_out_capture"] not in fold["trained_on"]
        assert fold["trained_on"], "a fold trained on nothing"
        # Every scorer reports a real AUC in [0, 1].
        for key in ("auc_autoencoder", "auc_rule_gtpu_tunnel_flood", "auc_trivial_tunnel_rate"):
            assert 0.0 <= fold[key] <= 1.0


def test_kfold_variance_reports_a_spread():
    groups = capture_groups(_REAL_DATASET_DIR)
    # Two seeds, k=2, kept small so the test is fast but still exercises the
    # multi-measurement path that produces a standard deviation.
    v = pooled_kfold_variance(groups, k=2, seeds=[42, 1])
    assert v["n_measurements"] == 4
    assert 0.0 <= v["auc_min"] <= v["auc_mean"] <= v["auc_max"] <= 1.0
    assert v["auc_std"] >= 0.0


def test_head_to_head_includes_the_model_free_baselines():
    """The comparison must actually contain the baselines the ML is claimed
    to beat, scored on the same holdout by the same AUC. A table with only
    the autoencoder in it proves nothing.
    """
    groups = capture_groups(_REAL_DATASET_DIR)
    h = head_to_head(groups, seed=42, max_fpr=0.01)
    for scorer in ("autoencoder", "rule_gtpu_tunnel_flood", "trivial_tunnel_rate"):
        assert scorer in h
        assert 0.0 <= h[scorer]["auc"] <= 1.0
        assert 0.0 <= h[scorer]["recall_at_max_fpr"] <= 1.0
        assert h[scorer]["fpr"] <= 0.01 + 1e-9
