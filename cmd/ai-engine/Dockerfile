# Pinned to the multi-arch manifest-list digest (linux/amd64 + linux/arm64,
# see the root Dockerfile's matching comment for the digest-vs-tag rationale
# and .github/dependabot.yml for how this stays current).
FROM python:3.14-slim@sha256:caaf356f40667c496d405780745b9ac25771c189a51dfcc42430d531ea09f8a2 AS base

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

RUN useradd --system --no-create-home --uid 65532 --user-group sentinel5g \
    && chown -R 65532:65532 /app
# NUMERIC, not the "sentinel5g" name this used to carry. Kubernetes cannot
# verify that a named user is non-root without reading the image's
# /etc/passwd, which it doesn't do -- so a pod spec with
# `runAsNonRoot: true` refuses to start this image outright, with
# CreateContainerConfigError and "image has non-numeric user (sentinel5g),
# cannot verify user is non-root". That went unnoticed because
# deployments/quickstart/ai-engine.yaml sets no securityContext at all;
# charts/sentinel5g-ai-engine does, as any hardened deployment would.
# The useradd above stays, so the uid still has a real passwd entry inside
# the container.
USER 65532:65532

EXPOSE 8090

# sentinel_ai/healthcheck.py picks between /healthz (HTTP mode) and the
# dedicated metrics server's /metrics (NATS worker mode, which has no HTTP
# app of its own) based on AI_ENGINE_MODE -- see that module's docstring.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["python", "-m", "sentinel_ai.healthcheck"]

ENTRYPOINT ["python", "-m", "sentinel_ai.server"]
