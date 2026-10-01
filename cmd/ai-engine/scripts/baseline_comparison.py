"""Capture-independent holdout and a baseline the model has to beat.

This exists because the headline accuracy numbers in
docs/paper-data/02-ai-training-inference.md were produced by
scripts/evaluate_model.py, which splits train and holdout inside the SAME
pooled set of normal samples. That measures how well the autoencoder
reconstructs held-out rows drawn from captures it also trained on -- not
whether it generalizes to a capture it never saw, and not whether it earns
its complexity over simply thresholding a rate. ROADMAP.md Phase 4's third
item asks for both. This script answers both, and prints whatever it finds,
including the uncomfortable answer that a trivial rule may match the model.

It does three things, all on the real captures (scripts/build_real_dataset's
capture_groups, one definition of which pcap is what):

1. Leave-one-capture-out (LOCO). For each NORMAL capture, train the
   autoencoder on the OTHER normal captures only, then score the held-out
   normal capture (label 0) against every anomalous capture (label 1). The
   held-out capture's packets were never seen in training, by any row, so
   this is generalization rather than reconstruction. With only two real
   normal captures today this is two folds -- thin, and said so -- but two
   honest folds beat a within-pool split.

2. Pooled k-fold across seeds, to put a variance on the single AUC the
   paper reports. "AUC 0.98" from one seed and one split is one sample of a
   distribution; this reports its spread.

3. The baseline comparison the paper never ran: the autoencoder's
   reconstruction error against two detectors that use no model at all --
   the shipped rule's own signal (tunnel-rate gated by has_teid) and the
   raw tunnel-rate feature thresholded directly -- scored on the identical
   labelled set, by the identical AUC. If the model does not clearly beat a
   one-line threshold, that is the finding, and it belongs in the open.

Run from cmd/ai-engine/: python scripts/baseline_comparison.py
"""

from __future__ import annotations

import argparse
import json

import numpy as np
import torch

from scripts.build_real_dataset import _REAL_DATASET_DIR, capture_groups
from scripts.evaluate_model import auc, confusion_at, roc_points
from sentinel_ai.features import FEATURE_NAMES
from sentinel_ai.model import Autoencoder, reconstruction_error, train

_TUNNEL_RATE_IDX = FEATURE_NAMES.index("tunnel_rate_norm")
_HAS_TEID_IDX = FEATURE_NAMES.index("has_teid")
_UNTUNNELED_RATE_IDX = FEATURE_NAMES.index("untunneled_rate_norm")

# The per-sample threat score the production server emits is
# reconstruction_error / (reference_error * 4), clamped to [0, 1]
# (sentinel_ai/server.py). AUC is threshold-free and monotonic under that
# transform, so the raw reconstruction error ranks identically to the
# shipped score and is used directly here -- no ONNX export needed for a
# ranking metric.


def _train_autoencoder(normal_train: np.ndarray, seed: int = 42) -> Autoencoder:
    model = Autoencoder(input_dim=normal_train.shape[1])
    torch.manual_seed(seed)
    train(model, torch.from_numpy(normal_train), epochs=200, lr=1e-2)
    return model


def _autoencoder_scores(model: Autoencoder, rows: np.ndarray) -> np.ndarray:
    return reconstruction_error(model, torch.from_numpy(rows)).numpy()


def _rule_scores(rows: np.ndarray) -> np.ndarray:
    """The shipped rule's own signal as a rank: tunnel rate, but only for
    rows that carry a tunnel identity (has_teid == 1). A row with no TEID
    scores 0, exactly as GTPUFloodDetector takes no per-tunnel action on it.
    This is the detector in pkg/detect expressed as a score, so its ROC is
    the rule's ROC.
    """
    return rows[:, _TUNNEL_RATE_IDX] * rows[:, _HAS_TEID_IDX]


def _tunnel_rate_scores(rows: np.ndarray) -> np.ndarray:
    """The most trivial baseline there is: the raw per-tunnel rate feature,
    thresholded directly, no TEID gate, no model. If this matches the
    autoencoder, the autoencoder is buying nothing here.
    """
    return rows[:, _TUNNEL_RATE_IDX]


def _untunneled_rate_scores(rows: np.ndarray) -> np.ndarray:
    """A second trivial baseline for the untunneled storms (the per-source
    rate the model keeps for non-tunnel traffic). Included so the comparison
    isn't rigged toward the one feature the floods happen to move."""
    return rows[:, _UNTUNNELED_RATE_IDX]


def _auc_for(scores: np.ndarray, labels: np.ndarray) -> float:
    return auc(roc_points(scores, labels))


def leave_one_capture_out(groups, seed: int) -> dict:
    """Trains on all-but-one NORMAL capture, scores the held-out normal
    capture against all anomalous captures. One fold per real normal
    capture (the synthetic group is never a holdout -- "capture" means pcap).
    """
    normal_caps = [g for g in groups if g.label == 0 and not g.synthetic]
    anomalous = np.concatenate([g.features() for g in groups if g.label == 1])

    folds = []
    for held in normal_caps:
        train_caps = [g for g in normal_caps if g.name != held.name]
        if not train_caps:
            continue
        normal_train = np.concatenate([g.features() for g in train_caps])
        model = _train_autoencoder(normal_train, seed)

        held_rows = held.features()
        rows = np.concatenate([held_rows, anomalous])
        labels = np.concatenate([np.zeros(len(held_rows)), np.ones(len(anomalous))])

        ae = _auc_for(_autoencoder_scores(model, rows), labels)
        rule = _auc_for(_rule_scores(rows), labels)
        trivial = _auc_for(_tunnel_rate_scores(rows), labels)
        folds.append(
            {
                "held_out_capture": held.name,
                "trained_on": [g.name for g in train_caps],
                "held_out_normal_rows": int(len(held_rows)),
                "anomalous_rows": int(len(anomalous)),
                "auc_autoencoder": ae,
                "auc_rule_gtpu_tunnel_flood": rule,
                "auc_trivial_tunnel_rate": trivial,
            }
        )
    return {
        "folds": folds,
        "note": (
            "Only %d real normal capture(s) exist, so this is %d fold(s). "
            "More normal captures from different sessions are the single "
            "biggest thing that would strengthen this." % (len(normal_caps), len(folds))
        ),
    }


