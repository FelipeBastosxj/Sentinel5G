# Contributing to EventStream Observability Engine

Thank you for your interest in contributing to this project.

This document defines the contribution workflow, standards, and expectations.

Please read this document **and** [HARDNESS.md](./HARDNESS.md) before opening any Pull Request.

---

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Branch Strategy](#branch-strategy)
- [Commit Convention](#commit-convention)
- [Pull Request Process](#pull-request-process)
- [Engineering Standards](#engineering-standards)
- [Definition of Done](#definition-of-done)

---

## Code of Conduct

This project follows a standard code of conduct.

Be respectful, constructive, and professional in all interactions.

---

## Getting Started

### Prerequisites

- Docker and Docker Compose
- Node.js 22+
- npm

### Setup

```bash
# Clone the repository
git clone https://github.com/FelipeBastosxj/eventstream-observability-engine.git
cd eventstream-observability-engine

# Copy environment variables
cp .env.example .env

# Start infrastructure
docker-compose up -d

# Start services
make up
```

---

## Development Workflow

```bash
# Run all tests
make test

# View service logs
make logs

# Stop all services
make down

# Reset environment (removes volumes)
make reset
```

---

## Branch Strategy

| Branch | Purpose |
|--------|---------|
| `main` | Stable production-ready code |
| `dev` | Active development integration branch |
| `feat/*` | New features |
| `fix/*` | Bug fixes |
| `chore/*` | Maintenance, tooling, dependencies |
| `docs/*` | Documentation only changes |

All Pull Requests must target the `dev` branch.

`main` is updated only via release merges from `dev`.

---

## Commit Convention

This project uses [Conventional Commits](https://www.conventionalcommits.org/).

### Format

```
<type>(<scope>): <description>
```

### Types

| Type | When to Use |
|------|------------|
| `feat` | A new feature |
| `fix` | A bug fix |
| `docs` | Documentation only changes |
| `chore` | Build process, tooling, dependencies |
| `test` | Adding or updating tests |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `perf` | Performance improvement |
| `ci` | CI/CD configuration changes |

### Examples

```bash
feat(ingestion-service): add Twilio webhook endpoint
fix(processing-service): handle null correlationId on retry
docs(readme): update architecture diagram
chore(infra): update Kafka version to 3.7
test(ingestion-service): add unit tests for canonical event mapper
```

---

## Pull Request Process

1. Fork the repository or create a feature branch from `dev`
2. Implement your changes following the [Engineering Standards](#engineering-standards)
3. Ensure all tests pass: `make test`
4. Update documentation if required
5. Fill in the Pull Request template completely
6. Request a review

Pull Requests that do not comply with `HARDNESS.md` will be closed without merge.

---

## Engineering Standards

All contributions must comply with the rules defined in [HARDNESS.md](./HARDNESS.md).

Key non-negotiable rules:

- **Event-Driven First** — all business flows occur through Kafka events
- **Stateless Services** — no local persistent state in services
- **Canonical Event Model** — all payloads normalized before entering the platform
- **Observability** — every service must expose traces, metrics, and structured logs
- **Clean Architecture** — strict layer separation (Controller → Application → Domain → Infrastructure)
- **No business logic in controllers**
- **No framework dependencies in domain layer**

---

## Definition of Done

A contribution is considered complete when:

- [ ] Architecture rules from `HARDNESS.md` are respected
- [ ] Unit tests are present and passing
- [ ] Kafka flow is preserved end-to-end
- [ ] OpenTelemetry instrumentation exists
- [ ] Docker Compose environment starts and works correctly
- [ ] Documentation is updated (README, inline comments, or dedicated doc)
- [ ] CHANGELOG.md is updated under `[Unreleased]`
- [ ] No linting errors
