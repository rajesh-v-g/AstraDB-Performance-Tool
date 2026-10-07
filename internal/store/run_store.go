// Package store provides persistent run history with buffered log writing.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
)

// RunMeta is the JSON manifest written to run.json for every run.
type RunMeta struct {
	RunID          string            `json:"run_id"`
	Workload       string            `json:"workload"`
	WorkloadSource string            `json:"workload_source,omitempty"`
	Phase          string            `json:"phase"`
	Params         map[string]string `json:"params,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	Status         string            `json:"status"` // "running" | "completed" | "failed" | "stopped"
	ExitCode       int               `json:"exit_code"`
	ErrorMsg       string            `json:"error_msg,omitempty"`
}

// activeLog tracks an open buffered log file for a live run.
type activeLog struct {
	file        *os.File
	writer      *strings.Builder // in-memory buffer
	mu          sync.Mutex
	flushTicker *time.Ticker
	stopFlush   chan struct{}
	runDir      string
}

// RunStore manages run manifests, buffered append logs, and report artifacts.
type RunStore struct {
	logsDir string
	mu      sync.Mutex
	active  map[string]*activeLog
}

// NewRunStore creates a RunStore that persists data under logsDir.
func NewRunStore(logsDir string) *RunStore {
	return &RunStore{
		logsDir: logsDir,
		active:  make(map[string]*activeLog),
	}
}

// NewRunID generates a run identifier in the format
// 20060102T150405_<workloadID>_<phase>.
func NewRunID(workloadID, phase string) string {
	ts := time.Now().UTC().Format("20060102T150405")
	// sanitise: keep only safe characters.
	safe := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '-' || r == '_' {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	return fmt.Sprintf("%s_%s_%s", ts, safe(workloadID), safe(phase))
}

// RedactParams returns a copy of params with sensitive keys removed.
func RedactParams(params map[string]string) map[string]string {
	redacted := make(map[string]string, len(params))
	for k, v := range params {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "token") || strings.Contains(lower, "password") ||
			strings.Contains(lower, "secret") {
			redacted[k] = "***"
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

// Create creates the run directory, writes run.json, opens the log file, and
// starts the background flush ticker.
func (rs *RunStore) Create(meta RunMeta) error {
	runDir := filepath.Join(rs.logsDir, meta.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", runDir, err)
	}

	// Write initial run.json with status "running".
	meta.Status = "running"
	if err := writeJSON(filepath.Join(runDir, "run.json"), meta); err != nil {
		return err
	}

	// Open log file.
	logPath := filepath.Join(runDir, "run.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("store: open log %q: %w", logPath, err)
	}

	al := &activeLog{
		file:        f,
		writer:      &strings.Builder{},
		flushTicker: time.NewTicker(500 * time.Millisecond),
		stopFlush:   make(chan struct{}),
		runDir:      runDir,
	}

	rs.mu.Lock()
	rs.active[meta.RunID] = al
	rs.mu.Unlock()

	// Background flush goroutine.
	go func() {
		for {
			select {
			case <-al.flushTicker.C:
				_ = rs.flushLog(meta.RunID) //nolint:errcheck
			case <-al.stopFlush:
				return
			}
		}
	}()

	return nil
}

// AppendLog appends a line to the in-memory log buffer for runID.
// The buffer is flushed to disk every 500ms by the background goroutine.
func (rs *RunStore) AppendLog(runID, line string) error {
	rs.mu.Lock()
	al, ok := rs.active[runID]
	rs.mu.Unlock()
	if !ok {
		return nil // run already completed; silently drop
	}
	al.mu.Lock()
	al.writer.WriteString(line + "\n")
	al.mu.Unlock()
	return nil
}

// flushLog writes the in-memory buffer to the log file.
// Caller must not hold rs.mu.
func (rs *RunStore) flushLog(runID string) error {
	rs.mu.Lock()
	al, ok := rs.active[runID]
	rs.mu.Unlock()
	if !ok {
		return nil
	}
	al.mu.Lock()
	data := al.writer.String()
	al.writer.Reset()
	al.mu.Unlock()

	if data == "" {
		return nil
	}
	_, err := al.file.WriteString(data)
	return err
}

// FlushLog performs an explicit flush + sync for the given runID.
// Called by phaseRunner on completion or stop.
func (rs *RunStore) FlushLog(runID string) error {
	if err := rs.flushLog(runID); err != nil {
		return err
	}
	rs.mu.Lock()
	al, ok := rs.active[runID]
	rs.mu.Unlock()
	if !ok {
		return nil
	}
	return al.file.Sync()
}

// Complete updates run.json with completion status, flushes the log, closes
// the file, and removes the run from the active map.
func (rs *RunStore) Complete(runID string, exitCode int) error {
	// Read existing manifest.
	rs.mu.Lock()
	al, ok := rs.active[runID]
	rs.mu.Unlock()

	var runDir string
	if ok {
		runDir = al.runDir
	} else {
		runDir = filepath.Join(rs.logsDir, runID)
	}

	meta, err := readMeta(runDir)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	meta.CompletedAt = &now
	meta.ExitCode = exitCode
	switch exitCode {
	case 0:
		meta.Status = "completed"
	default:
		meta.Status = "stopped"
	}

	if err := writeJSON(filepath.Join(runDir, "run.json"), meta); err != nil {
		return err
	}

	// Stop background flusher, flush, close.
	if ok {
		al.flushTicker.Stop()
		close(al.stopFlush)
		_ = rs.flushLog(runID)
		_ = al.file.Sync()
		_ = al.file.Close()

		rs.mu.Lock()
		delete(rs.active, runID)
		rs.mu.Unlock()
	}

	return nil
}

// WriteReport serialises snap to report.json in the run directory.
func (rs *RunStore) WriteReport(runID string, snap metrics.MetricSnapshot) error {
	runDir := filepath.Join(rs.logsDir, runID)
	return writeJSON(filepath.Join(runDir, "report.json"), snap)
}

// List scans logsDir, parses run.json files, and returns runs sorted by
// StartedAt descending (most recent first), capped at 50.
func (rs *RunStore) List() ([]RunMeta, error) {
	entries, err := os.ReadDir(rs.logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: list runs: %w", err)
	}

	var runs []RunMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := readMeta(filepath.Join(rs.logsDir, e.Name()))
		if err != nil {
			continue // skip corrupt entries
		}
		runs = append(runs, meta)
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	if len(runs) > 50 {
		runs = runs[:50]
	}
	return runs, nil
}

// GetLog reads and returns the full log content for runID.
// Returns ("", nil) when the log file does not exist.
func (rs *RunStore) GetLog(runID string) (string, error) {
	// Flush any buffered content first.
	_ = rs.flushLog(runID)
	path := filepath.Join(rs.logsDir, runID, "run.log")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: read log: %w", err)
	}
	return string(b), nil
}

// GetReport reads report.json for runID and returns the MetricSnapshot.
func (rs *RunStore) GetReport(runID string) (metrics.MetricSnapshot, error) {
	path := filepath.Join(rs.logsDir, runID, "report.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return metrics.MetricSnapshot{}, fmt.Errorf("store: read report: %w", err)
	}
	var snap metrics.MetricSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return metrics.MetricSnapshot{}, fmt.Errorf("store: decode report: %w", err)
	}
	return snap, nil
}

// writeJSON marshals v and atomically writes it to path.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal json: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("store: write json: %w", err)
	}
	return os.Rename(tmp, path)
}

// readMeta reads and parses run.json from runDir.
func readMeta(runDir string) (RunMeta, error) {
	b, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		return RunMeta{}, err
	}
	var meta RunMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return RunMeta{}, err
	}
	return meta, nil
}
