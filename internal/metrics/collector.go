// Package metrics provides per-worker HDR histogram collection and Prometheus
// text-format exposition.
package metrics

import (
	"sync/atomic"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
)

// LatencyStats holds the computed percentile latencies for one op type.
type LatencyStats struct {
	P50ms  float64 `json:"p50ms"`
	P95ms  float64 `json:"p95ms"`
	P99ms  float64 `json:"p99ms"`
	P999ms float64 `json:"p999ms"`
	Count  int64   `json:"count"`
}

// MetricSnapshot is a point-in-time view of the collector state.
// It is safe to serialise to JSON and to use as the Prometheus text payload.
type MetricSnapshot struct {
	Type        string                  `json:"type"`
	OpsTotal    int64                   `json:"ops_total"`
	OpsPerSec   float64                 `json:"ops_per_sec"`
	ErrorTotal  int64                   `json:"error_total"`
	P50ms       float64                 `json:"p50ms"`
	P95ms       float64                 `json:"p95ms"`
	P99ms       float64                 `json:"p99ms"`
	P999ms      float64                 `json:"p999ms"`
	Phase       string                  `json:"phase"`
	Workload    string                  `json:"workload"`
	Elapsed     float64                 `json:"elapsed_sec"`
	OpBreakdown map[string]LatencyStats `json:"op_breakdown,omitempty"`
}

// Collector holds per-worker HDR histograms and aggregate counters.
// The design is intentionally lock-free on the hot path: each worker writes to
// its own histogram slice; merging only happens at Snapshot time.
type Collector struct {
	workerCount  int
	opTypes      []string
	histograms   [][]*hdrhistogram.Histogram // [workerID][opTypeIndex]
	opsTotal     atomic.Int64
	errorTotal   atomic.Int64
	startTime    time.Time
	currentPhase atomic.Value // stores string
	workloadID   atomic.Value // stores string
}

// NewCollector creates a Collector pre-allocated for workerCount workers and the
// given opType labels.  opTypes are the names of the distinct operation types
// (e.g. ["insert", "select"]) — they are used both for histogram indexing and
// as Prometheus label values.
func NewCollector(workerCount int, opTypes []string) *Collector {
	c := &Collector{
		workerCount: workerCount,
		opTypes:     append([]string(nil), opTypes...),
		startTime:   time.Now(),
	}
	c.histograms = make([][]*hdrhistogram.Histogram, workerCount)
	for w := range c.histograms {
		c.histograms[w] = make([]*hdrhistogram.Histogram, len(opTypes))
		for o := range c.histograms[w] {
			// 1 µs – 60 s, 3 significant figures; values recorded in microseconds.
			c.histograms[w][o] = hdrhistogram.New(1, 60_000_000, 3)
		}
	}
	c.currentPhase.Store("")
	c.workloadID.Store("")
	return c
}

// SetPhase updates the current phase label (e.g. "rampup", "main").
func (c *Collector) SetPhase(phase string) { c.currentPhase.Store(phase) }

// SetWorkload updates the workload label.
func (c *Collector) SetWorkload(id string) { c.workloadID.Store(id) }

// WorkerView is a write-only view for a single worker goroutine.
// All writes go to that worker's private histogram slice — no contention.
type WorkerView struct {
	c        *Collector
	workerID int
}

// NewWorkerView returns a WorkerView for the given workerID.
// workerID must be in [0, workerCount).
func (c *Collector) NewWorkerView(workerID int) *WorkerView {
	return &WorkerView{c: c, workerID: workerID}
}

// Record adds one observation.  opTypeIndex is the index into the opTypes slice
// supplied to NewCollector.  latency is the observed operation duration.
// If err is non-nil, the error counter is incremented but the latency is still
// recorded.
func (v *WorkerView) Record(opTypeIndex int, latency time.Duration, err error) {
	us := latency.Microseconds()
	if us < 1 {
		us = 1
	}
	_ = v.c.histograms[v.workerID][opTypeIndex].RecordValue(us) //nolint:errcheck
	v.c.opsTotal.Add(1)
	if err != nil {
		v.c.errorTotal.Add(1)
	}
}

// Snapshot merges all worker histograms into a single MetricSnapshot.
// It is safe to call concurrently with workers calling Record.
// Values are in milliseconds (µs / 1000).
func (c *Collector) Snapshot() MetricSnapshot {
	elapsed := time.Since(c.startTime).Seconds()
	opsTotal := c.opsTotal.Load()
	var opsPerSec float64
	if elapsed > 0 {
		opsPerSec = float64(opsTotal) / elapsed
	}

	// Merge per-worker histograms per op type.
	breakdown := make(map[string]LatencyStats, len(c.opTypes))
	merged := hdrhistogram.New(1, 60_000_000, 3)

	for opIdx, opName := range c.opTypes {
		opMerged := hdrhistogram.New(1, 60_000_000, 3)
		for w := range c.histograms {
			opMerged.Merge(c.histograms[w][opIdx])
		}
		merged.Merge(opMerged)
		breakdown[opName] = LatencyStats{
			P50ms:  usToMs(opMerged.ValueAtQuantile(50)),
			P95ms:  usToMs(opMerged.ValueAtQuantile(95)),
			P99ms:  usToMs(opMerged.ValueAtQuantile(99)),
			P999ms: usToMs(opMerged.ValueAtQuantile(99.9)),
			Count:  opMerged.TotalCount(),
		}
	}

	phase, _ := c.currentPhase.Load().(string)
	workload, _ := c.workloadID.Load().(string)

	return MetricSnapshot{
		Type:        "metrics",
		OpsTotal:    opsTotal,
		OpsPerSec:   opsPerSec,
		ErrorTotal:  c.errorTotal.Load(),
		P50ms:       usToMs(merged.ValueAtQuantile(50)),
		P95ms:       usToMs(merged.ValueAtQuantile(95)),
		P99ms:       usToMs(merged.ValueAtQuantile(99)),
		P999ms:      usToMs(merged.ValueAtQuantile(99.9)),
		Phase:       phase,
		Workload:    workload,
		Elapsed:     elapsed,
		OpBreakdown: breakdown,
	}
}

// Reset reinitialises all histograms and zeroes the counters.
// Called between phases.
func (c *Collector) Reset() {
	for w := range c.histograms {
		for o := range c.histograms[w] {
			c.histograms[w][o].Reset()
		}
	}
	c.opsTotal.Store(0)
	c.errorTotal.Store(0)
	c.startTime = time.Now()
}

// usToMs converts microseconds to milliseconds, rounding to 3 decimal places.
func usToMs(us int64) float64 {
	return float64(us) / 1000.0
}
