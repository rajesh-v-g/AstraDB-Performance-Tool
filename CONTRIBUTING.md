# Contributing

Thank you for your interest in contributing to cassandra-go-perf-tool!

## Prerequisites

- Go 1.23+
- Docker + Docker Compose (for integration testing)
- `golangci-lint` (for linting)

## Development Setup

```bash
git clone https://github.com/rajesh-v-g/cassandra-go-perf-tool
cd cassandra-go-perf-tool
cp .env.example .env
make dev
```

## Code Structure

- `cmd/cassperf/` — entry point; signal handling; server startup
- `internal/config/` — configuration from environment variables
- `internal/workload/` — workload types, YAML loader, validator, registry
- `internal/binding/` — pre-compiled CQL template engine
- `internal/driver/` — gocql session factory + Executor interface
- `internal/job/` — job manager, worker pool, phase runner
- `internal/metrics/` — HDR histogram collector + Prometheus handler
- `internal/store/` — run history persistence
- `internal/sse/` — SSE fan-out broadcaster
- `internal/api/` — HTTP handlers and chi router
- `web/` — single-page UI (vanilla HTML/CSS/JS)
- `workloads/` — built-in YAML workload definitions
- `observability/` — Prometheus and Grafana configuration

## Testing

```bash
make test    # unit tests with race detector
```

Integration tests (require a live Cassandra or Astra instance) are gated with the build tag `integration`:

```bash
CASSANDRA_TEST_HOST=localhost go test -tags integration ./internal/driver/...
```

## Submitting Changes

1. Fork the repository.
2. Create a branch: `git checkout -b feature/my-feature`
3. Make your changes, add tests, and verify: `make test && make lint`
4. Open a pull request with a clear description.

## Coding Conventions

- Follow standard Go formatting (`gofmt`).
- Keep packages focused; avoid circular dependencies.
- No `panic` outside of `init()` — return errors.
- All public API must have a doc comment.
- Credentials (`token`, `password`) must never be logged or stored — use `[REDACTED]`.
