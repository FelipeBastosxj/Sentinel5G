"""Re-measures the false-positive-rate confidence bound on a LARGE real
benign capture, the one gap §2.9 named as the only thing that moves the
number: orders of magnitude more real benign traffic. Trains the autoencoder
on the committed real normal data, scores a held-out slice of a large
lab-captured benign set, and reports the Clopper-Pearson upper bound and what
it means in wrong-mitigations-per-second.

Usage (from cmd/ai-engine/):
  python scripts/fpr_large_benign.py --benign /path/to/benign_large.pcap
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

import numpy as np
import torch

from scripts.build_real_dataset import _REAL_DATASET_DIR, events_from_capture
from scripts.fpr_confidence import THRESHOLDS, RATES_PPS, clopper_pearson_upper
from scripts.build_real_dataset import capture_groups
from sentinel_ai.features import extract_features
from sentinel_ai.model import Autoencoder, reconstruction_error, train


def _score(model, rows, reference_error):
    err = reconstruction_error(model, torch.from_numpy(rows)).numpy()
    return np.clip(err / (reference_error * 4.0), 0.0, 1.0)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--benign", required=True, help="large benign GTP-U pcap")
    ap.add_argument("--alpha", type=float, default=0.05)
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()

    # Train on the committed real normal captures (the model a deployment
    # would ship), then score the LARGE lab benign set the model never saw.
    import random

    rng = random.Random(args.seed)  # nosec B311
    groups = capture_groups(_REAL_DATASET_DIR, args.seed)
    train_events = [e for g in groups if g.label == 0 and not g.synthetic for e in g.events]
    train_rows = np.asarray([extract_features(e) for e in train_events], dtype=np.float32)

    model = Autoencoder(input_dim=train_rows.shape[1])
    torch.manual_seed(args.seed)
    train(model, torch.from_numpy(train_rows), epochs=200, lr=1e-2)
    ref = float(
        np.percentile(reconstruction_error(model, torch.from_numpy(train_rows)).numpy(), 99)
    )

    benign_events = events_from_capture(
        Path(args.benign), protocol="GTP-U", dest_port_override=2152, spread_time_of_day=rng
    )
    benign_rows = np.asarray([extract_features(e) for e in benign_events], dtype=np.float32)
    scores = _score(model, benign_rows, ref)

    n = len(scores)
    out = {"benign_packets": int(n), "trained_on": int(len(train_rows)), "alpha": args.alpha}
    for name, t in THRESHOLDS.items():
        k = int(np.sum(scores >= t))
        upper = clopper_pearson_upper(k, n, args.alpha)
        out[name] = {
            "false_positives": k,
            "point_fpr": k / n if n else 0.0,
            "upper_bound_95": upper,
            "wrong_mitigations_per_second_at_upper_bound": {
                str(r): round(upper * r, 2) for r in RATES_PPS
            },
        }
    print(json.dumps(out, indent=2))


if __name__ == "__main__":
    main()
