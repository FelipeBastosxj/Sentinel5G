# Paper / Dossier Evidence Data

This folder collects primary-source technical evidence about Sentinel5G —
performance data, AI model evaluation, architecture/engineering decisions,
validation logs, and community metrics — organized for reuse in a paper or
an evidentiary dossier (e.g. extraordinary-ability immigration filings).

Snapshot baseline: commit `3746a89` (2026-09-06), repo
[`FelipeBastosxj/Sentinel5G`](https://github.com/FelipeBastosxj/Sentinel5G).
Measurements since then cite their own commit inline (§2.5, §2.6 of the AI
document were taken on 2026-09-11/13 against the Phase 2.5 branch).

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
| [`test-environment.md`](test-environment.md) | The two lab environments every measurement here was taken on: the original WSL2 core (single UE) and the native-Linux core (four UEs) that replaced it, with build steps and the generator traps found on the way |
| [`real-dataset/`](real-dataset/README.md) | Single-UE real GTP-U captures (2026-09-07), the training set for §2.4–§2.5 |
| [`real-dataset-v2/`](real-dataset-v2/README.md) | Multi-UE captures (2026-09-11): four tunnels behind one gNB, one flooding — the data behind §2.6 |

## Resolved: the WSL2 live-validation background session

An earlier draft of this folder flagged a gap — a specific
normal-vs-signaling-storm score comparison that lived only in a separate,
unreachable background Claude Code session's terminal history. That gap is
closed: `02-ai-training-inference.md` §2.2 now has a freshly measured,
independently reproducible replacement (real `tcpdump` captures scored
through the real production model), plus the first real PCAP pair this
project has (§2.3). The session-log number was not recovered and is not
needed — the fresh measurement supersedes it.

## Resolved: the WSL2 environment itself

Several files here used to cite `memory/wsl2_real_test_environment.md`, a
path that never existed in the repository. What it described is now
[`test-environment.md`](test-environment.md), alongside the native-Linux
lab that superseded it. Two of that environment's stated limits — a single
UE, and an in-tunnel flood ceiling of ~63 pkt/s attributed to WSL2 — are
addressed by [`real-dataset-v2/`](real-dataset-v2/README.md): four UEs, and
a measurement showing the ceiling was flood-ping's, not the environment's
(the same tunnel carries ~125,000 pkt/s from a real generator).
