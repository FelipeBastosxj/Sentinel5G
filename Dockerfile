# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS builder
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/operator/ cmd/operator/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=linux go build -a -o manager ./cmd/operator

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
