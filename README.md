# cassandra-go-perf-tool

A self-contained, single-binary Go application for running CQL performance workloads against Apache Cassandra or DataStax Astra DB — using the `gocql` driver natively, with no JVM, no external process, and no third-party benchmark engine.

- **Live metrics** streamed to the browser via Server-Sent Events (SSE)
- **Phase timeline** in the UI — see exactly which phase is running and which are done
- **Prometheus + Grafana** pre-provisioned, zero manual configuration
- **Custom YAML workloads** uploadable through the UI at runtime
- Single static binary; the entire UI is embedded at compile time

---

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quickstart — full stack with Podman Compose](#quickstart--full-stack-with-podman-compose)
3. [Waiting for the stack to be healthy](#waiting-for-the-stack-to-be-healthy)
4. [Using the UI](#using-the-ui)
   - [Connecting to local Cassandra (inside the stack)](#connecting-to-local-cassandra-inside-the-stack)
   - [Connecting to DataStax Astra DB](#connecting-to-datastax-astra-db)
   - [Running a benchmark](#running-a-benchmark)
   - [Status bar and phase timeline](#status-bar-and-phase-timeline)
5. [Testing via the REST API (curl)](#testing-via-the-rest-api-curl)
   - [Health check](#1-health-check)
   - [List workloads](#2-list-workloads)
   - [Start a run](#3-start-a-run)
   - [Stream live events (SSE)](#4-stream-live-events-sse)
   - [Check run history](#5-check-run-history)
   - [Download report](#6-download-report)
   - [Stop a run](#7-stop-a-run)
6. [Verifying Prometheus and Grafana](#verifying-prometheus-and-grafana)
7. [Built-in Workloads](#built-in-workloads)
8. [Custom YAML Workloads](#custom-yaml-workloads)
9. [Local Development (no containers)](#local-development-no-containers)
10. [Running Unit Tests](#running-unit-tests)
11. [Integration Test (live Cassandra)](#integration-test-live-cassandra)
12. [Environment Variables](#environment-variables)
13. [Named Volumes](#named-volumes)
14. [Architecture](#architecture)
15. [Makefile Targets](#makefile-targets)

---

## Prerequisites

| Tool | Minimum version | Check |
|---|---|---|
| Go | 1.23 | `go version` |
| Podman | 4.x | `podman --version` |
| podman-compose | 1.x | `podman-compose --version` |
| curl + jq | any | `curl --version && jq --version` |

> **Docker users:** every `podman-compose` command below is identical with `docker compose` (v2 plugin syntax, not the legacy `docker-compose`).

---

## Quickstart — full stack with Podman Compose

```bash
git clone https://github.com/rajesh-v-g/cassandra-go-perf-tool
cd cassandra-go-perf-tool

# Build the cassperf image and start all four services
podman-compose up --build -d
```

This starts:

| Service | Local port | Purpose |
|---|---|---|
| `cassperf` | 3000 | UI + REST API + SSE stream |
| `cassandra` | 9042 | Cassandra 5.0 (test target) |
| `prometheus` | 9091 | Scrapes cassperf every 5 s |
| `grafana` | 3001 | Pre-provisioned dashboard |

---

## Waiting for the stack to be healthy

Cassandra takes **60–120 seconds** to be ready on first boot. Wait for both services before running any benchmark.

### cassperf

```bash
# Polls until HTTP 200 or 30 tries
for i in $(seq 1 30); do
  curl -sf http://localhost:3000/health && echo " ✓ cassperf ready" && break
  echo "[$i/30] cassperf not ready yet…"; sleep 5
done
```

### Cassandra

```bash
# Polls the container health status (up to ~3 min)
for i in $(seq 1 24); do
  STATUS=$(podman inspect --format='{{.State.Health.Status}}' \
    cassandra-go-perf-tool_cassandra_1 2>/dev/null || echo "missing")
  echo "[$i/24] cassandra: $STATUS"
  [ "$STATUS" = "healthy" ] && break
  sleep 8
done
```

> The container name may vary by podman-compose version. Run `podman ps` to find the exact name.

---

## Using the UI

Open **[http://localhost:3000](http://localhost:3000)**.

### Connecting to local Cassandra (inside the stack)

| Field | Value |
|---|---|
| Driver | **Cassandra** |
| Hosts | `cassandra` *(Docker/Podman network name)* |
| Port | `9042` |
| Username | *(leave blank — Cassandra 5 has no auth by default)* |
| Password | *(leave blank)* |
| Keyspace | `test` |

> If you run the binary directly on the host (outside a container), use `localhost` as the host.

### Connecting to DataStax Astra DB

1. In the [Astra console](https://astra.datastax.com), generate an **Application Token** (`AstraCS:…`).
2. Download the **Secure Connect Bundle** (SCB) `.zip` for your database.
3. In the UI select the **Astra** radio button.
4. Paste the token into the **Application Token** field.
5. Click **Upload** next to the SCB selector and choose your `.zip` file.
6. Select the uploaded SCB from the dropdown.
7. Fill in your **Keyspace** name.

### Running a benchmark

1. **Select a workload** from the left panel (built-in or custom).
2. **Select a phase** — `All` runs every phase in canonical order (`schema → rampup → main → truncate`).  
   Individual phase buttons let you run a single phase in isolation.
3. Optionally expand **▸ Advanced** to set threads, target rate, duration, or cycles.
4. Click **▶ Run**.

### Status bar and phase timeline

Once Run is clicked the status bar appears between the run controls and the parameters panel:

```
⟳ Phase: rampup   [ schema ✓ ] ─── [ rampup ⟳ ] ─── [ main ] ─── [ truncate ]   00:12
```

- **Spinner + phase text** — shows the currently executing phase name.
- **Pill chain** — each phase pill transitions from dim (pending) → blue pulsing (active) → green (done).
- **Elapsed timer** (top-right of the bar) — counts `MM:SS` while the run is active.
- On completion: `✅ Run completed` with the final elapsed time, then the bar fades after 5 s.
- On error: `❌ <reason>` stays visible for 8 s.

---

## Testing via the REST API (curl)

All examples assume the stack is up and healthy. Replace `cassandra` with `localhost` if you are hitting Cassandra from outside the container network.

### 1. Health check

```bash
curl -s http://localhost:3000/health | jq .
```
```json
{ "status": "ok", "version": "dev", "uptime_sec": 42 }
```

### 2. List workloads

```bash
curl -s http://localhost:3000/api/v1/workloads | jq '.[] | {id, label, source}'
```
```json
{ "id": "go-keyvalue",       "label": "Key-Value (Go)",          "source": "builtin-go" }
{ "id": "go-keyvalue-batch", "label": "Key-Value Batch (Go)",    "source": "builtin-go" }
{ "id": "go-iot",            "label": "IoT Time-Series (Go)",    "source": "builtin-go" }
{ "id": "cql-keyvalue",      "label": "cql-keyvalue",            "source": "yaml-builtin" }
{ "id": "cql-iot",           "label": "cql-iot",                 "source": "yaml-builtin" }
```

### 3. Start a run

Run the **Key-Value** workload for 30 seconds against the local Cassandra container:

```bash
curl -s -X POST http://localhost:3000/api/v1/run \
  -H 'Content-Type: application/json' \
  -d '{
    "workload_id":       "go-keyvalue",
    "phase":             "all",
    "driver":            "cassandra",
    "hosts":             ["cassandra"],
    "cassandra_port":    9042,
    "keyspace":          "test",
    "consistency_level": "LOCAL_QUORUM",
    "threads":           10,
    "target_rate":       2000,
    "duration_seconds":  30
  }' | jq .
```
```json
{ "run_id": "go-keyvalue_all_20250601T143012Z", "status": "started" }
```

Save the `run_id` — you will need it for history and report lookups.

**Run only the `rampup` phase (skip schema if table already exists):**

```bash
curl -s -X POST http://localhost:3000/api/v1/run \
  -H 'Content-Type: application/json' \
  -d '{
    "workload_id":      "go-keyvalue",
    "phase":            "rampup",
    "driver":           "cassandra",
    "hosts":            ["cassandra"],
    "cassandra_port":   9042,
    "keyspace":         "test",
    "threads":          20,
    "target_rate":      5000,
    "cycles":           100000
  }' | jq .
```

**Astra DB example:**

```bash
curl -s -X POST http://localhost:3000/api/v1/run \
  -H 'Content-Type: application/json' \
  -d '{
    "workload_id":       "go-keyvalue",
    "phase":             "all",
    "driver":            "astra",
    "token":             "AstraCS:xxxx",
    "scb_id":            "<id-from-upload>",
    "keyspace":          "my_keyspace",
    "consistency_level": "LOCAL_QUORUM",
    "threads":           30,
    "target_rate":       5000,
    "duration_seconds":  60
  }' | jq .
```

### 4. Stream live events (SSE)

Open a second terminal while a run is active:

```bash
curl -sN http://localhost:3000/api/v1/run/stream
```

You will see a stream of newline-delimited JSON frames:

```
data: {"type":"log","message":"[info] phases to run: [schema rampup main truncate]"}
data: {"type":"phase_start","phase":"schema"}
data: {"type":"log","message":"[info] schema op \"create_table\" ok"}
data: {"type":"phase_end","phase":"schema"}
data: {"type":"phase_start","phase":"rampup"}
data: {"type":"metrics","type":"metrics","ops_total":1834,"ops_per_sec":1821.4,"p50ms":2.1,"p95ms":5.3,"p99ms":9.8,"p999ms":18.0,"error_total":0,"phase":"rampup"}
data: {"type":"phase_end","phase":"rampup"}
data: {"type":"phase_start","phase":"main"}
...
data: {"type":"done"}
```

| Event type | Payload fields | Meaning |
|---|---|---|
| `log` | `message` | Informational log line |
| `phase_start` | `phase` | A phase just began executing |
| `phase_end` | `phase` | A phase just finished |
| `metrics` | `ops_total`, `ops_per_sec`, `p50ms`, `p95ms`, `p99ms`, `p999ms`, `error_total`, `phase` | 1-second metrics snapshot |
| `done` | — | Run finished; stream will close |

### 5. Check run history

```bash
# Last 10 runs
curl -s http://localhost:3000/api/v1/history | \
  jq '.[] | {run_id, workload, phase, status, started_at}'
```

```bash
# Check status of a specific run
RUN_ID="go-keyvalue_all_20250601T143012Z"
curl -s "http://localhost:3000/api/v1/history/${RUN_ID}" | jq .
```

### 6. Download report

```bash
RUN_ID="go-keyvalue_all_20250601T143012Z"

# Final metrics snapshot (JSON)
curl -s "http://localhost:3000/api/v1/history/${RUN_ID}/report" | \
  jq '{ops_total, ops_per_sec, p50ms, p99ms, error_total}'
```
```json
{ "ops_total": 59847, "ops_per_sec": 1994.9, "p50ms": 2.3, "p99ms": 11.7, "error_total": 0 }
```

```bash
# Full run log (plain text)
curl -s "http://localhost:3000/api/v1/history/${RUN_ID}/log"
```

### 7. Stop a run

```bash
curl -s -X POST http://localhost:3000/api/v1/run/stop | jq .
```
```json
{ "status": "stopped" }
```

---

## Verifying Prometheus and Grafana

### Prometheus targets

```bash
# All scrape targets and their health
curl -s 'http://localhost:9091/api/v1/targets' | \
  jq '.data.activeTargets[] | {job: .labels.job, health, lastError}'
```
```json
{ "job": "cassperf", "health": "up", "lastError": "" }
```

### Query a metric

```bash
# Total ops recorded since last run
curl -s 'http://localhost:9091/api/v1/query?query=cassperf_ops_total' | \
  jq '.data.result[0].value[1]'

# P99 latency (milliseconds)
curl -s 'http://localhost:9091/api/v1/query?query=cassperf_p99_ms' | \
  jq '.data.result[0].value[1]'

# Current ops/sec rate
curl -s 'http://localhost:9091/api/v1/query?query=cassperf_ops_per_sec' | \
  jq '.data.result[0].value[1]'
```

### Grafana

```bash
# API health check (no browser required)
curl -s -u admin:admin http://localhost:3001/api/health | jq .
```
```json
{ "commit": "...", "database": "ok", "version": "11.1.0" }
```

**Browser:**

1. Open [http://localhost:3001](http://localhost:3001)
2. Login: **admin / admin**
3. Go to **Dashboards** → the `cassandra-go-perf-tool` dashboard is pre-provisioned
4. Panels: throughput (ops/sec), error rate, run-active indicator, P50/P95/P99/P999 per op type

---

## Built-in Workloads

| ID | Label | Phases | Description |
|---|---|---|---|
| `go-keyvalue` | Key-Value (Go) | schema, rampup, main, truncate | Point reads and writes on a single `text PRIMARY KEY` table |
| `go-keyvalue-batch` | Key-Value Batch (Go) | schema, rampup, main, truncate | Same table, writes via 5-statement `UNLOGGED BATCH` |
| `go-iot` | IoT Time-Series (Go) | schema, rampup, main, truncate | Sensor data ingestion and range queries by device+time |
| `cql-keyvalue` | cql-keyvalue | schema, rampup, main, truncate | YAML-defined key-value workload |
| `cql-iot` | cql-iot | schema, rampup, main, truncate | YAML-defined IoT workload |

**Common parameters** (all Go built-ins accept these via the UI or `extra_params` in the API):

| Parameter | Default | Description |
|---|---|---|
| `keyspace` | `test` | Target keyspace |
| `table` | workload-specific | Target table name |
| `read_ratio` | `5` | Relative weight of read ops in `main` phase |
| `write_ratio` | `5` | Relative weight of write ops in `main` phase |

---

## Custom YAML Workloads

### Via the UI

1. Click **+ New Workload** in the Workloads panel.
2. Give it a unique name (slug, no spaces).
3. Paste your YAML — the editor validates syntax live.
4. Click **Save** — the workload appears immediately in the list.

### YAML format

```yaml
blocks:
  schema:
    ops:
      create_ks: "CREATE KEYSPACE IF NOT EXISTS {keyspace} WITH replication = {'class':'SimpleStrategy','replication_factor':1}"
      create_tbl: "CREATE TABLE IF NOT EXISTS {keyspace}.orders (id uuid PRIMARY KEY, amount double)"

  rampup:
    params:
      prepared: true
    ops:
      insert: "INSERT INTO {keyspace}.orders (id, amount) VALUES ({uuid_key}, {float_value})"

  main-read:
    ratio: 8
    ops:
      select: "SELECT * FROM {keyspace}.orders WHERE id = {rw_key}"

  main-write:
    ratio: 2
    ops:
      insert: "INSERT INTO {keyspace}.orders (id, amount) VALUES ({rw_key}, {float_value})"

  truncate:
    ops:
      truncate: "TRUNCATE TABLE {keyspace}.orders"
```

**Available binding tokens:**

| Token | Generates |
|---|---|
| `{seq_key}` | Sequential integer string (monotonically increasing per worker) |
| `{seq_value}` | Random 256-byte string (sequential index) |
| `{rw_key}` | Random integer string within already-written range |
| `{rw_value}` | Random 256-byte string |
| `{uuid_key}` | Random UUID v4 |
| `{float_value}` | Random float64 as string |

### Via the API

```bash
curl -s -X POST http://localhost:3000/api/v1/custom-workloads \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "my-orders",
    "content": "blocks:\n  rampup:\n    ops:\n      insert: \"INSERT INTO test.orders (id, v) VALUES ({uuid_key}, {seq_value})\"\n"
  }' | jq .
```

---

## Local Development (no containers)

```bash
cp .env.example .env
# Edit .env to point at your local Cassandra or Astra instance

make dev    # runs with -tags dev; web/ is served from disk (no embed — live UI edits)
```

The dev build reads `web/index.html` from disk so changes to the UI are visible on page reload with no recompile.

To rebuild the embedded binary:

```bash
make build          # produces bin/cassperf (static, CGO_ENABLED=0)
./bin/cassperf
```

---

## Running Unit Tests

```bash
make test
```

This runs `go test ./... -count=1 -race -timeout 60s` across all packages and prints a per-function coverage summary.

Expected output — all packages should show `ok`:

```
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/api
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/binding
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/config
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/driver
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/job
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload
ok  github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload/builtin
```

---

## Integration Test (live Cassandra)

The driver package has integration tests gated behind the `integration` build tag. They require a running Cassandra node.

```bash
# Against the local stack (from the host, while stack is up)
CASSANDRA_TEST_HOST=localhost go test -tags integration ./internal/driver/... -v
```

Or against any reachable node:

```bash
CASSANDRA_TEST_HOST=192.168.1.100 go test -tags integration ./internal/driver/... -v
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `3000` | HTTP port — UI, REST API, SSE |
| `METRICS_PORT` | `9090` | Internal Prometheus `/metrics` port |
| `WORKLOADS_DIR` | `/app/workloads` | Directory for built-in YAML workloads |
| `CUSTOM_WORKLOADS_DIR` | `/app/workloads/custom` | Directory for user-uploaded custom workloads |
| `SCB_DIR` | `/app/scb` | Directory for uploaded Astra Secure Connect Bundles |
| `LOGS_DIR` | `/app/logs` | Directory for run history (manifests, logs, reports) |

Copy `.env.example` to `.env` and edit before running locally. The container reads these from `docker-compose.yml` environment block.

---

## Named Volumes

| Volume | Mount point | Survives `down`? | Contains |
|---|---|---|---|
| `cgpt_scb` | `/app/scb` | ✓ | Uploaded Astra Secure Connect Bundles |
| `cgpt_logs` | `/app/logs` | ✓ | Run history — JSON manifests, logs, report.json |
| `cgpt_workloads` | `/app/workloads/custom` | ✓ | User-authored custom YAML workloads |
| `prometheus_data` | `/prometheus` | ✓ | Prometheus time-series data |
| `grafana_data` | `/var/lib/grafana` | ✓ | Grafana state (dashboards, alert rules) |
| `cassandra_data` | `/var/lib/cassandra` | ✓ | Cassandra data files |

> **Warning:** `podman-compose down -v` removes **all** named volumes, including run history and uploaded SCBs. Omit `-v` to keep data between restarts.

---

## Architecture

```
Browser
  │  HTTP / SSE
  ▼
cassperf :3000
  ├── GET  /                     → embedded SPA (web/index.html)
  ├── GET  /api/v1/workloads     → list all workloads
  ├── POST /api/v1/run           → start benchmark (returns run_id)
  ├── POST /api/v1/run/stop      → cancel active run
  ├── GET  /api/v1/run/stream    → SSE: log / phase_start / phase_end / metrics / done
  ├── GET  /api/v1/history       → list past runs
  ├── GET  /api/v1/history/:id/report → final MetricSnapshot JSON
  ├── GET  /api/v1/history/:id/log    → plain-text run log
  ├── POST /api/v1/upload/scb    → upload SCB .zip
  └── POST/PUT/DELETE /api/v1/custom-workloads → CRUD custom workloads

cassperf :9090  ← Prometheus scrapes every 5 s
  └── GET /metrics               → Prometheus text format

Prometheus :9090 (host: 9091)
Grafana    :3000 (host: 3001)   → pre-provisioned dashboard
```

Two HTTP servers start in the same process. Port 9090 (internal) exposes only `/metrics` and is never accessible from the browser.

---

## Makefile Targets

```
make build        Build the binary  →  bin/cassperf  (static, CGO_ENABLED=0)
make test         Run all tests     →  race detector + coverage report
make run          Run with .env loaded (production embed)
make dev          Run in dev mode   →  web/ served from disk, no rebuild needed
make docker-build Build Docker image tagged cassperf:latest
make lint         Run golangci-lint
make clean        Remove bin/ and coverage.out
make help         Print all targets with descriptions
```
