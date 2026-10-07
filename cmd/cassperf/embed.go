//go:build !dev

package main

import (
	"io/fs"

	root "github.com/rajesh-v-g/cassandra-go-perf-tool"
)

// webFS returns the embedded web/ subdirectory as an fs.FS.
func webFS() fs.FS {
	sub, err := fs.Sub(root.Web, "web")
	if err != nil {
		panic("embed: failed to sub web/: " + err.Error())
	}
	return sub
}

// workloadsFS returns the embedded workloads/ subdirectory.
func workloadsFS() fs.FS {
	sub, err := fs.Sub(root.Workloads, "workloads")
	if err != nil {
		panic("embed: failed to sub workloads/: " + err.Error())
	}
	return sub
}
