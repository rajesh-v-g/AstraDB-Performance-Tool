package workload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ValidateYAML runs validation on raw YAML text and returns a non-nil error
// if it is invalid. Used by the API handlers before persisting.
func (r *Registry) ValidateYAML(content string) error {
	result := Validate(content)
	if !result.Valid {
		return fmt.Errorf("%s", result.Error)
	}
	return nil
}

// SaveCustomWorkload writes content to a new YAML file in customWorkloadsDir,
// refreshes the cache, and returns the generated workload ID.
//
// The filename is built as "<millis>_<slugified-name>.yaml".
func (r *Registry) SaveCustomWorkload(name, content string) (string, error) {
	r.mu.RLock()
	dir := r.customWorkloadsDir
	r.mu.RUnlock()

	if dir == "" {
		return "", fmt.Errorf("registry: custom workloads directory not configured")
	}

	id := fmt.Sprintf("%d_%s", time.Now().UnixMilli(), slugify(name))
	filename := id + ".yaml"
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("registry: write custom workload: %w", err)
	}

	r.Reload()
	return id, nil
}

// UpdateCustomWorkload replaces the YAML content for an existing custom workload.
func (r *Registry) UpdateCustomWorkload(id, content string) error {
	r.mu.RLock()
	dir := r.customWorkloadsDir
	r.mu.RUnlock()

	// Find the file with matching ID prefix.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("registry: list custom workloads: %w", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) && !e.IsDir() {
			path := filepath.Join(dir, e.Name())
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return fmt.Errorf("registry: update custom workload: %w", err)
			}
			r.Reload()
			return nil
		}
	}
	return fmt.Errorf("registry: custom workload %q not found", id)
}

// DeleteCustomWorkload removes the custom workload file for id.
func (r *Registry) DeleteCustomWorkload(id string) error {
	r.mu.RLock()
	dir := r.customWorkloadsDir
	r.mu.RUnlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("registry: list custom workloads: %w", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) && !e.IsDir() {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return fmt.Errorf("registry: delete custom workload: %w", err)
			}
			r.Reload()
			return nil
		}
	}
	return fmt.Errorf("registry: custom workload %q not found", id)
}

// GetCustomYAML reads and returns the raw YAML text for a custom workload.
func (r *Registry) GetCustomYAML(id string) (string, error) {
	r.mu.RLock()
	dir := r.customWorkloadsDir
	r.mu.RUnlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("registry: list custom workloads: %w", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) && !e.IsDir() {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
	}
	return "", fmt.Errorf("registry: custom workload %q not found", id)
}

// slugify converts a display name to a safe filename-compatible slug.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	s := b.String()
	if s == "" {
		return "workload"
	}
	return s
}
