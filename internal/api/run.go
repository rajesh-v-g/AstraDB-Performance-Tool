package api

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/job"
)

// safeIdentRe validates CQL identifiers (keyspace / table names) that are
// substituted literally into CQL strings. Only alphanumerics and underscores
// are permitted — no quotes, semicolons, or spaces.
var safeIdentRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

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

	// Validate keyspace name to prevent CQL injection via literal substitution.
	if req.Keyspace != "" && !safeIdentRe.MatchString(req.Keyspace) {
		writeError(w, http.StatusBadRequest, "keyspace contains invalid characters (only a-z, A-Z, 0-9, _ allowed)")
		return
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
// Returns "stopped" when a run was cancelled, "idle" when there was nothing to stop.
func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	if s.manager.Status() == job.StatusIdle {
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
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
	// Ask the manager for the active run ID under its own lock, then read
	// the log for that specific ID. This avoids the TOCTOU of listing all
	// runs and checking the status field of the first entry.
	if runID := s.manager.ActiveRunID(); runID != "" {
		replayLog, _ = s.store.GetLog(runID)
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
