package job

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/driver"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

// WorkloadNotFoundError is returned when the requested workload ID is not in the registry.
type WorkloadNotFoundError struct{ ID string }

func (e *WorkloadNotFoundError) Error() string {
	return fmt.Sprintf("job: workload %q not found", e.ID)
}

// SessionError wraps the underlying CQL session creation error.
type SessionError struct{ Cause error }

func (e *SessionError) Error() string { return "job: session error: " + e.Cause.Error() }
func (e *SessionError) Unwrap() error { return e.Cause }

// runPhases determines the phase execution sequence and runs each phase in order.
func runPhases(
	ctx context.Context,
	runID string,
	wd *workload.WorkloadDef,
	params RunParams,
	exec driver.Executor,
	col *metrics.Collector,
	rs *store.RunStore,
	bc *sse.Broadcaster,
) error {
	// Signal the UI that the session is established and phases are about to start.
	bc.Broadcast(statusEvent("session_ok", "Connected — starting phases"))

	// Compile all CQL templates; resolves TEMPLATE(name,default) and {param} tokens.
	if err := wd.CompileTemplates(params.ExtraParams); err != nil {
		return fmt.Errorf("compile templates: %w", err)
	}

	phases := phasesToRun(wd.Phases, params.Phase, params.SkipSchema)
	_ = rs.AppendLog(runID, fmt.Sprintf("[info] phases to run: %v", phases))
	bc.Broadcast(logEvent(fmt.Sprintf("[info] phases to run: %v", phases)))

	for _, phase := range phases {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		blocks := blocksForPhase(wd.Blocks, phase)
		if len(blocks) == 0 {
			_ = rs.AppendLog(runID, fmt.Sprintf("[warn] no blocks for phase %q, skipping", phase))
			continue
		}

		col.Reset()
		col.SetPhase(phase)

		_ = rs.AppendLog(runID, fmt.Sprintf("[info] starting phase %q", phase))
		bc.Broadcast(logEvent(fmt.Sprintf("[info] starting phase %q", phase)))
		bc.Broadcast(phaseEvent("phase_start", phase))

		var err error
		switch phase {
		case "schema", "truncate":
			err = runSchemaPhase(ctx, runID, blocks, exec, rs, bc)
		default:
			err = runWorkerPhase(ctx, runID, phase, blocks, params, exec, col, rs, bc)
		}

		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("phase %q: %w", phase, err)
		}

		_ = rs.AppendLog(runID, fmt.Sprintf("[info] phase %q done", phase))
		bc.Broadcast(logEvent(fmt.Sprintf("[info] phase %q done", phase)))
		bc.Broadcast(phaseEvent("phase_end", phase))
	}

	// Write final report.
	snap := col.Snapshot()
	_ = rs.WriteReport(runID, snap)
	snapJSON, _ := json.Marshal(snap)
	bc.Broadcast(string(snapJSON))

	return nil
}

