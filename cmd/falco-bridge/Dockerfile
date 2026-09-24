# syntax=docker/dockerfile:1

# Same digest-pinning rationale as the top-level Dockerfile: the tag stays
# for human/tooling context, the digest is what `docker build` actually
# resolves, so a same-tag upstream rebuild can't silently change what gets
# built. .github/dependabot.yml's "docker" ecosystem keeps this current.
FROM golang:1.27-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS builder
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/falco-bridge/ cmd/falco-bridge/
COPY pkg/events/ pkg/events/
COPY pkg/falco/ pkg/falco/

RUN CGO_ENABLED=0 GOOS=linux go build -a -o falco-bridge ./cmd/falco-bridge

FROM gcr.io/distroless/static:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7
WORKDIR /
COPY --from=builder /workspace/falco-bridge .
USER 65532:65532

# `falco-bridge healthcheck` is a plain HTTP GET against this same process's
# own /healthz -- distroless/static has no shell and no curl/wget, so the
# running binary itself is the only thing HEALTHCHECK could possibly exec
# here. start-period gives it time to connect to NATS before the first
# check can fail it.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/falco-bridge", "healthcheck"]

ENTRYPOINT ["/falco-bridge"]
