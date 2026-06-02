// Package dex is a thin HTTP client for a dex `serve` daemon. moongit uses
// it to surface code-intelligence ("Intel") for a repo: index status from
// dex's /v1/status, plus semantic and symbol search. It speaks only the
// handful of /v1 endpoints the Intel tab needs and nothing more.
//
// dex identifies projects by the SHA256 of their real filesystem path and
// exposes that as an opaque {id}. moongit has no direct link from a bare
// repo to dex's working-tree checkout, so we resolve a repo to a dex
// project by matching the repo name against the basename of each indexed
// project root (see ResolveProject).
package dex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrProjectNotFound is returned by ResolveProject when no indexed dex
// project matches the repo name.
var ErrProjectNotFound = errors.New("dex: no indexed project matches this repo")

const (
	// defaultDexTimeout caps the cheap dex calls (status, search, symbol,
	// summaries) — they answer in well under a second when healthy, so a tight
	// bound surfaces a wedged daemon quickly.
	defaultDexTimeout = 20 * time.Second
	// askDexTimeout is the budget for /ask, which runs an LLM generation that
	// routinely takes tens of seconds under embedding/GPU load. The old shared
	// 15s client timeout was too tight and 502'd the Explore "Ask" feature
	// even when dex was healthy (#127).
	askDexTimeout = 90 * time.Second
)

// Client talks to a dex serve daemon. The zero value is not usable; use New.
// A nil *Client is valid and means "dex not configured" — callers should
// treat that as the Intel feature being disabled.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	// Per-op deadlines: cheap calls use timeout; Ask uses askTimeout. Kept as
	// fields (not the bare consts) so they're tunable and testable.
	timeout    time.Duration
	askTimeout time.Duration
}

// New returns a Client for baseURL (e.g. http://127.0.0.1:8080), or nil when
// baseURL is empty so callers can use a nil client as "disabled".
func New(baseURL, token string) *Client {
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		// No fixed Timeout here: it would cap every call uniformly (the bug in
		// #127, where /ask needs far longer than cheap calls). Per-call
		// deadlines are applied in do/Ask instead.
		http:       &http.Client{},
		timeout:    defaultDexTimeout,
		askTimeout: askDexTimeout,
	}
}

// Enabled reports whether dex integration is configured.
func (c *Client) Enabled() bool { return c != nil }

// ProjectStatus mirrors the per-project block of dex's /v1/status response.
type ProjectStatus struct {
	ID               string `json:"id"`
	Root             string `json:"root"`
	Chunks           int    `json:"chunks"`
	Files            int    `json:"files"`
	Dim              int    `json:"dim"`
	EmbedModel       string `json:"embed_model"`
	LastIndexed      string `json:"last_indexed"`
	PendingSummaries int    `json:"pending_summaries"`
}

// Status mirrors the subset of dex's /v1/status response the Intel tab uses.
type Status struct {
	Endpoint  string          `json:"endpoint"`
	Reachable bool            `json:"reachable"`
	Model     string          `json:"model"`
	Version   string          `json:"version"`
	Projects  []ProjectStatus `json:"projects"`
}

// Hit is one search result chunk, common to semantic and symbol search.
type Hit struct {
	Path      string  `json:"path"`
	Kind      string  `json:"kind"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float32 `json:"score"`
	Role      string  `json:"role,omitempty"`
	Content   string  `json:"content,omitempty"`
}

// SearchResult is the shared shape of dex's search/semantic and
// search/symbol responses. For the `ask` kind it also carries the
// richer fields dex's /ask returns — the synthesized prose answer,
// next_action, suggested_reads, annotations — so the UI can render
// the same summary the `dex ask` CLI prints. Those fields are nil
// for non-ask kinds.
type SearchResult struct {
	Status string `json:"status"`
	Hint   string `json:"hint,omitempty"`
	Hits   []Hit  `json:"hits"`
	// Answer is dex's synthesized, citation-bearing prose response to an
	// /ask question — the headline of the new /ask shape. AnswerModel names
	// the chat model that produced it (rendered as attribution). Both are
	// empty when dex's chat leg is unreachable (the response degrades to the
	// evidence bundle below) and for non-ask kinds.
	Answer         string                `json:"answer,omitempty"`
	AnswerModel    string                `json:"answer_model,omitempty"`
	NextAction     string                `json:"next_action,omitempty"`
	Avoid          string                `json:"avoid,omitempty"`
	SuggestedReads []SuggestedRead       `json:"suggested_reads,omitempty"`
	Annotations    map[string]Annotation `json:"annotations,omitempty"`
	Graph          *Graph                `json:"graph,omitempty"`
}

// Graph is the call-graph context dex returns alongside an /ask response.
// Nodes are the relevant symbols (functions, packages, structs); edges are
// the typed relationships between them (calls, declares, etc.).
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID            string `json:"id"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Kind          string `json:"kind,omitempty"`
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind,omitempty"`
}

