import json
from pathlib import Path

import numpy as np
import torch

from scripts.generate_synthetic_dataset import generate
from sentinel_ai.model import Autoencoder, export_onnx, reconstruction_error, train
from sentinel_ai.server import ScoringEngine


def test_train_export_infer_pipeline_scores_anomalies_higher(tmp_path: Path):
    normal, anomalous = generate(num_normal=300, num_anomalous=60, seed=7)

    model = Autoencoder(input_dim=normal.shape[1])
    train(model, torch.from_numpy(normal), epochs=150, lr=1e-2)

    errors = reconstruction_error(model, torch.from_numpy(normal)).numpy()
    reference_error = float(np.percentile(errors, 99))

    model_path = tmp_path / "autoencoder.onnx"
    export_onnx(model, model_path, input_dim=normal.shape[1])
    (tmp_path / "autoencoder.norm.json").write_text(
        json.dumps({"reference_error": reference_error})
    )

    engine = ScoringEngine(str(model_path))

    normal_scores = [engine.score_features(list(row)) for row in normal]
    anomalous_scores = [engine.score_features(list(row)) for row in anomalous]

    assert np.mean(anomalous_scores) > np.mean(normal_scores)


def test_scoring_engine_defaults_reference_error_when_sidecar_missing(tmp_path: Path):
    model = Autoencoder(input_dim=12)
    model_path = tmp_path / "autoencoder_no_sidecar.onnx"
    export_onnx(model, model_path, input_dim=12)

    engine = ScoringEngine(str(model_path))
    score = engine.score_features([0.0] * 12)

    assert 0.0 <= score <= 1.0
