package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// recordedReq captures what the mock moongit API saw, so a test can assert the
// wrapper hit the right endpoint with the right method and body.
type recordedReq struct {
	method string
	path   string // path + "?" + rawquery
	body   []byte
}

// mockAPI spins an httptest server that records each request and replies with
// the (status, jsonBody) the test queued under the matching "METHOD /path"
// route key (path without query). It fails the test on an unrouted request.
func mockAPI(t *testing.T, routes map[string]mockResp) (*mcpServer, *[]recordedReq) {
	t.Helper()
	var got []recordedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := r.URL.Path
		if r.URL.RawQuery != "" {
			q += "?" + r.URL.RawQuery
		}
		got = append(got, recordedReq{method: r.Method, path: q, body: body})

		resp, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			t.Errorf("unrouted request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(srv.Close)
	return &mcpServer{target: target{server: srv.URL, owner: "alice", repo: "demo"}}, &got
}

type mockResp struct {
	status int
	body   string
}

func TestMCPIssueList(t *testing.T) {
	m, got := mockAPI(t, map[string]mockResp{
		"GET /api/repos/alice/demo/issues": {http.StatusOK, `[{"number":1,"title":"first","state":"todo"},{"number":2,"title":"second","state":"in_progress"}]`},
	})
	_, out, err := m.issueList(context.Background(), nil, issueListInput{State: "todo,in_progress", Limit: 50})
	if err != nil {
		t.Fatalf("issueList: %v", err)
	}
	if out.Status != statusOK {
		t.Fatalf("status = %q, err = %q", out.Status, out.Error)
	}
	if len(out.Issues) != 2 || out.Issues[0].Number != 1 {
		t.Fatalf("issues = %+v", out.Issues)
	}
	// The state + limit filters made it onto the query string.
	last := (*got)[len(*got)-1]
	if last.path != "/api/repos/alice/demo/issues?limit=50&state=todo%2Cin_progress" {
		t.Errorf("query = %q", last.path)
	}
}

func TestMCPIssueCreate(t *testing.T) {
	m, got := mockAPI(t, map[string]mockResp{
		"POST /api/repos/alice/demo/issues": {http.StatusCreated, `{"number":7,"title":"wire it","state":"todo"}`},
	})
	_, out, err := m.issueCreate(context.Background(), nil, issueCreateInput{Title: "wire it", Body: "the plan"})
	if err != nil {
		t.Fatalf("issueCreate: %v", err)
	}
	if out.Status != statusOK || out.Issue == nil || out.Issue.Number != 7 {
		t.Fatalf("out = %+v (err %q)", out, out.Error)
	}
	var sent api.CreateIssueRequest
	if err := json.Unmarshal((*got)[0].body, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Title != "wire it" || sent.Body != "the plan" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestMCPIssueCreateValidation(t *testing.T) {
	m, _ := mockAPI(t, map[string]mockResp{}) // no route: a valid call would 404
	_, out, err := m.issueCreate(context.Background(), nil, issueCreateInput{Title: ""})
	if err != nil {
		t.Fatalf("issueCreate returned a Go error, want structured: %v", err)
	}
	if out.Status != statusError || out.Error == "" {
		t.Fatalf("want structured error, got %+v", out)
	}
}

func TestMCPReviewCreate(t *testing.T) {
	m, got := mockAPI(t, map[string]mockResp{
		"POST /api/repos/alice/demo/code-comments": {http.StatusCreated, `{"id":42,"path":"main.go","start_line":10,"end_line":12,"ref":"feat/x","body":"nit"}`},
	})
	// EndLine omitted should default to StartLine on the server request.
	_, out, err := m.reviewCreate(context.Background(), nil, reviewCreateInput{
		Path: "main.go", StartLine: 10, EndLine: 12, Body: "nit", Ref: "feat/x",
	})
	if err != nil {
		t.Fatalf("reviewCreate: %v", err)
	}
	if out.Status != statusOK || out.Comment == nil || out.Comment.ID != 42 {
		t.Fatalf("out = %+v (err %q)", out, out.Error)
	}
	var sent api.CreateCodeCommentRequest
	if err := json.Unmarshal((*got)[0].body, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Path != "main.go" || sent.StartLine != 10 || sent.EndLine != 12 || sent.Ref != "feat/x" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestMCPReviewCreateDefaultsEndLine(t *testing.T) {
	m, got := mockAPI(t, map[string]mockResp{
		"POST /api/repos/alice/demo/code-comments": {http.StatusCreated, `{"id":1,"path":"a.go","start_line":5,"end_line":5}`},
	})
	if _, out, err := m.reviewCreate(context.Background(), nil, reviewCreateInput{Path: "a.go", StartLine: 5, Body: "x"}); err != nil || out.Status != statusOK {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	var sent api.CreateCodeCommentRequest
	_ = json.Unmarshal((*got)[0].body, &sent)
	if sent.EndLine != 5 {
		t.Errorf("end_line defaulted to %d, want 5", sent.EndLine)
	}
}

func TestMCPReviewResolve(t *testing.T) {
	m, got := mockAPI(t, map[string]mockResp{
		"PATCH /api/repos/alice/demo/code-comments/9": {http.StatusOK, `{}`},
	})
	_, out, err := m.reviewSetResolved(true)(context.Background(), nil, reviewIDInput{ID: 9})
	if err != nil || out.Status != statusOK {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	var sent api.UpdateCodeCommentRequest
	_ = json.Unmarshal((*got)[0].body, &sent)
	if sent.Resolved == nil || *sent.Resolved != true {
		t.Errorf("resolved flag = %v, want true", sent.Resolved)
	}
}

func TestMCPAgentSpawn(t *testing.T) {
	m, got := mockAPI(t, map[string]mockResp{
		"POST /api/repos/alice/demo/issues/3/agent": {http.StatusAccepted, `{"number":11,"kind":"agent","issue_number":3,"status":"queued"}`},
	})
	_, out, err := m.agentSpawn(context.Background(), nil, agentSpawnInput{IssueNumber: 3, Model: "claude-edit"})
	if err != nil || out.Status != statusOK || out.Run == nil || out.Run.Kind != "agent" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	var sent api.SpawnAgentRequest
	_ = json.Unmarshal((*got)[0].body, &sent)
	if sent.Model != "claude-edit" {
		t.Errorf("sent model = %q", sent.Model)
	}
}

func TestMCPServerError(t *testing.T) {
	m, _ := mockAPI(t, map[string]mockResp{
		"POST /api/repos/alice/demo/issues/5/claim": {http.StatusConflict, `{"error":"already claimed"}`},
	})
	_, out, err := m.issueClaim(context.Background(), nil, issueClaimInput{Number: 5, State: "in_progress"})
	if err != nil {
		t.Fatalf("want structured error, got Go error: %v", err)
	}
	if out.Status != statusError || out.Error == "" {
		t.Fatalf("out = %+v", out)
	}
}

// TestMCPProfileFilter confirms the "review" profile (#184) only registers
// read tools + review_* + issue_comment — the shim-side enforcement seam — and
// drops everything that mutates issue state, spawns agents, or triggers
// pipelines. "full" (and the empty default) advertise the whole toolset.
func TestMCPProfileFilter(t *testing.T) {
	listTools := func(t *testing.T, profile string) map[string]bool {
		t.Helper()
		m := &mcpServer{target: target{server: "http://x", owner: "a", repo: "b"}, profile: profile}
		ctx := context.Background()
		serverT, clientT := sdk.NewInMemoryTransports()
		ss, err := m.newServer().Connect(ctx, serverT, nil)
		if err != nil {
			t.Fatalf("server connect: %v", err)
		}
		defer ss.Close()
		client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
		cs, err := client.Connect(ctx, clientT, nil)
		if err != nil {
			t.Fatalf("client connect: %v", err)
		}
		defer cs.Close()
		lt, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		have := map[string]bool{}
		for _, tool := range lt.Tools {
			have[tool.Name] = true
		}
		return have
	}

	review := listTools(t, profileReview)
	wantReview := []string{
		"issue_list", "issue_show", "issue_comment",
		"review_list", "review_create", "review_resolve", "review_reopen",
		"pipeline_list", "pipeline_get",
	}
	for _, w := range wantReview {
		if !review[w] {
			t.Errorf("review profile should expose %q", w)
		}
	}
	if len(review) != len(wantReview) {
		t.Errorf("review profile advertised %d tools, want %d: %v", len(review), len(wantReview), review)
	}
	for _, denied := range []string{
		"issue_create", "issue_claim", "issue_unclaim", "issue_set_state",
		"pipeline_trigger", "agent_spawn", "agent_turn",
	} {
		if review[denied] {
			t.Errorf("review profile must NOT expose %q", denied)
		}
	}

	// "full" and the empty default expose everything (16 tools).
	if got := len(listTools(t, profileFull)); got != 16 {
		t.Errorf("full profile advertised %d tools, want 16", got)
	}
	if got := len(listTools(t, "")); got != 16 {
		t.Errorf("empty (default) profile advertised %d tools, want 16", got)
	}
}

// TestMCPRoundTrip exercises the whole server over the SDK's in-memory
// transport: it confirms tool-schema inference doesn't panic at registration,
// every expected tool is advertised, and a tool call round-trips its structured
// output back to the client.
func TestMCPRoundTrip(t *testing.T) {
	m, _ := mockAPI(t, map[string]mockResp{
		"POST /api/repos/alice/demo/code-comments": {http.StatusCreated, `{"id":1,"path":"x.go","start_line":1,"end_line":1,"body":"hi"}`},
	})

	ctx := context.Background()
	serverT, clientT := sdk.NewInMemoryTransports()
	srv := m.newServer()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// All expected tools are advertised.
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := []string{
		"issue_list", "issue_show", "issue_create", "issue_comment", "issue_claim",
		"issue_unclaim", "issue_set_state", "review_list", "review_create",
		"review_resolve", "review_reopen", "pipeline_trigger", "pipeline_list",
		"pipeline_get", "agent_spawn", "agent_turn",
	}
	have := map[string]bool{}
	for _, tool := range lt.Tools {
		have[tool.Name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("tool %q not advertised", w)
		}
	}
	if len(lt.Tools) != len(want) {
		t.Errorf("advertised %d tools, want %d", len(lt.Tools), len(want))
	}

	// A tool call round-trips structured output.
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "review_create",
		Arguments: map[string]any{"path": "x.go", "start_line": 1, "body": "hi"},
	})
	if err != nil {
		t.Fatalf("call review_create: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out codeCommentOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode structured: %v", err)
	}
	if out.Status != statusOK || out.Comment == nil || out.Comment.ID != 1 {
		t.Fatalf("structured out = %+v", out)
	}
}
