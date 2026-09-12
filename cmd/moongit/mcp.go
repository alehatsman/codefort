package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/alehatsman/moongit/internal/api"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// runMCP serves the moongit toolset over stdio as an MCP server — the
// `mgit mcp` entrypoint: a pure stdio<->REST
// proxy that carries no local state. Every tool is a thin wrapper over the same
// endpoints the CLI subcommands call, so an agent gets issue/review/pipeline
// primitives without a shell — the channel that works under claude's headless
// bypassPermissions where Bash does not (#157).
//
// Identity and target come from the same plumbing the rest of the CLI uses:
// the repo is resolved from the checkout's git remotes (or MOONGIT_SERVER), and
// requests authenticate with MOONGIT_TOKEN. The server is scoped to that one
// repo for its lifetime — owner/repo overrides are a later addition.
func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	profile := fs.String("profile", profileFull, "tool profile scoping which tools are registered: full | review")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp takes no arguments (got %v)", fs.Args())
	}
	if !validProfile(*profile) {
		return fmt.Errorf("mcp: invalid profile %q (want full or review)", *profile)
	}

	tgt, err := discoverTarget()
	if err != nil {
		return fmt.Errorf("mcp: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return (&mcpServer{target: tgt, profile: *profile}).run(ctx)
}

// Tool profiles (#184) scope which tools the shim registers. The mapping is
// owned here — mgit owns its own toolset — and the server only names the
// profile over the wire; these strings are that contract (mirror of
// storage.ToolProfile{Full,Review}).
const (
	profileFull   = "full"
	profileReview = "review"
)

// reviewProfileTools is the slice of tools the "review" profile exposes: read
// tools + review_* + issue_comment (so a review agent can survey, anchor
// findings, and report on its driving issue), but nothing that mutates issue
// state, spawns agents, or triggers pipelines.
var reviewProfileTools = map[string]bool{
	"issue_list":     true,
	"issue_show":     true,
	"issue_comment":  true,
	"review_list":    true,
	"review_create":  true,
	"review_resolve": true,
	"review_reopen":  true,
	"pipeline_list":  true,
	"pipeline_get":   true,
	"pr_list":        true,
	"pr_show":        true,
}

func validProfile(p string) bool { return p == profileFull || p == profileReview }

// mcpServer holds the resolved target and tool profile for the session.
// Handlers are methods so they can reach it (and the package-level
// httpDo/decodeError helpers).
type mcpServer struct {
	target  target
	profile string
}

// allows reports whether the session's profile exposes the named tool. "full"
// (the default) exposes everything; "review" is restricted to its slice.
func (m *mcpServer) allows(name string) bool {
	if m.profile == "" || m.profile == profileFull {
		return true
	}
	return reviewProfileTools[name]
}

// addTool registers a tool only when the session's profile permits it — the
// shim-side enforcement seam (#184). It mirrors sdk.AddTool's signature so the
// registration sites read unchanged apart from the receiver.
func addTool[In, Out any](m *mcpServer, srv *sdk.Server, t *sdk.Tool, h sdk.ToolHandlerFor[In, Out]) {
	if m.allows(t.Name) {
		sdk.AddTool(srv, t, h)
	}
}

// call issues an authenticated request against the session's target repo and,
// on the expected status, decodes the JSON body into out (when non-nil). Any
// other status — or a transport error — becomes a Go error carrying the
// server's message; tool handlers turn that into a structured error output.
// path is appended to the repo base and must start with "/".
func (m *mcpServer) call(method, path string, reqBody any, okStatus int, out any) error {
	var rdr io.Reader
	contentType := ""
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
		contentType = "application/json"
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s%s", m.target.server, m.target.owner, m.target.repo, path)
	resp, raw, err := httpDo(method, endpoint, rdr, contentType)
	if err != nil {
		return err
	}
	if resp.StatusCode != okStatus {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// run registers the toolset and blocks serving stdio until ctx is cancelled or
// the transport closes.
func (m *mcpServer) run(ctx context.Context) error {
	return m.newServer().Run(ctx, &sdk.StdioTransport{})
}

// newServer builds the MCP server and registers the toolset the session's
// profile permits (addTool gates each registration, #184; "full" exposes all).
// Split out from run so tests can drive it over an in-memory transport.
func (m *mcpServer) newServer() *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{
		Name:    "moongit",
		Version: mcpVersion,
	}, nil)

	// ── issues ──────────────────────────────────────────────────────────────
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_list",
		Description: "List issues (slim — no body). Filter: state, assignee (null=unassigned), label, query. Views: ready, blocked, epics.",
	}, m.issueList)
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_show",
		Description: "Show an issue with its comments.",
	}, m.issueShow)
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_create",
		Description: "Create an issue.",
	}, m.issueCreate)
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_comment",
		Description: "Post a comment on an issue.",
	}, m.issueComment)
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_claim",
		Description: "Claim an issue. Optional state transition. Fails if already claimed by another.",
	}, m.issueClaim)
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_unclaim",
		Description: "Release your claim on an issue.",
	}, m.issueUnclaim)
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_set_state",
		Description: "Set issue state: todo|in_progress|done|closed.",
	}, m.issueSetState)
	addTool(m, srv, &sdk.Tool{
		Name:        "issue_update",
		Description: "Partial update: title, body, labels, parent. parent=0 clears; empty labels array clears labels.",
	}, m.issueUpdate)

	// ── PRs ──────────────────────────────────────────────────────────────────
	addTool(m, srv, &sdk.Tool{
		Name:        "pr_list",
		Description: "List pull requests. Filter by state (open|merged|closed; default open).",
	}, m.prList)
	addTool(m, srv, &sdk.Tool{
		Name:        "pr_show",
		Description: "Show a PR with diff and review comments.",
	}, m.prShow)
	addTool(m, srv, &sdk.Tool{
		Name:        "pr_create",
		Description: "Open a PR from head into base.",
	}, m.prCreate)
	addTool(m, srv, &sdk.Tool{
		Name:        "pr_merge",
		Description: "Merge a PR. method: ff-only (preferred) or merge.",
	}, m.prMerge)

	// ── reviews (code comments anchored to a file's line range on a branch) ──
	addTool(m, srv, &sdk.Tool{
		Name:        "review_list",
		Description: "List code-review comments. Filter by ref, path, state (open|resolved|all).",
	}, m.reviewList)
	addTool(m, srv, &sdk.Tool{
		Name:        "review_create",
		Description: "Post a code-review comment on a file line-range (1-based, inclusive) on a branch.",
	}, m.reviewCreate)
	addTool(m, srv, &sdk.Tool{
		Name:        "review_resolve",
		Description: "Mark a code-review comment resolved, by its id.",
	}, m.reviewSetResolved(true))
	addTool(m, srv, &sdk.Tool{
		Name:        "review_reopen",
		Description: "Reopen a previously-resolved code-review comment, by its id.",
	}, m.reviewSetResolved(false))

	// ── pipelines (CI runs) ──────────────────────────────────────────────────
	addTool(m, srv, &sdk.Tool{
		Name:        "pipeline_trigger",
		Description: "Trigger a CI run for a ref (branch, tag, SHA).",
	}, m.pipelineTrigger)
	addTool(m, srv, &sdk.Tool{
		Name:        "pipeline_list",
		Description: "List runs. Filter by kind (ci|agent).",
	}, m.pipelineList)
	addTool(m, srv, &sdk.Tool{
		Name:        "pipeline_get",
		Description: "Get one run with jobs and (for agent runs) follow-up turns.",
	}, m.pipelineGet)

	// ── agents ───────────────────────────────────────────────────────────────
	addTool(m, srv, &sdk.Tool{
		Name:        "agent_spawn",
		Description: "Spawn an agent on an issue. Options: ref, model (claude-edit), tool_profile (full|review).",
	}, m.agentSpawn)
	addTool(m, srv, &sdk.Tool{
		Name:        "agent_turn",
		Description: "Queue a follow-up message on an agent run.",
	}, m.agentTurn)

	return srv
}

