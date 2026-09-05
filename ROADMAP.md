# Roadmap

Sentinel5G is at an early, foundational stage: the four-layer architecture
described in [`docs/architecture.md`](docs/architecture.md) is implemented
end to end as a working reference, but several pieces are intentionally
scoped down for a first release. This page tracks what's next, and is meant
to be read alongside the gaps called out in `docs/getting-started.md`,
`docs/integrations.md`, and `CONTRIBUTING.md`.

## Phase 0 — Foundations (current)

- [x] `TelecomSecurityPolicy` CRD, reconciler, and in-memory policy index.
- [x] `bpf/packet_filter.c`: XDP capture with a blocklist map and a
      per-source signaling-rate counter (UAPI headers, not CO-RE/`vmlinux.h`).
- [x] Autoencoder-based AI engine: synthetic dataset generation, training,
      ONNX export, and an HTTP + NATS-worker inference server.
- [x] Closed-loop mitigation: eBPF blocklist push + Istio
      `AuthorizationPolicy` quarantine, gated by per-policy `autoMitigate`.
- [x] Helm chart, kustomize manifests, CI (lint/test/SBOM/scan/sign).

## Phase 1 — Real-world signal

- [ ] Replace the synthetic dataset with real (or realistically replayed)
      GTP-U/SIP/SMPP traffic; validate sensitivity thresholds against it.
- [ ] `controller-gen` wired into `make manifests`, replacing the
      hand-maintained `zz_generated.deepcopy.go` and CRD YAML.
- [ ] CO-RE (`vmlinux.h`-based) BPF program for portability across kernel
      struct layouts, replacing the current UAPI-header approach.
- [ ] Finalizer-based cleanup: automatically unblock/un-quarantine a
      workload when its `TelecomSecurityPolicy` is deleted, rather than
      requiring a manual `Release`/`Unblock`.

## Phase 2 — Deeper integrations

- [ ] Cilium-native capture path (Hubble flow API or a Cilium custom BPF
      program) as an alternative to the standalone XDP attachment.
- [ ] Falco output bridging into `NormalizedEvent`, so syscall-level signals
      feed the same AI engine as network-level ones.
- [ ] Additional `pkg/mesh.Adapter` implementations beyond Istio (Linkerd,
      Cilium mesh mode).
- [ ] Prometheus instrumentation for the AI engine (request latency,
      inference count), not just the operator's `controller-runtime` metrics.

## Phase 3 — Scale & multi-cluster

- [ ] Multi-cluster policy propagation.
- [ ] Load-testing harness validating the <0.2ms/packet and single-digit-ms
      mitigation-latency targets in `docs/observability.md` under sustained
      throughput, not just unit tests.
- [ ] Poetry-based lockfile support for `cmd/ai-engine` alongside the
      current `pyproject.toml`/`requirements.txt` pair, if the community
      wants a stricter reproducible-build story.

Have an idea that isn't here? Open an issue — see
[`CONTRIBUTING.md`](CONTRIBUTING.md).
