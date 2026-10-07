// Package api provides the HTTP handlers and chi router wiring.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/config"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/job"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/metrics"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/sse"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/store"
	"github.com/rajesh-v-g/cassandra-go-perf-tool/internal/workload"
)

// Server holds all the HTTP handler dependencies.
type Server struct {
	cfg         *config.Config
	registry    *workload.Registry
	store       *store.RunStore
	broadcaster *sse.Broadcaster
	manager     *job.Manager
	collector   *metrics.Collector
	router      chi.Router
	webFS       fs.FS
}

// NewServer wires all dependencies and returns a fully-configured Server.
// webFS is used for the embedded SPA (pass nil to use os.DirFS("web") in dev).
func NewServer(
	cfg *config.Config,
	registry *workload.Registry,
	rs *store.RunStore,
	bc *sse.Broadcaster,
	mgr *job.Manager,
	col *metrics.Collector,
	webFS fs.FS,
) *Server {
	s := &Server{
		cfg:         cfg,
		registry:    registry,
		store:       rs,
		broadcaster: bc,
		manager:     mgr,
		collector:   col,
		webFS:       webFS,
	}
	s.router = s.buildRouter()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	// Middleware.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(chiSlogLogger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Health check.
	r.Get("/health", s.handleHealth)

	// API v1 routes.
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/workloads", s.handleListWorkloads)

		// Custom workloads CRUD.
		r.Get("/custom-workloads", s.handleListCustomWorkloads)
		r.Post("/custom-workloads", s.handleCreateCustomWorkload)
		r.Get("/custom-workloads/{id}", s.handleGetCustomWorkload)
		r.Put("/custom-workloads/{id}", s.handleUpdateCustomWorkload)
		r.Delete("/custom-workloads/{id}", s.handleDeleteCustomWorkload)

		// SCB upload.
		r.Get("/upload/scb", s.handleListSCB)
		r.Post("/upload/scb", s.handleUploadSCB)
		r.Delete("/upload/scb/{id}", s.handleDeleteSCB)

		// Run control.
		r.Post("/run", s.handleStartRun)
		r.Post("/run/stop", s.handleStopRun)
		r.Get("/run/status", s.handleRunStatus)
		r.Get("/run/stream", s.handleSSEStream)

		// History.
		r.Get("/history", s.handleListHistory)
		r.Get("/history/{runID}/log", s.handleGetLog)
		r.Get("/history/{runID}/report", s.handleGetReport)
	})

	// SPA static file server — serves web/ directory.
	if s.webFS != nil {
		// Static assets (js, css, etc.) under /static/.
		// Strip the /static/ prefix and serve directly from the FS root.
		r.Get("/static/*", func(w http.ResponseWriter, r *http.Request) {
			// chi wildcard already includes the leading slash, e.g. "/js-yaml.min.js"
			p := strings.TrimPrefix(r.URL.Path, "/static/")
			serveFile(w, r, s.webFS, "static/"+p)
		})
		// Root → index.html (read directly, no redirect).
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			serveFile(w, r, s.webFS, "index.html")
		})
	}

	return r
}

// handleHealth responds with the application status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"version":    s.cfg.Version,
		"uptime_sec": time.Since(s.cfg.StartTime).Seconds(),
	})
}

// ---- CORS middleware --------------------------------------------------------

// allowedOrigins is the set of origins permitted for cross-origin requests.
// Restrict to localhost development ports; do not use "*" in production because
// any page could trigger benchmark runs against the server.
var allowedOrigins = map[string]bool{
	"http://localhost:3000": true,
	"http://localhost:3001": true,
	"http://127.0.0.1:3000": true,
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- slog request logger ----------------------------------------------------

func chiSlogLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start),
		)
	})
}

// ---- file serving -----------------------------------------------------------

// contentTypes maps file extensions to MIME types.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "application/javascript",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".ico":   "image/x-icon",
	".png":   "image/png",
	".woff2": "font/woff2",
}

// serveFile reads a file from fsys and writes it directly to w.
// No redirects are issued — content is always served as-is.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.Error(w, "not found: "+name, http.StatusNotFound)
		return
	}
	// Determine content type from extension.
	ct := "application/octet-stream"
	for ext, mime := range contentTypes {
		if strings.HasSuffix(name, ext) {
			ct = mime
			break
		}
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
