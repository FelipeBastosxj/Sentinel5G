"""Calibrates the autoencoder on a deployment's OWN N3 traffic, in one command,
and refuses to hand over a model that has not proved itself on traffic it
never trained on.

Why this exists: the published model flags ~75-88% of real application
traffic; the same model trained and calibrated on that network's traffic
flagged none of ~2 million packets, including an independent session
(docs/paper-data/02-ai-training-inference.md §2.11.2-§2.11.3). Local
calibration is the difference between those two numbers, so it should not be
a sequence of scripts.

What it does, in order:

  1. reads one or more benign N3 captures (`tcpdump -i <n3> -s 0 -w n3.pcap
     'udp port 2152'`; a truncated snaplen is fine, the original length is
     what is used);
  2. trains on a sample of the EARLIER part of the traffic and holds out the
     later part (chronological, so the validation is traffic from after the
     model's training window, as it will be in production);
  3. scores the held-out part and computes the exact 95% upper bound on the
     false-positive rate at each sensitivity tier;
  4. writes autoencoder.onnx (+ .onnx.data) and autoencoder.norm.json ONLY if
     that bound is at or below --max-fpr-upper-bound; otherwise exits 3 with
     the report and no model;
  5. reports what the calibrated model still detects on the committed real
     attack captures -- so the price of calibration (§2.11.4) is visible
     before the model is deployed, not after.

Usage (from cmd/ai-engine/):
  python scripts/calibrate.py --capture n3-monday.pcap n3-tuesday.pcap \\
      --out-dir models/calibrated

then, as docs/production-install.md step 3:
  kubectl create secret generic ai-engine-model -n sentinel5g-system \\
      --from-file=models/calibrated/autoencoder.onnx ...
"""

from __future__ import annotations

import argparse
import json
import random
import sys
from pathlib import Path

import numpy as np

from scripts.build_real_dataset import _REAL_DATASET_DIR, capture_groups, events_from_capture
from scripts.fpr_app_mix import _detection, _fit, _score
from scripts.fpr_confidence import THRESHOLDS, clopper_pearson_upper
from sentinel_ai.features import extract_features
from sentinel_ai.model import export_onnx

# §2.11.3 measured 3.7e-6 on lab application traffic; 1e-5 is ~1 wrong
# mitigation/s at 100k pkt/s, the operating point §2.10 called bounded.
DEFAULT_MAX_FPR_UPPER_BOUND = 1e-5


def _rows(paths: list[Path], rng: random.Random) -> np.ndarray:
    """Feature rows of every capture, in capture order then time order."""
    out = []
    for path in paths:
        # Rates from the real timestamps; absolute time of day spread, as for
        # every training set here (build_real_dataset.events_from_capture).
        events = events_from_capture(
            path, protocol="GTP-U", dest_port_override=2152, spread_time_of_day=rng
        )
        out.append(np.asarray([extract_features(e) for e in events], dtype=np.float32))
    return np.concatenate(out)


