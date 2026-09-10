from sentinel_ai.healthcheck import _target_url


def test_target_url_defaults_to_http_healthz(monkeypatch):
    monkeypatch.delenv("AI_ENGINE_MODE", raising=False)
    monkeypatch.delenv("AI_ENGINE_HTTP_ADDR", raising=False)
    assert _target_url() == "http://127.0.0.1:8090/healthz"


def test_target_url_http_mode_respects_custom_port(monkeypatch):
    monkeypatch.setenv("AI_ENGINE_MODE", "http")
    monkeypatch.setenv("AI_ENGINE_HTTP_ADDR", "0.0.0.0:9999")
    assert _target_url() == "http://127.0.0.1:9999/healthz"


def test_target_url_nats_mode_checks_metrics_port_instead(monkeypatch):
    # NATS worker mode has no HTTP app / /healthz at all (see server.py) --
    # the dedicated Prometheus /metrics server main() starts for that mode
    # is the closest available "is this process alive" signal.
    monkeypatch.setenv("AI_ENGINE_MODE", "nats")
    monkeypatch.setenv("AI_ENGINE_METRICS_ADDR", "0.0.0.0:9091")
    assert _target_url() == "http://127.0.0.1:9091/metrics"


def test_target_url_nats_mode_defaults_to_9090(monkeypatch):
    monkeypatch.setenv("AI_ENGINE_MODE", "nats")
    monkeypatch.delenv("AI_ENGINE_METRICS_ADDR", raising=False)
    assert _target_url() == "http://127.0.0.1:9090/metrics"
