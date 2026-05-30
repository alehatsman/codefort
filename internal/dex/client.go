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

// Client talks to a dex serve daemon. The zero value is not usable; use New.
// A nil *Client is valid and means "dex not configured" — callers should
// treat that as the Intel feature being disabled.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
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
		http:    &http.Client{Timeout: 15 * time.Second},
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
// richer fields dex's /ask returns — next_action, suggested_reads,
// annotations — so the UI can render the same kind of summary the
// `dex ask` CLI prints. Those fields are nil for non-ask kinds.
type SearchResult struct {
	Status         string                `json:"status"`
	Hint           string                `json:"hint,omitempty"`
	Hits           []Hit                 `json:"hits"`
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

// Ask sends a free-form question to dex's /ask endpoint. We flatten its
// semantic_hits into the common Hit shape so the Intel tab can reuse the
// existing renderer. The graph + suggested_reads sections of the response
// are richer but need a different UI; they're discarded for now.
func (c *Client) Ask(ctx context.Context, projectID, question string, k int) (*SearchResult, error) {
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

// Overview pulls the repo + package summary chunks dex generated at index
// time. dex has no first-class "enumerate by kind" endpoint, so we use
// two narrowly-targeted semantic queries (one each for repo vs package
// summaries) — empirically more reliable than a single broad sweep, since
// dex's ranking otherwise pushes the lone repo_summary chunk off the top
// when most matches come from package_summary content.
// Best-effort: very large repos may miss some packages if they fall
// outside the top k for the package query.
func (c *Client) Overview(ctx context.Context, projectID string) (*Overview, error) {
	out := &Overview{Packages: []PackageSummary{}}

	// Repo-level summary — typically a single chunk; k small.
	if res, err := c.Search(ctx, projectID, "repository overview purpose", 20); err == nil {
		for _, h := range res.Hits {
			if h.Kind == "repo_summary" && h.Content != "" {
				out.RepoSummary = h.Content
				break
			}
		}
	} else {
		return nil, err
	}

	// Package summaries — one per package, can be many. The package_summary
	// chunks all open with phrasing like "This package..." / "implements"
	// / "provides", so a prose-y query targeting that diction recalls
	// far more package_summary hits than a generic "summary" query
	// (which dex's reranker pushes off the top in favor of code chunks).
	// We union two complementary queries to maximize recall on small
	// index spends; cost is one extra dex round trip.
	queries := []string{
		"package",
		"this package contains implements provides functions",
	}
	seen := map[string]bool{}
	for _, q := range queries {
		res, err := c.Search(ctx, projectID, q, 1000)
		if err != nil {
			return nil, err
		}
		for _, h := range res.Hits {
			if h.Kind != "package_summary" || h.Content == "" || seen[h.Path] || isFixturePath(h.Path) {
				continue
			}
			seen[h.Path] = true
			out.Packages = append(out.Packages, PackageSummary{
				Path: h.Path, Summary: h.Content,
			})
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

// FileSummary returns the file_summary chunk dex composed for a single file
// path, or "" when dex has no summary for it (the common case — only some
// files get summarized). Like Overview, dex exposes no enumerate-by-kind
// endpoint, so we run one targeted search keyed on the path — its tokens
// dominate the lexical half of dex's hybrid ranking, so the file's own
// chunks rank top — and filter to the exact Path + file_summary kind.
func (c *Client) FileSummary(ctx context.Context, projectID, path string) (string, error) {
	res, err := c.Search(ctx, projectID, path+" file summary overview purpose", 30)
	if err != nil {
		return "", err
	}
	for _, h := range res.Hits {
		if h.Kind == "file_summary" && h.Path == path && h.Content != "" {
			return h.Content, nil
		}
	}
	return "", nil
}

// Callees returns call-graph successors of name (functions it invokes).
func (c *Client) Callees(ctx context.Context, projectID, name string, k int) (*SearchResult, error) {
	return c.callEdge(ctx, projectID, "callees", name, k)
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
// tab surfaces are unmarshaled; graph + suggested_reads are dropped on the
// floor for now (different UX, separate slice).
type askEnvelope struct {
	Status         string                `json:"status"`
	Hint           string                `json:"hint,omitempty"`
	Intent         string                `json:"intent,omitempty"`
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
