# Roadmap

This document describes the phased delivery plan for **EventStream Observability Engine**.

Each phase has a clear scope and set of deliverables. Phases are sequential but features within a phase may be developed in parallel.

---

## Phase 1 — Foundation

> **Goal:** Establish the core infrastructure, data contracts, and event ingestion pipeline.

- [x] Docker Compose infrastructure (Kafka, Zookeeper, Redis, ClickHouse)
- [x] Canonical Event Model schema and validation
- [x] Shared contracts library (`/shared/contracts`)
- [x] Ingestion Service scaffold (NestJS)
- [x] Ingestion Service — generic webhook endpoint
- [x] Ingestion Service — payload normalization to Canonical Event
- [x] Ingestion Service — publish to Kafka `events.raw`
- [x] `npm run` scripts for all developer workflow commands (`up`, `down`, `logs`, `test`, `reset`)
- [x] `.env.example` with all required variables

---

## Phase 2 — Event Processing

> **Goal:** Consume raw events and build a reliable processing pipeline.

- [x] Processing Service scaffold (NestJS)
- [x] Kafka consumer for `events.raw` topic
- [x] Event enrichment pipeline
- [x] Event validation against Canonical Event Model
- [x] Retry mechanism with exponential backoff
- [x] Dead Letter Queue (DLQ) for failed events — `events.alerts`
- [x] Event publishing to `events.processed` topic
- [x] Correlation ID propagation across services

---

## Phase 3 — Analytics & Storage

> **Goal:** Persist processed events and expose historical analytics.

- [x] ClickHouse schema design for events (`eventstream.events`, MergeTree, 90-day TTL)
- [x] ClickHouse writer from `events.processed` (non-blocking, non-fatal — HARDNESS §10)
- [ ] Aggregation pipelines (delivery rates, error rates, latency p95/p99)
- [x] Historical query API endpoints (`GET /events/recent?limit&channel&source`)
- [x] Event metrics publishing to `events.metrics`
- [ ] Time-series data modeling

---

## Phase 4 — Realtime Gateway

> **Goal:** Expose real-time event streams to the frontend dashboard.

- [x] Realtime Gateway scaffold (NestJS + WebSockets)
- [x] Kafka consumer bridging to WebSocket clients
- [x] Live event stream feed
- [x] Live metrics feed
- [ ] WebSocket authentication
- [x] Redis pub/sub for multi-instance gateway support

---

## Phase 5 — Angular Dashboard

> **Goal:** Build the real-time observability frontend.

- [x] Angular 20 project scaffold with feature-based architecture
- [x] Angular Signals for state management
- [x] WebSocket integration service
- [x] Live Event Stream view
- [x] Live Metrics view (ECharts — bar, doughnut, line/area charts)
- [x] Provider health dashboard
- [x] Event detail panel with full Canonical Event view
- [x] ClickHouse hydration on startup (events persisted across browser close)
- [x] `localStorage` persistence (data survives F5)
- [ ] Correlation ID trace explorer

---

## Phase 6 — Observability Stack

> **Goal:** Full distributed tracing, metrics, and log aggregation.

- [x] OpenTelemetry SDK integration in all backend services
- [x] Prometheus metrics exposure (`/metrics` endpoint) per service
- [x] Loki structured log shipping
- [x] OpenTelemetry Collector in Docker Compose
- [x] Correlation ID propagation in traces and logs
- [x] Grafana dashboards for Prometheus and Loki (stable datasource UIDs provisioned)

---

## Phase 7 — Provider Integrations

> **Goal:** Add first-class integration endpoints for major messaging providers.

- [x] Twilio webhook endpoint with signature validation
- [x] Infobip webhook endpoint with signature validation
- [x] SendGrid webhook endpoint with signature validation
- [x] Custom Integration SDK for generic providers
- [ ] Integration test suite per provider

---

## Phase 8 — Production Hardening

> **Goal:** Prepare the platform for production-grade reliability and security.

- [x] Rate limiting on all ingestion endpoints
- [x] Input validation and sanitization
- [x] Service-level health checks (`/health`)
- [ ] Consumer lag monitoring and alerting
- [ ] Backpressure handling
- [ ] Horizontal scaling documentation
- [ ] Load testing report
- [ ] Security audit checklist

---

## Long-Term Vision

- Multi-tenant event isolation
- Custom alerting rules engine
- Event replay from ClickHouse
- Provider SDK for custom integrations
- Public API documentation (OpenAPI)
- Helm charts for Kubernetes deployment