// mcpVersion is overridable at build time via -ldflags; "dev" by default.
var mcpVersion = "dev"

// ─── shared output shape ─────────────────────────────────────────────────────
//
// Every tool reports a status (statusOK | statusError) and, on failure, a
// human-readable error hint — handlers never return a Go error (which the SDK
// would surface as a protocol-level failure), so the model always gets a
// structured, actionable result.

const (
	statusOK    = "ok"
	statusError = "error"
)

// ─── issues ──────────────────────────────────────────────────────────────────

type issueListInput struct {
	State    string `json:"state,omitempty" jsonschema:"todo,in_progress,done,closed"`
	Assignee string `json:"assignee,omitempty" jsonschema:"null for unassigned"`
	Label    string `json:"label,omitempty" jsonschema:"exact label match"`
	Query    string `json:"query,omitempty" jsonschema:"keyword in title or body"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max results"`
	Ready    bool   `json:"ready,omitempty" jsonschema:"unclaimed todos with deps met"`
	Blocked  bool   `json:"blocked,omitempty" jsonschema:"todos with unmet deps"`
	Epics    bool   `json:"epics,omitempty" jsonschema:"issues with children"`
}

// issueSummary is the slim per-item view returned by issue_list — no body or
// timestamps, so a full survey fits in a fraction of the tokens a full Issue list would.
type issueSummary struct {
	Number       int               `json:"number"`
	Title        string            `json:"title"`
	State        api.IssueState    `json:"state"`
	Assignee     *string           `json:"assignee"`
	Labels       []string          `json:"labels"`
	ParentNumber *int              `json:"parent_number,omitempty"`
	Progress     *api.EpicProgress `json:"progress,omitempty"`
}