// phasesToRun determines the ordered phase list given the workload's available
// phases, the requested phase (or "all"), and the SkipSchema flag.
func phasesToRun(available []string, requested string, skipSchema bool) []string {
	if requested != "all" && requested != "" {
		return []string{requested}
	}
	// "all" — run in canonical order, optionally skipping schema.
	canonical := []string{"schema", "rampup", "main", "truncate"}
	avail := make(map[string]bool, len(available))
	for _, p := range available {
		avail[p] = true
	}
	var out []string
	for _, p := range canonical {
		if avail[p] {
			if skipSchema && p == "schema" {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

// blocksForPhase returns blocks whose name exactly matches phase or has phase
// as a dash-delimited prefix (e.g. "main-read" and "main-write" both belong to
// the "main" phase).
func blocksForPhase(blocks []workload.Block, phase string) []workload.Block {
	var out []workload.Block
	for _, b := range blocks {
		if b.Name == phase ||
			strings.HasPrefix(b.Name, phase+"-") ||
			strings.HasPrefix(b.Name, phase+"_") {
			out = append(out, b)
		}
	}
	return out
}

// runSchemaPhase executes schema/truncate operations sequentially in a single goroutine.
func runSchemaPhase(
	ctx context.Context,
	runID string,
	blocks []workload.Block,
	exec driver.Executor,
	rs *store.RunStore,
	bc *sse.Broadcaster,
) error {
	for _, blk := range blocks {
		for _, op := range blk.Ops {
			if op.Template == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			cql := op.Template.NewExecutor(1_000_000, 100).Execute(0)
			if err := exec.Query(cql).WithContext(ctx).Exec(); err != nil {
				msg := fmt.Sprintf("[warn] schema op %q: %v", op.Name, err)
				_ = rs.AppendLog(runID, msg)
				bc.Broadcast(logEvent(msg))
				// Schema errors are logged but not fatal (e.g. keyspace already exists).
			} else {
				msg := fmt.Sprintf("[info] schema op %q ok", op.Name)
				_ = rs.AppendLog(runID, msg)
				bc.Broadcast(logEvent(msg))
			}
		}
	}
	return nil
}

// runWorkerPhase runs a rampup or main phase using a worker goroutine pool.
func runWorkerPhase(
	ctx context.Context,
	runID string,
	phase string,
	blocks []workload.Block,
	params RunParams,
	exec driver.Executor,
	col *metrics.Collector,
	rs *store.RunStore,
	bc *sse.Broadcaster,
) error {
	threads := params.Threads
	if threads <= 0 {
		threads = 30
	}
	targetRate := params.TargetRate
	if targetRate <= 0 {
		targetRate = 5000
	}

	// Rate limiter shared across all workers.
	// Burst = threads so each worker can start immediately on phase begin.
	limiter := rate.NewLimiter(rate.Limit(targetRate), threads)

	// Flatten all ops from the phase blocks and assign op-type indices.
	var ops []opEntry
	opIndex := make(map[string]int)
	opNames := make(map[int]string)
	for _, blk := range blocks {
		for i := range blk.Ops {
			op := &blk.Ops[i]
			name := op.Name
			if name == "" {
				name = fmt.Sprintf("op%d", len(ops))
			}
			idx, exists := opIndex[name]
			if !exists {
				idx = len(opIndex)
				opIndex[name] = idx
				opNames[idx] = name
			}
			ops = append(ops, opEntry{template: op, opTypeIdx: idx})
		}
	}

	if len(ops) == 0 {
		return nil
	}

	// Progress ticker: broadcasts metrics + log summary every 1s.
	tickerDone := make(chan struct{})
	go func() {
		defer close(tickerDone)
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				snap := col.Snapshot()
				snapJSON, err := json.Marshal(snap)
				if err == nil {
					bc.Broadcast(string(snapJSON))
				}
				line := fmt.Sprintf("[metrics] ops=%d ops/s=%.0f p99=%.1fms errors=%d",
					snap.OpsTotal, snap.OpsPerSec, snap.P99ms, snap.ErrorTotal)
				_ = rs.AppendLog(runID, line)
				bc.Broadcast(logEvent(line))
			}
		}
	}()

	// Error sampler: surface each unique CQL error at most 3 times per phase.
	sampler := newErrorSampler(3)
	logErrFn := func(msg string) {
		if line := sampler.sample(msg); line != "" {
			_ = rs.AppendLog(runID, line)
			bc.Broadcast(logEvent(line))
		}
	}

	// Worker goroutines via errgroup.
	g, gctx := errgroup.WithContext(ctx)

	// Determine stop condition.
	cycles := params.Cycles
	duration := params.Duration

	for w := 0; w < threads; w++ {
		workerID := w
		g.Go(func() error {
			view := col.NewWorkerView(workerID)
			deadlineCtx := gctx
			if duration > 0 {
				var cancel context.CancelFunc
				deadlineCtx, cancel = context.WithTimeout(gctx, duration)
				defer cancel()
			}
			return runWorker(deadlineCtx, workerID, ops, exec, view, limiter, cycles, int64(threads), logErrFn)
		})
	}

	err := g.Wait()
	// Signal progress ticker to stop and wait for it.
	// (It also stops via ctx.Done(), but we drain it to avoid goroutine leak.)
	<-tickerDone

	if err != nil && gctx.Err() != nil {
		// Context cancelled — not a real error; caller checks ctx.Err().
		return nil
	}
	return err
}

// logEvent wraps a log line as a JSON SSE event.
func logEvent(line string) string {
	type logMsg struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	b, _ := json.Marshal(logMsg{Type: "log", Message: line})
	return string(b)
}

// phaseEvent emits a phase lifecycle SSE event with the given type and phase name.
// eventType is "phase_start" or "phase_end".
func phaseEvent(eventType, phase string) string {
	type phaseMsg struct {
		Type  string `json:"type"`
		Phase string `json:"phase"`
	}
	b, _ := json.Marshal(phaseMsg{Type: eventType, Phase: phase})
	return string(b)
}

// statusEvent emits a general status SSE event (sub-type + optional message).
// sub is a short machine-readable token, e.g. "session_ok" or "connecting".
func statusEvent(sub, msg string) string {
	type statusMsg struct {
		Type    string `json:"type"`
		Sub     string `json:"sub"`
		Message string `json:"message,omitempty"`
	}
	b, _ := json.Marshal(statusMsg{Type: "status", Sub: sub, Message: msg})
	return string(b)
}
