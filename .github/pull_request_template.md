## Description

<!-- Describe the changes introduced by this PR. Be clear and concise. -->

## Related Issue

Closes #<!-- Issue number -->

## Type of Change

<!-- Mark the relevant option with an [x] -->

- [ ] `feat` — New feature
- [ ] `fix` — Bug fix
- [ ] `docs` — Documentation only
- [ ] `chore` — Maintenance, tooling, dependencies
- [ ] `test` — Adding or updating tests
- [ ] `refactor` — Code refactoring (no functional change)
- [ ] `perf` — Performance improvement
- [ ] `ci` — CI/CD configuration

## Affected Service(s)

<!-- Mark all that apply -->

- [ ] `ingestion-service`
- [ ] `processing-service`
- [ ] `realtime-gateway`
- [ ] `webhook-service`
- [ ] `angular-dashboard`
- [ ] `infra / docker-compose`
- [ ] `shared / contracts`

## How Has This Been Tested?

<!-- Describe how you tested these changes. Include steps to reproduce. -->

## Architecture Compliance

<!-- Confirm this PR follows the rules defined in HARDNESS.md -->

- [ ] All business flows go through Kafka events
- [ ] Services remain stateless
- [ ] Canonical Event Model is respected
- [ ] Clean Architecture layers are respected (Controller → Application → Domain → Infrastructure)
- [ ] No business logic inside controllers
- [ ] No framework dependency inside domain layer

## Observability

- [ ] OpenTelemetry instrumentation added or updated
- [ ] Prometheus metrics added or updated (if applicable)
- [ ] Structured logs are in place
- [ ] Correlation ID propagation is maintained

## Definition of Done

- [ ] Tests pass (`make test`)
- [ ] No linting errors (`make lint`)
- [ ] Docker Compose environment starts correctly
- [ ] Documentation updated
- [ ] `CHANGELOG.md` updated under `[Unreleased]`

## Screenshots / Recordings

<!-- If applicable, add screenshots or recordings to help explain the changes (especially for frontend changes). -->
