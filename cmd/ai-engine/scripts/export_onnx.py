"""Exports a trained autoencoder (scripts/train.py output) to ONNX for
production inference (sentinel_ai/server.py via onnxruntime), and computes
the normalization sidecar (models/<name>.norm.json) sentinel_ai.server uses
to turn raw reconstruction error into a [0, 1] threat score.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

import numpy as np
import torch

from sentinel_ai.model import Autoencoder, export_onnx, reconstruction_error


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--weights", type=Path, default=Path("models/autoencoder.pt"))
    parser.add_argument("--dataset", type=Path, default=Path("data/synthetic_dataset.npz"))
    parser.add_argument("--output", type=Path, default=Path("models/autoencoder.onnx"))
    args = parser.parse_args()

    dataset = np.load(args.dataset)
    normal = torch.from_numpy(dataset["normal"])

    model = Autoencoder(input_dim=normal.shape[1])
    # weights_only=True restricts unpickling to plain tensors, not arbitrary
    # objects — torch.load's default is unsafe to point at anything but a
    # checkpoint you trust (train.py's own output, in the normal pipeline).
    model.load_state_dict(torch.load(args.weights, map_location="cpu", weights_only=True))

    errors = reconstruction_error(model, normal).numpy()
    reference_error = float(np.percentile(errors, 99))

    export_onnx(model, args.output, input_dim=normal.shape[1])

    norm_path = args.output.with_suffix("").with_suffix(".norm.json")
    norm_path.write_text(json.dumps({"reference_error": reference_error}, indent=2))

    print(f"exported ONNX model to {args.output}")
    print(f"wrote normalization sidecar to {norm_path} (reference_error={reference_error:.6f})")


if __name__ == "__main__":
    main()
