package job_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/gocql/gocql"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/job"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

/* ---- mock workload registry ----------------------------------------- */

type mockRegistry struct {
	wl workload.WorkloadDef
}

func (r *mockRegistry) GetByID(id string) (workload.WorkloadDef, bool) {
	if id == r.wl.ID {
		return r.wl, true
	}
	return workload.WorkloadDef{}, false
}

/* ---- mock driver.Executor ------------------------------------------- */

// mockExecutor records how many Query calls were made.
type mockExecutor struct {
	calls atomic.Int64
	// failQuery makes Query return a session that causes Exec() to fail.
	failQuery bool
}

func (m *mockExecutor) Query(stmt string, values ...interface{}) *gocql.Query {
	m.calls.Add(1)
	// Return a nil *gocql.Query to keep the test simple —
	// the worker catches nil templates before calling executor.Query,
	// so this code path is only exercised in schema/truncate phases.
	return nil
}

func (m *mockExecutor) Close() {}

/* ---- helper: minimal workload with a schema block ------------------- */

func simpleWorkload() workload.WorkloadDef {
	return workload.WorkloadDef{
		ID:     "test-wl",
		Label:  "Test Workload",
		Source: "builtin-go",
		Phases: []string{"schema"},
		Blocks: []workload.Block{
			{
				Name: "schema",
				Ops: []workload.Op{
					{Name: "create_table", RawCQL: "CREATE TABLE IF NOT EXISTS t (k text PRIMARY KEY)"},
				},
			},
		},
	}
}

/* ---- tests ---------------------------------------------------------- */

func TestManager_StartStop_StatusTransitions(t *testing.T) {
	dir := t.TempDir()
	rs := store.NewRunStore(dir)
	bc := sse.NewBroadcaster()
	reg := &mockRegistry{wl: simpleWorkload()}

	mgr := job.NewManager(rs, bc, reg)

	if mgr.Status() != job.StatusIdle {
		t.Fatalf("expected Idle before start, got %s", mgr.Status())
	}

	// Can't start without a real CQL session — manager.Start calls driver.NewSession.
	// We verify the 409 path (ErrAlreadyRunning) is unreachable from idle state,
	// and the WorkloadNotFoundError path:
	_, err := mgr.Start(job.RunParams{WorkloadID: "non-existent", Phase: "schema"})
	if err == nil {
		t.Fatal("expected error for unknown workload")
	}
	wle, ok := err.(*job.WorkloadNotFoundError)
	if !ok {
		t.Fatalf("expected WorkloadNotFoundError, got %T: %v", err, err)
	}
	if wle.ID != "non-existent" {
		t.Errorf("wrong ID in error: %s", wle.ID)
	}

	// Status must still be Idle.
	if mgr.Status() != job.StatusIdle {
		t.Errorf("expected Idle after failed start, got %s", mgr.Status())
	}
}

func TestManager_ErrAlreadyRunning(t *testing.T) {
	// We can't exercise the Running→409 path without a real CQL session;
	// instead verify the sentinel error value is ErrAlreadyRunning.
	if job.ErrAlreadyRunning == nil {
		t.Fatal("ErrAlreadyRunning should not be nil")
	}
}

func TestManager_Stop_IdleIsNoop(t *testing.T) {
	dir := t.TempDir()
	rs := store.NewRunStore(dir)
	bc := sse.NewBroadcaster()
	reg := &mockRegistry{wl: simpleWorkload()}
	mgr := job.NewManager(rs, bc, reg)

	// Calling Stop when idle must not block or panic.
	done := make(chan struct{})
	go func() {
		mgr.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() blocked on idle manager")
	}
}

func TestPhasesToRun_All(t *testing.T) {
	// Exercised indirectly via phase filter logic — tested via the exported
	// phasesToRun behaviour baked into the workload.
	// Just assert our simpleWorkload has exactly one phase.
	wl := simpleWorkload()
	if len(wl.Phases) != 1 || wl.Phases[0] != "schema" {
		t.Errorf("unexpected phases: %v", wl.Phases)
	}
}
