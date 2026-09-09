"""Prometheus metrics shared between the HTTP and NATS-worker modes (see
server.py) — defined once here so both entry points report under the same
metric names instead of each defining their own.

HTTP mode gets these (plus automatic per-endpoint request metrics) on its
existing app via prometheus_fastapi_instrumentator, exposed at GET /metrics
on the same port as /v1/score. NATS mode has no HTTP app at all, so main()
starts a small dedicated prometheus_client HTTP server
(AI_ENGINE_METRICS_ADDR) instead — see server.py.
"""

from __future__ import annotations

from prometheus_client import Counter, Histogram

SCORE_LATENCY_SECONDS = Histogram(
    "sentinel5g_ai_score_latency_seconds",
    "Time spent scoring a single NormalizedEvent (ScoringEngine.score_event), "
    "regardless of which mode (HTTP or NATS worker) triggered it.",
)

# Labeled by outcome rather than three separate counters, so a single
# `sum by (outcome) (...)` query in Prometheus shows the full breakdown of
# what run_nats_worker's handler did with its events -- mirrors the three
# branches in server.py's handler (term/ack/nak) 1:1:
#   "malformed"      -- msg.term(): payload didn't parse as JSON/a valid event.
#   "scored"         -- msg.ack(): scored and republished successfully.
#   "publish_failed" -- msg.nak(): scoring or NATS publish raised, will retry.
NATS_EVENTS_TOTAL = Counter(
    "sentinel5g_ai_nats_events_total",
    "NATS worker events processed, by outcome.",
    ["outcome"],
)