// SuggestedRead is a curated file:start_line-end_line range dex picked
// out for the question, with a one-liner reason and an optional content
// snippet. These are the most actionable items in an /ask response.
type SuggestedRead struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Reason    string `json:"reason,omitempty"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Annotation collects per-file extras dex pre-resolved for the answer:
// the nearest README-style doc, corresponding _test.go files, and the
// Go package name. Field names match dex's wire shape.
type Annotation struct {
	NearestDoc string   `json:"nearest_doc,omitempty"`
	Tests      []string `json:"tests,omitempty"`
	Package    string   `json:"package,omitempty"`
}

// Status fetches the daemon status, including all indexed projects.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var out Status
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResolveProject finds the indexed dex project whose root basename matches
// repoName (case-insensitive). Returns ErrProjectNotFound when none match.
func (c *Client) ResolveProject(ctx context.Context, repoName string) (ProjectStatus, error) {
	st, err := c.Status(ctx)
	if err != nil {
		return ProjectStatus{}, err
	}
	for _, p := range st.Projects {
		if strings.EqualFold(filepath.Base(p.Root), repoName) {
			return p, nil
		}
	}
	return ProjectStatus{}, ErrProjectNotFound
}

// Search runs a hybrid semantic search against the given dex project id.
func (c *Client) Search(ctx context.Context, projectID, query string, k int) (*SearchResult, error) {
	body := map[string]any{"query": query}
	if k > 0 {
		body["k"] = k
	}
	return c.search(ctx, "/v1/projects/"+projectID+"/search/semantic", body)
}

// FindSymbol looks up an exact identifier against the given dex project id.
func (c *Client) FindSymbol(ctx context.Context, projectID, name string, k int) (*SearchResult, error) {
	body := map[string]any{"name": name}
	if k > 0 {
		body["k"] = k
	}
	return c.search(ctx, "/v1/projects/"+projectID+"/search/symbol", body)
}

// Ask sends a free-form question to dex's /ask endpoint. dex's headline is the
// synthesized prose answer; we carry it through alongside the flattened
// semantic_hits (mapped into the common Hit shape so the Intel tab reuses the
// existing renderer) plus the graph + suggested_reads structure.
func (c *Client) Ask(ctx context.Context, projectID, question string, k int) (*SearchResult, error) {
	// /ask is an LLM generation — give it a generous budget instead of the
	// cheap-call default (#127).
	ctx, cancel := context.WithTimeout(ctx, c.askTimeout)
	defer cancel()

	body := map[string]any{"question": question}
	if k > 0 {
		body["k"] = k
	}
	var raw askEnvelope
	if err := c.do(ctx, http.MethodPost, "/v1/projects/"+projectID+"/ask", body, &raw); err != nil {
		return nil, err
	}
	return raw.toSearchResult(), nil
}

// Callers returns call-graph predecessors of name (functions that invoke it).
// Each hit is one call site, surfaced as a Hit pointing at the call-site
// file:line so the UI can render and link to it uniformly.
func (c *Client) Callers(ctx context.Context, projectID, name string, k int) (*SearchResult, error) {
	return c.callEdge(ctx, projectID, "callers", name, k)
}

// SummaryChunk is one enumerated summary from dex: the path it describes, its
// kind (file_summary | package_summary | repo_summary), and the prose. The
// repo summary carries path "." (dex's repo-root convention).
type SummaryChunk struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

type summariesResponse struct {
	Status    string         `json:"status"`
	Hint      string         `json:"hint"`
	Summaries []SummaryChunk `json:"summaries"`
}

// AllSummaries enumerates every summary chunk dex composed for the project in
// one direct call (GET /v1/projects/{id}/summaries) — no semantic search, so
// the result is the complete, stable set regardless of index size. This is the
// reliable source for the repo/package/file summaries behind the breadcrumb
// and file-tree hover tooltips and the Research tab's package list.
func (c *Client) AllSummaries(ctx context.Context, projectID string) ([]SummaryChunk, error) {
	var out summariesResponse
	if err := c.do(ctx, http.MethodGet, "/v1/projects/"+projectID+"/summaries", nil, &out); err != nil {
		return nil, err
	}
	return out.Summaries, nil
}

// Overview pulls the repo + package summary chunks dex generated at index
// time, via the enumerate endpoint so every package is included (an earlier
// semantic-search recall dropped packages the reranker buried under code
// chunks). Packages are sorted by path for stable rendering.
func (c *Client) Overview(ctx context.Context, projectID string) (*Overview, error) {
	out := &Overview{Packages: []PackageSummary{}}
	chunks, err := c.AllSummaries(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, ch := range chunks {
		switch ch.Kind {
		case "repo_summary":
			if ch.Content != "" {
				out.RepoSummary = ch.Content
			}
		case "package_summary":
			// "." is the repo-root package — represented by the repo summary
			// above, so it isn't a standalone package row.
			if ch.Content == "" || ch.Path == "." || isFixturePath(ch.Path) {
				continue
			}
			out.Packages = append(out.Packages, PackageSummary{Path: ch.Path, Summary: ch.Content})
		}
	}
	sort.Slice(out.Packages, func(i, j int) bool {
		return out.Packages[i].Path < out.Packages[j].Path
	})
	return out, nil
}

// Overview is the at-a-glance index dump: repo-level summary plus one
// summary per package dex was able to compose. Packages are sorted by
// path so the rendering order is stable across calls.
type Overview struct {
	RepoSummary string           `json:"repo_summary,omitempty"`
	Packages    []PackageSummary `json:"packages"`
}

type PackageSummary struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

// RepoSummary returns the repo_summary chunk dex composed for the project,
// or "" if none. dex exposes no enumerate-by-kind endpoint, so we recall it
// with a small kind-targeted search and filter to the repo_summary kind.
func (c *Client) RepoSummary(ctx context.Context, projectID string) (string, error) {
	res, err := c.Search(ctx, projectID, "repository overview purpose", 20)
	if err != nil {
		return "", err
	}
	for _, h := range res.Hits {
		if h.Kind == "repo_summary" && h.Content != "" {
			return h.Content, nil
		}
	}
	return "", nil
}

// PathSummary returns the summary chunk dex composed for an exact path of the
// given kind — "file_summary" for a file, "package_summary" for a directory —
// or "" when dex has none. dex exposes no enumerate-by-kind endpoint, so we
// run one targeted search keyed on the path: its tokens dominate the lexical
// half of dex's hybrid ranking, so the path's own chunks rank top regardless
// of repo size. We then filter to the exact Path + kind. This path-keyed
// recall is reliable where Overview()'s broad package enumeration is not —
// it never has to win a top-k slot against the whole index.
func (c *Client) PathSummary(ctx context.Context, projectID, path, kind string) (string, error) {
	noun := "file"
	if kind == "package_summary" {
		noun = "package"
	}
	res, err := c.Search(ctx, projectID, path+" "+noun+" summary overview purpose", 30)
	if err != nil {
		return "", err
	}
	for _, h := range res.Hits {
		if h.Kind == kind && h.Path == path && h.Content != "" {
			return h.Content, nil
		}
	}
	return "", nil
}

// FileSummary returns the file_summary chunk dex composed for a single file
// path, or "" when dex has no summary for it (the common case — only some
// files get summarized).
func (c *Client) FileSummary(ctx context.Context, projectID, path string) (string, error) {
	return c.PathSummary(ctx, projectID, path, "file_summary")
}

// Callees returns call-graph successors of name (functions it invokes).
func (c *Client) Callees(ctx context.Context, projectID, name string, k int) (*SearchResult, error) {
	return c.callEdge(ctx, projectID, "callees", name, k)
}

// PackageGraph is the whole internal package import DAG dex computed for a
// project: one node per internal package with import-graph centrality, and
// the internal-only import edges between them. Field names match dex's
// /graph/packages wire shape. Distinct from Graph above, which is the
// per-symbol call-graph context returned alongside an /ask answer.
type PackageGraph struct {
	Status string             `json:"status"`
	Hint   string             `json:"hint,omitempty"`
	Nodes  []PackageGraphNode `json:"nodes"`
	Edges  []PackageGraphEdge `json:"edges"`
}

// PackageGraphNode is one internal package. InDegree counts the distinct
// internal packages that import it (how load-bearing it is); OutDegree the
// distinct internal packages it imports; PageRank ranks it within the
// import DAG (foundation floats up). All three are derived by dex from the
// import edges — the call-graph centrality columns are zero on packages.
type PackageGraphNode struct {
	Package   string  `json:"package"`
	InDegree  int     `json:"in_degree"`
	OutDegree int     `json:"out_degree"`
	PageRank  float64 `json:"page_rank"`
}

// PackageGraphEdge is one internal import: FromPackage imports ToPackage.
type PackageGraphEdge struct {
	FromPackage string `json:"from_package"`
	ToPackage   string `json:"to_package"`
}

// PackageGraph fetches the internal package import DAG dex computed for the
// project (GET /v1/projects/{id}/graph/packages). It lets the Explore "Map
// of the codebase" rank and layer packages by real import structure instead
// of guessing from path names. A non-Go or un-graphed project comes back
// with Status "no-graph" and no nodes — the caller should fall back to its
// flat summary listing.
func (c *Client) PackageGraph(ctx context.Context, projectID string) (*PackageGraph, error) {
	var out PackageGraph
	if err := c.do(ctx, http.MethodGet, "/v1/projects/"+projectID+"/graph/packages", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) callEdge(ctx context.Context, projectID, edge, name string, k int) (*SearchResult, error) {
	body := map[string]any{"name": name}
	if k > 0 {
		body["k"] = k
	}
	var raw callEdgeEnvelope
	if err := c.do(ctx, http.MethodPost, "/v1/projects/"+projectID+"/graph/"+edge, body, &raw); err != nil {
		return nil, err
	}
	return raw.toSearchResult(), nil
}

func (c *Client) search(ctx context.Context, path string, body map[string]any) (*SearchResult, error) {
	var out SearchResult
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// do issues a request to dex, attaching the bearer token, and decodes a JSON
// response into out. Non-2xx responses become an error carrying dex's
// {"error":...} message when present.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	// Apply the cheap-call budget unless the caller already set a deadline
	// (Ask sets its own, longer one).
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("dex %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error != "" {
			return fmt.Errorf("dex %s %s: %s", method, path, e.Error)
		}
		return fmt.Errorf("dex %s %s: status %d", method, path, resp.StatusCode)
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// askEnvelope mirrors dex's /ask response shape. Only the fields the Intel
// tab surfaces are unmarshaled.
type askEnvelope struct {
	Status         string                `json:"status"`
	Hint           string                `json:"hint,omitempty"`
	Intent         string                `json:"intent,omitempty"`
	Answer         string                `json:"answer,omitempty"`
	AnswerModel    string                `json:"answer_model,omitempty"`
	SemanticHits   []askHit              `json:"semantic_hits"`
	NextAction     string                `json:"next_action,omitempty"`
	Avoid          string                `json:"avoid,omitempty"`
	SuggestedReads []SuggestedRead       `json:"suggested_reads,omitempty"`
	Annotations    map[string]Annotation `json:"annotations,omitempty"`
	Graph          *Graph                `json:"graph,omitempty"`
}

type askHit struct {
	Path      string  `json:"path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float32 `json:"score"`
	Kind      string  `json:"kind"`
	Reason    string  `json:"reason,omitempty"`
	Content   string  `json:"content,omitempty"`
}

