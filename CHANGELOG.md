# Changelog

Notable changes to Sentinel5G, per release. See `ROADMAP.md` for what's
planned next.

## [0.2.2] - 2026-09-08

### Added
- Images now also published to Docker Hub, and every published image
  (GHCR and Docker Hub alike) is now multi-arch (`linux/amd64` +
  `linux/arm64`) instead of `amd64`-only.
- `.github/workflows/e2e.yml`: builds this ref's own images and runs
  `scripts/quickstart.sh` against a real `kind` cluster on every PR --
  catches cluster-environment bugs (like the Istio-CRD one fixed in 0.2.1)
  in CI instead of by hand.
- `docs/troubleshooting.md`: consolidated guide for the install issues
  reported from cloud VMs and GitHub Codespaces that don't show up on a
  local dev machine (network egress, RBAC, DNS-in-kind, ARM64, eBPF).
- `scripts/quickstart.sh`: fast, root-caused preflight checks for network
  egress and RBAC permissions, instead of failing as a generic timeout
  deep inside `kind`/`helm`.

### Fixed
- The compiled eBPF object (`bpf/packet_filter.o`) was never actually
  included in the published operator image, and nothing documented how to
  get it there -- `ebpf.enabled: true` silently no-op'd on every real
  deployment. Now baked into the image at build time.
- `ebpf.enabled: true` didn't grant the Linux capabilities it needs
  (`CAP_BPF`/`CAP_NET_ADMIN`) -- now wired automatically.
- eBPF attach failures logged a bare error string; now classified (missing
  object, insufficient privilege, unknown interface, incompatible kernel).

### Changed
- The eBPF build no longer depends on `bpftool` or the host kernel's BTF
  (`bpf/headers/vmlinux_min.h` replaces the generated `vmlinux.h`), so it
  now also builds inside a plain `docker build`, not just on a real Linux
  machine.

## [0.2.1] - 2026-09-07

### Fixed
- Operator got stuck at `Phase: Monitoring` forever when Istio wasn't
  installed (the default `MESH_ADAPTER=istio` built a real Istio adapter
  unconditionally). Now falls back to a no-op mesh adapter, same as eBPF
  already does when not attached.

### Added
- `scripts/quickstart.sh`: one command from a fresh clone to a real
  mitigation firing, using published images.

## [0.2.0] - 2026-09-07

### Added
- Real training dataset from a live Open5GS+UERANSIM core, replacing the
  synthetic-only dataset.
- Off-signaling-port scan detection in `bpf/packet_filter.c`.
- `controller-gen` wired into `make manifests`, CI drift check.
- CO-RE (`vmlinux.h`) build for the eBPF program.
- Finalizer-based cleanup on policy deletion.
- Automatic mitigation de-escalation after a quiet period.
- `LOG_LEVEL` now actually controls the operator's log output.
- `pkg/mesh` and the AI engine's HTTP/NATS layer gained test coverage.

### Fixed
- eBPF: non-initial IP fragments were parsed as if they had a real UDP
  header, corrupting telemetry.
- Per-node signaling events were silently dropped on multi-replica
  operator deployments.
- `pkg/ebpf.Loader.SignalRate()` always returned 0 for real events (a
  struct-padding bug, present since it shipped).
- Release pipeline now scans images with Trivy before publishing, not
  after.
- CI now catches drift between the CRD and the Helm chart's copy of it.

### Changed
- **Breaking:** the AI engine's NATS durable consumer was renamed to add
  queue-group support (enables horizontal scaling). Existing deployments
  need to redeploy.

## [0.1.0] - 2026-09-06

Initial public release: `TelecomSecurityPolicy` CRD, closed-loop
mitigation (eBPF blocklist + Istio quarantine), autoencoder-based AI
engine, Helm chart, and CI with SBOM/scan/sign.
