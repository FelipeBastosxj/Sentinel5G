"""Trains the Sentinel5G autoencoder on the "normal" half of the synthetic
dataset produced by generate_synthetic_dataset.py. Run that script first:

    python scripts/generate_synthetic_dataset.py
    python scripts/train.py
    python scripts/export_onnx.py

See docs/getting-started.md for the full local pipeline walkthrough.
"""

from __future__ import annotations

import argparse
from pathlib import Path

import numpy as np
import torch

from sentinel_ai.model import Autoencoder, train


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dataset", type=Path, default=Path("data/synthetic_dataset.npz"))
    parser.add_argument("--epochs", type=int, default=200)
    parser.add_argument("--lr", type=float, default=1e-3)
    parser.add_argument("--output", type=Path, default=Path("models/autoencoder.pt"))
    args = parser.parse_args()

    dataset = np.load(args.dataset)
    normal = torch.from_numpy(dataset["normal"])

    model = Autoencoder(input_dim=normal.shape[1])
    history = train(model, normal, epochs=args.epochs, lr=args.lr)

    args.output.parent.mkdir(parents=True, exist_ok=True)
    torch.save(model.state_dict(), args.output)

    print(f"trained {args.epochs} epochs, final loss={history[-1]:.6f}")
    print(f"saved weights to {args.output}")


if __name__ == "__main__":
    main()
