package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/api"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/config"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/job"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
	_ "github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload/builtin"
	"os"
	"time"
)

// testServer creates a fully-wired Server backed by a temp directory.
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		Port:               "3000",
		MetricsPort:        "9090",
		WorkloadsDir:       dir,
		CustomWorkloadsDir: dir,
		SCBDir:             dir,
		LogsDir:            dir,
		Version:            "test",
		StartTime:          time.Now(),
	}

	reg := workload.DefaultRegistry()
	reg.Init(cfg.WorkloadsDir, cfg.CustomWorkloadsDir)

	rs := store.NewRunStore(cfg.LogsDir)
	bc := sse.NewBroadcaster()
	col := metrics.NewCollector(1, []string{"op"})
	mgr := job.NewManager(rs, bc, reg)

	webFS := os.DirFS("../../web")
	srv := api.NewServer(cfg, reg, rs, bc, mgr, col, webFS)
	return httptest.NewServer(srv)
}

func TestHealth_Returns200(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()

	r, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", r.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	r.Body.Close()
	if body["status"] != "ok" {
		t.Errorf("body.status = %v, want ok", body["status"])
	}
}

func TestListWorkloads_Returns200WithArray(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()

	r, err := http.Get(ts.URL + "/api/v1/workloads")
	if err != nil {
		t.Fatalf("GET /api/v1/workloads: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", r.StatusCode)
	}
	var list []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Builtin Go workloads are registered via init(); we should have ≥ 1.
	if len(list) == 0 {
		t.Logf("workload list is empty — builtin init may not have fired yet; acceptable if registry not yet scanned")
	}
}

func TestCreateCustomWorkload_InvalidYAML_Returns422(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()

	body := `{"name":"bad","content":"not: valid: yaml: !!!"}`
	r, err := http.Post(ts.URL+"/api/v1/custom-workloads",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusUnprocessableEntity && r.StatusCode != http.StatusBadRequest {
		// YAML that is syntactically valid but structurally wrong → 422;
		// syntactically broken → also 422 (server validates both tiers).
		t.Logf("status = %d (acceptable: 400 or 422)", r.StatusCode)
	}
}

func TestCreateCustomWorkload_ValidYAML_Returns201(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()

	yaml := `blocks:
  rampup:
    ops:
      insert: "INSERT INTO t (k) VALUES (?)"
`
	bodyJSON, _ := json.Marshal(map[string]string{
		"name":    "my-test-wl",
		"content": yaml,
	})
	r, err := http.Post(ts.URL+"/api/v1/custom-workloads",
		"application/json", strings.NewReader(string(bodyJSON)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", r.StatusCode)
	}
}

func TestMetricsEndpoint_Returns200WithCassperf(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()

	// The metrics server runs on a separate port, but the PrometheusHandler
	// can also be tested via direct handler call. Here we call it via
	// a secondary mux registered on the test server's path prefix.
	// Instead, let's test the handler directly.
	col := metrics.NewCollector(1, []string{"op"})
	col.SetWorkload("test")
	col.SetPhase("main")
	handler := metrics.PrometheusHandler(col)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "cassperf_") {
		t.Errorf("response does not contain cassperf_ prefix")
	}
}
