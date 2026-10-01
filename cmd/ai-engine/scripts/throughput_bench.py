"""Measures how many events per second one AI-engine replica sustains.

ROADMAP.md Phase 4: score LATENCY is instrumented (SCORE_LATENCY_SECONDS),
but scoring is one ONNX call per event with no batching, and nothing measured
how many events per second a single replica holds -- which is what decides
the replica count in any sizing guide. This measures it, on this host,
single process, single thread, and also measures what batching WOULD buy,
since "one ONNX call per event" is the ceiling this quantifies.

Three paths, because the answer depends on where the work is:

1. score_features     -- ONNX inference only, the pure model cost.
2. score_event        -- extract_features + inference, one real event.
3. from_dict + score  -- the full wire path a NATS worker runs per message:
                         parse the JSON-shaped dict, build the event, extract,
                         infer. This is the number that sizes a deployment.

Plus a batched inference measurement: the same N feature vectors in ONE ONNX
run, to show the headroom a batching consumer would unlock over the
one-call-per-event path the engine uses today.

Run from cmd/ai-engine/: python scripts/throughput_bench.py
"""

from __future__ import annotations

import argparse
import json
import tempfile
import time
from pathlib import Path

import numpy as np
import torch

from scripts.build_real_dataset import _REAL_DATASET_DIR, build as build_real
from sentinel_ai.features import NormalizedEvent, extract_features
from sentinel_ai.model import Autoencoder, export_onnx, reconstruction_error, train
from sentinel_ai.server import ScoringEngine


def _train_and_export(out_dir: Path, seed: int) -> str:
    normal, _ = build_real(_REAL_DATASET_DIR)
    model = Autoencoder(input_dim=normal.shape[1])
    torch.manual_seed(seed)
    train(model, torch.from_numpy(normal), epochs=200, lr=1e-2)
    train_err = reconstruction_error(model, torch.from_numpy(normal)).numpy()
    reference_error = float(np.percentile(train_err, 99))

    model_path = out_dir / "bench.onnx"
    export_onnx(model, model_path, input_dim=normal.shape[1])
    (out_dir / "bench.norm.json").write_text(json.dumps({"reference_error": reference_error}))
    return str(model_path)


def _sample_event_dict() -> dict:
    # A realistic GTP-U event dict, the shape pkg/events publishes on NATS.
    return {
        "protocol": "GTP-U",
        "destPort": 2152,
        "payloadSize": 120,
        "ratePerSecond": 0.0,
        "malformed": False,
        "observedAt": "2026-10-01T12:00:00Z",
        "teid": 0x4D84,
        "tunnelRatePerSecond": 25.0,
    }


def _rate(fn, n: int, warmup: int) -> float:
    for _ in range(warmup):
        fn()
    start = time.perf_counter()
    for _ in range(n):
        fn()
    elapsed = time.perf_counter() - start
    return n / elapsed


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--iterations", type=int, default=20000)
    parser.add_argument("--warmup", type=int, default=2000)
    parser.add_argument("--batch", type=int, default=256)
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()

    with tempfile.TemporaryDirectory() as tmp:
        model_path = _train_and_export(Path(tmp), args.seed)
        engine = ScoringEngine(model_path)

        features = extract_features(NormalizedEvent.from_dict(_sample_event_dict()))
        event_dict = _sample_event_dict()

        features_rate = _rate(lambda: engine.score_features(features), args.iterations, args.warmup)
        event_rate = _rate(
            lambda: engine.score_event(NormalizedEvent.from_dict(event_dict)),
            args.iterations,
            args.warmup,
        )
        wire_rate = _rate(
            lambda: engine.score_features(extract_features(NormalizedEvent.from_dict(event_dict))),
            args.iterations,
            args.warmup,
        )

        # Batched inference: N vectors in one ONNX run, the headroom a
        # batching consumer would unlock. Measured as events/sec = batch /
        # per-batch-time.
        batch = np.asarray([features] * args.batch, dtype=np.float32)
        input_name = engine._session.get_inputs()[
            0
        ].name  # noqa: SLF001 (bench, same package intent)

        def run_batch():
            engine._session.run(None, {input_name: batch})  # noqa: SLF001

        batch_runs = max(1, args.iterations // args.batch)
        for _ in range(max(1, args.warmup // args.batch)):
            run_batch()
        start = time.perf_counter()
        for _ in range(batch_runs):
            run_batch()
        batched_rate = (batch_runs * args.batch) / (time.perf_counter() - start)

        result = {
            "host_note": "single process, single thread, CPUExecutionProvider",
            "iterations": args.iterations,
            "events_per_second": {
                "inference_only": round(features_rate),
                "score_event_extract_plus_infer": round(event_rate),
                "full_wire_path_from_dict": round(wire_rate),
                "batched_inference_batch_%d" % args.batch: round(batched_rate),
            },
            "batching_speedup_vs_per_event_inference": round(batched_rate / features_rate, 1),
        }
        print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