def pooled_kfold_variance(groups, k: int, seeds: list[int]) -> dict:
    """k-fold over the pooled normal set, repeated per seed, reporting the
    autoencoder AUC's spread -- the variance the paper's single number hides.
    """
    normal = np.concatenate([g.features() for g in groups if g.label == 0])
    anomalous = np.concatenate([g.features() for g in groups if g.label == 1])
    labels_anom = np.ones(len(anomalous))

    aucs = []
    for seed in seeds:
        rng = np.random.default_rng(seed)
        perm = rng.permutation(len(normal))
        folds = np.array_split(perm, k)
        for i in range(k):
            holdout_idx = folds[i]
            train_idx = np.concatenate([folds[j] for j in range(k) if j != i])
            model = _train_autoencoder(normal[train_idx], seed)

            held_rows = normal[holdout_idx]
            rows = np.concatenate([held_rows, anomalous])
            labels = np.concatenate([np.zeros(len(held_rows)), labels_anom])
            aucs.append(_auc_for(_autoencoder_scores(model, rows), labels))

    arr = np.asarray(aucs)
    return {
        "k": k,
        "seeds": seeds,
        "n_measurements": int(len(arr)),
        "auc_mean": float(arr.mean()),
        "auc_std": float(arr.std(ddof=1)) if len(arr) > 1 else 0.0,
        "auc_min": float(arr.min()),
        "auc_max": float(arr.max()),
    }


def _recall_at_max_fpr(scores: np.ndarray, labels: np.ndarray, max_fpr: float) -> dict:
    """The operationally honest metric for this problem: at a tolerable
    false-positive rate, how much of the attack is caught? Best-F1 is
    useless here -- the set is 97% anomalous, so "flag everything" wins F1
    for every scorer identically -- whereas a false-positive budget is
    exactly the knob a detection-only pilot turns (docs/production-install.md).
    Returns the highest recall reachable without exceeding max_fpr.
    """
    best = {"threshold": None, "recall": 0.0, "false_positive_rate": 0.0, "precision": 0.0}
    for t in np.linspace(scores.min(), scores.max(), 400):
        c = confusion_at(scores, labels, float(t))
        if c["false_positive_rate"] <= max_fpr and c["recall"] >= best["recall"]:
            best = {
                "threshold": float(t),
                "recall": c["recall"],
                "false_positive_rate": c["false_positive_rate"],
                "precision": c["precision"],
            }
    return best


def head_to_head(groups, seed: int, max_fpr: float = 0.01) -> dict:
    """Autoencoder vs the model-free baselines on one pooled holdout, by AUC
    (balance-insensitive ranking) and by recall at a fixed <=1% false-positive
    rate (the pilot's operating point). The table that answers "does the ML
    earn its keep", including where it does not.
    """
    normal = np.concatenate([g.features() for g in groups if g.label == 0])
    anomalous = np.concatenate([g.features() for g in groups if g.label == 1])

    rng = np.random.default_rng(seed)
    perm = rng.permutation(len(normal))
    split = int(len(normal) * 0.8)
    model = _train_autoencoder(normal[perm[:split]], seed)
    holdout = normal[perm[split:]]

    rows = np.concatenate([holdout, anomalous])
    labels = np.concatenate([np.zeros(len(holdout)), np.ones(len(anomalous))])

    scorers = {
        "autoencoder": _autoencoder_scores(model, rows),
        "rule_gtpu_tunnel_flood": _rule_scores(rows),
        "trivial_tunnel_rate": _tunnel_rate_scores(rows),
        "trivial_untunneled_rate": _untunneled_rate_scores(rows),
    }
    out = {
        "max_fpr": max_fpr,
        "holdout_normal_rows": int(len(holdout)),
        "anomalous_rows": int(len(anomalous)),
    }
    for name, scores in scorers.items():
        op = _recall_at_max_fpr(scores, labels, max_fpr)
        out[name] = {
            "auc": _auc_for(scores, labels),
            "recall_at_max_fpr": op["recall"],
            "fpr": op["false_positive_rate"],
            "precision_at_that_point": op["precision"],
        }
    return out


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--real-dataset-dir", default=str(_REAL_DATASET_DIR))
    parser.add_argument("--kfold", type=int, default=5)
    parser.add_argument("--seeds", type=int, nargs="+", default=[42, 1, 7])
    args = parser.parse_args()

    from pathlib import Path

    groups = capture_groups(Path(args.real_dataset_dir))

    result = {
        "captures": [
            {"name": g.name, "label": g.label, "rows": len(g.events), "synthetic": g.synthetic}
            for g in groups
        ],
        "leave_one_capture_out": leave_one_capture_out(groups, seed=args.seeds[0]),
        "pooled_kfold_variance": pooled_kfold_variance(groups, args.kfold, args.seeds),
        "head_to_head_on_pooled_holdout": head_to_head(groups, seed=args.seeds[0]),
    }
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
