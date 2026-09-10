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

    # Authentication/TLS for the NATS bus, all optional — empty preserves the
    # historical unauthenticated connection for local dev. See
    # pkg/events.Config on the Go side and docs/integrations.md for why an
    # unauthenticated bus is a real risk outside of local dev: anything that
    # can reach it can publish a forged NormalizedEvent/ThreatScoreEvent.
    nats_creds_file: str
    nats_username: str
    nats_password: str
    nats_tls_ca_file: str
    nats_tls_cert_file: str
    nats_tls_key_file: str

    # Must be explicitly true to run mode="nats" against a NATS bus with none
    # of the fields above set. Mirrors pkg/config.OperatorConfig's
    # NATSAllowUnauthenticated on the Go side -- both processes talk to the
    # same bus, so one can't be locked down while the other stays open.
    nats_allow_unauthenticated: bool

    # Where the NATS-worker mode's dedicated Prometheus /metrics server
    # binds (see server.py's main()) -- HTTP mode doesn't use this, it
    # exposes /metrics on http_addr instead via
    # prometheus_fastapi_instrumentator, since it already has an app.
    metrics_addr: str


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
        nats_creds_file=os.getenv("NATS_CREDENTIALS_FILE", ""),
        nats_username=os.getenv("NATS_USERNAME", ""),
        nats_password=os.getenv("NATS_PASSWORD", ""),
        nats_tls_ca_file=os.getenv("NATS_TLS_CA_FILE", ""),
        nats_tls_cert_file=os.getenv("NATS_TLS_CERT_FILE", ""),
        nats_tls_key_file=os.getenv("NATS_TLS_KEY_FILE", ""),
        nats_allow_unauthenticated=_getenv_bool("NATS_ALLOW_UNAUTHENTICATED", False),
        metrics_addr=os.getenv("AI_ENGINE_METRICS_ADDR", "0.0.0.0:9090"),
    )


def _getenv_bool(key: str, fallback: bool) -> bool:
    value = os.getenv(key)
    if not value:
        return fallback
    return value.strip().lower() in ("1", "t", "true", "yes")


def require_nats_credentials_if_unauthenticated_disallowed(settings: Settings) -> None:
    """Refuses to proceed if mode="nats" would connect to NATS with no
    credentials/TLS configured and nats_allow_unauthenticated wasn't set --
    anything able to reach an unauthenticated NATS_URL can forge a
    NormalizedEvent/ThreatScoreEvent (see docs/integrations.md's "Securing
    the NATS message bus"). Raises RuntimeError; HTTP mode never calls
    nats.connect at all, so it isn't checked here."""
    if settings.mode != "nats" or settings.nats_allow_unauthenticated:
        return

    has_credentials = bool(
        settings.nats_creds_file
        or settings.nats_username
        or settings.nats_tls_cert_file
        or settings.nats_tls_ca_file
    )
    if not has_credentials:
        raise RuntimeError(
            "refusing to start AI_ENGINE_MODE=nats with an unauthenticated NATS connection: "
            "set NATS_CREDENTIALS_FILE, NATS_USERNAME+NATS_PASSWORD, or "
            "NATS_TLS_CERT_FILE+NATS_TLS_KEY_FILE, or set NATS_ALLOW_UNAUTHENTICATED=true "
            'to opt into it explicitly (see docs/integrations.md\'s "Securing the NATS message bus")'
        )
