package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleListHistory returns the last 50 runs with params stripped.
func (s *Server) handleListHistory(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Strip params from the wire response (may contain redacted tokens etc).
	for i := range runs {
		runs[i].Params = nil
	}
	writeJSON(w, http.StatusOK, runs)
}

// handleGetLog serves the run.log text for a completed or active run.
func (s *Server) handleGetLog(w http.ResponseWriter, r *http.Request) {
	runID := runIDParam(r)
	if runID == "" {
		writeError(w, http.StatusBadRequest, "invalid run_id")
		return
	}
	content, err := s.store.GetLog(runID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="run.log"`)
	w.Write([]byte(content)) //nolint:errcheck
}

// handleGetReport returns the JSON MetricSnapshot for a completed run.
func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	runID := runIDParam(r)
	if runID == "" {
		writeError(w, http.StatusBadRequest, "invalid run_id")
		return
	}
	snap, err := s.store.GetReport(runID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// runIDParam extracts and validates the {runID} chi path param.
func runIDParam(r *http.Request) string {
	id := chi.URLParam(r, "runID")
	if !safeLongIDRe.MatchString(id) {
		return ""
	}
	return id
}
