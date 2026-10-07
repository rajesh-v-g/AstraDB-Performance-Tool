# syntax=docker/dockerfile:1
# ── builder stage ─────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Download dependencies first — invalidated only when go.mod / go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source and build a fully-static binary (no libc dependency).
COPY . .

ARG VERSION=dev
ARG GIT_SHA=unknown

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w -X github.com/rajesh-v-g/cassandra-go-perf-tool/internal/config.Version=${VERSION}" \
      -o /app/cassperf \
      ./cmd/cassperf

# Create volume-mount directories with correct ownership for the nonroot user
# (uid/gid 65532 used by distroless/nonroot).
RUN mkdir -p /app/scb /app/logs /app/workloads/custom \
 && chown -R 65532:65532 /app

# ── runtime stage ─────────────────────────────────────────────────────────────
# gcr.io/distroless/static-debian12 is ~2 MB: ca-certificates + tzdata, no shell.
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
ARG GIT_SHA=unknown
ARG BUILD_DATE=unknown

# OCI image labels (values injected by the GitHub Actions workflow).
LABEL org.opencontainers.image.title="astradb-performance-go" \
      org.opencontainers.image.description="CQL performance tool for Apache Cassandra and DataStax Astra DB" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/rajesh-v-g/cassandra-go-perf-tool" \
      org.opencontainers.image.licenses="Apache-2.0"

# Copy the binary and pre-created directories (ownership already set in builder).
COPY --from=builder /app /app

WORKDIR /app

# distroless/nonroot already sets USER 65532 — explicit here for clarity.
USER nonroot:nonroot

EXPOSE 3000

ENTRYPOINT ["/app/cassperf"]
