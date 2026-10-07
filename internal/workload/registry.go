package workload

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// registry is the package-level workload registry singleton.
var registry = &Registry{}

// DefaultRegistry returns the package-level singleton registry.
func DefaultRegistry() *Registry { return registry }

// Registry is the unified catalogue of all workloads — YAML (built-in + custom)
// and compiled-in Go workloads. It is safe for concurrent use.
type Registry struct {
	mu          sync.RWMutex
	yamlCache   []WorkloadDef
	goWorkloads []WorkloadDef
	loaded      bool

	// Configuration paths — set by Init before first use.
	workloadsDir       string
	customWorkloadsDir string
	workloadsFS        fs.FS // optional embedded FS for built-in YAMLs (set in Sub-Task 15)
}

// Init configures the registry with directory paths. Must be called before List or Reload.
func (r *Registry) Init(workloadsDir, customWorkloadsDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workloadsDir = workloadsDir
	r.customWorkloadsDir = customWorkloadsDir
	r.loaded = false
	r.yamlCache = nil
}

// SetEmbeddedFS sets the embedded filesystem used for built-in YAML scanning.
// When set, built-in YAMLs are loaded from the FS rather than from disk.
// This is called in Sub-Task 15 after the go:embed directive is added.
func (r *Registry) SetEmbeddedFS(fsys fs.FS) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workloadsFS = fsys
	r.loaded = false
	r.yamlCache = nil
}

// Register adds a compiled-in Go workload to the registry.
// The source field is always forced to "builtin-go".
// Go workloads survive Reload() — they are never evicted.
func (r *Registry) Register(def WorkloadDef) {
	def.Source = "builtin-go"
	r.mu.Lock()
	defer r.mu.Unlock()
	r.goWorkloads = append(r.goWorkloads, def)
}

// List returns the merged workload catalogue: YAML built-in + YAML custom + Go built-ins.
// The first call scans the filesystem; subsequent calls return the cache.
// The cache is invalidated only by Reload().
func (r *Registry) List() []WorkloadDef {
	r.mu.RLock()
	if r.loaded {
		all := make([]WorkloadDef, len(r.yamlCache)+len(r.goWorkloads))
		copy(all, r.yamlCache)
		copy(all[len(r.yamlCache):], r.goWorkloads)
		r.mu.RUnlock()
		return all
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-checked locking.
	if r.loaded {
		all := make([]WorkloadDef, len(r.yamlCache)+len(r.goWorkloads))
		copy(all, r.yamlCache)
		copy(all[len(r.yamlCache):], r.goWorkloads)
		return all
	}

	var cache []WorkloadDef
	if r.workloadsFS != nil {
		cache = append(cache, r.scanFS(r.workloadsFS, "yaml-builtin")...)
	} else if r.workloadsDir != "" {
		cache = append(cache, r.scan(r.workloadsDir, "yaml-builtin")...)
	}
	if r.customWorkloadsDir != "" {
		cache = append(cache, r.scan(r.customWorkloadsDir, "yaml-custom")...)
	}
	r.yamlCache = cache
	r.loaded = true

	all := make([]WorkloadDef, len(r.yamlCache)+len(r.goWorkloads))
	copy(all, r.yamlCache)
	copy(all[len(r.yamlCache):], r.goWorkloads)
	return all
}

// Reload clears the YAML cache so the next List() re-scans disk.
// Go workloads are unaffected.
func (r *Registry) Reload() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = false
	r.yamlCache = nil
}

// GetByID returns the workload with the given ID, or (zero, false) if not found.
func (r *Registry) GetByID(id string) (WorkloadDef, bool) {
	for _, def := range r.List() {
		if def.ID == id {
			return def, true
		}
	}
	return WorkloadDef{}, false
}

// scan reads a directory and loads all .yaml / .yml files as WorkloadDef values.
// Files that fail to load are skipped with a warning log.
func (r *Registry) scan(dir, source string) []WorkloadDef {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("workload registry: scan directory error", "dir", dir, "err", err)
		}
		return nil
	}

	var defs []WorkloadDef
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		def, err := LoadFile(path, source)
		if err != nil {
			slog.Warn("workload registry: skip file", "path", path, "err", err)
			continue
		}
		defs = append(defs, def)
	}
	return defs
}

// scanFS reads .yaml / .yml files from an fs.FS (e.g. an embedded filesystem)
// and loads them as WorkloadDef values with the given source label.
func (r *Registry) scanFS(fsys fs.FS, source string) []WorkloadDef {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		slog.Warn("workload registry: scanFS error", "err", err)
		return nil
	}
	var defs []WorkloadDef
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			slog.Warn("workload registry: scanFS read error", "file", name, "err", err)
			continue
		}
		def, err := LoadBytes(data, name, source)
		if err != nil {
			slog.Warn("workload registry: scanFS skip file", "file", name, "err", err)
			continue
		}
		defs = append(defs, def)
	}
	return defs
}
