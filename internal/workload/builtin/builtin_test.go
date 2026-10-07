package builtin_test

import (
	"testing"

	_ "github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload/builtin"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

func TestBuiltinWorkloads_Registered(t *testing.T) {
	// The blank import of builtin triggers init() which registers the 3 Go workloads
	// into the package-level DefaultRegistry.
	reg := workload.DefaultRegistry()

	defs := reg.List()
	goWorkloads := make(map[string]workload.WorkloadDef)
	for _, d := range defs {
		if d.Source == "builtin-go" {
			goWorkloads[d.ID] = d
		}
	}

	wantIDs := []string{"go-keyvalue", "go-keyvalue-batch", "go-iot"}
	for _, id := range wantIDs {
		d, ok := goWorkloads[id]
		if !ok {
			t.Errorf("builtin workload %q not registered", id)
			continue
		}
		if len(d.Blocks) == 0 {
			t.Errorf("workload %q has no blocks", id)
		}
		if len(d.Parameters) == 0 {
			t.Errorf("workload %q has no parameters", id)
		}
		if len(d.Phases) == 0 {
			t.Errorf("workload %q has no phases", id)
		}
	}
}

func TestBuiltinWorkloads_HasCorrectPhases(t *testing.T) {
	reg := workload.DefaultRegistry()
	d, ok := reg.GetByID("go-keyvalue")
	if !ok {
		t.Fatal("go-keyvalue not found")
	}
	phaseSet := make(map[string]bool)
	for _, p := range d.Phases {
		phaseSet[p] = true
	}
	for _, want := range []string{"schema", "rampup", "main", "truncate"} {
		if !phaseSet[want] {
			t.Errorf("phase %q missing from go-keyvalue; phases = %v", want, d.Phases)
		}
	}
}

func TestBuiltinWorkloads_BatchHasBatchOps(t *testing.T) {
	reg := workload.DefaultRegistry()
	d, ok := reg.GetByID("go-keyvalue-batch")
	if !ok {
		t.Fatal("go-keyvalue-batch not found")
	}
	foundBatch := false
	for _, b := range d.Blocks {
		for _, op := range b.Ops {
			if len(op.RawCQL) > 0 {
				foundBatch = true
			}
		}
	}
	if !foundBatch {
		t.Error("go-keyvalue-batch has no ops with CQL")
	}
}
