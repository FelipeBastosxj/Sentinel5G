"""Tests the AI-engine consumer's handling of batched vs single event
messages (parse_event_batch) and the threat-result it builds
(build_threat_result). These are the consumer half of the Phase 4 NATS
batching change: the Go Publisher may now coalesce events into one message,
and the worker must handle both shapes without a live bus.
"""

from __future__ import annotations

import json
import tempfile
from pathlib import Path

import numpy as np
import pytest
import torch

from scripts.build_real_dataset import _REAL_DATASET_DIR, build as build_real
from sentinel_ai.model import Autoencoder, export_onnx, reconstruction_error, train
from sentinel_ai.server import ScoringEngine, build_threat_result, parse_event_batch


def _event(teid: int = 0x4D84) -> dict:
    return {
        "eventId": f"evt-{teid}",
        "namespace": "telecom-core",
        "podName": "upf-0",
        "sourceIp": "10.0.0.1",
        "protocol": "GTP-U",
        "destPort": 2152,
        "payloadSize": 120,
        "teid": teid,
        "tunnelRatePerSecond": 25.0,
    }


def test_parse_event_batch_accepts_single_object():
    # The pre-batching wire shape: a lone JSON object becomes a 1-element list.
    out = parse_event_batch(json.dumps(_event()).encode())
    assert isinstance(out, list) and len(out) == 1
    assert out[0]["teid"] == 0x4D84


def test_parse_event_batch_accepts_array():
    batch = [_event(1), _event(2), _event(3)]
    out = parse_event_batch(json.dumps(batch).encode())
    assert len(out) == 3
    assert [e["teid"] for e in out] == [1, 2, 3]


def test_parse_event_batch_rejects_neither_shape():
    for bad in [b"42", b'"a string"', b"[1, 2, 3]", b"null"]:
        with pytest.raises((ValueError, TypeError)):
            parse_event_batch(bad)


def test_parse_event_batch_rejects_malformed_json():
    with pytest.raises(json.JSONDecodeError):
        parse_event_batch(b"{not json")


@pytest.fixture(scope="module")
def engine():
    with tempfile.TemporaryDirectory() as tmp:
        normal, _ = build_real(_REAL_DATASET_DIR)
        model = Autoencoder(input_dim=normal.shape[1])
        torch.manual_seed(42)
        train(model, torch.from_numpy(normal), epochs=50, lr=1e-2)
        ref = float(
            np.percentile(reconstruction_error(model, torch.from_numpy(normal)).numpy(), 99)
        )
        path = Path(tmp) / "m.onnx"
        export_onnx(model, path, input_dim=normal.shape[1])
        (Path(tmp) / "m.norm.json").write_text(json.dumps({"reference_error": ref}))
        yield ScoringEngine(str(path))


def test_build_threat_result_shape_and_teid_passthrough(engine):
    result = build_threat_result(_event(0x5FFF), engine)
    assert result["teid"] == 0x5FFF  # carried through for per-tunnel mitigation
    assert result["sourceIp"] == "10.0.0.1"
    assert result["namespace"] == "telecom-core"
    assert 0.0 <= result["score"] <= 1.0
    assert result["model"] == "autoencoder-v1"
    assert result["sourceEventId"] == "evt-24575"


def test_a_batch_scores_every_event(engine):
    # The property a batching consumer must hold: one result per event in the
    # batch, each with its own TEID -- not one result for the whole message.
    batch = parse_event_batch(json.dumps([_event(1), _event(2)]).encode())
    results = [build_threat_result(p, engine) for p in batch]
    assert [r["teid"] for r in results] == [1, 2]
