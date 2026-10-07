package builtin

import "github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"

// iotWorkload returns the compiled-in IoT time-series workload definition.
func iotWorkload() workload.WorkloadDef {
	return workload.WorkloadDef{
		ID:          "go-iot",
		Label:       "IoT Time-Series (Go)",
		Source:      "builtin-go",
		Description: "An IoT time-series workload simulating sensor data ingestion and querying. Compiled-in Go definition.",
		Phases:      []string{"schema", "rampup", "main", "truncate"},
		Parameters: map[string]workload.Parameter{
			"keyspace":    {Default: "test", Type: "string", Label: "Keyspace"},
			"table":       {Default: "iot_sensor_data", Type: "string", Label: "Table Name"},
			"numdevices":  {Default: "1000", Type: "number", Label: "Number of Devices"},
			"read_ratio":  {Default: "5", Type: "number", Label: "Read Ratio"},
			"write_ratio": {Default: "5", Type: "number", Label: "Write Ratio"},
			"read_limit":  {Default: "100", Type: "number", Label: "Read Limit"},
		},
		Blocks: []workload.Block{
			{
				Name:     "schema_astra",
				Prepared: false,
				Ops: []workload.Op{
					{
						Name: "create_table",
						RawCQL: "CREATE TABLE IF NOT EXISTS test.iot_sensor_data (" +
							"device_id text, time timestamp, sensor_value double, " +
							"PRIMARY KEY (device_id, time)) WITH CLUSTERING ORDER BY (time DESC)",
					},
				},
			},
			{
				Name:     "rampup",
				Prepared: true,
				Ops: []workload.Op{
					{
						Name:   "insert",
						RawCQL: "INSERT INTO test.iot_sensor_data (device_id, time, sensor_value) VALUES ('device-{seq_device_id}', toTimestamp(now()), {seq_sensor_value})",
					},
				},
			},
			{
				Name:     "main-read",
				Ratio:    5,
				Prepared: true,
				Ops: []workload.Op{
					{
						Name:   "select_recent",
						RawCQL: "SELECT device_id, time, sensor_value FROM test.iot_sensor_data WHERE device_id='device-{rw_device_id}' LIMIT 100",
					},
				},
			},
			{
				Name:     "main-write",
				Ratio:    5,
				Prepared: true,
				Ops: []workload.Op{
					{
						Name:   "insert",
						RawCQL: "INSERT INTO test.iot_sensor_data (device_id, time, sensor_value) VALUES ('device-{rw_device_id}', toTimestamp(now()), {rw_sensor_value})",
					},
				},
			},
			{
				Name:     "truncate",
				Prepared: false,
				Ops: []workload.Op{
					{Name: "truncate", RawCQL: "TRUNCATE TABLE test.iot_sensor_data"},
				},
			},
		},
	}
}
