"""Tests for scripts/calibrate.py, the one-command local calibration.

Run against the committed application-mix sample (real-dataset-v4), so the
model is calibrated on real traffic exactly as an operator's would be. What
must hold: a model is written only when it passes its own held-out bound,
and the files written are the ones the production server scores with -- the
exported ONNX plus its sidecar, loaded by sentinel_ai.server.ScoringEngine,
keep held-out benign traffic below every sensitivity tier.
"""

from __future__ import annotations

import json
import random
from pathlib import Path

import numpy as np
import pytest

from scripts.calibrate import _rows, calibrate

SAMPLE = (
    Path(__file__).resolve().parents[3]
    / "docs"
    / "paper-data"
    / "real-dataset-v4"
    / "app_mix_sample.pcap"
)


@pytest.fixture(scope="module")
def accepted(tmp_path_factory):
    out = tmp_path_factory.mktemp("calibrated")
    # 18k held-out packets bound the FPR at ~1.7e-4 even with zero false
    # positives, so the acceptance bar is set where this sample can clear it.
    report = calibrate([SAMPLE], out, rows=2_000, max_fpr_upper_bound=1e-3)
    return out, report


def test_an_accepted_calibration_writes_the_model_and_its_report(accepted):
    out, report = accepted
    assert report["accepted"]
    assert report["false_positives_on_held_out"]["high"]["false_positives"] == 0
    assert (out / "autoencoder.onnx").exists()
    norm = json.loads((out / "autoencoder.norm.json").read_text())
    assert norm["reference_error"] == pytest.approx(report["reference_error"])
    assert json.loads((out / "calibration-report.json").read_text())["accepted"]


def test_a_refused_calibration_writes_no_model(tmp_path):
    report = calibrate([SAMPLE], tmp_path, rows=2_000, max_fpr_upper_bound=1e-6)
    assert not report["accepted"]
    assert not (tmp_path / "autoencoder.onnx").exists()
    assert not (tmp_path / "autoencoder.norm.json").exists()
    assert (tmp_path / "calibration-report.json").exists()


def test_the_production_server_scores_benign_traffic_below_every_tier(accepted):
    from sentinel_ai.server import ScoringEngine

    out, report = accepted
    engine = ScoringEngine(str(out / "autoencoder.onnx"))

    rows = _rows([SAMPLE], random.Random(0))[-200:]
    served = np.asarray([engine.score_features(r.tolist()) for r in rows])
    assert served.shape == (200,)
    assert np.all((served >= 0.0) & (served <= 1.0))
    # Held-out benign traffic stays below the most sensitive tier.
    assert np.all(served < 0.68)


def test_the_report_states_the_price_of_calibration(accepted):
    _, report = accepted
    detection = report["detection_on_committed_attacks"]
    assert detection["real_storm_udpflood"]["flagged_high"] == 1.0
    assert "multi_ue_one_flooding" in detection
