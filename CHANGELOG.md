# Changelog

All notable changes to **EventStream Observability Engine** will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/) and follows the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

---

## [Unreleased]

### Planned
- ClickHouse writer service (`events.processed` → analytical storage)
- Historical query REST API (top of ClickHouse)
- Consumer lag exporter for Prometheus
- WebSocket JWT authentication
- Multi-instance gateway scale-out via Redis pub/sub backplane (currently scaffolded)
- ECharts visualizations wired to real metrics streams

---

## [0.3.0] — 2026-06-06

### Fixed
- **`shared/@eventstream/schemas` — Zod v4 compatibility**
  - `canonical-event.schema.ts`: `z.record(z.unknown())` → `z.record(z.string(), z.unknown())` — Zod v4 requires explicit key schema
  - `validate.helper.ts`: `result.error.errors` → `result.error.issues` — property renamed in Zod v4
- **`shared/*/package.json` — runtime module resolution**
  - `"main"` and `"types"` now point to `"dist/index.js"` / `"dist/index.d.ts"` instead of `"src/index.ts"`, preventing Node 22 from attempting to load TypeScript sources directly via ESM stripping (which fails on extensionless imports)
- **Docker multi-stage builds — all four services**
  - Shared packages (`contracts` → `utils` → `schemas`) are now compiled independently in dependency order inside each build stage before the service is compiled
  - `shared/schemas` was missing from the runtime stage in all Dockerfiles; added `COPY --from=build` for all four
  - `CMD` entries corrected to the actual path emitted by `tsc` (services without an explicit `rootDir` in `tsconfig.json` emit to `dist/services/<name>/src/main.js`; services with `rootDir: "./src"` emit to `dist/main.js`)
  - Build-time assertion added: `find dist -name 'main.js'` fails the image build immediately if the output path shifts again, making regressions visible at build time rather than container startup

### Added
- **`shared/utils/tsconfig.build.json`** — dedicated build config with `rootDir: "./src"` and `paths` resolving `@eventstream/contracts` to `../contracts/dist`, preventing `tsc` from including contracts sources and expanding the implicit `rootDir` to the monorepo root
- **`shared/schemas/tsconfig.build.json`** — same pattern as `utils`, resolving both `@eventstream/contracts` and `@eventstream/utils` to their respective `dist/` outputs

### Changed
- `shared/schemas/package.json`: Zod peer dependency range updated from `"^3.23.0"` to `"^4.0.0"` to reflect the API used (`z.record` two-argument form, `ZodError.issues`)

---

## [0.2.0] — 2026-06-06

### Added
- **Shared `@eventstream/contracts`**
  - `CanonicalEvent` interface aligned with MASTER_PROMPT (added mandatory `channel` field, made all properties `readonly` for immutability — HARDNESS §4)
  - `Channel`, `EventType`, `EventSource`, `KafkaTopic`, `KafkaConsumerGroup` enums
  - Provider payload typings: Twilio, Infobip, SendGrid (at the integration boundary — HARDNESS §5)
- **Shared `@eventstream/utils`**
  - `generateUuid` / `isUuid`, `extractOrCreateCorrelationId`, `nowIsoUtc`
  - `createLogger` — structured JSON logger with correlation-id propagation (HARDNESS §8)
  - `buildCanonicalEvent` factory that produces frozen, fully-populated events
  - `exponentialBackoffMs` / `sleep` helpers for retry policies
  - Unit tests for every utility
- **Shared `@eventstream/schemas`**
  - Zod schemas for `CanonicalEvent` and every supported provider payload
  - `validateOrThrow` helper raising a typed `SchemaValidationError`
- **`ingestion-service`** (NestJS, Clean Architecture)
  - `POST /ingest` with rate limiting (`@nestjs/throttler`) — HARDNESS §9
  - Zod validation pipeline → Kafka producer publishing to `events.raw`
  - Idempotent kafkajs producer with acks=all, header propagation, graceful shutdown
  - `/health` and `/metrics` endpoints (prom-client)
  - Correlation-ID middleware backed by `AsyncLocalStorage`
  - OpenTelemetry tracing bootstrap (opt-in via `OTEL_ENABLED`)
  - Unit tests for the `IngestEventUseCase`
