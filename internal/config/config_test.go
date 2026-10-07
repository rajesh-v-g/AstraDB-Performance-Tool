package config_test

import (
	"path/filepath"
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any overrides so we see the defaults.
	for _, k := range []string{"PORT", "METRICS_PORT", "WORKLOADS_DIR", "CUSTOM_WORKLOADS_DIR", "SCB_DIR", "LOGS_DIR"} {
		t.Setenv(k, "")
	}

	tmp := t.TempDir()
	t.Setenv("CUSTOM_WORKLOADS_DIR", filepath.Join(tmp, "custom"))
	t.Setenv("SCB_DIR", filepath.Join(tmp, "scb"))
	t.Setenv("LOGS_DIR", filepath.Join(tmp, "logs"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Port != "3000" {
		t.Errorf("Port = %q, want %q", cfg.Port, "3000")
	}
	if cfg.MetricsPort != "9090" {
		t.Errorf("MetricsPort = %q, want %q", cfg.MetricsPort, "9090")
	}
	if cfg.Version != config.Version {
		t.Errorf("Version = %q, want %q", cfg.Version, config.Version)
	}
	if cfg.StartTime.IsZero() {
		t.Error("StartTime is zero")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PORT", "8080")
	t.Setenv("METRICS_PORT", "9100")
	t.Setenv("CUSTOM_WORKLOADS_DIR", filepath.Join(tmp, "cw"))
	t.Setenv("SCB_DIR", filepath.Join(tmp, "scb"))
	t.Setenv("LOGS_DIR", filepath.Join(tmp, "logs"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.MetricsPort != "9100" {
		t.Errorf("MetricsPort = %q, want %q", cfg.MetricsPort, "9100")
	}
}

func TestLoad_CreatesDirs(t *testing.T) {
	tmp := t.TempDir()
	cwDir := filepath.Join(tmp, "custom")
	scbDir := filepath.Join(tmp, "scb")
	logsDir := filepath.Join(tmp, "logs")

	t.Setenv("CUSTOM_WORKLOADS_DIR", cwDir)
	t.Setenv("SCB_DIR", scbDir)
	t.Setenv("LOGS_DIR", logsDir)
	// Suppress PORT/METRICS default dirs that might also be created.
	t.Setenv("PORT", "3000")
	t.Setenv("METRICS_PORT", "9090")

	if _, err := config.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	for _, dir := range []string{cwDir, scbDir, logsDir} {
		if !dirExists(t, dir) {
			t.Errorf("directory not created: %s", dir)
		}
	}
}

func dirExists(t *testing.T, path string) bool {
	t.Helper()
	info, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	_ = info
	return true
}