func (a askEnvelope) toSearchResult() *SearchResult {
	out := &SearchResult{
		Status:         a.Status,
		Hits:           make([]Hit, 0, len(a.SemanticHits)),
		Answer:         a.Answer,
		AnswerModel:    a.AnswerModel,
		NextAction:     a.NextAction,
		Avoid:          a.Avoid,
		SuggestedReads: a.SuggestedReads,
		Annotations:    a.Annotations,
		Graph:          a.Graph,
	}
	if a.Hint != "" {
		out.Hint = a.Hint
	} else if a.Intent != "" {
		out.Hint = "intent: " + a.Intent
	}
	for _, h := range a.SemanticHits {
		role := a.Intent
		if h.Reason != "" && role != "" {
			role = a.Intent + " · " + h.Reason
		} else if h.Reason != "" {
			role = h.Reason
		}
		out.Hits = append(out.Hits, Hit{
			Path:      h.Path,
			Kind:      h.Kind,
			StartLine: h.StartLine,
			EndLine:   h.EndLine,
			Score:     h.Score,
			Role:      role,
			Content:   h.Content,
		})
	}
	return out
}

// callEdgeEnvelope mirrors dex's /graph/callers and /graph/callees
// responses. Targets are the symbol(s) the request resolved to; hits are
// the call-graph neighbors.
type callEdgeEnvelope struct {
	Status  string        `json:"status"`
	Hint    string        `json:"hint,omitempty"`
	Targets []callTarget  `json:"targets"`
	Hits    []callEdgeHit `json:"hits"`
}

