# HARDNESS.md

# EventStream Observability Engine

Engineering Governance Document

---

# Purpose

This document defines the mandatory architectural, engineering, security, and scalability rules of the platform.

These rules are non-negotiable.

---

# 1. Project Scope

The platform exists to:

* ingest events
* process events
* provide observability
* demonstrate distributed systems patterns

The platform is not:

* an SMS gateway
* a billing system
* a CRM
* a marketing platform

---

# 2. Event-Driven Rule

All business flows must be event-driven.

Kafka is the primary communication backbone.

Avoid direct service-to-service business communication whenever possible.

---

# 3. Stateless Services Rule

All services must remain stateless.

Persistent state belongs to:

* Kafka
* Redis
* ClickHouse

Services must support horizontal scaling.

---

# 4. Canonical Event Rule

Every event entering the platform must be transformed into the Canonical Event Model.

Required fields:

* eventId
* eventType
* channel
* timestamp
* source
* correlationId

Events must be:

* immutable
* versioned
* validated

---

# 5. Integration Boundary Rule

External providers are considered Edge Integrations.

Examples:

* Twilio
* Infobip
* SendGrid
* Internal Messaging Systems

---

## Integration Responsibilities

Integration endpoints may:

* receive payloads
* validate requests
* normalize payloads
* create Canonical Events

Integration endpoints must not:

* contain business logic
* bypass Kafka
* access analytics databases directly

---

## Canonical Event Enforcement

The core platform must never consume provider-specific payloads directly.

Provider payloads must be transformed before entering Kafka.

---

# 6. Backend Architecture Rule

Backend services must follow Clean Architecture.

Layers:

* Controller
* Application
* Domain
* Infrastructure

Rules:

* Domain must remain framework independent
* Controllers must remain thin
* Business logic belongs in Application and Domain layers

---

# 7. Frontend Architecture Rule

Angular Signals is mandatory.

Feature-based architecture is mandatory.

Every feature owns:

* components
* state
* services
* models

Avoid:

* global stores
* oversized services
* god components

---

# 8. Observability Rule

Observability is a first-class concern.

Every service must expose:

* metrics
* traces
* structured logs

Correlation IDs must propagate through all services.

---

# 9. Security Rule

All external payloads must be validated.

Required:

* DTO validation
* schema validation
* rate limiting

Never trust external input.

---

# 10. Scalability Rule

Services must support:

* horizontal scaling
* retry-safe consumers
* consumer lag handling
* backpressure handling

Avoid shared in-memory state.

---

# 11. Infrastructure Rule

The platform must run locally using Docker Compose.

Cloud providers must remain optional.

Developers should be able to clone the repository and run the entire platform locally.

---

# 12. Performance Rule

Prefer:

* asynchronous processing
* batching
* event streaming

Avoid:

* unnecessary synchronous workflows
* excessive abstractions
* premature optimization

---

# 13. Documentation Rule

Architecture decisions must be documented.

The docs directory is part of the project.

Required documents:

* architecture.md
* event-model.md
* integrations.md
* observability.md

Major architectural changes must update documentation.

---

# 14. Definition of Done

A feature is complete only if:

* architecture rules are respected
* tests pass
* observability exists
* documentation is updated
* Docker environment works
* Kafka flow remains intact
