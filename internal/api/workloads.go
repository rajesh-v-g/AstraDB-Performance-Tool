package api

import (
	"net/http"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

// handleListWorkloads returns the full workload catalogue.
// The Blocks field is zeroed so internal execution details are not exposed.
func (s *Server) handleListWorkloads(w http.ResponseWriter, r *http.Request) {
	all := s.registry.List()
	// Strip blocks from the wire representation.
	for i := range all {
		all[i].Blocks = nil
	}
	writeJSON(w, http.StatusOK, all)
}

// handleListCustomWorkloads lists only yaml-custom workloads.
func (s *Server) handleListCustomWorkloads(w http.ResponseWriter, r *http.Request) {
	all := s.registry.List()
	var custom []workload.WorkloadDef
	for _, d := range all {
		if d.Source == "yaml-custom" {
			d.Blocks = nil
			custom = append(custom, d)
		}
	}
	if custom == nil {
		custom = []workload.WorkloadDef{}
	}
	writeJSON(w, http.StatusOK, custom)
}

// handleGetCustomWorkload returns a single custom workload's YAML content.
func (s *Server) handleGetCustomWorkload(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	content, err := s.registry.GetCustomYAML(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Write([]byte(content)) //nolint:errcheck
}

// handleCreateCustomWorkload validates and persists a new custom workload.
func (s *Server) handleCreateCustomWorkload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" || body.Content == "" {
		writeError(w, http.StatusBadRequest, "name and content are required")
		return
	}
	if err := s.registry.ValidateYAML(body.Content); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	id, err := s.registry.SaveCustomWorkload(body.Name, body.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleUpdateCustomWorkload replaces an existing custom workload's YAML.
func (s *Server) handleUpdateCustomWorkload(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.registry.ValidateYAML(body.Content); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": err.Error(),
		})
		return
	}
	if err := s.registry.UpdateCustomWorkload(id, body.Content); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

// handleDeleteCustomWorkload removes a custom workload by ID.
func (s *Server) handleDeleteCustomWorkload(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.registry.DeleteCustomWorkload(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// idParam extracts the {id} chi path param and validates it.
func idParam(r *http.Request) string {
	id := chiURLParam(r, "id")
	if !safeIDRe.MatchString(id) {
		return ""
	}
	return id
}
