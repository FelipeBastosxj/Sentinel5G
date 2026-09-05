SHELL := /bin/bash
IMG_OPERATOR  ?= ghcr.io/sentinel5g/sentinel5g-operator:latest
IMG_AI_ENGINE ?= ghcr.io/sentinel5g/sentinel5g-ai-engine:latest

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

.PHONY: manifests
manifests:
	@echo "controller-gen is not wired in yet (see ROADMAP.md)."
	@echo "Edit api/v1alpha1/zz_generated.deepcopy.go and config/crd/bases/*.yaml by hand,"
	@echo "keeping charts/sentinel5g-operator/templates/crd.yaml in sync."

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

## --- Containers -------------------------------------------------------------

.PHONY: docker-build-operator
docker-build-operator:
	docker build -t $(IMG_OPERATOR) .

.PHONY: docker-build-ai-engine
docker-build-ai-engine:
	docker build -t $(IMG_AI_ENGINE) cmd/ai-engine

## --- Helm / Kustomize ---------------------------------------------------

.PHONY: helm-lint
helm-lint:
	helm lint charts/sentinel5g-operator

.PHONY: helm-template
helm-template:
	helm template sentinel5g charts/sentinel5g-operator

## --- Everything -------------------------------------------------------------

.PHONY: ci
ci: vet test bpf ai-engine-test helm-lint