type callTarget struct {
	QualifiedName string `json:"qualified_name"`
	Package       string `json:"package,omitempty"`
	Kind          string `json:"kind"`
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
}

type callEdgeHit struct {
	QualifiedName string `json:"qualified_name"`
	Package       string `json:"package,omitempty"`
	Kind          string `json:"kind"`
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	CallSitePath  string `json:"call_site_path"`
	CallSiteLine  int    `json:"call_site_line"`
	Content       string `json:"content,omitempty"`
}

func (e callEdgeEnvelope) toSearchResult() *SearchResult {
	out := &SearchResult{Status: e.Status, Hits: make([]Hit, 0, len(e.Hits))}
	switch {
	case e.Hint != "":
		out.Hint = e.Hint
	case len(e.Targets) > 0:
		names := make([]string, 0, len(e.Targets))
		for _, t := range e.Targets {
			n := t.QualifiedName
			if t.Package != "" {
				n = t.Package + "." + n
			}
			names = append(names, n)
		}
		out.Hint = "resolved: " + strings.Join(names, ", ")
	}
	for _, h := range e.Hits {
		// Surface the call site (where the call expression sits) as the
		// primary location — clicking through goes to the caller's code,
		// which is what someone exploring "who calls Foo?" wants.
		out.Hits = append(out.Hits, Hit{
			Path:      h.CallSitePath,
			Kind:      h.Kind,
			StartLine: h.CallSiteLine,
			EndLine:   h.CallSiteLine,
			Role:      h.QualifiedName,
			Content:   h.Content,
		})
	}
	return out
}

// isFixturePath reports whether p sits under a `testdata/` segment.
// Mirrors dex's own filter for LLM_GUIDE.md rendering — these test
// fixtures aren't part of the project's shipped surface, surfacing
// them as "packages" in the Intel overview just inflates the list.
func isFixturePath(p string) bool {
	if p == "testdata" || strings.HasPrefix(p, "testdata/") {
		return true
	}
	return strings.Contains(p, "/testdata/") || strings.HasSuffix(p, "/testdata")
}
