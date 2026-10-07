# Astra Performance Tool

A benchmarking tool for DataStax Astra DB and Apache Cassandra with a live web UI, built-in workloads, and Prometheus/Grafana metrics.

## Quickstart

```bash
make docker-build   # build image
make docker-run     # start cassperf + cassandra + prometheus + grafana
```

UI → http://localhost:3000  
Grafana → http://localhost:3001

## Usage

1. Select a workload from the left panel
2. Fill in connection details (Astra token + SCB, or Cassandra host)
3. Choose a phase (`All`, `Schema`, `Rampup`, `Main`, `Truncate`)
4. Click **Run**

## Workloads

| Workload | Description |
|---|---|
| `cql-keyvalue` | Point reads/writes against a key-value table |
| `cql-keyvalue-batch` | Same table, batched writes |
| `cql-iot` | Time-series IoT sensor inserts and reads |
| `cql-burst` | High-rate burst spike against an existing keyvalue table (run keyvalue rampup first) |

Custom YAML workloads can be created via the **+ New Workload** button in the UI.

## Make Targets

```
make docker-build     build local image
make docker-run       start all services
make docker-stop      stop and remove containers
make docker-status    show container state
make logs             tail all service logs
make health           check cassperf is responding
make test             run unit tests
make dev              run locally without containers (web/ served from disk)
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `CASSPERF_ADDR` | `:3000` | Listen address |
| `CASSPERF_DATA_DIR` | `./data` | Run history and uploaded SCBs |
| `CASSPERF_WORKLOAD_DIR` | `./workloads` | YAML workload files |
