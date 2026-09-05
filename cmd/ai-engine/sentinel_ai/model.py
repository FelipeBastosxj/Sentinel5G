"""Autoencoder model definition, training loop, and ONNX export used by
scripts/train.py and scripts/export_onnx.py. Production inference loads the
exported ONNX graph via onnxruntime (sentinel_ai/server.py); this module's
torch dependency is not required at serving time.
"""

from __future__ import annotations

from pathlib import Path

import torch
from torch import nn

from .features import FEATURE_VECTOR_SIZE


class Autoencoder(nn.Module):
    """A small feed-forward autoencoder.

    Normal telecom signaling traffic is assumed to lie on a low-dimensional
    manifold the encoder learns to reconstruct cheaply; anomalous traffic
    (signaling storms, malformed protocol framing, off-hours scanning)
    reconstructs poorly, and that reconstruction error is the raw anomaly
    signal turned into a threat score by sentinel_ai.server.ScoringEngine.
    """

    def __init__(self, input_dim: int = FEATURE_VECTOR_SIZE, latent_dim: int = 4):
        super().__init__()
        self.encoder = nn.Sequential(
            nn.Linear(input_dim, 8),
            nn.ReLU(),
            nn.Linear(8, latent_dim),
            nn.ReLU(),
        )
        self.decoder = nn.Sequential(
            nn.Linear(latent_dim, 8),
            nn.ReLU(),
            nn.Linear(8, input_dim),
        )

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        return self.decoder(self.encoder(x))


def train(
    model: Autoencoder, data: torch.Tensor, epochs: int = 200, lr: float = 1e-3
) -> list[float]:
    """Trains model on data — assumed to be "normal" traffic only, standard
    practice for autoencoder-based anomaly detection — returning the loss
    history for observability and tests.
    """
    optimizer = torch.optim.Adam(model.parameters(), lr=lr)
    loss_fn = nn.MSELoss()

    history: list[float] = []
    model.train()
    for _ in range(epochs):
        optimizer.zero_grad()
        reconstruction = model(data)
        loss = loss_fn(reconstruction, data)
        loss.backward()
        optimizer.step()
        history.append(float(loss.item()))
    return history


def reconstruction_error(model: Autoencoder, data: torch.Tensor) -> torch.Tensor:
    """Per-sample mean squared reconstruction error: the raw anomaly signal
    before normalization to a [0, 1] threat score.
    """
    model.eval()
    with torch.no_grad():
        reconstruction = model(data)
        return torch.mean((reconstruction - data) ** 2, dim=-1)


def export_onnx(
    model: Autoencoder, output_path: Path, input_dim: int = FEATURE_VECTOR_SIZE
) -> None:
    """Exports model to ONNX for production inference via onnxruntime."""
    model.eval()
    dummy_input = torch.zeros(1, input_dim)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    batch = torch.export.Dim("batch")
    torch.onnx.export(
        model,
        (dummy_input,),
        str(output_path),
        input_names=["features"],
        output_names=["reconstruction"],
        dynamic_shapes={"x": {0: batch}},
        opset_version=18,
    )
