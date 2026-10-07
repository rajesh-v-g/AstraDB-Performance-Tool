package metrics_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
)

func TestCollector_Snapshot_MergesHistograms(t *testing.T) {
	opTypes := []string{"insert", "select"}
	col := metrics.NewCollector(4, opTypes)
	col.SetWorkload("test-workload")
	col.SetPhase("main")

	// Record latencies from each of the 4 workers.
	for w := 0; w < 4; w++ {
		view := col.NewWorkerView(w)
		// Record 100 x 2ms for insert (opIndex 0).
		for i := 0; i < 100; i++ {
			view.Record(0, 2*time.Millisecond, nil)
		}
		// Record 100 x 10ms for select (opIndex 1), with 1 error.
		for i := 0; i < 100; i++ {
			var err error
			if i == 0 {
				err = errTest
			}
			view.Record(1, 10*time.Millisecond, err)
		}
	}

	snap := col.Snapshot()

	if snap.OpsTotal != 800 {
		t.Errorf("OpsTotal = %d, want 800", snap.OpsTotal)
	}
	if snap.ErrorTotal != 4 { // 1 error × 4 workers
		t.Errorf("ErrorTotal = %d, want 4", snap.ErrorTotal)
	}
	if snap.Phase != "main" {
		t.Errorf("Phase = %q, want %q", snap.Phase, "main")
	}
	if snap.Workload != "test-workload" {
		t.Errorf("Workload = %q, want %q", snap.Workload, "test-workload")
	}

	// P99 for insert should be close to 2ms.
	insert, ok := snap.OpBreakdown["insert"]
	if !ok {
		t.Fatal("missing insert op breakdown")
	}
	if insert.P99ms < 1.0 || insert.P99ms > 5.0 {
		t.Errorf("insert P99ms = %f, want ~2ms", insert.P99ms)
	}

	// P99 for select should be close to 10ms.
	sel, ok := snap.OpBreakdown["select"]
	if !ok {
		t.Fatal("missing select op breakdown")
	}
	if sel.P99ms < 5.0 || sel.P99ms > 20.0 {
		t.Errorf("select P99ms = %f, want ~10ms", sel.P99ms)
	}

	// Overall P99 should be dominated by the 10ms select ops.
	if snap.P99ms < 5.0 {
		t.Errorf("overall P99ms = %f, want >= 5ms", snap.P99ms)
	}
}

func TestCollector_Reset_ZerosCounters(t *testing.T) {
	col := metrics.NewCollector(2, []string{"write"})
	view := col.NewWorkerView(0)
	view.Record(0, 5*time.Millisecond, nil)

	snap := col.Snapshot()
	if snap.OpsTotal == 0 {
		t.Fatal("OpsTotal should be non-zero before reset")
	}

	col.Reset()
	snap = col.Snapshot()
	if snap.OpsTotal != 0 {
		t.Errorf("OpsTotal after Reset = %d, want 0", snap.OpsTotal)
	}
	if snap.P99ms != 0 {
		t.Errorf("P99ms after Reset = %f, want 0", snap.P99ms)
	}
}

func TestPrometheusHandler_ContainsExpectedMetrics(t *testing.T) {
	col := metrics.NewCollector(2, []string{"insert"})
	col.SetWorkload("cql-keyvalue")
	col.SetPhase("main")

	view := col.NewWorkerView(0)
	view.Record(0, 5*time.Millisecond, nil)
	view.Record(0, 15*time.Millisecond, nil)

	handler := metrics.PrometheusHandler(col)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	body := rr.Body.String()

	wantMetrics := []string{
		"cassperf_ops_total",
		"cassperf_ops_per_sec",
		"cassperf_error_total",
		"cassperf_latency_p50_ms",
		"cassperf_latency_p95_ms",
		"cassperf_latency_p99_ms",
		"cassperf_latency_p999_ms",
		"cassperf_run_active",
	}
	for _, name := range wantMetrics {
		if !strings.Contains(body, name) {
			t.Errorf("Prometheus response missing metric %q", name)
		}
	}

	// run_active should be 1 because phase is set.
	if !strings.Contains(body, "cassperf_run_active 1") {
		t.Errorf("expected cassperf_run_active 1, body:\n%s", body)
	}
}

func TestPrometheusHandler_RunInactive(t *testing.T) {
	col := metrics.NewCollector(1, []string{"write"})
	// Phase is empty → run_active 0.
	handler := metrics.PrometheusHandler(col)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "cassperf_run_active 0") {
		t.Errorf("expected cassperf_run_active 0, body:\n%s", body)
	}
}

// errTest is a sentinel error used in tests.
var errTest = &testErr{}

type testErr struct{}

func (e *testErr) Error() string { return "test error" }
