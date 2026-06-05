# Roadmap

This document describes the phased delivery plan for **EventStream Observability Engine**.

Each phase has a clear scope and set of deliverables. Phases are sequential but features within a phase may be developed in parallel.

---

## Phase 1 — Foundation

> **Goal:** Establish the core infrastructure, data contracts, and event ingestion pipeline.

- [ ] Docker Compose infrastructure (Kafka, Zookeeper, Redis, ClickHouse)
- [ ] Canonical Event Model schema and validation
- [ ] Shared contracts library (`/shared/contracts`)
- [ ] Ingestion Service scaffold (NestJS)
- [ ] Ingestion Service — generic webhook endpoint
- [ ] Ingestion Service — payload normalization to Canonical Event
- [ ] Ingestion Service — publish to Kafka `events.raw`
- [ ] Makefile with `up`, `down`, `logs`, `test`, `reset` commands
- [ ] `.env.example` with all required variables

---

## Phase 2 — Event Processing

> **Goal:** Consume raw events and build a reliable processing pipeline.

- [ ] Processing Service scaffold (NestJS)
- [ ] Kafka consumer for `events.raw` topic
- [ ] Event enrichment pipeline
- [ ] Event validation against Canonical Event Model
- [ ] Retry mechanism with exponential backoff
- [ ] Dead Letter Queue (DLQ) for failed events — `events.alerts`
- [ ] Event publishing to `events.processed` topic
- [ ] Correlation ID propagation across services

---

## Phase 3 — Analytics & Storage

> **Goal:** Persist processed events and expose historical analytics.

- [ ] ClickHouse schema design for events
- [ ] ClickHouse writer from `events.processed`
- [ ] Aggregation pipelines (delivery rates, error rates, latency)
- [ ] Historical query API endpoints
- [ ] Event metrics publishing to `events.metrics`
- [ ] Time-series data modeling

---

## Phase 4 — Realtime Gateway

> **Goal:** Expose real-time event streams to the frontend dashboard.

- [ ] Realtime Gateway scaffold (NestJS + WebSockets)
- [ ] Kafka consumer bridging to WebSocket clients
- [ ] Live event stream feed
- [ ] Live metrics feed
- [ ] WebSocket authentication
- [ ] Redis pub/sub for multi-instance gateway support

---

## Phase 5 — Angular Dashboard

> **Goal:** Build the real-time observability frontend.

- [ ] Angular 20 project scaffold with feature-based architecture
- [ ] Angular Signals for state management
- [ ] WebSocket integration service
- [ ] Live Event Stream view
- [ ] Live Metrics view (ECharts)
- [ ] Provider health dashboard
- [ ] Event detail panel with full Canonical Event view
- [ ] Correlation ID trace explorer

---

## Phase 6 — Observability Stack

> **Goal:** Full distributed tracing, metrics, and log aggregation.

- [ ] OpenTelemetry SDK integration in all backend services
- [ ] Prometheus metrics exposure (`/metrics` endpoint) per service
- [ ] Loki structured log shipping
- [ ] OpenTelemetry Collector in Docker Compose
- [ ] Correlation ID propagation in traces and logs
- [ ] Grafana dashboards for Prometheus and Loki

---

## Phase 7 — Provider Integrations

> **Goal:** Add first-class integration endpoints for major messaging providers.

- [ ] Twilio webhook endpoint with signature validation
- [ ] Infobip webhook endpoint with signature validation
- [ ] SendGrid webhook endpoint with signature validation
- [ ] Custom Integration SDK for generic providers
- [ ] Integration test suite per provider

---

## Phase 8 — Production Hardening

> **Goal:** Prepare the platform for production-grade reliability and security.

- [ ] Rate limiting on all ingestion endpoints
- [ ] Input validation and sanitization
- [ ] Service-level health checks (`/health`)
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
