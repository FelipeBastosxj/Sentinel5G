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
- [x] Optional NATS auth/TLS (`pkg/events.Config`, mirrored on the Python
      side) and `gosec`/`bandit`/`govulncheck`/`pip-audit` in CI — see
      `docs/integrations.md`'s "Securing the NATS message bus" section.
- [x] Layer 1 -> Layer 2 bridge (`pkg/ingestion`): until now,
      `bpf/packet_filter.c` exported only the `blocklist` and `signal_rate`
      maps — nothing turned an individual kernel-observed packet into a
      `NormalizedEvent`, so the AI engine had never scored real traffic, only
      synthetic or hand-published events. `signaling_events` (a ring buffer)
      plus `pkg/ingestion.Publisher` and `pkg/controller.PodIPIndex` close
      that gap; verified against real GTP-U from a live Open5GS+UERANSIM 5G
      core, through the real trained model, producing a real
      `ThreatScoreEvent` correlated back to its source packet.

## Phase 1 — Real-world signal

- [x] Real dataset at scale, retrained, thresholds validated against it
      (`docs/paper-data/real-dataset/`, `cmd/ai-engine/scripts/
      build_real_dataset.py`) — 5 capture pairs against the live
      Open5GS+UERANSIM core (1,889 normal + 32,235 anomalous samples,
      superseding the original 30/618-packet pair). "Validate" surfaced a
      real, load-bearing negative result, not a clean pass: the only sample
      type that is both genuinely protocol-real GTP-U *and* actually
      observable by `bpf/packet_filter.c` today — an in-tunnel flood-ping
      storm — scores *below* the normal baseline on the real-trained model
      and is caught at 0/3,348 recall at every production sensitivity tier.
      The real-trained model's near-identical pooled AUC to the synthetic
      one (0.9449 vs 0.9459) is driven almost entirely by a non-protocol-
      real high-rate UDP injection needed to reach the synthetic training
      range, plus a scan category the current kernel program can't observe
      in production at all (see the item below). Real SIP/SMPP remain
      synthetic-only — this Open5GS deployment runs no IMS or SMPP
      infrastructure. Full breakdown and honest reading:
      `docs/paper-data/02-ai-training-inference.md` section 2.4.
      Reproduce: `python scripts/build_real_dataset.py && python
      scripts/evaluate_model.py --source real` from `cmd/ai-engine/`.
      Whether the real flood-ping ceiling (~60 pkt/s) reflects a
      WSL2-specific throughput cap or a realistic real-attacker bound is
      still open — needs a non-WSL2, higher-throughput environment to
      settle, not assumed either way here.
- [ ] Layer 1 can't see non-signaling-port traffic at all: `bpf/
      packet_filter.c`'s `is_signaling_port()` gates both
      `track_signal_rate()` and `emit_signaling_event()`, so any UDP packet
      outside port 2152/5060 is `XDP_PASS`ed with zero observation emitted
      — a real "scan"/off-protocol-probe capture (`real_scan.pcap` above)
      scores maximally anomalous *if* scored, but no code path today ever
      turns it into a `NormalizedEvent` in production. Needs a real design
      pass before touching the XDP hot path, not a quick patch — the
      options considered so far trade off differently against
      `CLAUDE.md`'s <0.2ms/packet budget and the eBPF maps' fixed sizing:
      (1) a generic per-source rate counter gated by a minimum threshold
      before emitting for non-signaling ports (cheap, but only gives a rate
      signal, not payload visibility), vs (2) emitting for all UDP
      unconditionally and filtering in `pkg/ingestion` instead (simple, but
      reintroduces exactly the per-packet userspace cost eBPF exists to
      avoid). Deliberately deferred rather than decided under today's push.
- [x] `controller-gen` wired into `make manifests` (`make manifests` /
      `controller-gen` targets), replacing the hand-maintained
      `zz_generated.deepcopy.go` and CRD YAML — CI now fails on drift
      between the Go types and the generated output (ci.yml's "manifests"
      step). Regenerating also caught two validations the hand-written CRD
      had that the Go markers didn't (`status.phase`'s enum, the `tsp`
      short name) — now real `+kubebuilder:validation:Enum` /
      `+kubebuilder:resource:shortName` markers instead of drift.
- [x] CO-RE (`vmlinux.h`-based) BPF program, replacing the UAPI-header
      approach (`bpf/Makefile` generates `bpf/headers/vmlinux.h` at build
      time from the build machine's kernel BTF, gitignored, not committed).
      Worth being precise about what this actually bought, rather than
      overselling it: `ethhdr`/`iphdr`/`udphdr` are wire-format structs
      whose layout is fixed by the Ethernet/IP/UDP protocols, not kernel
      build config, so CO-RE's actual relocation mechanism
      (`BPF_CORE_READ()`) has nothing to protect here and isn't used — the
      real, concrete win is dropping the UAPI-header build dependency
      (no more `linux-libc-dev`/`linux-headers-$(uname -r)`, no more the
      `<asm/types.h>` multiarch include-path workaround), not struct-layout
      portability. Verified: two independent clean rebuilds produced an
      identical 17184-byte object; attached to the live Open5GS+UERANSIM
      core's `lo` interface, real GTP-U traffic still tracked correctly in
      `signal_rate` (functional parity with the pre-CO-RE build), detached
      cleanly, core undisturbed throughout.
- [x] Finalizer-based cleanup: `security.sentinel5g.io/finalizer`
      (`pkg/controller.Reconciler.finalize`) releases any mesh quarantine
      and unblocks every source IP a policy pushed into the eBPF blocklist
      (tracked in the new `Status.BlockedSourceIPs`) before the object is
      actually deleted — no more manual `kubectl delete authorizationpolicy`
      to clean up after retiring a policy. Deliberately narrower than the
      de-escalation item below: this only fires on policy *deletion*, never
      on a later low score while the policy still exists.
- [ ] Automatic de-escalation: today, once `EbpfBlock`/`IsolatePod` fires,
      nothing ever calls `Unblock`/`Release` again even if the offending
      source's score later drops — this is a deliberate fail-safe (don't
      auto-restore access to something that scored as an active threat), not
      an oversight, but it means every mitigation is currently a one-way
      door requiring manual intervention (`kubectl delete authorizationpolicy
      ...` / restarting the eBPF attach) to reverse. Worth a real design pass
      — likely a minimum-dwell-time plus a sustained-low-score requirement —
      rather than a naive "unblock on the next low score" rule, which would
      itself be a trivial evasion.

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
