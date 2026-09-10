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
  `api/v1alpha1/*_types.go`, run `make manifests` (wraps `controller-gen`) to
  regenerate `zz_generated.deepcopy.go` and `config/crd/bases/*.yaml` — CI
  fails on drift between the Go types and that generated output, so don't
  hand-edit either file. `charts/sentinel5g-operator/templates/crd.yaml` is
  a separate, still hand-copied file (see the note at its own top for why);
  update it to match `config/crd/bases/*.yaml`'s new content too.
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

- CI must pass (lint + tests across every changed toolchain). `.github/
  workflows/ci.yml`'s four jobs (`go`, `python`, `bpf`, `helm-lint`) are
  path-filtered via a `changes` job (`dorny/paths-filter`) — a PR touching
  only `cmd/ai-engine/` shows the other three as skipped, not failed; that's
  expected, not a CI problem. Editing `ci.yml` itself always runs every job.
- CRD manifests must be updated if the API changed.
- eBPF changes must be reviewed for kernel-version compatibility and stack
  usage, not just for compiling locally.

## Reporting issues

Use [GitHub Issues](https://github.com/FelipeBastosxj/Sentinel5G/issues) with
the provided bug report / feature request templates.

## Maintainer setup: publishing releases

`.github/workflows/release.yml` (triggered by pushing a `vX.Y.Z` tag) builds,
scans, and publishes multi-arch (`linux/amd64`+`linux/arm64`) images to both
GHCR and Docker Hub. GHCR only needs the repo's own `GITHUB_TOKEN` (already
available to every workflow run), but Docker Hub needs two repository
secrets that aren't set up automatically:

1. Create a Docker Hub account/organization for the project, if there isn't
   one already.
2. Create an [Access
   Token](https://docs.docker.com/security/for-developers/access-tokens/)
   (not your account password) scoped to push to that account/org.
3. Add it as two repository secrets under **Settings → Secrets and
   variables → Actions**: `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.

Without these secrets, the release workflow's Docker Hub login step fails —
nothing in the workflow itself can create the account or token for you.
