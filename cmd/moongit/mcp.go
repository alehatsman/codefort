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
// `mgit mcp` entrypoint. It mirrors dex's `dex mcp` shim: a pure stdio<->REST
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp takes no arguments (got %v)", fs.Args())
	}

	tgt, err := discoverTarget()
	if err != nil {
		return fmt.Errorf("mcp: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return (&mcpServer{target: tgt}).run(ctx)
}

// mcpServer holds the resolved target for the session. Handlers are methods so
// they can reach it (and the package-level httpDo/decodeError helpers).
type mcpServer struct {
	target target
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

// newServer builds the MCP server and registers the full toolset. Split out
// from run so tests can drive it over an in-memory transport.
func (m *mcpServer) newServer() *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{
		Name:    "moongit",
		Version: mcpVersion,
	}, nil)

	// ── issues ──────────────────────────────────────────────────────────────
	sdk.AddTool(srv, &sdk.Tool{
		Name: "issue_list",
		Description: "List issues in the repo. Filter by state(s) (comma-separated: " +
			"todo,in_progress,done,closed), by assignee ('null' for unassigned), or by a " +
			"keyword in title/body. Survey this before claiming work.",
	}, m.issueList)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "issue_show",
		Description: "Show one issue (title, state, author, assignee, body) plus its comment timeline.",
	}, m.issueShow)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "issue_create",
		Description: "Create a new issue. The author is stamped from the token identity. Returns the new issue.",
	}, m.issueCreate)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "issue_comment",
		Description: "Post a comment on an issue — use this to report progress at real checkpoints.",
	}, m.issueComment)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "issue_claim",
		Description: "Atomically claim (assign yourself) an issue, optionally transitioning its state " +
			"(e.g. in_progress). Fails if the issue is already claimed by someone else. Claim before coding.",
	}, m.issueClaim)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "issue_unclaim",
		Description: "Release your claim on an issue so someone else can pick it up.",
	}, m.issueUnclaim)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "issue_set_state",
		Description: "Set an issue's state to one of: todo, in_progress, done, closed.",
	}, m.issueSetState)

	// ── reviews (code comments anchored to a file's line range on a branch) ──
	sdk.AddTool(srv, &sdk.Tool{
		Name: "review_list",
		Description: "List code-review comments anchored to file line-ranges on a branch. Scope by " +
			"ref (branch; defaults to the repo default), by path, and by state (open|resolved|all). " +
			"Each comment carries its file, line range, author, body, and the source snippet.",
	}, m.reviewList)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "review_create",
		Description: "Post a code-review comment anchored to PATH's line range (1-based, inclusive) on a " +
			"branch REF. This is how a review agent records findings — no shell needed. The author and " +
			"the ref's commit SHA are stamped server-side.",
	}, m.reviewCreate)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "review_resolve",
		Description: "Mark a code-review comment resolved, by its id.",
	}, m.reviewSetResolved(true))
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "review_reopen",
		Description: "Reopen a previously-resolved code-review comment, by its id.",
	}, m.reviewSetResolved(false))

	// ── pipelines (CI runs) ──────────────────────────────────────────────────
	sdk.AddTool(srv, &sdk.Tool{
		Name: "pipeline_trigger",
		Description: "Trigger a CI run for a ref (branch, tag, or commit SHA) without a push. Returns the " +
			"queued run.",
	}, m.pipelineTrigger)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "pipeline_list",
		Description: "List recent runs. Optionally cap with limit and filter by kind ('ci' or 'agent'). " +
			"Returns each run's number, kind, ref, status, and timestamps.",
	}, m.pipelineList)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "pipeline_get",
		Description: "Get one run by number, including its jobs (and, for an agent run, its follow-up turns).",
	}, m.pipelineGet)

	// ── agents ───────────────────────────────────────────────────────────────
	sdk.AddTool(srv, &sdk.Tool{
		Name: "agent_spawn",
		Description: "Spawn an agent run to work an issue in a container. Optionally pin the base ref " +
			"(defaults to repo HEAD), pick the execution model (claude-edit|mooncake-pilot; empty uses the " +
			"server default), and allow shell for the mooncake-pilot model. Returns the queued run.",
	}, m.agentSpawn)
	sdk.AddTool(srv, &sdk.Tool{
		Name: "agent_turn",
		Description: "Queue a follow-up message on an agent run that's awaiting input (or running — it " +
			"queues behind the current turn). Addressed by run number.",
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
	State    string `json:"state,omitempty" jsonschema:"comma-separated states to filter by (todo,in_progress,done,closed)"`
	Assignee string `json:"assignee,omitempty" jsonschema:"filter by assignee; 'null' for unassigned"`
	Query    string `json:"query,omitempty" jsonschema:"keyword to match in title or body"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max results (default 100, max 1000)"`
}

type issueListOutput struct {
	Status string      `json:"status"`
	Error  string      `json:"error,omitempty"`
	Issues []api.Issue `json:"issues,omitempty"`
}

func (m *mcpServer) issueList(_ context.Context, _ *sdk.CallToolRequest, in issueListInput) (*sdk.CallToolResult, issueListOutput, error) {
	q := url.Values{}
	if in.State != "" {
		q.Set("state", in.State)
	}
	if in.Assignee != "" {
		q.Set("assignee", in.Assignee)
	}
	if in.Query != "" {
		q.Set("q", in.Query)
	}
	if in.Limit > 0 {
		q.Set("limit", strconv.Itoa(in.Limit))
	}
	path := "/issues"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var issues []api.Issue
	if err := m.call(http.MethodGet, path, nil, http.StatusOK, &issues); err != nil {
		return nil, issueListOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, issueListOutput{Status: statusOK, Issues: issues}, nil
}

type issueShowInput struct {
	Number int `json:"number" jsonschema:"the issue number"`
}

type issueShowOutput struct {
	Status   string        `json:"status"`
	Error    string        `json:"error,omitempty"`
	Issue    *api.Issue    `json:"issue,omitempty"`
	Comments []api.Comment `json:"comments,omitempty"`
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
	var comments []api.Comment
	_ = m.call(http.MethodGet, fmt.Sprintf("/issues/%d/comments", in.Number), nil, http.StatusOK, &comments)
	return nil, issueShowOutput{Status: statusOK, Issue: &iss, Comments: comments}, nil
}

type issueCreateInput struct {
	Title string `json:"title" jsonschema:"issue title (required)"`
	Body  string `json:"body,omitempty" jsonschema:"issue body (markdown)"`
}

type issueOutput struct {
	Status string     `json:"status"`
	Error  string     `json:"error,omitempty"`
	Issue  *api.Issue `json:"issue,omitempty"`
}

func (m *mcpServer) issueCreate(_ context.Context, _ *sdk.CallToolRequest, in issueCreateInput) (*sdk.CallToolResult, issueOutput, error) {
	if in.Title == "" {
		return nil, issueOutput{Status: statusError, Error: "title is required"}, nil
	}
	var iss api.Issue
	req := api.CreateIssueRequest{Title: in.Title, Body: in.Body}
	if err := m.call(http.MethodPost, "/issues", req, http.StatusCreated, &iss); err != nil {
		return nil, issueOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, issueOutput{Status: statusOK, Issue: &iss}, nil
}

type issueCommentInput struct {
	Number int    `json:"number" jsonschema:"the issue number"`
	Body   string `json:"body" jsonschema:"comment text (required)"`
}

type commentOutput struct {
	Status  string       `json:"status"`
	Error   string       `json:"error,omitempty"`
	Comment *api.Comment `json:"comment,omitempty"`
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
	return nil, commentOutput{Status: statusOK, Comment: &c}, nil
}

type issueClaimInput struct {
	Number int    `json:"number" jsonschema:"the issue number"`
	State  string `json:"state,omitempty" jsonschema:"optional state transition on claim (e.g. in_progress)"`
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
	return nil, issueOutput{Status: statusOK, Issue: &iss}, nil
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
	State  string `json:"state" jsonschema:"new state: todo | in_progress | done | closed"`
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
	return nil, issueOutput{Status: statusOK, Issue: &iss}, nil
}

// ─── reviews ─────────────────────────────────────────────────────────────────

type reviewListInput struct {
	Ref   string `json:"ref,omitempty" jsonschema:"branch to review (defaults to the repo's default branch)"`
	Path  string `json:"path,omitempty" jsonschema:"scope to a single file path"`
	State string `json:"state,omitempty" jsonschema:"open | resolved | all (default open)"`
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
	Path      string `json:"path" jsonschema:"file path to anchor the comment to (required)"`
	StartLine int    `json:"start_line" jsonschema:"first line of the range, 1-based (required)"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"last line of the range, 1-based inclusive (defaults to start_line)"`
	Body      string `json:"body" jsonschema:"comment text (required)"`
	Ref       string `json:"ref,omitempty" jsonschema:"branch to anchor on (defaults to the repo's default branch)"`
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
	Ref string `json:"ref" jsonschema:"branch, tag, or commit SHA to run (required)"`
}

type runOutput struct {
	Status string     `json:"status"`
	Error  string     `json:"error,omitempty"`
	Run    *api.CIRun `json:"run,omitempty"`
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
	return nil, runOutput{Status: statusOK, Run: &run}, nil
}

type pipelineListInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"max runs to return"`
	Kind  string `json:"kind,omitempty" jsonschema:"filter by kind: 'ci' or 'agent'"`
}

type runListOutput struct {
	Status string      `json:"status"`
	Error  string      `json:"error,omitempty"`
	Runs   []api.CIRun `json:"runs,omitempty"`
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
	var runs []api.CIRun
	if err := m.call(http.MethodGet, path, nil, http.StatusOK, &runs); err != nil {
		return nil, runListOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, runListOutput{Status: statusOK, Runs: runs}, nil
}

type pipelineGetInput struct {
	Number int `json:"number" jsonschema:"the run number"`
}

type runDetailOutput struct {
	Status string           `json:"status"`
	Error  string           `json:"error,omitempty"`
	Run    *api.CIRunDetail `json:"run,omitempty"`
}

func (m *mcpServer) pipelineGet(_ context.Context, _ *sdk.CallToolRequest, in pipelineGetInput) (*sdk.CallToolResult, runDetailOutput, error) {
	if in.Number <= 0 {
		return nil, runDetailOutput{Status: statusError, Error: "number must be a positive run number"}, nil
	}
	var detail api.CIRunDetail
	if err := m.call(http.MethodGet, fmt.Sprintf("/runs/%d", in.Number), nil, http.StatusOK, &detail); err != nil {
		return nil, runDetailOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, runDetailOutput{Status: statusOK, Run: &detail}, nil
}

// ─── agents ──────────────────────────────────────────────────────────────────

type agentSpawnInput struct {
	IssueNumber int    `json:"issue_number" jsonschema:"the issue the agent should work (required)"`
	Ref         string `json:"ref,omitempty" jsonschema:"base ref to check out (defaults to repo HEAD)"`
	Model       string `json:"model,omitempty" jsonschema:"execution model: claude-edit | mooncake-pilot (empty uses the server default)"`
	AllowShell  bool   `json:"allow_shell,omitempty" jsonschema:"for mooncake-pilot, allow the plan to run shell/cmd actions"`
}

func (m *mcpServer) agentSpawn(_ context.Context, _ *sdk.CallToolRequest, in agentSpawnInput) (*sdk.CallToolResult, runOutput, error) {
	if in.IssueNumber <= 0 {
		return nil, runOutput{Status: statusError, Error: "issue_number must be a positive issue number"}, nil
	}
	var run api.CIRun
	req := api.SpawnAgentRequest{Ref: in.Ref, Model: in.Model, AllowShell: in.AllowShell}
	if err := m.call(http.MethodPost, fmt.Sprintf("/issues/%d/agent", in.IssueNumber), req, http.StatusAccepted, &run); err != nil {
		return nil, runOutput{Status: statusError, Error: err.Error()}, nil
	}
	return nil, runOutput{Status: statusOK, Run: &run}, nil
}

type agentTurnInput struct {
	RunNumber int    `json:"run_number" jsonschema:"the agent run number to message (required)"`
	Text      string `json:"text" jsonschema:"the follow-up message to the agent (required)"`
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
