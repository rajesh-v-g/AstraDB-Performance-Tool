package workload_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

const testWorkloadYAML = `
description: A test workload.
blocks:
  rampup:
    ops:
      insert: INSERT INTO ks.t (k, v) VALUES ('k', 'v')
`

func newTestRegistry(t *testing.T) *workload.Registry {
	t.Helper()
	tmp := t.TempDir()

	// Write a valid YAML workload file to tmp.
	path := filepath.Join(tmp, "test-workload.yaml")
	if err := os.WriteFile(path, []byte(testWorkloadYAML), 0o644); err != nil {
		t.Fatalf("write test workload: %v", err)
	}

	reg := &workload.Registry{}
	reg.Init(tmp, "")
	return reg
}

func TestRegistry_ListYAML(t *testing.T) {
	reg := newTestRegistry(t)
	defs := reg.List()
	if len(defs) == 0 {
		t.Fatal("List() returned empty — expected at least 1 YAML workload")
	}
	found := false
	for _, d := range defs {
		if d.ID == "test-workload" {
			found = true
		}
	}
	if !found {
		t.Errorf("test-workload not found in List(); got IDs: %v", ids(defs))
	}
}

func TestRegistry_Register_GoWorkload(t *testing.T) {
	reg := &workload.Registry{}
	reg.Init("", "")

	reg.Register(workload.WorkloadDef{
		ID:    "go-test",
		Label: "Go Test",
	})

	defs := reg.List()
	if len(defs) != 1 {
		t.Fatalf("List() = %d items, want 1", len(defs))
	}
	if defs[0].Source != "builtin-go" {
		t.Errorf("Source = %q, want %q", defs[0].Source, "builtin-go")
	}
}

func TestRegistry_Reload(t *testing.T) {
	tmp := t.TempDir()
	path1 := filepath.Join(tmp, "w1.yaml")
	if err := os.WriteFile(path1, []byte(testWorkloadYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := &workload.Registry{}
	reg.Init(tmp, "")

	defs := reg.List()
	if len(defs) != 1 {
		t.Fatalf("initial List() = %d, want 1", len(defs))
	}

	// Add a second file and reload.
	path2 := filepath.Join(tmp, "w2.yaml")
	if err := os.WriteFile(path2, []byte(testWorkloadYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	reg.Reload()

	defs = reg.List()
	if len(defs) != 2 {
		t.Fatalf("after Reload List() = %d, want 2", len(defs))
	}
}

func TestRegistry_ReloadPreservesGoWorkloads(t *testing.T) {
	reg := &workload.Registry{}
	reg.Init("", "")
	reg.Register(workload.WorkloadDef{ID: "go-permanent"})

	reg.Reload()

	defs := reg.List()
	found := false
	for _, d := range defs {
		if d.ID == "go-permanent" {
			found = true
		}
	}
	if !found {
		t.Error("Go workload evicted by Reload()")
	}
}

func TestRegistry_GetByID(t *testing.T) {
	reg := newTestRegistry(t)
	reg.Register(workload.WorkloadDef{ID: "go-find-me"})

	if _, ok := reg.GetByID("test-workload"); !ok {
		t.Error("GetByID('test-workload') returned false")
	}
	if _, ok := reg.GetByID("go-find-me"); !ok {
		t.Error("GetByID('go-find-me') returned false")
	}
	if _, ok := reg.GetByID("does-not-exist"); ok {
		t.Error("GetByID('does-not-exist') returned true")
	}
}

func ids(defs []workload.WorkloadDef) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.ID)
	}
	return out
}
