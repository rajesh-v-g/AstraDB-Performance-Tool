// Package job implements the benchmark job manager and worker pool.
package job

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/driver"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

// JobStatus represents the current state of the job manager.
type JobStatus string

const (
	StatusIdle    JobStatus = "idle"
	StatusRunning JobStatus = "running"
)

// ErrAlreadyRunning is returned by Manager.Start when a run is already active.
var ErrAlreadyRunning = fmt.Errorf("job: a run is already active")

// RunParams carries all parameters needed to start a benchmark run.
type RunParams struct {
	WorkloadID       string
	Phase            string
	Token            string // Astra token; never persisted
	SCBPath          string // path to SCB zip (Astra) or "" (Cassandra)
	Driver           string // "astra" | "cassandra"
	Keyspace         string
	ConsistencyLevel string
	Hosts            []string
	CassandraPort    int
	CassandraUser    string
	CassandraPass    string
	Threads          int
	TargetRate       int
	Cycles           int64
	Duration         time.Duration
	SkipSchema       bool
	ExtraParams      map[string]string
}

// activeJob tracks a running job.
type activeJob struct {
	runID     string
	cancel    context.CancelFunc
	done      chan struct{}
	collector *metrics.Collector
}

// WorkloadRegistry is the interface the Manager uses to look up workloads.
type WorkloadRegistry interface {
	GetByID(id string) (workload.WorkloadDef, bool)
}

// Manager is a singleton that ensures at most one benchmark runs at a time.
type Manager struct {
	mu          sync.Mutex
	state       JobStatus
	current     *activeJob
	runStore    *store.RunStore
	broadcaster *sse.Broadcaster
	registry    WorkloadRegistry
}

// NewManager creates a Manager wired to the provided dependencies.
func NewManager(rs *store.RunStore, bc *sse.Broadcaster, reg WorkloadRegistry) *Manager {
	return &Manager{
		state:       StatusIdle,
		runStore:    rs,
		broadcaster: bc,
		registry:    reg,
	}
}

// Status returns the current job state.
func (m *Manager) Status() JobStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Start launches a new benchmark run in the background.
// Returns (runID, nil) on success, or ("", error) if a run is already active.
func (m *Manager) Start(params RunParams) (string, error) {
	m.mu.Lock()
	if m.state == StatusRunning {
		m.mu.Unlock()
		return "", ErrAlreadyRunning
	}

	// Resolve workload.
	wdVal, ok := m.registry.GetByID(params.WorkloadID)
	if !ok {
		m.mu.Unlock()
		return "", &WorkloadNotFoundError{ID: params.WorkloadID}
	}
	// Work on a copy so the registry's cached definition is not mutated.
	wd := wdVal

	runID := store.NewRunID(params.WorkloadID, params.Phase)

	// Build CQL session.
	exec, err := driver.NewSession(driver.SessionParams{
		Driver:           params.Driver,
		Token:            params.Token,
		SCBPath:          params.SCBPath,
		Hosts:            params.Hosts,
		Port:             params.CassandraPort,
		Username:         params.CassandraUser,
		Password:         params.CassandraPass,
		Keyspace:         params.Keyspace,
		ConsistencyLevel: params.ConsistencyLevel,
	})
	if err != nil {
		m.mu.Unlock()
		return "", &SessionError{Cause: err}
	}

	// Collect op types from the workload for histogram allocation.
	opTypes := collectOpTypes(wd)
	if len(opTypes) == 0 {
		opTypes = []string{"op"}
	}
	threads := params.Threads
	if threads <= 0 {
		threads = 30
	}
	col := metrics.NewCollector(threads, opTypes)
	col.SetWorkload(params.WorkloadID)

	var ctx context.Context
	var cancel context.CancelFunc
	if params.Duration > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), params.Duration)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	aj := &activeJob{
		runID:     runID,
		cancel:    cancel,
		done:      make(chan struct{}),
		collector: col,
	}
	m.state = StatusRunning
	m.current = aj
	m.mu.Unlock()

	// Persist run manifest.
	redacted := store.RedactParams(extraParamsToMap(params))
	_ = m.runStore.Create(store.RunMeta{
		RunID:          runID,
		Workload:       params.WorkloadID,
		WorkloadSource: wd.Source,
		Phase:          params.Phase,
		Params:         redacted,
		StartedAt:      time.Now().UTC(),
	})

	// Launch background phase runner.
	go func() {
		exitCode := 0
		if err := runPhases(ctx, runID, &wd, params, exec, col, m.runStore, m.broadcaster); err != nil {
			if ctx.Err() == nil {
				// Non-cancellation error.
				_ = m.runStore.AppendLog(runID, "[error] "+err.Error())
			}
			exitCode = 1
		}
		exec.Close()
		_ = m.runStore.Complete(runID, exitCode)
		m.broadcaster.Close()

		m.mu.Lock()
		m.state = StatusIdle
		m.current = nil
		m.mu.Unlock()

		close(aj.done)
	}()

	return runID, nil
}

// Stop cancels the active run and waits for it to finish.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.state != StatusRunning || m.current == nil {
		m.mu.Unlock()
		return
	}
	aj := m.current
	m.mu.Unlock()

	aj.cancel()
	<-aj.done
}

// ActiveRunID returns the run ID of the currently running job, or "" if idle.
func (m *Manager) ActiveRunID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		return m.current.runID
	}
	return ""
}

// CurrentCollector returns the metrics collector for the active run, or nil if idle.
func (m *Manager) CurrentCollector() *metrics.Collector {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		return m.current.collector
	}
	return nil
}

// ---- helpers ----------------------------------------------------------------

// extraParamsToMap converts RunParams fields to a flat map for redaction/storage.
func extraParamsToMap(p RunParams) map[string]string {
	m := make(map[string]string)
	m["driver"] = p.Driver
	m["keyspace"] = p.Keyspace
	m["consistency"] = p.ConsistencyLevel
	if p.Token != "" {
		m["token"] = p.Token // will be redacted
	}
	for k, v := range p.ExtraParams {
		m[k] = v
	}
	return m
}

// collectOpTypes returns unique op names from the workload definition.
func collectOpTypes(wd workload.WorkloadDef) []string {
	seen := make(map[string]struct{})
	var types []string
	for _, b := range wd.Blocks {
		for _, op := range b.Ops {
			if _, ok := seen[op.Name]; !ok {
				seen[op.Name] = struct{}{}
				name := op.Name
				if name == "" {
					name = "op"
				}
				types = append(types, name)
			}
		}
	}
	return types
}
