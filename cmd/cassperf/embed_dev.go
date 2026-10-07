//go:build dev

package main

import (
	"io/fs"
	"os"
)

// webFS returns os.DirFS("web") in dev mode so local changes to index.html
// are reflected without rebuilding.
func webFS() fs.FS {
	return os.DirFS("web")
}

// workloadsFS returns os.DirFS("workloads") in dev mode.
func workloadsFS() fs.FS {
	return os.DirFS("workloads")
}
