# Contributing to Sentinel5G

Thanks for your interest in Sentinel5G! This project spans four fairly
different toolchains (Go, C/eBPF, Python, Kubernetes manifests), so please
read the section that matches what you're changing before opening a PR.

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By
participating, you are expected to uphold it.

## Development setup

See [`docs/getting-started.md`](docs/getting-started.md) for the full local
setup (Go module, AI engine train/export pipeline, docker-compose stack,
running the operator against a local cluster).

## Branch naming

- `feat/<short-description>`
- `fix/<short-description>`
- `docs/<short-description>`

## Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(ebpf): add support for GTP-U header inspection
fix(operator): resolve deadlock on policy update
docs(readme): update helm installation instructions
```

## Before opening a PR

- **Go (`api/`, `cmd/operator`, `pkg/`):** `go build ./...`, `go vet ./...`,
  `go test ./... -cover`, `golangci-lint run`. If you changed
  `api/v1alpha1/*_types.go`, update `zz_generated.deepcopy.go` and
  `config/crd/bases/*.yaml` / `charts/sentinel5g-operator/templates/crd.yaml`
  by hand to match (see the note at the top of `zz_generated.deepcopy.go`
  about wiring in `controller-gen`).
- **eBPF (`bpf/`):** `make -C bpf` must compile cleanly. Keep stack usage
  well under the 512-byte eBPF limit and stick to standard integer sizing
  (`__u32`, `__u64`, `__u8`).
- **Python (`cmd/ai-engine`):** `black --check .`, `flake8`, `pytest`, all
  run from `cmd/ai-engine`. Public functions should carry type hints.
- **Manifests (`config/`, `charts/`):** `helm lint charts/sentinel5g-operator`.
  If you change the CRD schema, update it in both `config/crd/bases/` and
  `charts/sentinel5g-operator/templates/crd.yaml` — see the note in that
  chart template for why they're not shared.

## PR requirements

- CI must pass (lint + tests across every changed toolchain).
- CRD manifests must be updated if the API changed.
- eBPF changes must be reviewed for kernel-version compatibility and stack
  usage, not just for compiling locally.

## Reporting issues

Use [GitHub Issues](https://github.com/sentinel5g/sentinel5g/issues) with
the provided bug report / feature request templates.
