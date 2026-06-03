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

	// ciSecret gates the loopback /internal/ci/events endpoint and is
	// injected into the push hook's environment. ciURL is the loopback base
	// URL the hook POSTs to. An empty ciSecret disables CI notifications (the
	// endpoint rejects and the hook short-circuits on the empty env var).
	ciSecret string
	ciURL    string

	// agentCanceler is the in-process CI runner, wired in by main via
	// SetAgentCanceler. It backs the force-stop endpoint (#146): the runner
	// holds the in-memory turn handles needed to interrupt a live container,
	// which a DB-only signal can't do. nil in tests that don't exercise cancel
	// (and on the read pool path) — the handler then 503s.
	agentCanceler AgentCanceler
}

// AgentCanceler force-stops a running agent run by id, returning true if the
// run was non-terminal and is now canceled. Implemented by the CI runner; kept
// as an interface here so internal/server doesn't depend on package main.
type AgentCanceler interface {
	CancelAgentRun(runID int64) bool
}

// SetAgentCanceler wires the runner-backed force-stop. Called once at startup.
func (s *Server) SetAgentCanceler(c AgentCanceler) { s.agentCanceler = c }

func New(cfg *config.Config, db, rdb *sql.DB, logger *slog.Logger) *Server {
	secret := cfg.CISecret
	if secret == "" {
		gen, err := generateCISecret()
		if err != nil {
			logger.Error("ci: generate secret failed; CI notifications disabled", "err", err)
		}
		secret = gen
	}
	return &Server{
		cfg:      cfg,
		db:       db,
		rdb:      rdb,
		logger:   logger,
		dex:      dex.New(cfg.DexURL, cfg.DexToken),
		ciSecret: secret,
		ciURL:    loopbackURL(cfg.Addr),
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
		case r.URL.Path == "/internal/ci/events":
			// Loopback-only, CI-secret-gated; off the Bearer /api surface.
			s.handleCIEvents(w, r)
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

	mux.HandleFunc("GET /api/events", s.handleEvents)

	// Cross-repo aggregate feeds backing the top-level (non-repo) list views.
	mux.HandleFunc("GET /api/issues", s.handleListAllIssues)
	mux.HandleFunc("GET /api/pulls", s.handleListAllPulls)
	mux.HandleFunc("GET /api/runs", s.handleListAllRuns)

	mux.HandleFunc("GET /api/tokens", s.handleListTokens)
	mux.HandleFunc("POST /api/tokens", s.handleCreateToken)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.handleRevokeToken)

	mux.HandleFunc("GET /api/ssh-keys", s.handleListSSHKeys)
	mux.HandleFunc("POST /api/ssh-keys", s.handleCreateSSHKey)
	mux.HandleFunc("DELETE /api/ssh-keys/{id}", s.handleDeleteSSHKey)

	mux.HandleFunc("GET /api/repos", s.handleListRepos)
	mux.HandleFunc("POST /api/repos", s.handleCreateRepo)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}", s.handleGetRepo)
	mux.HandleFunc("DELETE /api/repos/{owner}/{repo}", s.handleDeleteRepo)
	mux.HandleFunc("PATCH /api/repos/{owner}/{repo}", s.handleUpdateRepo)

	mux.HandleFunc("GET /api/repos/{owner}/{repo}/refs", s.handleListRefs)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/tree", s.handleTree)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/blob", s.handleBlob)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/raw", s.handleRaw)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/commits", s.handleCommits)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/commit/{sha}", s.handleCommit)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/tree-commits", s.handleTreeCommits)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/compare", s.handleCompare)

	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues", s.handleCreateIssue)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues", s.handleListIssues)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues/{number}", s.handleGetIssue)
	mux.HandleFunc("PATCH /api/repos/{owner}/{repo}/issues/{number}", s.handleUpdateIssue)
	mux.HandleFunc("DELETE /api/repos/{owner}/{repo}/issues/{number}", s.handleDeleteIssue)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/claim", s.handleClaimIssue)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/unclaim", s.handleUnclaimIssue)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues/{number}/commits", s.handleIssueCommits)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/agent", s.handleSpawnAgent)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/issues/{number}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/issues/{number}/comments", s.handleCreateComment)
	mux.HandleFunc("DELETE /api/repos/{owner}/{repo}/issues/{number}/comments/{comment_id}", s.handleDeleteComment)

	mux.HandleFunc("POST /api/repos/{owner}/{repo}/pulls", s.handleCreatePull)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/pulls", s.handleListPulls)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/pulls/{number}", s.handleGetPull)
	mux.HandleFunc("PATCH /api/repos/{owner}/{repo}/pulls/{number}", s.handleUpdatePull)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/pulls/{number}/merge", s.handleMergePull)

	mux.HandleFunc("GET /api/repos/{owner}/{repo}/code-comments", s.handleListCodeComments)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/code-comments", s.handleCreateCodeComment)
	mux.HandleFunc("PATCH /api/repos/{owner}/{repo}/code-comments/{id}", s.handlePatchCodeComment)
	mux.HandleFunc("DELETE /api/repos/{owner}/{repo}/code-comments/{id}", s.handleDeleteCodeComment)

	mux.HandleFunc("GET /api/repos/{owner}/{repo}/runs", s.handleListCIRuns)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/runs", s.handleTriggerCIRun)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/runs/{number}", s.handleGetCIRun)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/runs/{number}/jobs/{job}/events", s.handleCIJobEvents)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/runs/{number}/rerun", s.handleRerunCIRun)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/runs/{number}/turns", s.handleCreateAgentTurn)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/runs/{number}/finish", s.handleFinishAgentRun)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/runs/{number}/cancel", s.handleCancelAgentRun)
	mux.HandleFunc("GET /api/settings/agent", s.handleGetAgentSettings)
	mux.HandleFunc("PUT /api/settings/agent", s.handleUpdateAgentSettings)

	mux.HandleFunc("GET /api/repos/{owner}/{repo}/intel", s.handleIntel)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/intel/overview", s.handleIntelOverview)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/intel/package-graph", s.handleIntelPackageGraph)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/intel/file-summary", s.handleIntelFileSummary)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/intel/summaries", s.handleIntelSummaries)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/intel/search", s.handleIntelSearch)

	mux.HandleFunc("GET /api/repos/{owner}/{repo}/specs", s.handleListSpecs)
	mux.HandleFunc("POST /api/repos/{owner}/{repo}/specs/search", s.handleSearchSpecs)
	mux.HandleFunc("GET /api/repos/{owner}/{repo}/specs/{path...}", s.handleGetSpec)

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

// Flush promotes the underlying writer's http.Flusher so SSE endpoints (e.g.
// the CI job-events stream) still detect streaming support through the
// logging wrapper. Embedding http.ResponseWriter does not surface Flush,
// since it isn't part of that interface.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
