package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/api"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/config"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/job"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
	_ "github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload/builtin"
)

func main() {
	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// Workload registry.
	reg := workload.DefaultRegistry()
	reg.Init(cfg.WorkloadsDir, cfg.CustomWorkloadsDir)
	// Set embedded FS for built-in YAMLs (no-op in dev mode since workloadsFS
	// returns os.DirFS; in production it serves from the embedded binary).
	reg.SetEmbeddedFS(workloadsFS())
	// builtin Go workloads are registered via init() in the builtin package.

	// Core singletons.
	rs := store.NewRunStore(cfg.LogsDir)
	bc := sse.NewBroadcaster()
	col := metrics.NewCollector(1, []string{"op"}) // placeholder; per-run collector is in job.Manager
	mgr := job.NewManager(rs, bc, reg)

	// Main HTTP server (port 3000).
	appServer := api.NewServer(cfg, reg, rs, bc, mgr, col, webFS())
	mainHTTP := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      appServer,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE streams are long-lived
		IdleTimeout:  120 * time.Second,
	}

	// Metrics server (port 9090) — Prometheus scrape target.
	// Runs on a separate port so Prometheus scrapes don't queue behind SSE connections.
	metricsHTTP := &http.Server{
		Addr:         ":" + cfg.MetricsPort,
		Handler:      http.HandlerFunc(metrics.PrometheusHandler(col)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start metrics server in background.
	go func() {
		slog.Info("metrics server listening", "addr", metricsHTTP.Addr)
		if err := metricsHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "err", err)
		}
	}()

	// Start main server in background.
	go func() {
		slog.Info("cassandra-go-perf-tool starting", "addr", mainHTTP.Addr, "version", cfg.Version)
		if err := mainHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("main server error", "err", err)
		}
	}()

	// Wait for shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	slog.Info("shutdown signal received; stopping")

	// Graceful shutdown: stop active job, then HTTP servers.
	mgr.Stop()

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = mainHTTP.Shutdown(shutCtx)
	_ = metricsHTTP.Shutdown(shutCtx)

	slog.Info("shutdown complete")
}
