# HARDNESS.md

# EventStream Observability Engine

Engineering Governance Document

---

# Purpose

This document defines the non-negotiable architectural and engineering rules of the platform.

Violations must be considered invalid implementations.

---

# 1. System Definition

The platform is a:

Distributed Messaging Observability Platform.

Its purpose is:

* event ingestion
* event processing
* event observability
* telemetry analysis

The platform is not:

* an SMS gateway
* a billing platform
* a CRM
* a monolith

---

# 2. Event-Driven First Rule

All business flow must occur through events.

Kafka is the backbone of the platform.

Direct business communication between services is forbidden.

---

# 3. Stateless Services Rule

All services must be stateless.

Allowed state stores:

* Redis
* ClickHouse
* Kafka

Local memory must never be considered persistent state.

---

# 4. Integration Boundary Rule

External systems are Edge Integrations.

Examples:

* Twilio
* Infobip
* SendGrid
* Internal Messaging Systems

---

## 4.1 Integration Endpoint Rule

External systems must communicate through dedicated integration endpoints.

Examples:

* /integrations/twilio/webhook
* /integrations/infobip/webhook
* /integrations/custom/webhook

---

## 4.2 Integration Responsibilities

Integration endpoints may:

* receive payloads
* validate signatures
* normalize payloads
* create Canonical Events

Integration endpoints must not:

* contain business logic
* write directly to databases
* bypass Kafka

---

## 4.3 Canonical Event Enforcement

Provider-specific payloads must be transformed into Canonical Events before entering Kafka.

The core platform must never consume provider-specific payloads directly.

---

# 5. Canonical Event Model

Required:

* eventId
* eventType
* timestamp
* source
* correlationId

Optional:

* metadata
* payload

Events are immutable.

Events are versioned.

---

# 6. Backend Architecture Rule

Backend must follow Clean Architecture.

Layers:

* Controller
* Application
* Domain
* Infrastructure

Rules:

* Domain cannot depend on frameworks
* Controllers cannot contain business logic
* Infrastructure cannot leak into domain

---

# 7. Frontend Architecture Rule

Angular Signals is mandatory.

Feature-based architecture is mandatory.

Structure:

/features

/dashboard
/events
/metrics
/providers

Rules:

* no global state libraries
* no god modules
* self-contained features

---

# 8. Observability Rule

Every service must expose:

* traces
* metrics
* structured logs

Required technologies:

* OpenTelemetry
* Prometheus
* Loki

Correlation IDs must propagate across the entire event flow.

---

# 9. Security Rule

All external payloads must be validated.

Rate limiting is mandatory on ingestion endpoints.

Trust no external payload.

---

# 10. Scalability Rule

Services must support horizontal scaling.

No shared state.

Backpressure handling is required.

Consumer lag handling is required.

Retry-safe consumers are required.

---

# 11. Infrastructure Rule

The platform must run locally via Docker Compose.

Required services:

* Kafka
* Zookeeper
* Redis
* ClickHouse

Cloud services are optional and must not be required.

---

# 12. Performance Rule

Prefer:

* asynchronous processing
* batch operations
* non-blocking consumers

Avoid:

* synchronous bottlenecks
* unnecessary serialization
* excessive abstractions

---

# 13. Definition of Done

A feature is complete only if:

* architecture rules are respected
* tests pass
* observability exists
* Docker environment works
* Kafka flow is preserved
* documentation is updated
