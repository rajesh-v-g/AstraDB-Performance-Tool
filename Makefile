##  cassandra-go-perf-tool — run `make` to see all targets

PROJECT := cassandra-go-perf-tool
CMD     := ./cmd/cassperf
BIN     := bin/cassperf
COMPOSE := podman-compose

export DOCKER_BUILDKIT := 1
export BUILDAH_FORMAT  := docker

.DEFAULT_GOAL := help
.PHONY: help build test fmt lint \
        docker-build docker-run docker-stop docker-status \
        health logs clean

# ─────────────────────────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  cassandra-go-perf-tool"
	@echo ""
	@echo "  \033[1mGo\033[0m"
	@echo "    \033[36mbuild\033[0m         compile static binary → bin/cassperf"
	@echo "    \033[36mtest\033[0m          run tests with race detector + coverage"
	@echo "    \033[36mfmt\033[0m           format all Go source files"
	@echo "    \033[36mlint\033[0m          run golangci-lint"
	@echo ""
	@echo "  \033[1mDocker / Podman\033[0m"
	@echo "    \033[36mdocker-build\033[0m   build the cassperf image"
	@echo "    \033[36mdocker-run\033[0m     start all 4 services, wait until healthy"
	@echo "    \033[36mdocker-stop\033[0m    stop + remove all containers"
	@echo "    \033[36mdocker-status\033[0m  show running/stopped state of each service"
	@echo ""
	@echo "  \033[1mOps\033[0m"
	@echo "    \033[36mhealth\033[0m        check cassperf is responding"
	@echo "    \033[36mlogs\033[0m          tail all service logs (Ctrl-C to exit)"
	@echo "    \033[36mclean\033[0m         remove containers, volumes, image, build artifacts"
	@echo ""

# ── Go ────────────────────────────────────────────────────────────────────────

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -o $(BIN) $(CMD)

test:
	go test ./... -count=1 -race -timeout 60s -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1

fmt:
	gofmt -w -s .

lint:
	golangci-lint run ./...

# ── Docker / Podman ──────────────────────────────────────────────────────────

docker-build:
	$(COMPOSE) build cassperf

docker-run:
	$(COMPOSE) up --build -d
	@printf "Waiting for cassperf"; \
	for i in $$(seq 1 40); do \
	  curl -sf http://localhost:3000/health >/dev/null 2>&1 \
	    && printf "\n\033[32m▲  up\033[0m  http://localhost:3000  |  Grafana http://localhost:3001\n" \
	    && exit 0; \
	  printf "."; sleep 5; \
	done; \
	printf "\n\033[31m✗  timed out\033[0m\n"; exit 1

docker-stop:
	@podman ps -a --format '{{.Names}}' \
	  | grep '^$(PROJECT)_' \
	  | xargs -r podman rm -f 2>/dev/null || true
	@echo "stopped"

docker-status:
	@echo ""
	@podman ps -a --format '{{.Names}}\t{{.Status}}\t{{.Ports}}' \
	  | grep '^$(PROJECT)_' \
	  | awk 'BEGIN{OFS=""} { \
	      split($$0, f, "\t"); \
	      name=f[1]; status=f[2]; ports=f[3]; \
	      sub(/^$(PROJECT)_/, "", name); \
	      sub(/_[0-9]+$$/, "", name); \
	      if (status ~ /^Up/) color="\033[32m"; \
	      else color="\033[31m"; \
	      printf "  %-14s %s%s\033[0m", name, color, status; \
	      if (ports != "") printf "  \033[2m%s\033[0m", ports; \
	      printf "\n"; \
	    }' \
	  || echo "  no containers found"
	@echo ""

# ── Ops ───────────────────────────────────────────────────────────────────────

health:
	@curl -sf http://localhost:3000/health \
	  && echo "  \033[32mhealthy\033[0m" \
	  || echo "  \033[31munreachable\033[0m"

logs:
	$(COMPOSE) logs -f

# ── Clean ─────────────────────────────────────────────────────────────────────

clean:
	@echo "removing containers..."
	@podman ps -a --format '{{.Names}}' \
	  | grep '^$(PROJECT)_' \
	  | xargs -r podman rm -f 2>/dev/null || true
	@echo "removing volumes..."
	@podman volume ls --format '{{.Name}}' \
	  | grep -E '^(cgpt_|cassandra_data|prometheus_data|grafana_data)' \
	  | xargs -r podman volume rm 2>/dev/null || true
	@echo "removing image..."
	@podman image rm -f localhost/$(PROJECT)_cassperf 2>/dev/null || true
	@echo "removing build artifacts..."
	@rm -rf bin/ coverage.out
	@echo "done"
