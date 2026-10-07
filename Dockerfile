# syntax=docker/dockerfile:1
# ── builder stage ─────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Download dependencies first — cached as a separate layer.
# This layer is only invalidated when go.mod or go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source and build.
# The go build cache is mounted so incremental rebuilds skip unchanged packages.
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o /app/cassperf ./cmd/cassperf

# ── runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/cassperf /app/cassperf

# Built-in YAML workloads embedded in the image
COPY workloads/ /app/workloads/

# Named volume mount points (custom workloads live here at runtime)
RUN mkdir -p /app/scb /app/logs /app/workloads/custom

EXPOSE 3000

ENTRYPOINT ["/app/cassperf"]