- **`webhook-service`** (NestJS, Clean Architecture)
  - `POST /integrations/{twilio,infobip,sendgrid,custom}/webhook`
  - HMAC signature validators per provider (Twilio Auth-Token, Infobip / SendGrid HMAC)
  - Domain `Normalizer` port + provider-specific normalizers translating raw payloads into `CanonicalEvent`
  - `IngestionHttpClient` forwarding to ingestion-service via native `fetch`
  - Same observability / health / metrics / correlation stack as ingestion-service
- **`processing-service`** (NestJS, Clean Architecture)
  - Kafka consumer for `events.raw` with `eachMessage` handler
  - `ProcessEventUseCase` orchestrating enrichment + retry
  - `BaseEnricher` adding `processedAt`, `processingService` and routing metadata
  - `RetryPolicy` with exponential backoff + jitter, configurable max attempts
  - Dead-letter queue producer routing exhausted events to `events.alerts`
  - Publishes enriched events to `events.processed` and aggregated metrics to `events.metrics`
- **`realtime-gateway`** (NestJS, Clean Architecture)
  - Socket.IO `EventsGateway` (path `/ws`, CORS configurable)
  - Kafka consumer for `events.processed` + `events.metrics`
  - Redis pub/sub backplane for multi-instance broadcast fan-out
  - `BroadcastEventUseCase` bridging Kafka events to connected clients
- **`frontend/angular-dashboard`** (Angular 20, standalone components, Signals only — HARDNESS §7)
  - Feature-based architecture: `dashboard`, `events`, `metrics`, `integrations`
  - `core/realtime-client.service.ts` (Socket.IO) and `core/events-store.service.ts` (Signals)
  - Provider health view, live event stream, metrics charts (ECharts ready)
  - No global stores, no shared mutable state
- **Infrastructure / Observability stack** (Docker Compose `--profile observability`)
  - OpenTelemetry Collector, Prometheus, Loki, Tempo, Grafana
  - Pre-provisioned datasources + “EventStream — Overview” dashboard
  - Makefile targets: `make obs-up`, `make obs-down`, `make obs-logs`
- **Docs**
  - `docs/event-model.md` updated to reflect the mandatory `channel` field
  - `docs/integrations.md` describing every integration endpoint and signature policy
  - `docs/observability.md` describing the metrics, traces, and logs contract
- **CI**
  - GitHub Actions matrix builds for all four services + Angular dashboard
  - CI gate job that fails the workflow on any downstream failure

### Changed
- `infra/docker-compose.yml` extended with observability profile, named volumes for Prometheus / Loki / Tempo / Grafana
- `Makefile` now uses `docker compose -f infra/docker-compose.yml` and exposes observability targets
- `.env.example` includes `INGESTION_SERVICE_URL`, `OTEL_ENABLED`, observability port mappings
- `package.json` (root) workspaces remain `shared/*`, `services/*`, `frontend/angular-dashboard`

### Compliance
- HARDNESS §4 — Canonical Event Rule satisfied (channel added, validation enforced, factory freezes events)
- HARDNESS §5 — Integration Boundary Rule satisfied (provider payloads only exist inside webhook-service normalizers)
- HARDNESS §6 — Clean Architecture: Controller / Application / Domain / Infrastructure folders in every service; domain has no NestJS / kafkajs imports
- HARDNESS §7 — Feature-based Angular structure, Signals only, no global store
- HARDNESS §8 — Each service exposes `/metrics`, structured logs, OTel bootstrap, correlation-id propagation
- HARDNESS §9 — Zod validation + `@nestjs/throttler` rate limiting + HMAC signature checks
- HARDNESS §10 — Stateless services, retry-safe consumers, DLQ
- HARDNESS §13 — `integrations.md` and `observability.md` added; `event-model.md` updated

---

## [0.1.0] — 2026-06-05

### Added
- Initial repository structure and monorepo layout
- `README.md` with full architecture overview and getting started guide
- `HARDNESS.md` engineering governance document defining non-negotiable architecture rules
- `MASTER_PROMPT.md` defining system identity, principles, and AI assistant context
- `CONTRIBUTING.md` with contribution guidelines and workflow
- `ROADMAP.md` with phased delivery plan
- `Makefile` with developer workflow commands
- `.env.example` with all environment variable definitions
- GitHub Actions CI workflow for lint, test, and build
- GitHub Actions Release workflow for automated release notes
- GitHub Issue templates (bug report, feature request)
- GitHub Pull Request template
- Apache 2.0 License

---

[Unreleased]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/releases/tag/v0.1.0
