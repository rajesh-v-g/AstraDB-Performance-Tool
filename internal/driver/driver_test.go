package driver_test

import (
	"testing"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/driver"
)

func TestParseConsistency_Defaults(t *testing.T) {
	// Verify the package compiles and SessionParams has expected fields.
	// The actual session creation requires a live cluster; tested in integration tests.
	_ = driver.SessionParams{
		Driver:           "cassandra",
		Hosts:            []string{"localhost"},
		Port:             9042,
		Keyspace:         "test",
		ConsistencyLevel: "LOCAL_QUORUM",
	}
}

func TestSessionParams_AstraFields(t *testing.T) {
	_ = driver.SessionParams{
		Driver:  "astra",
		Token:   "AstraCS:test",
		SCBPath: "/tmp/secure-connect-bundle.zip",
	}
}
