# Changelog

All notable changes to **EventStream Observability Engine** will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/) and follows the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

---

## [Unreleased]

### Planned
- Consumer lag exporter for Prometheus
- WebSocket JWT authentication
- Correlation ID trace explorer (frontend)
- Aggregation pipelines (delivery rates, error rates, latency p95/p99)
- Time-series data modeling in ClickHouse

---

## [0.4.0] — 2026-06-06

### Fixed

- **`infra/grafana/datasources.yml` — stable datasource UIDs**
  - Added explicit `uid: prometheus`, `uid: loki`, and `uid: tempo` to all provisioned datasources.
  Without fixed UIDs Grafana assigns a random string on every restart; the pre-provisioned
  dashboard references all datasources by `uid`, so every panel showed "datasource not found"
  even with Prometheus fully healthy and scraping all targets.

- **`processing-service` — ClickHouse DDL uses HTTP POST**
  - `ClickHouseAdapter.exec()` now always sends `POST` requests.
  Previously DDL statements (no body) were sent with `GET`, but the ClickHouse HTTP interface
  is read-only for `GET` requests (error 164: `READONLY`). As a result `CREATE TABLE` failed
  silently on every startup, the `eventstream.events` table was never created, and every
  `save()` call raised "Table does not exist" — ClickHouse remained permanently empty.

- **`shared/schemas` — `CanonicalEvent` schema: optional server-side fields**
  - `eventId`, `timestamp`, and `correlationId` are now `.optional()` in the Zod schema.
  The `IngestEventUseCase` backfills these three fields server-side (UUID v4, UTC ISO-8601,
  `X-Correlation-ID` header) so clients are not required to send them. Making them `.required()`
  was rejecting every valid inbound payload and returning HTTP 400.

- **Port and URL alignment across all services**
  - `webhook-service/EnvService`: now reads `WEBHOOK_PORT` (default `3002`) and
    `INGESTION_BASE_URL` (default `http://localhost:3001`) — matching docker-compose env keys.
  - `processing-service/EnvService`: now reads `PROCESSING_PORT` (default `3003`).
  - `realtime-gateway/EnvService`: now reads `GATEWAY_PORT` (default `3004`).

- **`webhook-service` — `publish is not a function` runtime error**
  - `ProcessWebhookUseCase` was injecting `EVENT_PUBLISHER_TOKEN` / `EventPublisherPort`
    (the Kafka-style `publish()` port) instead of `CANONICAL_EVENT_FORWARDER_PORT` /
    `CanonicalEventForwarderPort` (the HTTP `forward()` port implemented by
    `IngestionHttpClient`). Fixed injection token, renamed `publisher` → `forwarder`,
    `publish()` → `forward()`. `IntegrationsModule` now uses `useExisting` so the
    adapter is constructed with its own dependencies.

### Added — Phase 3: Historical Event Storage

- **`infra/clickhouse/init.sql`**
  Schema for `eventstream.events` — MergeTree engine, partitioned by month, ordered by
  `(toDate(timestamp), event_id)`, 90-day TTL on `timestamp`. Mounted as
  `/docker-entrypoint-initdb.d/init.sql:ro` in docker-compose.

- **`processing-service` — `EventStorePort` outbound port**
  Hexagonal `EventStorePort` interface (`save(event)`, `findRecent(options)`) in
  `domain/ports/`. `EVENT_STORE_PORT` symbol for NestJS DI. Domain layer stays
  framework-agnostic (HARDNESS §6).

- **`processing-service` — `ClickHouseAdapter` + `ClickHouseModule`**
  HTTP-based adapter using Node 22 native `fetch` (no extra dependencies).
  `OnModuleInit` creates the table with `CREATE TABLE IF NOT EXISTS` using POST.
  `save()` inserts via `INSERT INTO events FORMAT JSONEachRow`.
  Both `save()` and `findRecent()` are non-fatal — errors are logged as warnings
  and never interrupt the Kafka processing pipeline (HARDNESS §10).

- **`processing-service` — `GET /events/recent` REST endpoint**
  `EventsQueryController` exposes historical queries:
  `GET /events/recent?limit=100&channel=SMS&source=twilio`.
  CORS enabled in `main.ts` (`origin: '*'`, `methods: 'GET'`) so the Angular
  frontend can call it directly.

- **`infra/docker-compose.yml` — ClickHouse wiring for processing-service**
  Added `CLICKHOUSE_HOST`, `CLICKHOUSE_PORT`, `CLICKHOUSE_DATABASE`, `CLICKHOUSE_USER`,
  `CLICKHOUSE_PASSWORD` env vars. Added `depends_on: clickhouse: condition: service_healthy`
  so processing-service never starts before ClickHouse is ready.

### Added — Frontend: ECharts + Persistence

- **`MetricsPageComponent` — three live ECharts charts**
  Native `echarts` (no `ngx-echarts`) loaded via dynamic `import('echarts')` so it ships
  as a separate lazy chunk and never bloats the initial bundle:
  - **Horizontal bar** — events per channel
  - **Doughnut / pie** — events per event type
  - **Line / area** — throughput per minute (last 30 buckets)
  All chart options are `computed<EChartsOption>()` signals; an `effect()` calls
  `chart.setOption()` on every signal change. Charts are disposed in `ngOnDestroy()`
  to prevent memory leaks (HARDNESS §7).

- **`EventsApiService`** (`core/services/events-api.service.ts`)
  Calls `GET /events/recent` on processing-service with a 5-second `AbortController`
  timeout. Returns an empty array on any error — frontend never fails on a cold backend.

- **`MetricsStore` + `EventsStore` — ClickHouse hydration and localStorage persistence**
  On construction both stores:
  1. Restore the last known state from `localStorage` (keys `es:metrics:events` /
     `es:events:all`) so data survives an F5 or browser restart.
  2. Call `EventsApiService.fetchRecent()` to hydrate from ClickHouse — events that
     arrived while the browser was closed are merged in (deduped by `eventId`).
  A persisting `effect()` writes back to `localStorage` on every signal change.
  Capacity: `MetricsStore` 5 000 events, `EventsStore` 1 000 events (LRU-trim).

- **`environments/environment.ts`** — `processingUrl: 'http://localhost:3003'` added.

### Added — Scripts

- **`check_infra.sh`** — diagnostic script that checks Prometheus target health,
  ClickHouse event count, processing-service metrics, and ingestion-service metrics
  in a single pass. Useful to verify the full stack is wired before load-testing.

- **`test_event.sh`** — quick smoke test that sends a `STATUS_EVENT` via
  `POST /ingest` and prints the response, useful for verifying the ingestion
  pipeline end-to-end.

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

[Unreleased]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/releases/tag/v0.1.0
