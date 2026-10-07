package builtin

import "github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"

// keyvalueBatchWorkload returns the compiled-in key-value batch workload definition.
func keyvalueBatchWorkload() workload.WorkloadDef {
	rampupCQL := "BEGIN UNLOGGED BATCH\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({seq_key0}, {seq_value0})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({seq_key1}, {seq_value1})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({seq_key2}, {seq_value2})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({seq_key3}, {seq_value3})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({seq_key4}, {seq_value4})\n" +
		"APPLY BATCH"

	mainWriteCQL := "BEGIN UNLOGGED BATCH\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({rw_key0}, {rw_value0})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({rw_key1}, {rw_value1})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({rw_key2}, {rw_value2})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({rw_key3}, {rw_value3})\n" +
		"  INSERT INTO test.keyvalue (key, value) VALUES ({rw_key4}, {rw_value4})\n" +
		"APPLY BATCH"

	return workload.WorkloadDef{
		ID:          "go-keyvalue-batch",
		Label:       "Key-Value Batch (Go)",
		Source:      "builtin-go",
		Description: "A key-value workload using batched writes for higher write throughput. Compiled-in Go definition.",
		Phases:      []string{"schema", "rampup", "main", "truncate"},
		Parameters: map[string]workload.Parameter{
			"keyspace":    {Default: "test", Type: "string", Label: "Keyspace"},
			"table":       {Default: "keyvalue", Type: "string", Label: "Table Name"},
			"valuesize":   {Default: "256", Type: "number", Label: "Value Size (bytes)"},
			"read_ratio":  {Default: "5", Type: "number", Label: "Read Ratio"},
			"write_ratio": {Default: "5", Type: "number", Label: "Write Ratio"},
		},
		Blocks: []workload.Block{
			{
				Name:     "schema_astra",
				Prepared: false,
				Ops: []workload.Op{
					{Name: "create_table", RawCQL: "CREATE TABLE IF NOT EXISTS test.keyvalue (key text PRIMARY KEY, value text)"},
				},
			},
			{
				Name:     "rampup",
				Prepared: true,
				Ops: []workload.Op{
					{Name: "batch_insert", RawCQL: rampupCQL},
				},
			},
			{
				Name:     "main-read",
				Ratio:    5,
				Prepared: true,
				Ops: []workload.Op{
					{Name: "select", RawCQL: "SELECT * FROM test.keyvalue WHERE key={rw_key}"},
				},
			},
			{
				Name:     "main-write",
				Ratio:    5,
				Prepared: true,
				Ops: []workload.Op{
					{Name: "batch_insert", RawCQL: mainWriteCQL},
				},
			},
			{
				Name:     "truncate",
				Prepared: false,
				Ops: []workload.Op{
					{Name: "truncate", RawCQL: "TRUNCATE TABLE test.keyvalue"},
				},
			},
		},
	}
}
