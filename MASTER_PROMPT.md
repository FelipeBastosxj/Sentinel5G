# EventStream Observability Engine

## Mission

Build a production-grade open-source platform for studying, implementing, and operating distributed messaging systems.

The platform focuses on:

* event ingestion
* event processing
* messaging observability
* telemetry analytics
* provider integrations
* distributed systems patterns

The goal is to provide a practical reference implementation for engineers working with messaging infrastructures and event-driven architectures.

---

# Project Identity

EventStream Observability Engine is a:

* Distributed Messaging Observability Platform
* Event-Driven Architecture Reference Project
* Real-Time Telemetry Platform
* Open Source Engineering Project

The platform is intended to demonstrate practical engineering patterns that can be applied to messaging systems and distributed infrastructures.

---

# Architectural Principles

Mandatory:

* Event-Driven Architecture
* Kafka-Centric Design
* Clean Architecture
* Stateless Services
* Observability First
* Feature-Based Frontend Architecture
* Docker Compose Development Environment

Preferred:

* Simplicity over complexity
* Explicit contracts over implicit behavior
* Low operational overhead
* Modular design

---

# Technology Stack

Backend:

* NestJS
* Kafka
* Redis
* ClickHouse
* OpenTelemetry
* Prometheus
* Loki

Frontend:

* Angular 20+
* Angular Signals
* ECharts
* WebSockets

Infrastructure:

* Docker Compose

---

# Repository Structure

/services
ingestion-service
processing-service
realtime-gateway
webhook-service

/frontend
angular-dashboard

/shared
contracts
schemas
utils

/docs

/infra

---

# Canonical Event Model

Every event must contain:

* eventId
* eventType
* channel
* timestamp
* source
* correlationId

Optional:

* metadata
* payload

Example:

{
"eventId": "uuid",
"eventType": "DELIVERY_EVENT",
"channel": "sms",
"timestamp": "2026-01-01T12:00:00Z",
"source": "twilio",
"correlationId": "abc-123"
}

Supported channels:

* sms
* email
* whatsapp
* push
* internal

Initial development focuses on SMS integrations.

Events must be immutable and versioned.

---

# Integration Boundary

External systems communicate through Integration Endpoints.

Examples:

* /integrations/twilio/webhook
* /integrations/infobip/webhook
* /integrations/custom/webhook

Responsibilities:

* receive external payloads
* validate requests
* normalize data
* create Canonical Events
* publish events to Kafka

The core platform must never consume provider-specific payloads directly.

---

# Kafka Topics

events.raw

events.processed

events.metrics

events.alerts

---

# Backend Rules

Use Clean Architecture.

Layers:

* Controller
* Application
* Domain
* Infrastructure

Rules:

* Controllers must not contain business logic
* Domain must not depend on frameworks
* Infrastructure concerns must remain isolated
* Kafka is the system backbone

---

# Frontend Rules

Angular Signals is mandatory.

Feature-based architecture is mandatory.

Structure:

/features
/dashboard
/events
/metrics
/integrations

Each feature must contain:

* components
* services
* models
* state

No global state libraries.

No shared mutable state.

---

# Observability

Every service must expose:

* traces
* metrics
* structured logs

Required:

* OpenTelemetry
* Prometheus

Optional:

* Loki

Correlation IDs must propagate through the entire event flow.

---

# Infrastructure

Everything must run locally through Docker Compose.

Required services:

* Kafka
* Zookeeper
* Redis
* ClickHouse

Optional:

* Prometheus
* Loki
* OpenTelemetry Collector

The project must remain self-hosted friendly.

---

# Output Expectations

Generated code must:

* follow architecture rules
* be production-oriented
* be maintainable
* be testable
* prioritize clarity over abstraction
* prioritize operational simplicity
