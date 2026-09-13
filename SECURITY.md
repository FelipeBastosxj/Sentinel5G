# Security Policy

Sentinel5G is a security tool for telecom infrastructure, so vulnerabilities
in it carry more weight than in most projects — please report them
responsibly rather than through a public issue.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting rather than a public issue or
PR: go to the [Security tab](https://github.com/FelipeBastosxj/Sentinel5G/security)
of this repository and select **Report a vulnerability**. This opens a
private advisory visible only to maintainers until a fix is ready, and lets
you attach reproduction details (payloads, configs, logs) that shouldn't be
public before a patch ships.

If you're unable to use GitHub's reporting flow, open a regular issue asking
a maintainer to reach out for a private channel instead of any public issue
that describes the vulnerability itself.

Please include, as far as you're able to:

- The affected component (`bpf/`, `pkg/`, `cmd/ai-engine`, a specific Helm
  chart template, etc.) and version/commit.
- A description of the vulnerability and its impact — for Sentinel5G
  specifically, that often means: can it forge/suppress a `ThreatScoreEvent`
  and trigger or block a real mitigation, bypass the eBPF blocklist, escape
  the AI engine's `/v1/score` input handling, or escalate the elevated
  capabilities `ebpf.enabled: true` grants the manager container.
- Steps to reproduce, or a proof of concept if you have one.
- Any suggested fix or mitigation, if you have one — not required.

## Response

This is a small, early-stage project (see `README.md`'s "reference
implementation" status and `ROADMAP.md`), maintained without a dedicated
security team, so please treat the timelines below as a good-faith target,
not a contractual SLA:

- **Acknowledgement:** within 5 business days.
- **Initial assessment** (severity, affected versions, rough fix timeline):
  within 10 business days of acknowledgement.
- **Fix or mitigation:** timeline depends on severity and complexity — a
  critical, easily-exploitable issue (e.g. one that lets an unauthenticated
  party trigger a real automated mitigation, or bypass eBPF enforcement
  entirely) is prioritized over a hardening improvement with no known
  exploit path.

## Severity, roughly

There's no formal CVSS scoring process for a project this size, but reports
are triaged along these lines:

- **Critical:** remote, unauthenticated compromise of the operator, AI
  engine, or eBPF probe; forging a `ThreatScoreEvent` to trigger unwanted
  mitigation against a real workload without reachable NATS credentials (see
  `docs/integrations.md`'s "Securing the NATS message bus"); privilege
  escalation via the elevated capabilities `ebpf.enabled: true` grants.
- **High:** a way to bypass the eBPF blocklist or mesh quarantine once
  applied; a way to suppress or delay real detections; a supply-chain issue
  in the published images/SBOM/signing pipeline (`.github/workflows/release.yml`).
- **Medium/Low:** issues that need an already-privileged position to
  exploit (e.g. a NATS bus that was left unauthenticated against this
  project's own explicit guidance), denial-of-service against a single
  component that doesn't cascade, or hardening gaps without a concrete
  exploit path.

## Supported versions

Sentinel5G is pre-1.0 (`v0.x`, see `CHANGELOG.md`): only the most recently
published release gets security fixes. There's no long-term support branch
yet — upgrading to the latest release is the supported way to pick up a fix.

## Things worth knowing when assessing a report

- Anything that can publish to `NATS_THREATS_SUBJECT` can trigger a real
  mitigation, which is why both the operator and the AI engine refuse an
  unauthenticated bus unless told otherwise. A forged event with
  `model: "rule:..."` and `score: 1.0` clears every sensitivity tier by
  design (`pkg/detect` relies on exactly that), so bus authentication is the
  control, not the threshold.
- The model artifact is a trust boundary: a tampered `autoencoder.norm.json`
  silently rescales every score. The published `sentinel5g-model` image is
  cosign-signed; a `model.existingSecret` is whatever you put in it.
- Wire-derived strings never become Prometheus label values as-is
  (`pkg/controller/metrics.go`'s `scoreSourceLabel` maps them to a closed
  set), so a forged `model` string is not a series-cardinality vector.

## Scope

In scope: this repository's own code (`bpf/`, `pkg/`, `cmd/`,
`charts/sentinel5g-operator`, `charts/sentinel5g-ai-engine`, `config/`,
`scripts/`, the container images — including the published model artifact
`sentinel5g-model` — and the SBOM/signing pipeline in
`.github/workflows/release.yml`).

Out of scope: vulnerabilities in upstream dependencies (report those
upstream — `govulncheck`/`pip-audit`/`bandit`/`gosec` already run in CI to
catch known ones, see `.github/workflows/ci.yml`) and issues that require an
already-compromised cluster or node to exploit.
