import asyncio
import json
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from fastapi.testclient import TestClient

from sentinel_ai.config import Settings
from sentinel_ai.model import Autoencoder, export_onnx
from sentinel_ai.server import ScoringEngine, create_app, run_nats_worker


def _real_engine(tmp_path: Path) -> ScoringEngine:
    # A real (untrained) exported model is enough here -- these tests exercise
    # the HTTP/NATS wrapping layer around ScoringEngine, not scoring accuracy
    # (see tests/test_inference.py for that).
    model = Autoencoder(input_dim=12)
    model_path = tmp_path / "autoencoder.onnx"
    export_onnx(model, model_path, input_dim=12)
    return ScoringEngine(str(model_path))


def test_healthz(tmp_path: Path):
    client = TestClient(create_app(_real_engine(tmp_path)))
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


def test_score_endpoint_happy_path(tmp_path: Path):
    client = TestClient(create_app(_real_engine(tmp_path)))
    resp = client.post(
        "/v1/score",
        json={
            "protocol": "SIP",
            "destPort": 5060,
            "payloadSize": 256,
            "ratePerSecond": 3000,
            "malformed": False,
        },
    )
    assert resp.status_code == 200
    body = resp.json()
    assert 0.0 <= body["score"] <= 1.0
    assert body["model"] == "autoencoder-v1"


def test_score_endpoint_defaults_for_missing_fields(tmp_path: Path):
    # ScoreRequest's fields all carry defaults -- an empty body must still score.
    client = TestClient(create_app(_real_engine(tmp_path)))
    resp = client.post("/v1/score", json={})
    assert resp.status_code == 200
    assert 0.0 <= resp.json()["score"] <= 1.0


def _settings(**overrides) -> Settings:
    base = dict(
        http_addr="0.0.0.0:8090",
        mode="nats",
        model_path="unused-in-these-tests",
        feature_vector_size=12,
        nats_url="nats://127.0.0.1:4222",
        nats_stream_name="SENTINEL5G",
        nats_events_subject="sentinel5g.events.normalized",
        nats_threats_subject="sentinel5g.threats.scored",
        nats_creds_file="",
        nats_username="",
        nats_password="",
        nats_tls_ca_file="",
        nats_tls_cert_file="",
        nats_tls_key_file="",
    )
    base.update(overrides)
    return Settings(**base)


def _fake_msg(data: bytes) -> MagicMock:
    msg = MagicMock()
    msg.data = data
    msg.ack = AsyncMock()
    msg.nak = AsyncMock()
    msg.term = AsyncMock()
    return msg


async def _capture_subscribed_handler(settings: Settings, engine: ScoringEngine, js: MagicMock):
    """Runs run_nats_worker just long enough for it to call js.subscribe(),
    then cancels it (it loops forever afterward by design) and returns the
    handler callback js.subscribe was invoked with."""
    nc = MagicMock()
    nc.jetstream = MagicMock(return_value=js)
    js.stream_info = AsyncMock()  # stream already exists: skip add_stream
    js.subscribe = AsyncMock()

    with patch("nats.connect", new=AsyncMock(return_value=nc)):
        with pytest.raises(asyncio.TimeoutError):
            await asyncio.wait_for(run_nats_worker(settings, engine), timeout=0.2)

    js.subscribe.assert_awaited_once()
    _, kwargs = js.subscribe.call_args
    return kwargs


def test_run_nats_worker_subscribes_with_a_queue_group(tmp_path: Path):
    js = MagicMock()
    kwargs = asyncio.run(_capture_subscribed_handler(_settings(), _real_engine(tmp_path), js))

    # Real load-balancing across replicas needs BOTH durable and queue set to
    # the same group name -- see server.py's _WORKER_GROUP doc comment for
    # why (a durable's deliver_group can't be added after the fact).
    assert kwargs["durable"] == kwargs["queue"]
    assert kwargs["durable"]
    assert callable(kwargs["cb"])


def test_run_nats_worker_handler_acks_on_success(tmp_path: Path):
    js = MagicMock()
    js.publish = AsyncMock()
    kwargs = asyncio.run(_capture_subscribed_handler(_settings(), _real_engine(tmp_path), js))
    handler = kwargs["cb"]

    msg = _fake_msg(json.dumps({"protocol": "SIP", "destPort": 5060}).encode())
    asyncio.run(handler(msg))

    js.publish.assert_awaited_once()
    msg.ack.assert_awaited_once()
    msg.nak.assert_not_awaited()
    msg.term.assert_not_awaited()


def test_run_nats_worker_handler_terms_malformed_payload(tmp_path: Path):
    js = MagicMock()
    js.publish = AsyncMock()
    kwargs = asyncio.run(_capture_subscribed_handler(_settings(), _real_engine(tmp_path), js))
    handler = kwargs["cb"]

    msg = _fake_msg(b"not valid json")
    asyncio.run(handler(msg))

    msg.term.assert_awaited_once()
    msg.ack.assert_not_awaited()
    msg.nak.assert_not_awaited()
    js.publish.assert_not_awaited()


def test_run_nats_worker_handler_naks_on_publish_failure(tmp_path: Path):
    js = MagicMock()
    js.publish = AsyncMock(side_effect=RuntimeError("nats publish failed"))
    kwargs = asyncio.run(_capture_subscribed_handler(_settings(), _real_engine(tmp_path), js))
    handler = kwargs["cb"]

    msg = _fake_msg(json.dumps({"protocol": "GTP-U", "destPort": 2152}).encode())
    asyncio.run(handler(msg))

    msg.nak.assert_awaited_once()
    msg.ack.assert_not_awaited()
    msg.term.assert_not_awaited()