def calibrate(
    captures: list[Path],
    out_dir: Path,
    *,
    train_fraction: float = 0.5,
    rows: int = 20_000,
    max_fpr_upper_bound: float = DEFAULT_MAX_FPR_UPPER_BOUND,
    tier: str = "high",
    alpha: float = 0.05,
    seed: int = 42,
) -> dict:
    """Runs the whole calibration; returns the report. The model files are
    written to out_dir only when report["accepted"] is True."""
    rng = random.Random(seed)  # nosec B311
    data = _rows(captures, rng)
    split = int(len(data) * train_fraction)
    if split == 0 or split == len(data):
        raise ValueError(f"need packets on both sides of the split, got {len(data)}")

    np_rng = np.random.default_rng(seed)
    train_idx = np_rng.choice(split, size=min(rows, split), replace=False)
    model, reference_error = _fit(data[train_idx], seed)
    scores = _score(model, data[split:], reference_error)

    n = len(scores)
    tiers = {}
    for name, t in THRESHOLDS.items():
        k = int(np.sum(scores >= t))
        upper = clopper_pearson_upper(k, n, alpha)
        tiers[name] = {
            "false_positives": k,
            "upper_bound_95": upper,
            "wrong_mitigations_per_s_at_100000": round(upper * 100_000, 2),
        }

    accepted = tiers[tier]["upper_bound_95"] <= max_fpr_upper_bound
    report = {
        "captures": [p.name for p in captures],
        "packets": int(len(data)),
        "trained_on_rows": int(len(train_idx)),
        "validated_on_packets": int(n),
        "reference_error": reference_error,
        "tier": tier,
        "max_fpr_upper_bound": max_fpr_upper_bound,
        "false_positives_on_held_out": tiers,
        "accepted": accepted,
        "detection_on_committed_attacks": _detection(
            model, reference_error, capture_groups(_REAL_DATASET_DIR, seed)
        ),
    }

    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "calibration-report.json").write_text(json.dumps(report, indent=2) + "\n")
    if accepted:
        export_onnx(model, out_dir / "autoencoder.onnx", input_dim=data.shape[1])
        (out_dir / "autoencoder.norm.json").write_text(
            json.dumps({"reference_error": reference_error}, indent=2) + "\n"
        )
    return report


def main() -> None:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--capture", required=True, nargs="+", type=Path)
    ap.add_argument("--out-dir", type=Path, default=Path("models/calibrated"))
    ap.add_argument("--train-fraction", type=float, default=0.5)
    ap.add_argument("--rows", type=int, default=20_000, help="training rows sampled")
    ap.add_argument(
        "--max-fpr-upper-bound",
        type=float,
        default=DEFAULT_MAX_FPR_UPPER_BOUND,
        help="refuse the model above this 95%% upper bound on the held-out FPR",
    )
    ap.add_argument("--tier", choices=THRESHOLDS, default="high")
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()

    report = calibrate(
        args.capture,
        args.out_dir,
        train_fraction=args.train_fraction,
        rows=args.rows,
        max_fpr_upper_bound=args.max_fpr_upper_bound,
        tier=args.tier,
        seed=args.seed,
    )
    t = report["false_positives_on_held_out"][args.tier]
    print(
        f"held out {report['validated_on_packets']} packets: {t['false_positives']} false "
        f"positives at '{args.tier}', 95% upper bound {t['upper_bound_95']:.3g} "
        f"(<= {t['wrong_mitigations_per_s_at_100000']} wrong mitigations/s at 100k pkt/s)"
    )
    missed = [
        name
        for name, d in report["detection_on_committed_attacks"].items()
        if d[f"flagged_{args.tier}"] < 0.5
    ]
    if missed:
        print(
            "WARNING: the calibrated model flags under half of these committed attack "
            f"captures: {', '.join(missed)}. Keep the rules or other controls in front "
            "of them (docs/paper-data/02-ai-training-inference.md §2.11.4)."
        )
    if not report["accepted"]:
        print(
            f"REFUSED: upper bound above {args.max_fpr_upper_bound:g}; no model written. "
            "Capture more benign traffic, or check the capture is benign. Report: "
            f"{args.out_dir / 'calibration-report.json'}",
            file=sys.stderr,
        )
        sys.exit(3)
    d = args.out_dir
    print(f"wrote {d / 'autoencoder.onnx'} and {d / 'autoencoder.norm.json'}. Deploy with:")
    print(
        "  kubectl create secret generic ai-engine-model -n sentinel5g-system \\\n"
        f"    --from-file=autoencoder.onnx={d / 'autoencoder.onnx'} \\\n"
        f"    --from-file=autoencoder.onnx.data={d / 'autoencoder.onnx.data'} \\\n"
        f"    --from-file=autoencoder.norm.json={d / 'autoencoder.norm.json'}\n"
        "  helm upgrade --install sentinel5g-ai-engine ... "
        "--set model.image.repository=null --set model.existingSecret=ai-engine-model"
    )


if __name__ == "__main__":
    main()
