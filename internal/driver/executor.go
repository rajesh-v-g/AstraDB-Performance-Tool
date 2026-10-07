// Package driver provides the gocql session factory and the Executor interface
// that wraps the CQL driver for testability.
package driver

import "github.com/gocql/gocql"

// Executor is the interface used by the job worker to execute CQL statements.
// *gocql.Session satisfies this interface directly — no wrapper needed.
// Tests inject a mockExecutor without requiring a live Cassandra cluster.
type Executor interface {
	// Query creates a new Query using the session's configured consistency.
	Query(stmt string, values ...interface{}) *gocql.Query
	// Close terminates the underlying session or connection pool.
	Close()
}
