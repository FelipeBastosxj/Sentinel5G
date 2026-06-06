# Contributing to Open Telecom Webhook Observability

Thank you for your interest in contributing!

This project is **fully open source** (MIT) and designed to welcome community contributions from the start.

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
- [Good First Issues](#good-first-issues)

---

## Code of Conduct

Be respectful, constructive, and professional in all interactions.

---

## Getting Started

### Prerequisites

- Docker and Docker Compose
- Node.js 22+
- npm 10+

### Setup

```bash
# Clone the repository
git clone https://github.com/FelipeBastosxj/open-telecom-webhook-observability.git
cd open-telecom-webhook-observability

# Copy environment variables
cp .env.example .env

# Start infrastructure (PostgreSQL + Redis)
npm run infra:up

# Start all services
npm run services:up
```

---

## Development Workflow

```bash
# Run all tests
npm run test

# View service logs
npm run services:logs

# Stop all services
npm run services:down

# Reset environment (removes volumes)
npm run reset
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
feat(ingestion-service): add Twilio signature validation
fix(processing-service): handle null MessageSid on status callback
docs(readme): update quick start for PostgreSQL setup
chore(infra): upgrade postgres image to 16-alpine
test(ingestion-service): add unit tests for Twilio normaliser
feat(provider): add Vonage SMS webhook support
```

---

## Pull Request Process

1. Fork the repository or create a feature branch from `dev`
2. Implement your changes following the [Engineering Standards](#engineering-standards)
3. Ensure all tests pass: `npm run test`
4. Update documentation if required
5. Fill in the Pull Request template completely
6. Request a review

Pull Requests that do not comply with `HARDNESS.md` will be closed without merge.

---

## Engineering Standards

All contributions must comply with the rules defined in [HARDNESS.md](./HARDNESS.md).

Key non-negotiable rules:

- **Webhook-First** — all business flows start from an inbound HTTP webhook
- **PostgreSQL as source of truth** — no in-memory state, no external event buses in MVP
- **WebhookEvent Model** — all payloads normalised to `WebhookEvent` before persistence
- **Stateless Services** — no local persistent state in services
- **Clean Architecture** — strict layer separation (Controller → Application → Domain → Infrastructure)
- **No business logic in controllers**
- **No framework dependencies in domain layer**
- **Input validation** — every inbound payload must be validated

---

## Definition of Done

A contribution is considered complete when:

- [ ] Architecture rules from `HARDNESS.md` are respected
- [ ] Unit tests are present and passing
- [ ] PostgreSQL schema changes include a migration or updated `init.sql`
- [ ] New provider integrations include a normaliser and test payload
- [ ] Docker Compose environment starts and works correctly
- [ ] Documentation is updated (README, inline comments, or dedicated doc)
- [ ] CHANGELOG.md is updated under `[Unreleased]`
- [ ] No linting errors

---

## Good First Issues

Look for issues labelled:

- `good first issue` — well-scoped tasks for new contributors
- `help wanted` — tasks where community input is especially welcome
- `provider:vonage`, `provider:messagebird` etc. — new provider integrations

