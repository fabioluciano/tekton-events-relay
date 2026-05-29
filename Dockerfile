# syntax=docker/dockerfile:1.6

# ---------- builder ----------
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /out/tekton-events-relay ./cmd/receiver

# ---------- runtime ----------
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/tekton-events-relay /tekton-events-relay

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/tekton-events-relay"]
CMD ["--config", "/etc/tekton-events-relay/config.json"]
