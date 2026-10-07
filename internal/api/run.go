package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/job"
)

// runRequest is the JSON body for POST /api/v1/run.
type runRequest struct {
	WorkloadID       string            `json:"workload_id"`
	Phase            string            `json:"phase"`
	Driver           string            `json:"driver"`
	Token            string            `json:"token"`
	SCBID            string            `json:"scb_id"`
	Hosts            []string          `json:"hosts"`
	CassandraPort    int               `json:"cassandra_port"`
	CassandraUser    string            `json:"cassandra_user"`
	CassandraPass    string            `json:"cassandra_password"`
	Keyspace         string            `json:"keyspace"`
	ConsistencyLevel string            `json:"consistency_level"`
	Threads          int               `json:"threads"`
	TargetRate       int               `json:"target_rate"`
	Cycles           int64             `json:"cycles"`
	DurationSeconds  int               `json:"duration_seconds"`
	SkipSchema       bool              `json:"skip_schema"`
	ExtraParams      map[string]string `json:"extra_params"`
}

// handleStartRun starts a new benchmark run.
func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.WorkloadID == "" {
		writeError(w, http.StatusBadRequest, "workload_id is required")
		return
	}
	if req.Phase == "" {
		req.Phase = "all"
	}

	// Resolve SCB path from scb_id.
	var scbPath string
	if req.SCBID != "" {
		path, err := resolveSCBPath(s.cfg.SCBDir, req.SCBID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "scb_id not found: "+err.Error())
			return
		}
		scbPath = path
	}

	params := job.RunParams{
		WorkloadID:       req.WorkloadID,
		Phase:            req.Phase,
		Driver:           req.Driver,
		Token:            req.Token,
		SCBPath:          scbPath,
		Hosts:            req.Hosts,
		CassandraPort:    req.CassandraPort,
		CassandraUser:    req.CassandraUser,
		CassandraPass:    req.CassandraPass,
		Keyspace:         req.Keyspace,
		ConsistencyLevel: req.ConsistencyLevel,
		Threads:          req.Threads,
		TargetRate:       req.TargetRate,
		Cycles:           req.Cycles,
		Duration:         time.Duration(req.DurationSeconds) * time.Second,
		SkipSchema:       req.SkipSchema,
		ExtraParams:      req.ExtraParams,
	}

	runID, err := s.manager.Start(params)
	if err != nil {
		switch err {
		case job.ErrAlreadyRunning:
			writeError(w, http.StatusConflict, "a run is already active")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"run_id": runID,
		"status": "started",
	})
}

// handleStopRun cancels the active run.
func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	s.manager.Stop()
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleRunStatus returns the current job state.
func (s *Server) handleRunStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": string(s.manager.Status())})
}

// handleSSEStream subscribes the caller to the SSE broadcast channel.
// The current run log is replayed first so late joiners catch up.
func (s *Server) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	replayLog := ""
	// Best-effort: find the most recent running run and replay its log.
	runs, err := s.store.List()
	if err == nil && len(runs) > 0 && runs[0].Status == "running" {
		replayLog, _ = s.store.GetLog(runs[0].RunID)
	}
	s.broadcaster.Subscribe(w, r, replayLog)
}

// resolveSCBPath scans scbDir for a file whose name starts with id + "_".
func resolveSCBPath(scbDir, id string) (string, error) {
	entries, err := os.ReadDir(scbDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id+"_") {
			return filepath.Join(scbDir, e.Name()), nil
		}
	}
	return "", &scbNotFoundError{id: id}
}

type scbNotFoundError struct{ id string }

func (e *scbNotFoundError) Error() string { return "scb " + e.id + " not found" }
