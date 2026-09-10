import asyncio
import json
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from fastapi.testclient import TestClient
from prometheus_client import REGISTRY

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


def test_metrics_endpoint_exposes_score_latency_and_http_metrics(tmp_path: Path):
    # sentinel5g_ai_score_latency_seconds_count (like every prometheus_client
    # metric) lives in a process-global registry, shared across every
    # TestClient/app created in this file's other tests -- diff before/after
    # rather than asserting an absolute count.
    before = REGISTRY.get_sample_value("sentinel5g_ai_score_latency_seconds_count") or 0.0

    client = TestClient(create_app(_real_engine(tmp_path)))
    client.post("/v1/score", json={"protocol": "SIP", "destPort": 5060})

    resp = client.get("/metrics")

    assert resp.status_code == 200
    # sentinel_ai.metrics.SCORE_LATENCY_SECONDS -- confirms ScoringEngine.
    # score_event's timing block actually reaches the process-global
    # registry Instrumentator.expose() serves, not just that it doesn't
    # raise. Recorded synchronously inside the request handler (unlike
    # prometheus_fastapi_instrumentator's own per-request metric below), so
    # an exact before/after diff is reliable here.
    after = REGISTRY.get_sample_value("sentinel5g_ai_score_latency_seconds_count")
    assert after == before + 1
    # prometheus_fastapi_instrumentator's default per-request metric family
    # -- confirms Instrumentator().instrument(app) is actually wired in,
    # not just that /metrics happens to return 200. A metric's HELP/TYPE
    # lines are emitted as soon as it's registered, regardless of whether
    # any request has been recorded under it yet, so this doesn't depend on
    # this specific request having already been grouped by handler by the
    # time /metrics is fetched -- unlike per-route sample values, which
    # this library records via a response background task whose completion
    # isn't guaranteed synchronous with TestClient.
    assert "# TYPE http_request_duration_seconds histogram" in resp.text


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
        nats_allow_unauthenticated=True,
        metrics_addr="0.0.0.0:9090",
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


def _nats_events_total(outcome: str) -> float:
    # get_sample_value returns None (not 0.0) for a label combo never
    # incremented yet -- callers diff before/after rather than asserting an
    # absolute value, since NATS_EVENTS_TOTAL is a module-global shared
    # across every test in this file/process.
    return REGISTRY.get_sample_value("sentinel5g_ai_nats_events_total", {"outcome": outcome}) or 0.0


def test_run_nats_worker_handler_acks_on_success(tmp_path: Path):
    js = MagicMock()
    js.publish = AsyncMock()
    kwargs = asyncio.run(_capture_subscribed_handler(_settings(), _real_engine(tmp_path), js))
    handler = kwargs["cb"]
    before = _nats_events_total("scored")

    msg = _fake_msg(json.dumps({"protocol": "SIP", "destPort": 5060}).encode())
    asyncio.run(handler(msg))

    js.publish.assert_awaited_once()
    msg.ack.assert_awaited_once()
    msg.nak.assert_not_awaited()
    msg.term.assert_not_awaited()
    assert _nats_events_total("scored") == before + 1


def test_run_nats_worker_handler_terms_malformed_payload(tmp_path: Path):
    js = MagicMock()
    js.publish = AsyncMock()
    kwargs = asyncio.run(_capture_subscribed_handler(_settings(), _real_engine(tmp_path), js))
    handler = kwargs["cb"]
    before = _nats_events_total("malformed")

    msg = _fake_msg(b"not valid json")
    asyncio.run(handler(msg))

    msg.term.assert_awaited_once()
    msg.ack.assert_not_awaited()
    msg.nak.assert_not_awaited()
    js.publish.assert_not_awaited()
    assert _nats_events_total("malformed") == before + 1


def test_run_nats_worker_handler_naks_on_publish_failure(tmp_path: Path):
    js = MagicMock()
    js.publish = AsyncMock(side_effect=RuntimeError("nats publish failed"))
    kwargs = asyncio.run(_capture_subscribed_handler(_settings(), _real_engine(tmp_path), js))
    handler = kwargs["cb"]
    before = _nats_events_total("publish_failed")

    msg = _fake_msg(json.dumps({"protocol": "GTP-U", "destPort": 2152}).encode())
    asyncio.run(handler(msg))

    msg.nak.assert_awaited_once()
    msg.ack.assert_not_awaited()
    assert _nats_events_total("publish_failed") == before + 1
    msg.term.assert_not_awaited()
