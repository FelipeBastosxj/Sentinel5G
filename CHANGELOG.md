# Changelog

All notable changes to **EventStream Observability Engine** will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/) and follows the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

---

## [Unreleased]

### Planned
- Docker Compose infrastructure setup (Kafka, Zookeeper, Redis, ClickHouse)
- Canonical Event Model definition and schema validation
- Ingestion Service with provider-agnostic webhook endpoints
- Processing Service with Kafka consumer and event enrichment
- Realtime Gateway with WebSocket support
- Angular 20 Dashboard with live metrics and event stream viewer
- OpenTelemetry distributed tracing across all services
- Prometheus metrics exposure per service
- Loki structured log aggregation
- Dead Letter Queue (DLQ) for failed events
- Retry mechanisms with exponential backoff
- Twilio, Infobip, and SendGrid integration endpoints

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

[Unreleased]: https://github.com/FelipeBastosxj/eventstream-observability-engine/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/FelipeBastosxj/eventstream-observability-engine/releases/tag/v0.1.0
