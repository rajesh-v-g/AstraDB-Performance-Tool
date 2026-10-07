package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// handleListSCB lists uploaded SCB zip files.
func (s *Server) handleListSCB(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.cfg.SCBDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type scbEntry struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	var out []scbEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".zip") {
			continue
		}
		// ID is the numeric prefix before the first underscore.
		id := strings.SplitN(name, "_", 2)[0]
		out = append(out, scbEntry{ID: id, Filename: name})
	}
	if out == nil {
		out = []scbEntry{}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUploadSCB accepts a multipart form with a .zip file and stores it.
func (s *Server) handleUploadSCB(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "multipart parse: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field missing: "+err.Error())
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		writeError(w, http.StatusBadRequest, "only .zip files are accepted")
		return
	}

	// Generate a numeric ID from the current timestamp millisecond.
	id := fmt.Sprintf("%d", time.Now().UnixMilli())
	destName := id + "_" + filepath.Base(header.Filename)
	destPath := filepath.Join(s.cfg.SCBDir, destName)

	buf := make([]byte, 32<<20)
	var data []byte
	for {
		n, err2 := file.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err2 != nil {
			break
		}
	}

	if err := os.WriteFile(destPath, data, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "write: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "filename": destName})
}

// handleDeleteSCB removes an uploaded SCB file by ID.
func (s *Server) handleDeleteSCB(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	// Scan directory for a file prefixed with the given ID.
	entries, err := os.ReadDir(s.cfg.SCBDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id+"_") {
			if err := os.Remove(filepath.Join(s.cfg.SCBDir, e.Name())); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
	}
	writeError(w, http.StatusNotFound, "SCB not found")
}
