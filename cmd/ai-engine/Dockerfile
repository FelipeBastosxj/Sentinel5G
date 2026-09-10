# Pinned to the multi-arch manifest-list digest (linux/amd64 + linux/arm64,
# see the root Dockerfile's matching comment for the digest-vs-tag rationale
# and .github/dependabot.yml for how this stays current).
FROM python:3.11-slim@sha256:9534e5a8e315485d4061ed659af0fd78a284c015f9b73661b41d6bab25604534 AS base

WORKDIR /app

# python:3.11-slim bundles its own pip/setuptools/wheel via ensurepip, which
# lag behind on CVE fixes (e.g. wheel<0.46.2, jaraco.context<6.1.0 pulled in
# transitively by setuptools) -- upgrade them before installing the app so
# the final image doesn't carry known-fixed vulnerabilities forward.
RUN pip install --no-cache-dir --upgrade pip setuptools wheel

COPY pyproject.toml ./
COPY sentinel_ai ./sentinel_ai
COPY scripts ./scripts

RUN pip install --no-cache-dir . \
    # pip/setuptools/wheel are build-time only -- `python -m
    # sentinel_ai.server` never imports them, and pip vendors its own
    # internal copies of older packages (e.g. an old setuptools/msgpack)
    # that Trivy flags even on pip's own latest release, with no real
    # exposure since our app never touches pip._vendor. Removing them
    # drops that noise and shrinks the image.
    && pip uninstall -y pip setuptools wheel

COPY models/.gitkeep ./models/.gitkeep

ENV AI_ENGINE_HTTP_ADDR=0.0.0.0:8090 \
    AI_ENGINE_MODE=http \
    MODEL_PATH=/app/models/autoencoder.onnx

RUN useradd --system --no-create-home --uid 65532 sentinel5g \
    && chown -R sentinel5g /app
USER sentinel5g

EXPOSE 8090

# sentinel_ai/healthcheck.py picks between /healthz (HTTP mode) and the
# dedicated metrics server's /metrics (NATS worker mode, which has no HTTP
# app of its own) based on AI_ENGINE_MODE -- see that module's docstring.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["python", "-m", "sentinel_ai.healthcheck"]

ENTRYPOINT ["python", "-m", "sentinel_ai.server"]
