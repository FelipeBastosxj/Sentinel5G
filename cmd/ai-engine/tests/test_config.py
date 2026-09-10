import pytest

from sentinel_ai.config import Settings, require_nats_credentials_if_unauthenticated_disallowed


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
        nats_allow_unauthenticated=False,
        metrics_addr="0.0.0.0:9090",
    )
    base.update(overrides)
    return Settings(**base)


def test_refuses_nats_mode_with_no_credentials_and_not_opted_in():
    with pytest.raises(RuntimeError):
        require_nats_credentials_if_unauthenticated_disallowed(_settings())


def test_allows_nats_mode_when_explicitly_opted_in():
    require_nats_credentials_if_unauthenticated_disallowed(_settings(nats_allow_unauthenticated=True))


@pytest.mark.parametrize(
    "field",
    ["nats_creds_file", "nats_username", "nats_tls_cert_file", "nats_tls_ca_file"],
)
def test_allows_nats_mode_when_any_credential_is_set(field):
    require_nats_credentials_if_unauthenticated_disallowed(_settings(**{field: "set"}))


def test_http_mode_is_never_checked():
    # HTTP mode never calls nats.connect, so it should never be blocked by
    # this check regardless of credentials/allow_unauthenticated.
    require_nats_credentials_if_unauthenticated_disallowed(_settings(mode="http"))
