# syntax=docker/dockerfile:1.6

# ---------- builder ----------
FROM golang:1.26-alpine AS builder

WORKDIR /src

ENV GOCACHE=/cache/go-build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/cache/go-build \
    go mod download -x

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/cache/go-build \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build \
    -mod=readonly \
    -ldflags="-s -w -X main.version=${VERSION:-dev}" \
    -trimpath \
    -o /out/tekton-events-relay \
    ./cmd/receiver

# ---------- ci (inject pre-built binary — no QEMU) ----------
# In CI: docker build --target ci ...  (TARGETARCH set automatically by buildx platform)
FROM gcr.io/distroless/static:nonroot AS ci
ARG BINARY
LABEL org.opencontainers.image.title="tekton-events-relay" \
      org.opencontainers.image.description="CloudEvents receiver that reports pipeline execution status to multiple SCM providers" \
      org.opencontainers.image.url="https://github.com/fabioluciano/tekton-events-relay" \
      org.opencontainers.image.source="https://github.com/fabioluciano/tekton-events-relay" \
      org.opencontainers.image.vendor="fabioluciano" \
      org.opencontainers.image.licenses="MIT"
COPY ${BINARY} /tekton-events-relay
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/tekton-events-relay"]
CMD ["--config", "/etc/tekton-events-relay/config.yaml"]

# ---------- runtime (default, local dev via builder above) ----------
FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.title="tekton-events-relay" \
      org.opencontainers.image.description="CloudEvents receiver that reports pipeline execution status to multiple SCM providers" \
      org.opencontainers.image.url="https://github.com/fabioluciano/tekton-events-relay" \
      org.opencontainers.image.source="https://github.com/fabioluciano/tekton-events-relay" \
      org.opencontainers.image.vendor="fabioluciano" \
      org.opencontainers.image.licenses="MIT"

COPY --from=builder /out/tekton-events-relay /tekton-events-relay

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/tekton-events-relay"]
CMD ["--config", "/etc/tekton-events-relay/config.yaml"]
