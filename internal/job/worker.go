package job

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/time/rate"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/binding"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/driver"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
)

// errorSampler deduplicates CQL error messages across workers so that each
// unique error is surfaced at most maxPerMsg times, keeping the log readable
// at high ops/sec.
type errorSampler struct {
	mu        sync.Mutex
	seen      map[string]int
	maxPerMsg int
}

func newErrorSampler(maxPerMsg int) *errorSampler {
	return &errorSampler{seen: make(map[string]int), maxPerMsg: maxPerMsg}
}

// sample returns a formatted log line the first maxPerMsg times a given error
// message is seen, and "" for subsequent occurrences.
func (s *errorSampler) sample(errMsg string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.seen[errMsg]
	if n < s.maxPerMsg {
		s.seen[errMsg]++
		return fmt.Sprintf("[error] cql: %s", errMsg)
	}
	return ""
}

// runWorker is the hot-path per-goroutine execution loop.
// It runs until ctx is cancelled, cycles are exhausted, or the rate limiter
// returns an error.
//
// Parameters:
//   - ctx:       cancellable context; cancel signals stop
//   - workerID:  unique worker index (used for counter partitioning)
//   - ops:       flattened list of opEntry values to execute round-robin
//   - exec:      CQL executor (shared across workers; *gocql.Session is thread-safe)
//   - view:      per-worker histogram view (no mutex on hot path)
//   - limiter:   shared rate limiter
//   - maxCycles: total op count across ALL workers; 0 = unlimited
//   - nWorkers:  number of workers (used to partition the cycle space)
//   - logErr:    callback invoked with a formatted error line (already sampled)
func runWorker(
	ctx context.Context,
	workerID int,
	ops []opEntry,
	exec driver.Executor,
	view *metrics.WorkerView,
	limiter *rate.Limiter,
	maxCycles int64,
	nWorkers int64,
	logErr func(string),
) error {
	// Each worker handles a slice of the total cycle space.
	// If maxCycles == 0, workers run until ctx is cancelled.
	perWorkerCycles := int64(0)
	if maxCycles > 0 && nWorkers > 0 {
		perWorkerCycles = maxCycles / nWorkers
		if int64(workerID) < maxCycles%nWorkers {
			perWorkerCycles++
		}
	}

	const defaultMaxKeys = 10_000_000
	const defaultValueSize = 100

	// Pre-create one Executor per op so the hot loop only calls Execute,
	// not NewExecutor (which allocates a rand.Rand + strings.Builder each time).
	executors := make([]*binding.Executor, len(ops))
	for i, e := range ops {
		if e.template != nil && e.template.Template != nil {
			executors[i] = e.template.Template.NewExecutor(defaultMaxKeys, defaultValueSize)
		}
	}

	opCount := int64(len(ops))
	counter := int64(workerID) // interleaved start position

	var localCycles int64
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if perWorkerCycles > 0 && localCycles >= perWorkerCycles {
			return nil
		}

		// Wait for rate limiter token.
		if err := limiter.Wait(ctx); err != nil {
			return nil // ctx cancelled
		}

		// Pick op round-robin.
		opIdx := counter % opCount
		entry := ops[opIdx]

		if executors[opIdx] == nil {
			counter++
			localCycles++
			continue
		}

		cql := executors[opIdx].Execute(counter)

		start := monotonicNow()
		err := exec.Query(cql).WithContext(ctx).Exec()
		elapsed := monotonicSince(start)

		// Ignore context cancellation/deadline errors — these are clean shutdowns,
		// not real CQL failures. Don't log or count them as errors.
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logErr(err.Error())
			view.Record(entry.opTypeIdx, elapsed, err)
		} else if err == nil {
			view.Record(entry.opTypeIdx, elapsed, nil)
		}

		counter += nWorkers
		localCycles++
	}
}
