package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store"
)

func TestRunStore_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	rs := store.NewRunStore(dir)

	runID := store.NewRunID("test-workload", "main")
	if runID == "" {
		t.Fatal("NewRunID returned empty string")
	}

	meta := store.RunMeta{
		RunID:     runID,
		Workload:  "test-workload",
		Phase:     "main",
		StartedAt: time.Now().UTC(),
	}

	// Create.
	if err := rs.Create(meta); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// AppendLog.
	lines := []string{"line one", "line two", "line three"}
	for _, l := range lines {
		if err := rs.AppendLog(runID, l); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	// FlushLog.
	if err := rs.FlushLog(runID); err != nil {
		t.Fatalf("FlushLog: %v", err)
	}

	// GetLog before Complete.
	content, err := rs.GetLog(runID)
	if err != nil {
		t.Fatalf("GetLog: %v", err)
	}
	for _, l := range lines {
		if !containsString(content, l) {
			t.Errorf("log missing line %q", l)
		}
	}

	// WriteReport.
	snap := metrics.MetricSnapshot{
		Type:     "metrics",
		OpsTotal: 42,
		Phase:    "main",
		Workload: "test-workload",
	}
	if err := rs.WriteReport(runID, snap); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	// Verify report.json exists.
	reportPath := filepath.Join(dir, runID, "report.json")
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("report.json not found: %v", err)
	}

	// Complete.
	if err := rs.Complete(runID, 0); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// List returns 1 run.
	runs, err := rs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("List returned %d runs, want 1", len(runs))
	}
	if runs[0].Status != "completed" {
		t.Errorf("status = %q, want completed", runs[0].Status)
	}

	// GetReport.
	got, err := rs.GetReport(runID)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if got.OpsTotal != 42 {
		t.Errorf("OpsTotal = %d, want 42", got.OpsTotal)
	}
}

func TestRedactParams(t *testing.T) {
	params := map[string]string{
		"token":    "AstraCS:super-secret",
		"keyspace": "my_ks",
		"password": "hunter2",
	}
	redacted := store.RedactParams(params)
	if redacted["token"] != "***" {
		t.Errorf("token not redacted: %q", redacted["token"])
	}
	if redacted["password"] != "***" {
		t.Errorf("password not redacted: %q", redacted["password"])
	}
	if redacted["keyspace"] != "my_ks" {
		t.Errorf("keyspace should not be redacted, got %q", redacted["keyspace"])
	}
}

func TestNewRunID_Format(t *testing.T) {
	id := store.NewRunID("cql-keyvalue", "main")
	if len(id) < 20 {
		t.Errorf("runID too short: %q", id)
	}
	// Should contain workload and phase.
	if !containsString(id, "cql") {
		t.Errorf("runID missing workload fragment: %q", id)
	}
	if !containsString(id, "main") {
		t.Errorf("runID missing phase: %q", id)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAny(s, sub))
}

func containsAny(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
