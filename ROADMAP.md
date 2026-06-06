# Roadmap

**Open Telecom Webhook Observability** — phased delivery plan.

Phases are sequential. Features within a phase may be developed in parallel.
Community contributions are welcome at every phase — see [CONTRIBUTING.md](./CONTRIBUTING.md).

---

## Phase 1 — MVP: Twilio Webhook Inspector (current)

> **Goal:** Working end-to-end webhook receiver and real-time inspector for Twilio.

### Infrastructure
- [x] PostgreSQL schema (`workspaces`, `webhook_events`, `event_timelines` view)
- [x] Redis (rate limiting, optional query cache)
- [x] Docker Compose with one-command startup

### Backend
- [x] `ingestion-service` — `POST /:workspaceId/:token` endpoint
- [x] Token validation per workspace
- [x] Raw header + payload capture
- [x] `processing-service` — Twilio payload normalisation to `WebhookEvent`
- [x] PostgreSQL persistence
- [x] `realtime-gateway` — WebSocket server for live event push
- [x] `GET /events` with filter params (type, status, from, to, sid, time range)
- [x] `GET /events/:id` full event detail
- [x] `GET /timelines/:threadId` — MessageSid / CallSid timeline
- [x] `GET /metrics` — aggregated metrics from PostgreSQL
- [x] Health check endpoints on all services

### Frontend (Angular)
- [x] Workspace selector
- [x] Live event list (WebSocket-driven)
- [x] Event detail panel (headers + formatted payload)
- [x] Event timeline view (by MessageSid / CallSid)
- [x] Metrics dashboard (delivery rate, failure rate, volume)
- [x] Search and filter bar

### Twilio Events Supported
- [x] `message.inbound` (incoming SMS / WhatsApp)
- [x] `message.status.*` (sent, delivered, undelivered, failed, read)
- [x] `call.inbound` / `call.outbound`
- [x] `call.status.*` (initiated, ringing, in-progress, completed, busy, no-answer, failed)

---

## Phase 2 — Provider Expansion

> **Goal:** Support additional major telecom providers.

- [ ] Vonage SMS webhooks
- [ ] Vonage Voice webhooks
- [ ] MessageBird SMS status callbacks
- [ ] Infobip delivery reports
- [ ] Plivo SMS + voice webhooks
- [ ] Configurable signature validation per provider
- [ ] Provider health badge in dashboard

---

## Phase 3 — Developer Experience

> **Goal:** Make the platform indispensable for debugging integrations.

- [ ] Webhook Replay — resend any stored event to a target URL
- [ ] Request diff — compare two events side-by-side
- [ ] Workspace API key management UI
- [ ] Event export (JSON / CSV)
- [ ] Shareable event permalink
- [ ] Public read-only workspace links
- [ ] CLI tool (`npx telecom-webhook inspect`)

---

## Phase 4 — Alerting and Automation

> **Goal:** Notify developers when things go wrong.

- [ ] Alert rules engine (e.g. failure rate > 5% in 5 min)
- [ ] Notification channels: email, Slack, webhook
- [ ] Event retention policy (configurable TTL per workspace)
- [ ] Automated silence / acknowledge rules

---

## Phase 5 — Scale and Production

> **Goal:** Prepare for high-volume production deployments.

- [ ] Horizontal scaling documentation + load testing report
- [ ] Helm chart for Kubernetes
- [ ] Multi-region ingestion (optional Kafka layer for fan-out)
- [ ] OpenAPI specification (`/openapi.json`)
- [ ] Official SDK packages (`@telecom-webhook/node`, `@telecom-webhook/python`)
- [ ] Rate limiting per workspace (configurable)
- [ ] Tenant isolation (row-level security in PostgreSQL)

---

## Long-Term Vision

- White-label / self-hosted SaaS mode
- Plugin system for custom normalisers
- AI-assisted failure diagnostics
- Integration test runner (record + replay flows)
