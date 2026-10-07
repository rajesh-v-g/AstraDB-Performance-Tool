package metrics

import (
	"fmt"
	"net/http"
)

// PrometheusHandler returns an http.HandlerFunc that writes the current
// collector state as Prometheus text format on every request.
//
// All metric names use the "cassperf_" prefix.  Labels used:
//   - workload: the active workload ID
//   - phase:    the current phase name
//   - op:       the operation type (latency metrics only)
func PrometheusHandler(col *Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := col.Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		wl := snap.Workload
		ph := snap.Phase

		fmt.Fprintf(w, "# HELP cassperf_ops_total Total CQL operations executed in the current or last run\n")
		fmt.Fprintf(w, "# TYPE cassperf_ops_total gauge\n")
		fmt.Fprintf(w, "cassperf_ops_total{workload=%q,phase=%q} %d\n", wl, ph, snap.OpsTotal)

		fmt.Fprintf(w, "# HELP cassperf_ops_per_sec Current ops/sec (last window)\n")
		fmt.Fprintf(w, "# TYPE cassperf_ops_per_sec gauge\n")
		fmt.Fprintf(w, "cassperf_ops_per_sec{workload=%q,phase=%q} %g\n", wl, ph, snap.OpsPerSec)

		fmt.Fprintf(w, "# HELP cassperf_error_total Total failed CQL operations\n")
		fmt.Fprintf(w, "# TYPE cassperf_error_total gauge\n")
		fmt.Fprintf(w, "cassperf_error_total{workload=%q,phase=%q} %d\n", wl, ph, snap.ErrorTotal)

		// Per the Prometheus text format spec, HELP must immediately precede
		// TYPE, and both must appear before the first sample for each metric.
		fmt.Fprintf(w, "# HELP cassperf_latency_p50_ms P50 latency milliseconds\n")
		fmt.Fprintf(w, "# TYPE cassperf_latency_p50_ms gauge\n")
		fmt.Fprintf(w, "# HELP cassperf_latency_p95_ms P95 latency milliseconds\n")
		fmt.Fprintf(w, "# TYPE cassperf_latency_p95_ms gauge\n")
		fmt.Fprintf(w, "# HELP cassperf_latency_p99_ms P99 latency milliseconds\n")
		fmt.Fprintf(w, "# TYPE cassperf_latency_p99_ms gauge\n")
		fmt.Fprintf(w, "# HELP cassperf_latency_p999_ms P999 latency milliseconds\n")
		fmt.Fprintf(w, "# TYPE cassperf_latency_p999_ms gauge\n")

		for op, ls := range snap.OpBreakdown {
			fmt.Fprintf(w, "cassperf_latency_p50_ms{workload=%q,op=%q,phase=%q} %g\n", wl, op, ph, ls.P50ms)
			fmt.Fprintf(w, "cassperf_latency_p95_ms{workload=%q,op=%q,phase=%q} %g\n", wl, op, ph, ls.P95ms)
			fmt.Fprintf(w, "cassperf_latency_p99_ms{workload=%q,op=%q,phase=%q} %g\n", wl, op, ph, ls.P99ms)
			fmt.Fprintf(w, "cassperf_latency_p999_ms{workload=%q,op=%q,phase=%q} %g\n", wl, op, ph, ls.P999ms)
		}

		// Fallback aggregated latency when no op breakdown is available.
		if len(snap.OpBreakdown) == 0 {
			fmt.Fprintf(w, "cassperf_latency_p50_ms{workload=%q,op=\"all\",phase=%q} %g\n", wl, ph, snap.P50ms)
			fmt.Fprintf(w, "cassperf_latency_p95_ms{workload=%q,op=\"all\",phase=%q} %g\n", wl, ph, snap.P95ms)
			fmt.Fprintf(w, "cassperf_latency_p99_ms{workload=%q,op=\"all\",phase=%q} %g\n", wl, ph, snap.P99ms)
			fmt.Fprintf(w, "cassperf_latency_p999_ms{workload=%q,op=\"all\",phase=%q} %g\n", wl, ph, snap.P999ms)
		}

		// 1 if a run is active (phase non-empty), 0 otherwise.
		active := 0
		if ph != "" {
			active = 1
		}
		fmt.Fprintf(w, "# HELP cassperf_run_active 1 if a benchmark run is currently active, 0 otherwise\n")
		fmt.Fprintf(w, "# TYPE cassperf_run_active gauge\n")
		fmt.Fprintf(w, "cassperf_run_active %d\n", active)
	}
}
