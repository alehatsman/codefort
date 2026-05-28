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
// search/symbol responses for the fields we care about.
type SearchResult struct {
	Status string `json:"status"`
	Hint   string `json:"hint,omitempty"`
	Hits   []Hit  `json:"hits"`
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
	Status       string   `json:"status"`
	Hint         string   `json:"hint,omitempty"`
	Intent       string   `json:"intent,omitempty"`
	SemanticHits []askHit `json:"semantic_hits"`
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
	out := &SearchResult{Status: a.Status, Hits: make([]Hit, 0, len(a.SemanticHits))}
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
