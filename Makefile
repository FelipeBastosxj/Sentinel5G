SHELL := /bin/bash
IMG_OPERATOR  ?= ghcr.io/felipebastosxj/sentinel5g-operator:latest
IMG_AI_ENGINE ?= ghcr.io/felipebastosxj/sentinel5g-ai-engine:latest
IMG_MODEL     ?= ghcr.io/felipebastosxj/sentinel5g-model:latest

.PHONY: all
all: build test

## --- Go operator ---------------------------------------------------------

.PHONY: build
build:
	go build -o bin/manager ./cmd/operator

.PHONY: run
run:
	go run ./cmd/operator

.PHONY: test
test:
	go test ./pkg/... ./api/... -v -cover

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	golangci-lint run

CONTROLLER_GEN ?= $(shell go env GOPATH)/bin/controller-gen

.PHONY: controller-gen
controller-gen:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5

.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) object paths="./api/v1alpha1/..."
	$(CONTROLLER_GEN) crd paths="./api/v1alpha1/..." output:crd:artifacts:config=config/crd/bases
	@echo "config/crd/bases/*.yaml regenerated -- keep charts/sentinel5g-operator/templates/crd.yaml"
	@echo "in sync by hand (it's templated for the crds.install toggle, not a plain copy)."

## --- eBPF -----------------------------------------------------------------

.PHONY: bpf
bpf:
	$(MAKE) -C bpf

.PHONY: bpf-clean
bpf-clean:
	$(MAKE) -C bpf clean

## --- AI engine (Python) ----------------------------------------------------

.PHONY: ai-engine-install
ai-engine-install:
	cd cmd/ai-engine && pip install -e ".[dev]"

.PHONY: ai-engine-test
ai-engine-test:
	cd cmd/ai-engine && pytest

.PHONY: ai-engine-lint
ai-engine-lint:
	cd cmd/ai-engine && black --check . && flake8 .

.PHONY: ai-engine-train
ai-engine-train:
	cd cmd/ai-engine && python scripts/generate_synthetic_dataset.py && \
		python scripts/train.py && python scripts/export_onnx.py

# Trains against the REAL captures committed under docs/paper-data/real-dataset/
# rather than the synthetic generator above. This is what the release
# workflow runs to build the published model artifact, so the artifact is
# reproducible from the repository alone -- previously this path existed only
# as loose commands in docs/getting-started.md.
.PHONY: ai-engine-train-real
ai-engine-train-real:
	cd cmd/ai-engine && python scripts/build_real_dataset.py && \
		python scripts/train.py --dataset data/real_dataset.npz --output models/autoencoder.pt && \
		python scripts/export_onnx.py --weights models/autoencoder.pt --dataset data/real_dataset.npz

## --- Containers -------------------------------------------------------------

.PHONY: docker-build-operator
docker-build-operator:
	docker build -t $(IMG_OPERATOR) .

.PHONY: docker-build-ai-engine
docker-build-ai-engine:
	docker build -t $(IMG_AI_ENGINE) cmd/ai-engine

# The model as its own OCI artifact, consumed by charts/sentinel5g-ai-engine's
# initContainer. Needs cmd/ai-engine/models/ populated first -- run
# ai-engine-train-real above.
.PHONY: docker-build-model
docker-build-model:
	docker build -f cmd/ai-engine/Dockerfile.model -t $(IMG_MODEL) cmd/ai-engine

## --- Helm / Kustomize ---------------------------------------------------

# Both charts, not just the operator's: the AI engine chart is what makes a
# deployment able to score at all, so leaving it out of `make ci` would mean
# the piece most likely to be misconfigured is the one nothing validates.
.PHONY: helm-lint
helm-lint:
	helm lint charts/sentinel5g-operator
	helm lint charts/sentinel5g-ai-engine

.PHONY: helm-template
helm-template:
	helm template sentinel5g charts/sentinel5g-operator
	helm template sentinel5g-ai-engine charts/sentinel5g-ai-engine

## --- Everything -------------------------------------------------------------

.PHONY: ci
ci: vet test bpf ai-engine-test helm-lint
