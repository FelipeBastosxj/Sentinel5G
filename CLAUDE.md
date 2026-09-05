# CLAUDE.md - Guidelines for Sentinel5G Project

## Overview
Sentinel5G is an open-source, cloud-native security operator designed to secure critical 5G/LTE and telecom containerized workloads. It leverages eBPF for kernel-level inspection, real-time unsupervised AI threat detection, and automated closed-loop remediation via Kubernetes Custom Resource Definitions (CRDs).

---

## Architectural Rules & Standards
- **Component Separation:**
  - `bpf/`: C/eBPF probes for kernel-level packet capture (XDP/TC).
  - `cmd/operator/` & `pkg/`: Go implementation for K8s Controller/Operator logic (using `kubebuilder`).
  - `cmd/ai-engine/`: Inference engine executing lightweight ONNX ML models.
  - `api/v1alpha1/`: K8s CRD API definitions (`TelecomSecurityPolicy`).
- **Performance Constraints:**
  - Latency penalty per packet must remain strictly under 0.2ms.
  - Memory footprints for in-kernel probes must be bounded using fixed-size eBPF maps.
- **Security & Compliance:**
  - Zero-Trust Architecture design aligned with CISA and NIST guidelines for critical infrastructure.
  - Automated CI/CD must generate Software Bill of Materials (SBOM) and run static container vulnerability scans (`trivy`).

---

## Development & Build Commands

### Go (Operator & Controller)
- **Build Operator:** `go build -o bin/manager cmd/operator/main.go`
- **Run Unit Tests:** `go test ./pkg/... ./api/... -v -cover`
- **Generate CRD Manifests:** `make manifests`
- **Linting:** `golangci-lint run`

### eBPF Probes (C / BPF)
- **Compile eBPF Bytecode:** `clang -O2 -target bpf -c bpf/headers/packet_filter.c -o bin/bpf/packet_filter.o`
- **Load / Test Probe locally:** `sudo bpftool prog load bin/bpf/packet_filter.o /sys/fs/bpf/sentinel_filter`

### AI Engine (Python / ONNX)
- **Setup Environment:** `poetry install` or `pip install -r requirements.txt`
- **Run Model Inference Test:** `pytest tests/test_inference.py`
- **Export PyTorch Model to ONNX:** `python scripts/export_onnx.py --model-dir models/`

---

## Code Style & Guidelines

### Go
- Standard `gofmt` and `goimports` formatting required.
- Idiomatic Go error handling: always check errors explicitly and wrap with context (e.g., `fmt.Errorf("failed to reconcile policy: %w", err)`).
- Use `controller-runtime` patterns for Kubernetes controllers. Avoid global state.

### C / eBPF
- Stick to CO-RE (Compile Once – Run Everywhere) using BPF CO-RE helpers.
- Use explicit integer sizing (`__u32`, `__u64`, `__u8`).
- Keep stack usage below the hard 512-byte eBPF limit per program.

### Python / AI
- Strict PEP8 compliance using `black` and `flake8`.
- Use type hints (`typing` module) for all public functions.
- Models must be exported in `.onnx` format for runtime execution to avoid Python GIL overhead in production.

---

## Git & Workflow Conventions
- **Branch Naming:** `feat/<short-description>`, `fix/<short-description>`, `docs/<short-description>`
- **Commit Messages:** Follow Conventional Commits format:
  - `feat(ebpf): add support for GTP-U header inspection`
  - `fix(operator): resolve deadlock on policy update`
  - `docs(readme): update helm installation instructions`
- **PR Requirements:** All PRs must pass unit tests, contain updated CRD manifests (if API changed), and pass eBPF kernel compatibility checks.