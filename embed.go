//go:build !dev

// Package cassandragoperftool provides the compiled-in SPA assets and built-in
// workload files. This file must remain at the project root so that web/ and
// workloads/ are reachable by the //go:embed directives.
package cassandragoperftool

import "embed"

// Web contains the embedded SPA (web/ directory).
//
//go:embed all:web
var Web embed.FS

// Workloads contains the built-in YAML workload files.
//
//go:embed workloads
var Workloads embed.FS
