//go:build dev

// Package cassandragoperftool provides stubs in dev mode.
package cassandragoperftool

import "embed"

// Web is empty in dev mode; os.DirFS("web") is used instead.
var Web embed.FS

// Workloads is empty in dev mode; os.DirFS("workloads") is used instead.
var Workloads embed.FS
