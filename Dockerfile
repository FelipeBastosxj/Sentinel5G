# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS builder
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/operator/ cmd/operator/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=linux go build -a -o manager ./cmd/operator

# Compiles bpf/packet_filter.c into the image, so a plain `docker build .`
# always produces a complete image -- no separate `make -C bpf` step for the
# caller to remember, and no dependency on the host's kernel BTF (see
# bpf/headers/vmlinux_min.h's own top comment for why that's possible here).
# Previously nothing put this object file in the image at all: --bpf-object
# defaulted to a path (see cmd/operator/main.go) that was simply never
# populated, so EbpfBlock actions silently no-op'd on every deployment that
# set ebpf.enabled: true expecting it to work.
FROM debian:bookworm-slim AS bpf-builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    clang llvm libbpf-dev make \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /workspace
COPY bpf/ bpf/
RUN make -C bpf

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
# Matches --bpf-object's default in cmd/operator/main.go.
COPY --from=bpf-builder /workspace/bpf/packet_filter.o /var/run/sentinel5g/packet_filter.o
USER 65532:65532

ENTRYPOINT ["/manager"]
