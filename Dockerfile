# syntax=docker/dockerfile:1

# Base images pinned to a digest (multi-arch manifest-list digest, covering
# both linux/amd64 and linux/arm64 -- see release.yml's multi-arch build) on
# top of the tag, not instead of it: the tag stays for human/tooling context,
# the digest is what `docker build` actually resolves, so a same-tag
# upstream rebuild can't silently change what gets built. This trades away
# automatic security-patch pickup on rebuild -- .github/dependabot.yml's
# "docker" ecosystem entries are what re-resolves and bumps these on a
# schedule instead; don't just delete a stale digest by hand, let Dependabot
# (or `docker buildx imagetools inspect <image>:<tag>`, re-run by hand) open
# the PR.
FROM golang:1.27-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS builder
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
FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS bpf-builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    clang llvm libbpf-dev make \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /workspace
COPY bpf/ bpf/
RUN make -C bpf

FROM gcr.io/distroless/static:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7
WORKDIR /
COPY --from=builder /workspace/manager .
# Matches --bpf-object's default in cmd/operator/main.go.
COPY --from=bpf-builder /workspace/bpf/packet_filter.o /var/run/sentinel5g/packet_filter.o
USER 65532:65532

# `manager healthcheck` (cmd/operator/healthcheck.go) is a plain HTTP GET
# against this same process's own /healthz -- distroless/static has no
# shell and no curl/wget, so the running binary itself is the only thing
# HEALTHCHECK could possibly exec here. start-period gives the manager time
# to connect to NATS/the API server before the first check can fail it.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/manager", "healthcheck"]

ENTRYPOINT ["/manager"]
