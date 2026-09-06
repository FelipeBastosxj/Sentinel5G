# Paper / Dossier Evidence Data

This folder collects primary-source technical evidence about Sentinel5G —
performance data, AI model evaluation, architecture/engineering decisions,
validation logs, and community metrics — organized for reuse in a paper or
an evidentiary dossier (e.g. extraordinary-ability immigration filings).

Snapshot baseline: commit `3746a89` (2026-09-06), repo
[`FelipeBastosxj/Sentinel5G`](https://github.com/FelipeBastosxj/Sentinel5G).

## Ground rule: no fabricated numbers

Every figure in this folder is one of exactly three things, and says which:

1. **Measured** — produced by a command listed right next to it, on this
   snapshot. Re-running that command should reproduce it (allowing for
   normal run-to-run noise in stochastic training).
2. **Sourced** — pulled verbatim from a commit message, CI config, or code
   comment already in this repo's history, cited by commit hash/file path.
3. **Pending** — explicitly not yet available, with the reason and the exact
   next step to produce it. Never presented as a number.

This distinction matters most for category 1 (benchmarks): `README.md` and
`docs/observability.md` are explicit that the `<0.2ms` / `<2%` / single-digit-ms
figures are **design targets**, not measurements — no load-testing harness
exists yet (`ROADMAP.md` Phase 3). This folder preserves that distinction
rather than laundering a target into an implied benchmark.

## Contents

| File | Category |
|---|---|
| [`01-performance-benchmarks.md`](01-performance-benchmarks.md) | Performance & latency: eBPF/XDP processing delay, Operator/AI-engine resource usage, NATS JetStream throughput |
| [`02-ai-training-inference.md`](02-ai-training-inference.md) | AI training & inference: accuracy/ROC/confusion matrix, real vs. synthetic score comparison, dataset evolution |
| [`03-architecture-engineering-decisions.md`](03-architecture-engineering-decisions.md) | Architecture diagrams and real engineering decisions/bottlenecks resolved, sourced from commit history |
| [`04-validation-testing-logs.md`](04-validation-testing-logs.md) | Integration test output (`go test -v`, `pytest`), CI/CD reports: coverage, SAST, SBOM |
| [`05-community-metrics.md`](05-community-metrics.md) | GitHub/registry community metrics (stars, forks, clones, image pulls) |

## Resolved: the WSL2 live-validation background session

An earlier draft of this folder flagged a gap — a specific
normal-vs-signaling-storm score comparison that lived only in a separate,
unreachable background Claude Code session's terminal history. That gap is
closed: `02-ai-training-inference.md` §2.2 now has a freshly measured,
independently reproducible replacement (real `tcpdump` captures scored
through the real production model), plus the first real PCAP pair this
project has (§2.3). The session-log number was not recovered and is not
needed — the fresh measurement supersedes it.
