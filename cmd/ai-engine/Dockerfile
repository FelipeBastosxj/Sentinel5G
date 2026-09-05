FROM python:3.11-slim AS base

WORKDIR /app

COPY pyproject.toml ./
COPY sentinel_ai ./sentinel_ai
COPY scripts ./scripts

RUN pip install --no-cache-dir .

COPY models/.gitkeep ./models/.gitkeep

ENV AI_ENGINE_HTTP_ADDR=0.0.0.0:8090 \
    AI_ENGINE_MODE=http \
    MODEL_PATH=/app/models/autoencoder.onnx

EXPOSE 8090

ENTRYPOINT ["python", "-m", "sentinel_ai.server"]
