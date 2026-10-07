package builtin

import "github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"

// keyvalueWorkload returns the compiled-in key-value workload definition.
func keyvalueWorkload() workload.WorkloadDef {
	return workload.WorkloadDef{
		ID:          "go-keyvalue",
		Label:       "Key-Value (Go)",
		Source:      "builtin-go",
		Description: "A key-value workload using simple text keys and text values. Compiled-in Go definition.",
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
					{Name: "create_table", RawCQL: "CREATE TABLE IF NOT EXISTS {keyspace}.{table} (key text PRIMARY KEY, value text)"},
				},
			},
			{
				Name:     "rampup",
				Prepared: true,
				Ops: []workload.Op{
					{Name: "insert", RawCQL: "INSERT INTO {keyspace}.{table} (key, value) VALUES ({seq_key}, {seq_value})"},
				},
			},
			{
				Name:     "main-read",
				Ratio:    5,
				Prepared: true,
				Ops: []workload.Op{
					{Name: "select", RawCQL: "SELECT * FROM {keyspace}.{table} WHERE key={rw_key}"},
				},
			},
			{
				Name:     "main-write",
				Ratio:    5,
				Prepared: true,
				Ops: []workload.Op{
					{Name: "insert", RawCQL: "INSERT INTO {keyspace}.{table} (key, value) VALUES ({rw_key}, {rw_value})"},
				},
			},
			{
				Name:     "truncate",
				Prepared: false,
				Ops: []workload.Op{
					{Name: "truncate", RawCQL: "TRUNCATE TABLE {keyspace}.{table}"},
				},
			},
		},
	}
}
