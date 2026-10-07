package workload_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

// workloadsDir returns the path to the workloads/ directory relative to this test file.
func workloadsDir() string {
	_, file, _, _ := runtime.Caller(0)
	// This test file is at internal/workload/yaml_loader_test.go
	// workloads/ is 2 levels up: internal/workload → internal → project root → workloads
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(root, "workloads")
}

func TestLoadFile_KeyValue(t *testing.T) {
	path := filepath.Join(workloadsDir(), "cql-keyvalue.yaml")
	def, err := workload.LoadFile(path, "yaml-builtin")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if def.ID != "cql-keyvalue" {
		t.Errorf("ID = %q, want %q", def.ID, "cql-keyvalue")
	}
	if def.Description == "" {
		t.Error("Description is empty")
	}
	if len(def.Blocks) == 0 {
		t.Error("Blocks is empty")
	}
	if len(def.Phases) == 0 {
		t.Error("Phases is empty")
	}
	// Expect schema, rampup, main, truncate phases.
	phaseSet := make(map[string]bool)
	for _, p := range def.Phases {
		phaseSet[p] = true
	}
	for _, want := range []string{"schema", "rampup", "main", "truncate"} {
		if !phaseSet[want] {
			t.Errorf("phase %q missing; phases = %v", want, def.Phases)
		}
	}
	if len(def.Parameters) == 0 {
		t.Error("Parameters is empty — expected TEMPLATE macros to be extracted")
	}
}

func TestLoadFile_KeyValueBatch(t *testing.T) {
	path := filepath.Join(workloadsDir(), "cql-keyvalue-batch.yaml")
	def, err := workload.LoadFile(path, "yaml-builtin")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(def.Blocks) < 3 {
		t.Errorf("expected ≥3 blocks, got %d", len(def.Blocks))
	}
}

func TestLoadFile_IoT(t *testing.T) {
	path := filepath.Join(workloadsDir(), "cql-iot.yaml")
	def, err := workload.LoadFile(path, "yaml-builtin")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if def.ID != "cql-iot" {
		t.Errorf("ID = %q, want %q", def.ID, "cql-iot")
	}
	if len(def.Blocks) < 4 {
		t.Errorf("expected ≥4 blocks (schema,rampup,main-read,main-write,[truncate]), got %d", len(def.Blocks))
	}
}

func TestLoadFile_NotFound(t *testing.T) {
	_, err := workload.LoadFile("/nonexistent/path/workload.yaml", "yaml-builtin")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}
