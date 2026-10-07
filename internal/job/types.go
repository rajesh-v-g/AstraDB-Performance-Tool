package job

import (
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

// opEntry pairs a workload op with its op-type histogram index.
type opEntry struct {
	template  *workload.Op
	opTypeIdx int
}

// monotonicNow returns the current time for latency measurement.
func monotonicNow() time.Time { return time.Now() }

// monotonicSince returns the duration since t.
func monotonicSince(t time.Time) time.Duration { return time.Since(t) }
