# HARDNESS.md

# Open Telecom Webhook Observability

Engineering Governance Document

---

## Purpose

This document defines the mandatory architectural, engineering, and security rules of the platform.

These rules are **non-negotiable**. Pull Requests that violate them will not be merged.

---

## 1. Project Scope

The platform exists to:

- Receive webhooks from telecom providers
- Normalise and persist them
- Display them in real time
- Enable debugging and analysis of telecom integrations

The platform is **not**:

- A message broker or event bus
- An SMS gateway or sending service
- A billing or CRM system

---

## 2. Webhook-First Rule

All business flows start from an inbound HTTP webhook.

There is no internal event bus in the MVP. Services communicate via:

- Direct HTTP calls
- PostgreSQL `pg_notify` / `LISTEN` for real-time fanout
- Redis for rate limiting and optional caching

Kafka and other message brokers are reserved for future phases when horizontal scale demands it.

---

## 3. Stateless Services Rule

All services must remain stateless.

Persistent state belongs exclusively to:

- PostgreSQL (events, workspaces, tokens)
- Redis (rate limiting state, cache)

Services must support horizontal scaling. No in-memory event queues.

---

## 4. WebhookEvent Model Rule

Every event entering the platform **must** be normalised into the `WebhookEvent` model
(defined in `shared/contracts/src/webhook-event.interface.ts`) before persistence.

Required fields after normalisation:

- `id` — UUID v4, server-assigned
- `workspaceId` — resolved from token
- `provider` — e.g. `'twilio'`
- `eventType` — normalised type string
- `receivedAt` — UTC timestamp at HTTP layer
- `headers` — all request headers
- `payload` — full raw request body

Raw provider payloads must **never** reach the database layer unnormalised.

---

## 5. Integration Boundary Rule

Provider normalisation logic lives exclusively in the `processing-service`.

`ingestion-service` responsibilities:

- Accept the HTTP request
- Validate the workspace token
- Capture headers and body verbatim
- Forward to processing (synchronously in MVP)

`ingestion-service` must **not**:

- Parse provider-specific fields
- Contain business logic per provider
- Access PostgreSQL directly for event writes

---

## 6. Backend Architecture Rule

Backend services follow Clean Architecture:

- **Controller** — HTTP, DTO validation, no business logic
- **Application** — use cases, orchestration
- **Domain** — entities, ports (interfaces), business rules; **framework-independent**
- **Infrastructure** — database adapters, HTTP clients, Redis; implements domain ports

Rules:

- Domain layer must have zero NestJS / Express / Fastify imports
- Business logic belongs in Application and Domain layers only
- Controllers must be thin (< 10 lines of logic)

---

## 7. Frontend Architecture Rule

Angular Signals is mandatory.

Feature-based architecture is mandatory.

Every feature owns: components, state, services, models.

Avoid: global stores, oversized services, god components.

---

## 8. Security Rule

All inbound payloads must be validated.

Required for every provider integration:

- DTO / schema validation on the raw body
- Token validation (workspace `endpointToken`)
- Provider signature validation where supported (e.g. Twilio `X-Twilio-Signature`)
- Rate limiting on ingestion endpoints

Never trust external input.

---

## 9. Database Rule

PostgreSQL is the single source of truth.

Rules:

- Schema changes must be delivered as a migration file or an update to `infra/postgres/init.sql`
- No raw SQL strings inline in application code — use parameterised queries or a query builder
- Every table must have a primary key of type `UUID`
- Indexes must be added for all filterable columns

---

## 10. Infrastructure Rule

The platform must run locally using a single `docker compose up` command.

Cloud providers are always optional.

No proprietary services in the base stack.

---

## 11. Documentation Rule

Architecture decisions must be documented.

Required documents:

- `docs/architecture.md`
- `docs/event-model.md`
- `docs/integrations.md`

Major architectural changes must update these documents and `CHANGELOG.md`.

---

## 12. Definition of Done

A feature is complete only if:

- Architecture rules above are respected
- Unit tests pass
- WebhookEvent model is preserved end-to-end
- PostgreSQL schema is updated if needed
- Docker Compose environment starts correctly
- Documentation is updated
- CHANGELOG.md updated under `[Unreleased]`
