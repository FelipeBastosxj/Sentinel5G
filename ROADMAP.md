# Roadmap

---

## Phase 1 — MVP (current)

> Working end-to-end webhook inspector for Twilio.

### Infrastructure
- [x] PostgreSQL schema (`workspaces`, `webhook_events`, indexes)
- [x] Redis (rate limiting, multi-instance WebSocket fanout)
- [x] Docker Compose — one-command startup (`npm run up`)

### Backend
- [x] `ingestion-service` — `POST /:workspaceId/:token` accepts all HTTP methods
- [x] Rate limiting (500 req/min per IP)
- [x] Provider detection from request headers
- [x] Raw header + body capture
- [x] `processing-service` — Twilio normalisation to `WebhookEvent`
- [x] PostgreSQL persistence with full headers + payload
- [x] `pg_notify` for real-time fanout
- [x] `GET /events/recent` + `GET /events/workspace/:id`
- [x] `POST /workspace/auto` — idempotent workspace provisioning
- [x] `realtime-gateway` — Socket.IO with workspace rooms
- [x] Redis pub/sub for multi-instance broadcast
- [x] Health endpoints on all services
- [x] Structured JSON logging with correlation IDs

### Frontend (Angular 17)
- [x] Workspace auto-provisioning on first visit
- [x] Unique webhook URL display
- [x] Live event list (WebSocket-driven)
- [x] Metrics page (per-channel, per-type, throughput charts)
- [x] Integrations page (provider success rates)

### Twilio events
- [x] `message.inbound` (incoming SMS / WhatsApp)
- [x] `message.status.*` (all delivery statuses + legacy `SmsStatus` alias)
- [x] `call.inbound` / `call.outbound` / `call.status.*`

---

## Phase 2 — Provider expansion

- [ ] Vonage SMS inbound + delivery status
- [ ] Vonage Voice webhooks
- [ ] MessageBird SMS status callbacks
- [ ] Infobip delivery reports
- [ ] Plivo SMS + voice webhooks
- [ ] Signature validation per provider
- [ ] Provider health badge in dashboard

---

## Phase 3 — Developer experience

- [ ] `GET /timelines/:messageSid` — conversation thread view
- [ ] Webhook replay — resend any stored event to a target URL
- [ ] Event export (JSON / CSV)
- [ ] Advanced search + filter (by status, number, date range, event type)
- [ ] Dark mode

---

## Phase 4 — Security

- [ ] JWT authentication for workspace API
- [ ] Workspace invitation tokens
- [ ] Twilio signature HMAC verification
- [ ] Event retention policy (configurable TTL)

---

## Phase 5 — Production readiness

- [ ] PostgreSQL connection pool (`pg-pool`)
- [ ] Kubernetes manifests (Helm chart)
- [ ] Horizontal scaling guide
- [ ] OpenTelemetry traces (optional Docker Compose profile)
- [ ] Prometheus metrics (optional Docker Compose profile)
