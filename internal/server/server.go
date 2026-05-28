package server

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/alehatsman/moongit/internal/config"
)

type Server struct {
	cfg    *config.Config
	db     *sql.DB
	logger *slog.Logger
}

func New(cfg *config.Config, db *sql.DB, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, db: db, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Issues API.
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues", s.handleCreateIssue)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues", s.handleListIssues)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues/{number}", s.handleGetIssue)
	mux.HandleFunc("PATCH /api/repos/{owner}/{repo}/issues/{number}", s.handleUpdateIssue)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/claim", s.handleClaimIssue)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/unclaim", s.handleUnclaimIssue)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues/{number}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/comments", s.handleCreateComment)

	// Git smart-HTTP. Pattern matches /{owner}/{repo}.git/{op...}.
	mux.HandleFunc("GET /{owner}/{repo}/info/refs", s.handleInfoRefs)
	mux.HandleFunc("POST /{owner}/{repo}/git-upload-pack", s.handleServiceRPC("git-upload-pack"))
	mux.HandleFunc("POST /{owner}/{repo}/git-receive-pack", s.handleServiceRPC("git-receive-pack"))

	return s.withLogging(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		s.logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"remote", r.RemoteAddr,
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
