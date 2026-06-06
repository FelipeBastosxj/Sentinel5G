# EventStream Observability Engine

> A Distributed Messaging Observability Platform for real-time event ingestion, processing, telemetry, and reliability analytics.

![License](https://img.shields.io/badge/license-Apache%202.0-blue)
![Status](https://img.shields.io/badge/status-active-green)
![Architecture](https://img.shields.io/badge/architecture-event--driven-orange)
![Backend](https://img.shields.io/badge/backend-NestJS-red)
![Frontend](https://img.shields.io/badge/frontend-Angular%2020-red)
![Kafka](https://img.shields.io/badge/streaming-Kafka-black)

---

# Overview

Modern messaging infrastructures generate millions of events every day across multiple providers, systems, and communication channels.

While providers expose raw events, engineering teams often lack:

* a unified event model
* real-time observability
* reliability analytics
* distributed tracing
* platform-wide telemetry

EventStream Observability Engine addresses this challenge by providing a provider-agnostic observability layer built around event-driven architecture principles.

---

# Why This Project Exists

Messaging systems are becoming increasingly distributed.

Organizations integrate with:

* SMS providers
* Email providers
* Notification services
* Internal messaging platforms
* Event streaming systems

Each provider exposes different payloads, APIs, and monitoring capabilities.

This project provides:

* Canonical Event Modeling
* Real-Time Event Processing
* Distributed Observability
* Provider-Agnostic Integrations
* Streaming Analytics

without coupling the platform to any specific provider.

---

# Architecture

```text
External Systems
        │
        ▼
Integration Endpoints
        │
        ▼
Canonical Event Model
        │
        ▼
Kafka
        │
        ▼
Processing Services
        │
        ├─────────────► ClickHouse
        │                   │
        │                   ▼
        │             Historical Analytics
        │
        ▼
Realtime Gateway
        │
        ▼
Angular Dashboard
```

---

# Core Principles

## Event-Driven First

Every business flow is represented as an event.

Kafka acts as the central event backbone.

---

## Provider Agnostic

The platform never consumes provider-specific payloads directly.

All incoming payloads are transformed into Canonical Events before entering the processing pipeline.

---

## Observability First

Every service must expose:

* traces
* metrics
* structured logs
* correlation IDs

Observability is a first-class concern.

---

## Stateless by Design

Services must remain stateless.

State is externalized to:

* Kafka
* Redis
* ClickHouse

---

# Canonical Event Model

All events entering the platform are transformed into the following structure:

```json
{
  "eventId": "uuid",
  "eventType": "DELIVERY_EVENT",
  "timestamp": "2026-01-01T12:00:00Z",
  "source": "twilio",
  "correlationId": "abc-123",
  "metadata": {},
  "payload": {}
}
```

Required fields:

* eventId
* eventType
* timestamp
* source
* correlationId

Events are immutable and versioned.

---

# Integration Endpoints

External providers communicate through dedicated integration endpoints.

Examples:

```text
POST /integrations/twilio/webhook
POST /integrations/infobip/webhook
POST /integrations/sendgrid/webhook
POST /integrations/custom/webhook
```

Responsibilities:

* receive provider payloads
* validate signatures
* normalize payloads
* create Canonical Events
* publish events to Kafka

The core platform never consumes provider-specific payloads directly.

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

# Repository Structure

```text
eventstream-observability-engine/

├── docs/
├── infra/
├── services/
│   ├── ingestion-service/
│   ├── processing-service/
│   ├── realtime-gateway/
│   └── webhook-service/
│
├── frontend/
│   └── angular-dashboard/
│
├── shared/
│   ├── contracts/
│   ├── schemas/
│   └── utils/
│
├── HARDNESS.md
├── README.md
├── ROADMAP.md
└── Makefile
```

---

# Getting Started

## Prerequisites

* Docker
* Docker Compose
* Node.js 22+
* npm

---

## Clone Repository

```bash
git clone https://github.com/your-username/eventstream-observability-engine.git

cd eventstream-observability-engine
```

---

## Start Infrastructure

```bash
docker-compose -f infra/docker-compose.yml up -d
```

This starts:

* Kafka
* Zookeeper
* Redis
* ClickHouse
* Kafka UI (http://localhost:8080)

To start the optional observability stack (Prometheus, Loki, Tempo, Grafana, OTel Collector):

```bash
make obs-up                 # or: docker compose -f infra/docker-compose.yml --profile observability up -d
```

Grafana is then available at http://localhost:3000 with the **EventStream — Overview** dashboard preloaded.

---

## Service ports

| Service | URL |
|---------|-----|
| ingestion-service | http://localhost:3001 (`POST /ingest`, `/health`, `/metrics`) |
| processing-service | http://localhost:3002 (`/health`, `/metrics`) |
| realtime-gateway | http://localhost:3003 (WebSocket `/ws`, `/health`, `/metrics`) |
| webhook-service | http://localhost:3004 (`POST /integrations/{provider}/webhook`, `/health`, `/metrics`) |
| Angular dashboard | http://localhost:4200 |

---

## Verify Services

```bash
docker ps
```

---

## Start Backend Services

```bash
make up
```

---

## Start Frontend

```bash
cd frontend/angular-dashboard

npm install

npm start
```

---

# Development Workflow

## Run Tests

```bash
make test
```

---

## View Logs

```bash
make logs
```

---

## Stop Everything

```bash
make down
```

---

## Reset Environment

```bash
make reset
```

---

# Roadmap

## Phase 1 — Foundation

* [ ] Docker Compose Infrastructure
* [ ] Kafka Setup
* [ ] Canonical Event Model
* [ ] Ingestion Service

---

## Phase 2 — Event Processing

* [ ] Processing Service
* [ ] Event Enrichment
* [ ] Retry Mechanisms
* [ ] Dead Letter Queue

---

## Phase 3 — Analytics

* [ ] ClickHouse Integration
* [ ] Aggregation Pipelines
* [ ] Historical Queries

---

## Phase 4 — Realtime

* [ ] WebSocket Gateway
* [ ] Angular Dashboard
* [ ] Live Metrics

---

## Phase 5 — Observability

* [ ] OpenTelemetry
* [ ] Prometheus
* [ ] Loki
* [ ] Distributed Tracing

---

## Phase 6 — Integrations

* [ ] Twilio Integration Endpoint
* [ ] Infobip Integration Endpoint
* [ ] SendGrid Integration Endpoint
* [ ] Custom Integration SDK

---

# Engineering Standards

This project follows strict engineering governance defined in:

```text
HARDNESS.md
```

The document defines:

* architecture constraints
* scalability rules
* security requirements
* observability requirements
* frontend and backend standards

All contributions must comply with HARDNESS.md.

---

# Contributing

Contributions are welcome.

Please read:

* CONTRIBUTING.md
* HARDNESS.md

before opening a Pull Request.

---

# License

Licensed under the Apache License 2.0.

See LICENSE for details.

---

# Long-Term Vision

EventStream Observability Engine aims to become a reference implementation for:

* distributed messaging observability
* event-driven architecture
* canonical event modeling
* real-time telemetry systems
* messaging infrastructure analytics

Built by engineers, for engineers.
