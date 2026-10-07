package builtin

import "github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"

func init() {
	workload.DefaultRegistry().Register(keyvalueWorkload())
	workload.DefaultRegistry().Register(keyvalueBatchWorkload())
	workload.DefaultRegistry().Register(iotWorkload())
}
