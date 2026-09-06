"""Inference server for the AI engine (Layer 3).

Exposes a synchronous HTTP scoring endpoint (`POST /v1/score`) used for
direct integration and tests, and — when AI_ENGINE_MODE=nats — an asyncio
worker loop that consumes normalized events from NATS JetStream and
republishes scored ThreatScoreEvents, matching pkg/events.Bus's subjects and
JSON schema on the Go side. Both entry points share the same ScoringEngine
so scoring logic is defined exactly once.
"""

from __future__ import annotations

import asyncio
import json
import logging
import time
import uuid
from pathlib import Path

import numpy as np
import onnxruntime as ort
from fastapi import FastAPI
from pydantic import BaseModel

from .config import Settings, load_settings
from .features import NormalizedEvent, extract_features

logger = logging.getLogger("sentinel_ai.server")


class ScoringEngine:
    """Loads a trained autoencoder (ONNX) plus its normalization sidecar and
    turns raw NormalizedEvents into a threat score in [0.0, 1.0].
    """

    def __init__(self, model_path: str):
        self._session = ort.InferenceSession(model_path, providers=["CPUExecutionProvider"])
        self._input_name = self._session.get_inputs()[0].name
        self._reference_error = self._load_reference_error(model_path)

    @staticmethod
    def _load_reference_error(model_path: str) -> float:
        norm_path = Path(model_path).with_suffix("").with_suffix(".norm.json")
        if not norm_path.exists():
            logger.warning(
                "no normalization sidecar at %s; defaulting reference_error=1.0", norm_path
            )
            return 1.0
        norm = json.loads(norm_path.read_text())
        return float(norm["reference_error"])

    def score_features(self, features: list[float]) -> float:
        batch = np.asarray([features], dtype=np.float32)
        (reconstruction,) = self._session.run(None, {self._input_name: batch})
        error = float(np.mean((reconstruction - batch) ** 2))
        normalized = error / (self._reference_error * 4.0)
        return max(0.0, min(1.0, normalized))

    def score_event(self, event: NormalizedEvent) -> float:
        return self.score_features(extract_features(event))


class ScoreRequest(BaseModel):
    protocol: str = "UNKNOWN"
    destPort: int = 0
    payloadSize: int = 0
    ratePerSecond: float = 0.0
    malformed: bool = False


class ScoreResponse(BaseModel):
    score: float
    model: str = "autoencoder-v1"


def create_app(engine: ScoringEngine) -> FastAPI:
    app = FastAPI(title="Sentinel5G AI Engine", version="0.1.0")

    @app.get("/healthz")
    def healthz() -> dict:
        return {"status": "ok"}

    @app.post("/v1/score", response_model=ScoreResponse)
    def score(request: ScoreRequest) -> ScoreResponse:
        event = NormalizedEvent.from_dict(request.model_dump())
        return ScoreResponse(score=engine.score_event(event))

    return app


async def _ensure_stream(js, settings: Settings) -> None:
    """Mirrors pkg/events/nats.go's ensureStream: creates the shared
    JetStream stream if this is the first service to connect, so the AI
    engine does not depend on the Go operator having started first.
    """
    import nats  # imported lazily so HTTP-only deployments stay light

    try:
        await js.stream_info(settings.nats_stream_name)
    except nats.js.errors.NotFoundError:
        await js.add_stream(
            name=settings.nats_stream_name,
            subjects=[settings.nats_events_subject, settings.nats_threats_subject],
            max_age=24 * 60 * 60,  # 24h, in seconds (nats-py's StreamConfig.max_age unit)
        )


# Bounds JetStream redelivery of an event whose handler keeps failing (e.g. a
# transient NATS publish error) — mirrors pkg/events.maxThreatScoreDeliveries
# on the Go side. Without a cap, a failing message NAKs forever.
_MAX_EVENT_DELIVERIES = 5


def _nats_connect_kwargs(settings: Settings) -> dict:
    """Builds nats.connect() auth/TLS kwargs from settings, all optional —
    mirrors pkg/events.Connect on the Go side. Empty settings reproduce the
    historical unauthenticated connection used for local dev; see
    docs/integrations.md for why that's a real risk outside of it.
    """
    kwargs: dict = {}
    if settings.nats_creds_file:
        kwargs["user_credentials"] = settings.nats_creds_file
    elif settings.nats_username:
        kwargs["user"] = settings.nats_username
        kwargs["password"] = settings.nats_password

    if settings.nats_tls_ca_file or settings.nats_tls_cert_file:
        import ssl

        ctx = ssl.create_default_context(cafile=settings.nats_tls_ca_file or None)
        if settings.nats_tls_cert_file:
            ctx.load_cert_chain(settings.nats_tls_cert_file, settings.nats_tls_key_file)
        kwargs["tls"] = ctx

    return kwargs


async def run_nats_worker(settings: Settings, engine: ScoringEngine) -> None:
    """Consumes NATS_EVENTS_SUBJECT and republishes NATS_THREATS_SUBJECT."""
    import nats  # imported lazily so HTTP-only deployments stay light
    from nats.js.api import ConsumerConfig

    nc = await nats.connect(settings.nats_url, **_nats_connect_kwargs(settings))
    js = nc.jetstream()
    await _ensure_stream(js, settings)

    async def handler(msg) -> None:
        try:
            payload = json.loads(msg.data)
            event = NormalizedEvent.from_dict(payload)
        except (json.JSONDecodeError, ValueError, TypeError):
            # Malformed payload will never parse on redelivery either; term
            # (not nak) drops it instead of retrying forever.
            logger.warning("dropping malformed event on %s", settings.nats_events_subject)
            await msg.term()
            return

        try:
            threat_score = engine.score_event(event)

            result = {
                "sourceEventId": payload.get("eventId", str(uuid.uuid4())),
                "namespace": payload.get("namespace", ""),
                "podName": payload.get("podName", ""),
                "sourceIp": payload.get("sourceIp", ""),
                "score": threat_score,
                "model": "autoencoder-v1",
                "detectedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            }
            await js.publish(settings.nats_threats_subject, json.dumps(result).encode())
            await msg.ack()
        except Exception:
            logger.exception("failed to score/publish event, will retry")
            await msg.nak()

    await js.subscribe(
        settings.nats_events_subject,
        durable="sentinel5g-ai-engine",
        cb=handler,
        config=ConsumerConfig(max_deliver=_MAX_EVENT_DELIVERIES),
    )
    logger.info(
        "subscribed to %s, publishing to %s",
        settings.nats_events_subject,
        settings.nats_threats_subject,
    )

    while True:
        await asyncio.sleep(3600)


def main() -> None:
    logging.basicConfig(level=logging.INFO)
    settings = load_settings()
    engine = ScoringEngine(settings.model_path)

    if settings.mode == "nats":
        asyncio.run(run_nats_worker(settings, engine))
        return

    import uvicorn

    host, _, port = settings.http_addr.partition(":")
    # Binding all interfaces is deliberate — this is a network-facing service
    # meant to be reached from inside the cluster. /v1/score has no
    # authentication of its own; restrict who can reach it with a
    # NetworkPolicy (see docs/integrations.md).
    uvicorn.run(create_app(engine), host=host or "0.0.0.0", port=int(port or 8090))  # nosec B104


if __name__ == "__main__":
    main()
