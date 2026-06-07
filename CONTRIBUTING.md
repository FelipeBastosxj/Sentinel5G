# Contributing

Thank you for your interest in contributing!

This project is open source (MIT) and welcomes contributions at all levels — bug fixes, documentation, new provider normalisers, and frontend features.

---

## Getting started

### Prerequisites

- Docker Desktop (or Docker Engine + Compose plugin)
- Node.js 22+ and npm 10+ (for running tests locally)
- Git

### Setup

```bash
git clone https://github.com/FelipeBastosxj/eventstream-observability-engine.git
cd eventstream-observability-engine
npm install
cp .env.example .env
npm run up
```

---

## Development workflow

### Run tests

```bash
npm test --workspaces           # all workspaces
npm test --workspace=services/ingestion-service  # single service
```

### Run services locally (without Docker)

Start only infrastructure via Docker, then run services directly:

```bash
docker compose -f infra/docker-compose.yml up -d postgres redis

DATABASE_URL=postgresql://webhook_user:webhook_pass@localhost:5432/telecom_webhooks \
PROCESSING_BASE_URL=http://localhost:3003 \
npm run start:dev --workspace=services/ingestion-service
```

---

## Branch strategy

| Branch | Purpose |
|---|---|
| `main` | Stable released code |
| `dev` | Active development |
| `feature/<name>` | Feature branches off `dev` |
| `fix/<name>` | Bug fixes off `dev` |

---

## Commit convention

[Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Vonage SMS normaliser
fix: handle SmsStatus alias in Twilio normaliser
docs: update getting-started guide
chore: remove unused files
test: fix IngestEventUseCase spec
```

---

## Pull request process

1. Fork the repository and branch off `dev`
2. Make changes with tests
3. Run `npm test --workspaces` — all tests must pass
4. Open a PR against `dev` with a clear description
5. Address review comments
6. Squash and merge

---

## Engineering standards

### Architecture rules

- **No message broker in the MVP** — services communicate via direct HTTP. `pg_notify` handles real-time fanout.
- **Clean Architecture** — use cases depend only on port interfaces, never on infrastructure.
- **No ORM** — raw `pg.Client` for all database access.
- **Thin controllers** — controllers only extract HTTP primitives and delegate to use cases.

### Code standards

- Structured JSON logs only — use `AppLoggerService`, never `console.log`
- Every service exposes `GET /health`
- New providers require: payload interface in `@telecom-webhook/contracts`, a normaliser class, and unit tests

### Where things belong

| Concern | Location |
|---|---|
| HTTP handling | Controller |
| Business logic | Use case |
| External I/O (DB, HTTP, Redis) | Adapter (infrastructure layer) |
| Shared types | `@telecom-webhook/contracts` |
| Shared helpers | `@telecom-webhook/utils` |

---

## Good first issues

- Add a new provider normaliser (Vonage, MessageBird, Infobip)
- Add `GET /timelines/:messageSid` for conversation view
- Add event search/filter to the Angular events page
- Write E2E tests for the webhook ingestion flow
- Add JWT authentication for the workspace API
