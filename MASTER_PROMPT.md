# MASTER_PROMPT.md

# EventStream Observability Engine

## Mission

Build a production-grade open-source Distributed Messaging Observability Platform.

The platform provides:

* event ingestion
* event processing
* event observability
* real-time telemetry
* reliability analytics
* distributed messaging infrastructure insights

The platform must be provider-agnostic.

It must support integration with external messaging systems through dedicated Integration Endpoints while maintaining a Canonical Event Model internally.

---

# Core Identity

This system IS:

* a distributed event processing platform
* a messaging observability engine
* a real-time telemetry platform
* a distributed systems reference architecture

This system is NOT:

* a telecom billing system
* an SMS gateway
* an SMPP server
* a CRM
* a monolithic application

---

# Architecture Principles

Mandatory:

* Event-Driven Architecture
* Kafka-Centric Design
* Stateless Services
* Clean Architecture
* Observability First
* Feature-Based Frontend Architecture
* Local-First Development
* Docker Compose Infrastructure

Preferred:

* Simplicity over abstraction
* Reproducibility over convenience
* Explicit contracts over implicit behavior

---

# Technology Stack

## Backend

* NestJS
* Kafka
* Redis
* ClickHouse
* OpenTelemetry
* Prometheus
* Loki

## Frontend

* Angular 20+
* Angular Signals
* ECharts
* WebSockets

## Infrastructure

* Docker Compose

---

# Monorepo Structure

/eventstream-engine

/services
/ingestion-service
/processing-service
/realtime-gateway
/webhook-service

/frontend
/angular-dashboard

/shared
/contracts
/schemas
/utils

/infra
docker-compose.yml

/docs
architecture.md
event-model.md

README.md
HARDNESS.md
Makefile

---

# Canonical Event Model

Every event must contain:

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

Events must be validated before entering Kafka.

---

# Integration Endpoints

The platform supports Edge Integrations.

Examples:

* /integrations/twilio/webhook
* /integrations/infobip/webhook
* /integrations/sendgrid/webhook
* /integrations/custom/webhook

Responsibilities:

* receive provider payloads
* validate signatures
* transform payloads
* create Canonical Events
* publish to Kafka

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

* Controllers
* Application
* Domain
* Infrastructure

Rules:

* No business logic inside controllers
* No framework dependency inside domain
* No direct database access outside infrastructure
* Kafka is the system backbone

---

# Frontend Rules

Angular Signals is mandatory.

RxJS may be used only for streams.

Feature-based structure required:

/features

/dashboard
/events
/metrics
/providers

Each feature must contain:

* components
* services
* models
* state
* ui

No global state libraries.

No god modules.

---

# Observability Requirements

Every service must implement:

* OpenTelemetry tracing
* Prometheus metrics
* Structured logging
* Correlation ID propagation

Observability is not optional.

---

# Infrastructure Requirements

Everything must run through Docker Compose.

Required services:

* Kafka
* Zookeeper
* Redis
* ClickHouse

Optional:

* Prometheus
* Loki
* OpenTelemetry Collector

Startup:

docker-compose up -d

---

# Scalability Requirements

Support:

* horizontal scaling
* retry-safe consumers
* consumer lag handling
* backpressure management

No shared in-memory state.

---

# Output Expectations

Generated code must:

* be production-oriented
* be modular
* be observable
* be testable
* follow architecture constraints
* prioritize maintainability
* prioritize low operational complexity
