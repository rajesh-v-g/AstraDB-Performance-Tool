// Package config provides centralised configuration loading from environment variables.
// All other packages receive *Config; none call os.Getenv directly.
package config

import (
	"fmt"
	"os"
	"time"
)

// Version is the application version string.
const Version = "1.0.0"

// Config holds all runtime configuration derived from environment variables.
// Once returned from Load it is immutable.
type Config struct {
	// Port is the TCP port for the main HTTP server (UI + REST API + SSE).
	Port string
	// MetricsPort is the TCP port for the Prometheus /metrics endpoint.
	MetricsPort string
	// WorkloadsDir is the directory containing built-in YAML workload files.
	WorkloadsDir string
	// CustomWorkloadsDir is the directory for user-authored YAML workloads (runtime volume).
	CustomWorkloadsDir string
	// SCBDir is the directory where uploaded Astra Secure Connect Bundles are stored.
	SCBDir string
	// LogsDir is the directory for run history JSON manifests, logs, and reports.
	LogsDir string
	// Version is the application version.
	Version string
	// StartTime records when the process started.
	StartTime time.Time
}

// Load reads environment variables, applies defaults, creates required directories,
// and returns an immutable *Config.
func Load() (*Config, error) {
	cfg := &Config{
		Port:               envOrDefault("PORT", "3000"),
		MetricsPort:        envOrDefault("METRICS_PORT", "9090"),
		WorkloadsDir:       envOrDefault("WORKLOADS_DIR", "/app/workloads"),
		CustomWorkloadsDir: envOrDefault("CUSTOM_WORKLOADS_DIR", "/app/workloads/custom"),
		SCBDir:             envOrDefault("SCB_DIR", "/app/scb"),
		LogsDir:            envOrDefault("LOGS_DIR", "/app/logs"),
		Version:            Version,
		StartTime:          time.Now(),
	}

	for _, dir := range []string{cfg.CustomWorkloadsDir, cfg.SCBDir, cfg.LogsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("config: create directory %q: %w", dir, err)
		}
	}

	return cfg, nil
}

// envOrDefault returns the value of the environment variable named by key, or
// defaultVal if the variable is unset or empty.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
