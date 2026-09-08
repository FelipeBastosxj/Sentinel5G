# Getting started

This walks through running every layer locally: NATS JetStream, the AI
engine's train → export → serve pipeline, and the operator against a local
Kubernetes cluster (e.g. [kind](https://kind.sigs.k8s.io/)).

## Prerequisites

- Go 1.26+ (matches `go.mod`'s `go`/`toolchain` directives; with the default
  `GOTOOLCHAIN=auto`, an older local Go silently downloads 1.26 the first
  time you build, which needs network access)
- Python 3.11+
- Docker (or another OCI runtime) and Docker Compose
- A local Kubernetes cluster (`kind`, `minikube`, or similar) and `kubectl`
- `clang`/`llvm` + `libbpf-dev`, only if you want to compile
  `bpf/packet_filter.c` yourself (Linux-only; see `bpf/Makefile`) — the
  operator's Dockerfile already does this for you as part of `docker build`,
  so this is only needed for local, outside-a-container iteration on
  `bpf/packet_filter.c` itself. No `bpftool`, kernel headers, or access to
  your own kernel's BTF required: the build uses
  `bpf/headers/vmlinux_min.h`, a small hand-maintained header, instead — see
  its top comment for why. See `docs/troubleshooting.md#ebpf` for what
  enabling eBPF enforcement in a deployed cluster needs.

## 1. Bootstrap the Go module

```sh
go mod tidy   # resolves go.sum against the dependencies pinned in go.mod
go build ./...
go test ./... -cover
```

## 2. Bring up NATS JetStream + the AI engine

The AI engine needs a trained, exported model before it can serve scores:

```sh
cd cmd/ai-engine
pip install -e ".[dev,train]"  # train.py/export_onnx.py need torch -- see requirements.txt's header

python scripts/generate_synthetic_dataset.py
python scripts/train.py
python scripts/export_onnx.py
pytest
```

This produces `models/autoencoder.onnx` and `models/autoencoder.norm.json`.
`generate_synthetic_dataset.py` fabricates traffic (it does not ship with,
or claim to represent, real telecom captures) purely to exercise the
pipeline end to end — see the module docstring and
`docs/architecture.md`.

### Training against real captures instead

`docs/paper-data/real-dataset/` bundles real GTP-U packet captures from a
live Open5GS+UERANSIM 5G core (see that directory's README for exactly how
they were produced, and its honestly-stated scope limits — single UE, no
real SIP/SMPP, roughly one hour of wall-clock capture time). To train
against those instead of synthetic data:

```sh
python scripts/build_real_dataset.py
python scripts/train.py --dataset data/real_dataset.npz --output models/autoencoder.pt
python scripts/export_onnx.py --weights models/autoencoder.pt --dataset data/real_dataset.npz
```

This overwrites the same `models/autoencoder.onnx` the server loads by
default. See `docs/paper-data/02-ai-training-inference.md` for the
accuracy/threshold numbers this produces, and
`scripts/build_real_dataset.py`'s module docstring for which anomaly types
in the resulting dataset are genuinely real vs. still an approximation (not
every synthetic anomaly type has an equally real production equivalent
yet).

With a model in place, bring up the local stack:

```sh
cd deployments
docker compose up --build
```

This starts NATS JetStream and the AI engine in `AI_ENGINE_MODE=nats`,
consuming `sentinel5g.events.normalized` and publishing
`sentinel5g.threats.scored`.

Alternatively, run the AI engine's HTTP mode directly for quick manual
testing without NATS:

```sh
cd cmd/ai-engine
AI_ENGINE_MODE=http python -m sentinel_ai.server
curl -X POST localhost:8090/v1/score -H 'content-type: application/json' \
  -d '{"protocol":"SIP","destPort":5060,"payloadSize":256,"ratePerSecond":3000,"malformed":false}'
```

## 3. Run the operator against a local cluster

Needs step 2's NATS instance already running (`docker compose up` in
`deployments/`, or at minimum a bare `docker run -p 4222:4222 nats:2.10-alpine
-js`) — the operator exits immediately if it can't reach `NATS_URL`.

```sh
kubectl apply -f config/crd/bases/security.sentinel5g.io_telecomsecuritypolicies.yaml
kubectl apply -f config/samples/security_v1alpha1_telecomsecuritypolicy.yaml -n telecom-core

export NATS_URL=nats://localhost:4222
go run ./cmd/operator
```

The operator moves the sample policy from `Pending` to `Monitoring`:

```sh
kubectl get telecomsecuritypolicy -n telecom-core protect-amf-core -o wide
```

Publish a synthetic `ThreatScoreEvent` above the default threshold (0.85)
to see closed-loop mitigation kick in (`autoMitigate: true` in the sample):

```sh
nats pub sentinel5g.threats.scored '{
  "sourceEventId": "demo-1",
  "namespace": "telecom-core",
  "podName": "amf-0",
  "sourceIp": "203.0.113.7",
  "score": 0.93,
  "model": "autoencoder-v1",
  "detectedAt": "2026-01-05T12:00:00Z"
}'
```

(`amf-0` must exist and carry `app: amf-service` for the watcher to match it
against the sample policy — `kubectl label pod amf-0 app=amf-service -n telecom-core`.)

## 4. Install via Helm -- an alternative to step 3, not a continuation of it

This runs the operator in-cluster from the published image instead of via
`go run` on your machine, and manages the CRD itself. **Don't run this on
top of step 3** if you already `kubectl apply`'d the CRD by hand there: Helm
refuses to adopt a resource it didn't create ("invalid ownership metadata;
missing key \"app.kubernetes.io/managed-by\""), confirmed the hard way while
verifying this exact sequence. Either pick one path for a given cluster, or
`kubectl delete -f config/crd/bases/security.sentinel5g.io_telecomsecuritypolicies.yaml`
first (this deletes any `TelecomSecurityPolicy` objects too) so Helm can
create it fresh.

```sh
helm lint charts/sentinel5g-operator
helm install sentinel5g charts/sentinel5g-operator --namespace sentinel5g-system --create-namespace \
  --set nats.url=nats://<your-nats-service>:4222
```

See `charts/sentinel5g-operator/values.yaml` for the `ebpf.enabled` toggle
and `docs/integrations.md` for what enabling it requires. The README's
[Quick Start](../README.md#-quick-start) walks this same path end to end,
including a minimal NATS instance and a demo workload to protect, if you
just want to see it work rather than run it against your own cluster/NATS.

## Known local-environment gaps

- If you're starting from a fresh clone without a `go.sum` yet, `go mod
  tidy` needs network access to a Go module proxy to populate it the first
  time.
- `bpf/packet_filter.c` only compiles on Linux — see the Prerequisites note
  above for what that needs.

## Something not working?

Network egress, DNS-inside-kind, RBAC, ARM64 image availability, and eBPF
kernel/capability requirements are all covered in
[`docs/troubleshooting.md`](troubleshooting.md), not repeated here.
