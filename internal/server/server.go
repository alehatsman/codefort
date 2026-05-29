package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/dex"
)

type Server struct {
	cfg *config.Config
	// db is the single-writer pool (all mutations). rdb is the read-only
	// pool — concurrent reads that don't queue behind the writer. Handlers
	// pick explicitly: writes use db, pure reads use rdb.
	db     *sql.DB
	rdb    *sql.DB
	logger *slog.Logger
	dex    *dex.Client // nil when MOONGIT_DEX_URL is unset (Intel disabled)
}

func New(cfg *config.Config, db, rdb *sql.DB, logger *slog.Logger) *Server {
	return &Server{
		cfg:    cfg,
		db:     db,
		rdb:    rdb,
		logger: logger,
		dex:    dex.New(cfg.DexURL, cfg.DexToken),
	}
}

// Handler composes the request graph. Routing is split across two muxes
// because git smart-HTTP uses bare-wildcard patterns (/{owner}/{repo}/...)
// that Go's ServeMux refuses to coexist with /api/ on the same mux. A
// top-level prefix dispatcher keeps them apart, and also cleanly scopes
// auth to the API mux.
func (s *Server) Handler() http.Handler {
	apiAuth := s.withAuth(s.apiHandler())
	// Basic auth (when configured) gates the human/git-facing surfaces;
	// /api keeps its Bearer-token auth and /healthz stays open.
	gitMux := s.withBasicAuth(s.gitHandler())
	web := s.webHandler() // nil when MOONGIT_WEB_DIR is unset
	if web != nil {
		web = s.withBasicAuth(web)
	}

	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			s.handleHealth(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			apiAuth.ServeHTTP(w, r)
		case isGitRequest(r):
			// Git smart-HTTP, no auth in this slice.
			gitMux.ServeHTTP(w, r)
		case web != nil:
			// Anything left is a browser route — serve the SPA.
			web.ServeHTTP(w, r)
		default:
			// Web disabled: keep the prior API+git-only behavior
			// (gitMux 404s unmatched paths).
			gitMux.ServeHTTP(w, r)
		}
	})

	return s.withLogging(root)
}

func (s *Server) apiHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/whoami", s.handleWhoami)

	mux.HandleFunc("GET /api/repos", s.handleListRepos)
	mux.HandleFunc("POST /api/repos", s.handleCreateRepo)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}", s.handleGetRepo)

	mux.HandleFunc("GET /api/repos/{owner}/{repo}/tree", s.handleTree)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/blob", s.handleBlob)

	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues", s.handleCreateIssue)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues", s.handleListIssues)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues/{number}", s.handleGetIssue)
	mux.HandleFunc("PATCH /api/repos/{owner}/{repo}/issues/{number}", s.handleUpdateIssue)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/claim", s.handleClaimIssue)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/unclaim", s.handleUnclaimIssue)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues/{number}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/comments", s.handleCreateComment)
	mux.HandleFunc("DELETE /api/repos/{owner}/{repo}/issues/{number}/comments/{comment_id}", s.handleDeleteComment)

	mux.HandleFunc("GET /api/repos/{owner}/{repo}/intel", s.handleIntel)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/intel/overview", s.handleIntelOverview)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/intel/search", s.handleIntelSearch)

	return mux
}

func (s *Server) gitHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{owner}/{repo}/info/refs", s.handleInfoRefs)
	mux.HandleFunc("POST /{owner}/{repo}/git-upload-pack", s.handleServiceRPC("git-upload-pack"))
	mux.HandleFunc("POST /{owner}/{repo}/git-receive-pack", s.handleServiceRPC("git-receive-pack"))
	return mux
}

// handleHealth probes the writer pool, not just process liveness. The
// single-writer connection is what wedges first under load, and a bare 200
// would read false-green to the supervisor while the control plane is stuck.
// A short timeout bounds how long a hung writer can hold the check open.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		s.logger.Error("healthz db check failed", "err", err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.Error(w, "db unavailable\n", http.StatusServiceUnavailable)
		return
	}

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
