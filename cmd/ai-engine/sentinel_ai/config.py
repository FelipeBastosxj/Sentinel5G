"""Environment-driven configuration for cmd/ai-engine, mirroring the
AI_ENGINE_* / NATS_* variables documented in .env.example at the repo root."""

from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    http_addr: str
    mode: str  # "http" (default, serves POST /v1/score) or "nats" (worker loop)
    model_path: str
    feature_vector_size: int
    nats_url: str
    nats_stream_name: str
    nats_events_subject: str
    nats_threats_subject: str


def load_settings() -> Settings:
    return Settings(
        http_addr=os.getenv("AI_ENGINE_HTTP_ADDR", "0.0.0.0:8090"),
        mode=os.getenv("AI_ENGINE_MODE", "http"),
        model_path=os.getenv("MODEL_PATH", "./models/autoencoder.onnx"),
        feature_vector_size=int(os.getenv("FEATURE_VECTOR_SIZE", "12")),
        nats_url=os.getenv("NATS_URL", "nats://localhost:4222"),
        nats_stream_name=os.getenv("NATS_STREAM_NAME", "SENTINEL5G"),
        nats_events_subject=os.getenv("NATS_EVENTS_SUBJECT", "sentinel5g.events.normalized"),
        nats_threats_subject=os.getenv("NATS_THREATS_SUBJECT", "sentinel5g.threats.scored"),
    )