type issueListOutput struct {
	Status string         `json:"status"`
	Error  string         `json:"error,omitempty"`
	Issues []issueSummary `json:"issues,omitempty"`
}

func (m *mcpServer) issueList(_ context.Context, _ *sdk.CallToolRequest, in issueListInput) (*sdk.CallToolResult, issueListOutput, error) {
	q := url.Values{}
	if in.State != "" {
		q.Set("state", in.State)
	}
	if in.Assignee != "" {
		q.Set("assignee", in.Assignee)
	}
	if in.Label != "" {
		q.Set("label", in.Label)
	}
	if in.Query != "" {
		q.Set("q", in.Query)
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	q.Set("limit", strconv.Itoa(limit))
	if in.Ready {
		q.Set("ready", "true")
	}
	if in.Blocked {
		q.Set("blocked", "true")
	}
	if in.Epics {
		q.Set("epics", "true")
	}
	path := "/issues"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var issues []api.Issue
	if err := m.call(http.MethodGet, path, nil, http.StatusOK, &issues); err != nil {
		return nil, issueListOutput{Status: statusError, Error: err.Error()}, nil
	}
	summaries := make([]issueSummary, len(issues))
	for i, iss := range issues {
		labels := iss.Labels
		if labels == nil {
			labels = []string{}
		}
		summaries[i] = issueSummary{
			Number:       iss.Number,
			Title:        iss.Title,
			State:        iss.State,
			Assignee:     iss.Assignee,
			Labels:       labels,
			ParentNumber: iss.ParentNumber,
			Progress:     iss.Progress,
		}
	}
	return nil, issueListOutput{Status: statusOK, Issues: summaries}, nil
}

type issueShowInput struct {
	Number int `json:"number" jsonschema:"the issue number"`
}

// mcpIssue strips internal fields (ID, timestamps) that add noise without aiding reasoning.
type mcpIssue struct {
	Number       int                     `json:"number"`
	Title        string                  `json:"title"`
	Body         string                  `json:"body,omitempty"`
	Author       string                  `json:"author"`
	State        api.IssueState          `json:"state"`
	Assignee     *string                 `json:"assignee"`
	ParentNumber *int                    `json:"parent_number,omitempty"`
	Children     []api.ChildIssueSummary `json:"children,omitempty"`
	DependsOn    []api.IssueRef          `json:"depends_on,omitempty"`
	Blocks       []api.IssueRef          `json:"blocks,omitempty"`
	Progress     *api.EpicProgress       `json:"progress,omitempty"`
	Labels       []string                `json:"labels"`
}

func toMCPIssue(iss *api.Issue) *mcpIssue {
	if iss == nil {
		return nil
	}
	labels := iss.Labels
	if labels == nil {
		labels = []string{}
	}
	return &mcpIssue{
		Number:       iss.Number,
		Title:        iss.Title,
		Body:         iss.Body,
		Author:       iss.Author,
		State:        iss.State,
		Assignee:     iss.Assignee,
		ParentNumber: iss.ParentNumber,
		Children:     iss.Children,
		DependsOn:    iss.DependsOn,
		Blocks:       iss.Blocks,
		Progress:     iss.Progress,
		Labels:       labels,
	}
}

// mcpComment strips ID, IssueID, and CreatedAt.
type mcpComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

// mcpCIRun strips timestamps and internal fields (CommitAuthor).
type mcpCIRun struct {
	Number         int    `json:"number"`
	Kind           string `json:"kind"`
	IssueNumber    *int   `json:"issue_number,omitempty"`
	ExecutionModel string `json:"execution_model,omitempty"`
	ToolProfile    string `json:"tool_profile,omitempty"`
	CommitSHA      string `json:"commit_sha"`
	CommitMsg      string `json:"commit_msg,omitempty"`
	Ref            string `json:"ref"`
	Event          string `json:"event"`
	Trigger        string `json:"trigger,omitempty"`
	Status         string `json:"status"`
}

func toMCPCIRun(r *api.CIRun) mcpCIRun {
	return mcpCIRun{
		Number:         r.Number,
		Kind:           r.Kind,
		IssueNumber:    r.IssueNumber,
		ExecutionModel: r.ExecutionModel,
		ToolProfile:    r.ToolProfile,
		CommitSHA:      r.CommitSHA,
		CommitMsg:      r.CommitMsg,
		Ref:            r.Ref,
		Event:          r.Event,
		Trigger:        r.Trigger,
		Status:         r.Status,
	}
}

type mcpCIRunDetail struct {
	mcpCIRun
	Jobs  []api.CIJob     `json:"jobs"`
	Turns []api.AgentTurn `json:"turns,omitempty"`
}

type issueShowOutput struct {
	Status   string       `json:"status"`
	Error    string       `json:"error,omitempty"`
	Issue    *mcpIssue    `json:"issue,omitempty"`
	Comments []mcpComment `json:"comments,omitempty"`
}

func (m *mcpServer) issueShow(_ context.Context, _ *sdk.CallToolRequest, in issueShowInput) (*sdk.CallToolResult, issueShowOutput, error) {
	if in.Number <= 0 {
		return nil, issueShowOutput{Status: statusError, Error: "number must be a positive issue number"}, nil
	}
	var iss api.Issue
	if err := m.call(http.MethodGet, fmt.Sprintf("/issues/%d", in.Number), nil, http.StatusOK, &iss); err != nil {
		return nil, issueShowOutput{Status: statusError, Error: err.Error()}, nil
	}
	// Comments are best-effort: an issue with none, or a transient comments
	// fetch error, shouldn't fail the show.
	var rawComments []api.Comment
	_ = m.call(http.MethodGet, fmt.Sprintf("/issues/%d/comments", in.Number), nil, http.StatusOK, &rawComments)
	comments := make([]mcpComment, len(rawComments))
	for i, c := range rawComments {
		comments[i] = mcpComment{Author: c.Author, Body: c.Body}
	}
	return nil, issueShowOutput{Status: statusOK, Issue: toMCPIssue(&iss), Comments: comments}, nil
}

type issueCreateInput struct {
	Title  string   `json:"title" jsonschema:"issue title"`
	Body   string   `json:"body,omitempty" jsonschema:"markdown body"`
	Parent int      `json:"parent,omitempty" jsonschema:"parent issue number"`
	Labels []string `json:"labels,omitempty" jsonschema:"initial labels"`
}

type issueOutput struct {
	Status string    `json:"status"`
	Error  string    `json:"error,omitempty"`
	Issue  *mcpIssue `json:"issue,omitempty"`
}

func (m *mcpServer) issueCreate(_ context.Context, _ *sdk.CallToolRequest, in issueCreateInput) (*sdk.CallToolResult, issueOutput, error) {
	if in.Title == "" {
		return nil, issueOutput{Status: statusError, Error: "title is required"}, nil
	}
	var iss api.Issue
	req := api.CreateIssueRequest{Title: in.Title, Body: in.Body, Labels: in.Labels}
	if in.Parent > 0 {
		req.Parent = &in.Parent
	}
	if err := m.call(http.MethodPost, "/issues", req, http.StatusCreated, &iss); err != nil {
		return nil, issueOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, issueOutput{Status: statusOK, Issue: toMCPIssue(&iss)}, nil
}

type issueCommentInput struct {
	Number int    `json:"number" jsonschema:"the issue number"`
	Body   string `json:"body" jsonschema:"comment text"`
}

type commentOutput struct {
	Status  string      `json:"status"`
	Error   string      `json:"error,omitempty"`
	Comment *mcpComment `json:"comment,omitempty"`
}

func (m *mcpServer) issueComment(_ context.Context, _ *sdk.CallToolRequest, in issueCommentInput) (*sdk.CallToolResult, commentOutput, error) {
	if in.Number <= 0 {
		return nil, commentOutput{Status: statusError, Error: "number must be a positive issue number"}, nil
	}
	if in.Body == "" {
		return nil, commentOutput{Status: statusError, Error: "body is required"}, nil
	}
	var c api.Comment
	req := api.CreateCommentRequest{Body: in.Body}
	if err := m.call(http.MethodPost, fmt.Sprintf("/issues/%d/comments", in.Number), req, http.StatusCreated, &c); err != nil {
		return nil, commentOutput{Status: statusError, Error: err.Error()}, nil
	}
	mc := mcpComment{Author: c.Author, Body: c.Body}
	return nil, commentOutput{Status: statusOK, Comment: &mc}, nil
}

type issueClaimInput struct {
	Number int    `json:"number" jsonschema:"the issue number"`
	State  string `json:"state,omitempty" jsonschema:"e.g. in_progress"`
}

func (m *mcpServer) issueClaim(_ context.Context, _ *sdk.CallToolRequest, in issueClaimInput) (*sdk.CallToolResult, issueOutput, error) {
	if in.Number <= 0 {
		return nil, issueOutput{Status: statusError, Error: "number must be a positive issue number"}, nil
	}
	state := api.IssueState(in.State)
	if in.State != "" && !state.Valid() {
		return nil, issueOutput{Status: statusError, Error: fmt.Sprintf("invalid state %q (want one of %v)", in.State, api.AllIssueStates)}, nil
	}
	var iss api.Issue
	req := api.ClaimRequest{State: state}
	if err := m.call(http.MethodPost, fmt.Sprintf("/issues/%d/claim", in.Number), req, http.StatusOK, &iss); err != nil {
		return nil, issueOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, issueOutput{Status: statusOK, Issue: toMCPIssue(&iss)}, nil
}

type issueNumberInput struct {
	Number int `json:"number" jsonschema:"the issue number"`
}

type statusOutput struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (m *mcpServer) issueUnclaim(_ context.Context, _ *sdk.CallToolRequest, in issueNumberInput) (*sdk.CallToolResult, statusOutput, error) {
	if in.Number <= 0 {
		return nil, statusOutput{Status: statusError, Error: "number must be a positive issue number"}, nil
	}
	if err := m.call(http.MethodPost, fmt.Sprintf("/issues/%d/unclaim", in.Number), nil, http.StatusOK, nil); err != nil {
		return nil, statusOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, statusOutput{Status: statusOK}, nil
}

type issueSetStateInput struct {
	Number int    `json:"number" jsonschema:"the issue number"`
	State  string `json:"state" jsonschema:"todo|in_progress|done|closed"`
}

func (m *mcpServer) issueSetState(_ context.Context, _ *sdk.CallToolRequest, in issueSetStateInput) (*sdk.CallToolResult, issueOutput, error) {
	if in.Number <= 0 {
		return nil, issueOutput{Status: statusError, Error: "number must be a positive issue number"}, nil
	}
	state := api.IssueState(in.State)
	if !state.Valid() {
		return nil, issueOutput{Status: statusError, Error: fmt.Sprintf("invalid state %q (want one of %v)", in.State, api.AllIssueStates)}, nil
	}
	var iss api.Issue
	req := api.UpdateIssueRequest{State: &state}
	if err := m.call(http.MethodPatch, fmt.Sprintf("/issues/%d", in.Number), req, http.StatusOK, &iss); err != nil {
		return nil, issueOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, issueOutput{Status: statusOK, Issue: toMCPIssue(&iss)}, nil
}

type issueUpdateInput struct {
	Number int      `json:"number" jsonschema:"issue number"`
	Title  *string  `json:"title,omitempty" jsonschema:"new title"`
	Body   *string  `json:"body,omitempty" jsonschema:"new body (markdown)"`
	Labels []string `json:"labels,omitempty" jsonschema:"empty array clears; omit=no change"`
	Parent *int     `json:"parent,omitempty" jsonschema:"0 clears parent; omit=no change"`
}

func (m *mcpServer) issueUpdate(_ context.Context, _ *sdk.CallToolRequest, in issueUpdateInput) (*sdk.CallToolResult, issueOutput, error) {
	if in.Number <= 0 {
		return nil, issueOutput{Status: statusError, Error: "number must be a positive issue number"}, nil
	}
	req := api.UpdateIssueRequest{}
	if in.Title != nil {
		if *in.Title == "" {
			return nil, issueOutput{Status: statusError, Error: "title cannot be empty"}, nil
		}
		req.Title = in.Title
	}
	if in.Body != nil {
		req.Body = in.Body
	}
	if in.Labels != nil {
		req.Labels = &in.Labels
	}
	if in.Parent != nil {
		req.Parent = in.Parent
	}
	if req.Title == nil && req.Body == nil && req.Labels == nil && req.Parent == nil {
		return nil, issueOutput{Status: statusError, Error: "at least one field (title, body, labels, parent) must be provided"}, nil
	}
	var iss api.Issue
	if err := m.call(http.MethodPatch, fmt.Sprintf("/issues/%d", in.Number), req, http.StatusOK, &iss); err != nil {
		return nil, issueOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, issueOutput{Status: statusOK, Issue: toMCPIssue(&iss)}, nil
}

// ─── PRs ─────────────────────────────────────────────────────────────────────

type prListInput struct {
	State string `json:"state,omitempty" jsonschema:"open|merged|closed"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results"`
}

type prListOutput struct {
	Status string            `json:"status"`
	Error  string            `json:"error,omitempty"`
	PRs    []api.PullRequest `json:"prs,omitempty"`
}

func (m *mcpServer) prList(_ context.Context, _ *sdk.CallToolRequest, in prListInput) (*sdk.CallToolResult, prListOutput, error) {
	q := url.Values{}
	if in.State != "" {
		q.Set("state", in.State)
	}
	if in.Limit > 0 {
		q.Set("limit", strconv.Itoa(in.Limit))
	}
	path := "/pulls"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var prs []api.PullRequest
	if err := m.call(http.MethodGet, path, nil, http.StatusOK, &prs); err != nil {
		return nil, prListOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, prListOutput{Status: statusOK, PRs: prs}, nil
}

type prShowInput struct {
	Number       int  `json:"number" jsonschema:"the PR number"`
	NoDiff       bool `json:"no_diff,omitempty" jsonschema:"omit the file diff entirely"`
	MaxDiffLines int  `json:"max_diff_lines,omitempty" jsonschema:"cap total diff lines across all files (default 300)"`
}

type prDetailOutput struct {
	Status string                 `json:"status"`
	Error  string                 `json:"error,omitempty"`
	PR     *api.PullRequestDetail `json:"pr,omitempty"`
}

// truncateDiff drops whole files from c.Files once total hunk lines exceed maxLines.
func truncateDiff(c *api.Compare, maxLines int) {
	total := 0
	for i := range c.Files {
		fileLines := 0
		for j := range c.Files[i].Hunks {
			fileLines += len(c.Files[i].Hunks[j].Lines)
		}
		if total+fileLines > maxLines {
			c.Files = c.Files[:i]
			return
		}
		total += fileLines
	}
}

func (m *mcpServer) prShow(_ context.Context, _ *sdk.CallToolRequest, in prShowInput) (*sdk.CallToolResult, prDetailOutput, error) {
	if in.Number <= 0 {
		return nil, prDetailOutput{Status: statusError, Error: "number must be a positive PR number"}, nil
	}
	var pr api.PullRequestDetail
	if err := m.call(http.MethodGet, fmt.Sprintf("/pulls/%d", in.Number), nil, http.StatusOK, &pr); err != nil {
		return nil, prDetailOutput{Status: statusError, Error: err.Error()}, nil
	}
	if in.NoDiff {
		pr.Compare.Files = nil
	} else {
		maxLines := in.MaxDiffLines
		if maxLines == 0 {
			maxLines = 300
		}
		truncateDiff(&pr.Compare, maxLines)
	}
	return nil, prDetailOutput{Status: statusOK, PR: &pr}, nil
}

type prCreateInput struct {
	Title string `json:"title" jsonschema:"PR title"`
	Head  string `json:"head" jsonschema:"head branch"`
	Base  string `json:"base" jsonschema:"base branch"`
	Body  string `json:"body,omitempty" jsonschema:"PR description"`
}

type prOutput struct {
	Status string           `json:"status"`
	Error  string           `json:"error,omitempty"`
	PR     *api.PullRequest `json:"pr,omitempty"`
}

func (m *mcpServer) prCreate(_ context.Context, _ *sdk.CallToolRequest, in prCreateInput) (*sdk.CallToolResult, prOutput, error) {
	if in.Title == "" || in.Head == "" || in.Base == "" {
		return nil, prOutput{Status: statusError, Error: "title, head, and base are required"}, nil
	}
	var pr api.PullRequest
	req := api.CreatePullRequest{Title: in.Title, Head: in.Head, Base: in.Base, Body: in.Body}
	if err := m.call(http.MethodPost, "/pulls", req, http.StatusCreated, &pr); err != nil {
		return nil, prOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, prOutput{Status: statusOK, PR: &pr}, nil
}

type prMergeInput struct {
	Number int    `json:"number" jsonschema:"the PR number"`
	Method string `json:"method,omitempty" jsonschema:"ff-only|merge"`
}

type prMergeOutput struct {
	Status string           `json:"status"`
	Error  string           `json:"error,omitempty"`
	Result *api.MergeResult `json:"result,omitempty"`
}

func (m *mcpServer) prMerge(_ context.Context, _ *sdk.CallToolRequest, in prMergeInput) (*sdk.CallToolResult, prMergeOutput, error) {
	if in.Number <= 0 {
		return nil, prMergeOutput{Status: statusError, Error: "number must be a positive PR number"}, nil
	}
	method := api.MergeMethod(in.Method)
	if in.Method != "" && !method.Valid() {
		return nil, prMergeOutput{Status: statusError, Error: fmt.Sprintf("invalid method %q (want merge or ff-only)", in.Method)}, nil
	}
	var result api.MergeResult
	req := api.MergeRequest{Method: method}
	if err := m.call(http.MethodPost, fmt.Sprintf("/pulls/%d/merge", in.Number), req, http.StatusOK, &result); err != nil {
		return nil, prMergeOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, prMergeOutput{Status: statusOK, Result: &result}, nil
}

// ─── reviews ─────────────────────────────────────────────────────────────────

type reviewListInput struct {
	Ref   string `json:"ref,omitempty" jsonschema:"branch (default: repo default)"`
	Path  string `json:"path,omitempty" jsonschema:"file path filter"`
	State string `json:"state,omitempty" jsonschema:"open|resolved|all"`
}

type reviewListOutput struct {
	Status   string            `json:"status"`
	Error    string            `json:"error,omitempty"`
	Comments []api.CodeComment `json:"comments,omitempty"`
}

func (m *mcpServer) reviewList(_ context.Context, _ *sdk.CallToolRequest, in reviewListInput) (*sdk.CallToolResult, reviewListOutput, error) {
	state := in.State
	if state == "" {
		state = "open"
	}
	switch state {
	case "open", "resolved", "all":
	default:
		return nil, reviewListOutput{Status: statusError, Error: fmt.Sprintf("invalid state %q (want open|resolved|all)", state)}, nil
	}
	q := url.Values{}
	if in.Ref != "" {
		q.Set("ref", in.Ref)
	}
	if in.Path != "" {
		q.Set("path", in.Path)
	}
	q.Set("state", state)
	var comments []api.CodeComment
	if err := m.call(http.MethodGet, "/code-comments?"+q.Encode(), nil, http.StatusOK, &comments); err != nil {
		return nil, reviewListOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, reviewListOutput{Status: statusOK, Comments: comments}, nil
}

type reviewCreateInput struct {
	Path      string `json:"path" jsonschema:"file path"`
	StartLine int    `json:"start_line" jsonschema:"1-based"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"defaults to start_line"`
	Body      string `json:"body" jsonschema:"comment text"`
	Ref       string `json:"ref,omitempty" jsonschema:"defaults to repo default"`
}

type codeCommentOutput struct {
	Status  string           `json:"status"`
	Error   string           `json:"error,omitempty"`
	Comment *api.CodeComment `json:"comment,omitempty"`
}

func (m *mcpServer) reviewCreate(_ context.Context, _ *sdk.CallToolRequest, in reviewCreateInput) (*sdk.CallToolResult, codeCommentOutput, error) {
	if in.Path == "" || in.Body == "" {
		return nil, codeCommentOutput{Status: statusError, Error: "path and body are required"}, nil
	}
	if in.StartLine < 1 {
		return nil, codeCommentOutput{Status: statusError, Error: "start_line must be 1-based (>= 1)"}, nil
	}
	end := in.EndLine
	if end == 0 {
		end = in.StartLine
	}
	if end < in.StartLine {
		return nil, codeCommentOutput{Status: statusError, Error: "end_line must be >= start_line"}, nil
	}
	var c api.CodeComment
	req := api.CreateCodeCommentRequest{
		Ref:       in.Ref,
		Path:      in.Path,
		StartLine: in.StartLine,
		EndLine:   end,
		Body:      in.Body,
	}
	if err := m.call(http.MethodPost, "/code-comments", req, http.StatusCreated, &c); err != nil {
		return nil, codeCommentOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, codeCommentOutput{Status: statusOK, Comment: &c}, nil
}

type reviewIDInput struct {
	ID int64 `json:"id" jsonschema:"the code-comment id"`
}

// reviewSetResolved builds the resolve/reopen handler — both PATCH the same
// endpoint, differing only in the resolved flag they send.
func (m *mcpServer) reviewSetResolved(resolved bool) sdk.ToolHandlerFor[reviewIDInput, statusOutput] {
	return func(_ context.Context, _ *sdk.CallToolRequest, in reviewIDInput) (*sdk.CallToolResult, statusOutput, error) {
		if in.ID <= 0 {
			return nil, statusOutput{Status: statusError, Error: "id must be a positive comment id"}, nil
		}
		req := api.UpdateCodeCommentRequest{Resolved: &resolved}
		if err := m.call(http.MethodPatch, fmt.Sprintf("/code-comments/%d", in.ID), req, http.StatusOK, nil); err != nil {
			return nil, statusOutput{Status: statusError, Error: err.Error()}, nil
		}
		return nil, statusOutput{Status: statusOK}, nil
	}
}

// ─── pipelines ───────────────────────────────────────────────────────────────

type pipelineTriggerInput struct {
	Ref string `json:"ref" jsonschema:"branch, tag, or SHA"`
}

type runOutput struct {
	Status string    `json:"status"`
	Error  string    `json:"error,omitempty"`
	Run    *mcpCIRun `json:"run,omitempty"`
}

func (m *mcpServer) pipelineTrigger(_ context.Context, _ *sdk.CallToolRequest, in pipelineTriggerInput) (*sdk.CallToolResult, runOutput, error) {
	if in.Ref == "" {
		return nil, runOutput{Status: statusError, Error: "ref is required"}, nil
	}
	var run api.CIRun
	req := api.TriggerCIRunRequest{Ref: in.Ref}
	if err := m.call(http.MethodPost, "/runs", req, http.StatusAccepted, &run); err != nil {
		return nil, runOutput{Status: statusError, Error: err.Error()}, nil
	}
	r := toMCPCIRun(&run)
	return nil, runOutput{Status: statusOK, Run: &r}, nil
}

type pipelineListInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"max runs to return"`
	Kind  string `json:"kind,omitempty" jsonschema:"ci|agent"`
}

type runListOutput struct {
	Status string     `json:"status"`
	Error  string     `json:"error,omitempty"`
	Runs   []mcpCIRun `json:"runs,omitempty"`
}

func (m *mcpServer) pipelineList(_ context.Context, _ *sdk.CallToolRequest, in pipelineListInput) (*sdk.CallToolResult, runListOutput, error) {
	q := url.Values{}
	if in.Limit > 0 {
		q.Set("limit", strconv.Itoa(in.Limit))
	}
	if in.Kind != "" {
		q.Set("kind", in.Kind)
	}
	path := "/runs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var apiRuns []api.CIRun
	if err := m.call(http.MethodGet, path, nil, http.StatusOK, &apiRuns); err != nil {
		return nil, runListOutput{Status: statusError, Error: err.Error()}, nil
	}
	slim := make([]mcpCIRun, len(apiRuns))
	for i := range apiRuns {
		slim[i] = toMCPCIRun(&apiRuns[i])
	}
	return nil, runListOutput{Status: statusOK, Runs: slim}, nil
}

type pipelineGetInput struct {
	Number    int `json:"number" jsonschema:"the run number"`
	LastTurns int `json:"last_turns,omitempty" jsonschema:"cap agent turns returned (default 5; 0=default)"`
}

type runDetailOutput struct {
	Status string          `json:"status"`
	Error  string          `json:"error,omitempty"`
	Run    *mcpCIRunDetail `json:"run,omitempty"`
}

func (m *mcpServer) pipelineGet(_ context.Context, _ *sdk.CallToolRequest, in pipelineGetInput) (*sdk.CallToolResult, runDetailOutput, error) {
	if in.Number <= 0 {
		return nil, runDetailOutput{Status: statusError, Error: "number must be a positive run number"}, nil
	}
	var detail api.CIRunDetail
	if err := m.call(http.MethodGet, fmt.Sprintf("/runs/%d", in.Number), nil, http.StatusOK, &detail); err != nil {
		return nil, runDetailOutput{Status: statusError, Error: err.Error()}, nil
	}
	lastTurns := in.LastTurns
	if lastTurns == 0 {
		lastTurns = 5
	}
	turns := detail.Turns
	if len(turns) > lastTurns {
		turns = turns[len(turns)-lastTurns:]
	}
	run := &mcpCIRunDetail{
		mcpCIRun: toMCPCIRun(&detail.CIRun),
		Jobs:     detail.Jobs,
		Turns:    turns,
	}
	return nil, runDetailOutput{Status: statusOK, Run: run}, nil
}

// ─── agents ──────────────────────────────────────────────────────────────────

type agentSpawnInput struct {
	IssueNumber int    `json:"issue_number" jsonschema:"issue number"`
	Ref         string `json:"ref,omitempty" jsonschema:"defaults to repo HEAD"`
	Model       string `json:"model,omitempty" jsonschema:"claude-edit"`
	ToolProfile string `json:"tool_profile,omitempty" jsonschema:"full|review"`
}

func (m *mcpServer) agentSpawn(_ context.Context, _ *sdk.CallToolRequest, in agentSpawnInput) (*sdk.CallToolResult, runOutput, error) {
	if in.IssueNumber <= 0 {
		return nil, runOutput{Status: statusError, Error: "issue_number must be a positive issue number"}, nil
	}
	var run api.CIRun
	req := api.SpawnAgentRequest{Ref: in.Ref, Model: in.Model, ToolProfile: in.ToolProfile}
	if err := m.call(http.MethodPost, fmt.Sprintf("/issues/%d/agent", in.IssueNumber), req, http.StatusAccepted, &run); err != nil {
		return nil, runOutput{Status: statusError, Error: err.Error()}, nil
	}
	r := toMCPCIRun(&run)
	return nil, runOutput{Status: statusOK, Run: &r}, nil
}

type agentTurnInput struct {
	RunNumber int    `json:"run_number" jsonschema:"run number"`
	Text      string `json:"text" jsonschema:"message text"`
}

type turnOutput struct {
	Status string         `json:"status"`
	Error  string         `json:"error,omitempty"`
	Turn   *api.AgentTurn `json:"turn,omitempty"`
}

func (m *mcpServer) agentTurn(_ context.Context, _ *sdk.CallToolRequest, in agentTurnInput) (*sdk.CallToolResult, turnOutput, error) {
	if in.RunNumber <= 0 {
		return nil, turnOutput{Status: statusError, Error: "run_number must be a positive run number"}, nil
	}
	if in.Text == "" {
		return nil, turnOutput{Status: statusError, Error: "text is required"}, nil
	}
	var turn api.AgentTurn
	req := api.CreateAgentTurnRequest{Text: in.Text}
	if err := m.call(http.MethodPost, fmt.Sprintf("/runs/%d/turns", in.RunNumber), req, http.StatusAccepted, &turn); err != nil {
		return nil, turnOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, turnOutput{Status: statusOK, Turn: &turn}, nil
}
