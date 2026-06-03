---
id: mcp-server
status: draft
owners: [aleh]
covers:
  - "cmd/moongit/mcp.go"
---
# MCP Server (mgit mcp)

## Intent

`mgit mcp` exposes moongit's control plane to an agent as MCP tools over stdio.
It exists because a headless Claude session reliably gets MCP tools but not a
shell — so an agent that can't run `mgit` on the command line can still file
issues, claim work, review code, and trigger pipelines through tool calls. It is
a pure stdio↔REST proxy with no local state: every tool is a thin wrapper over
the same `/api` endpoints the human UI and CLI use, so the MCP surface is the
agent-facing dual of the web UI, never a second source of truth. Tool profiles
let the operator hand an agent only the tools its job needs.

## Behavior

- WHEN `mgit mcp` starts, it serves moongit's toolset over stdio as an MCP server
  and blocks until the transport closes or its context is cancelled.
- WHERE identity and target come from, they reuse the rest of the CLI's plumbing:
  the repo is resolved from the checkout's git remotes (or `MOONGIT_SERVER`), and
  requests authenticate with `MOONGIT_TOKEN`; the session is scoped to that one
  repo for its lifetime.
- WHILE the session runs, it holds no state of its own — each tool call is a
  single authenticated request to the scoped repo's `/api` endpoint, the same one
  the corresponding CLI subcommand calls.
- WHEN a tool's underlying request fails (non-success status or transport error),
  the handler returns a structured error carrying the server's message rather
  than crashing the session.
- WHERE the toolset is concerned, it covers four groups over the data plane:
  issue (list/show/create/comment/claim/unclaim/set-state), review (list/create/
  resolve/reopen code comments), pipeline (trigger/list/get runs), and agent
  (spawn/turn).
- WHERE a tool profile is selected, it scopes which tools are registered: `full`
  (the default) exposes every tool; `review` exposes only read tools plus the
  review tools and issue-comment — enough to survey, anchor findings, and report
  on the driving issue, but nothing that mutates issue state, spawns agents, or
  triggers pipelines.
- WHILE a profile restricts the toolset, the restriction is enforced at
  registration: a disallowed tool is never advertised to the client, not merely
  rejected when called.
- WHERE the profile contract lives, mgit owns its own tool→profile mapping; the
  server only names the profile (`full`/`review`) over the wire, mirroring the
  stored tool-profile values.

## Non-goals

- **The endpoints the tools wrap.** Each tool's semantics (what a claim does, how
  a pipeline triggers) are owned by that subsystem's spec — issues, pull-requests,
  ci-pipelines, agent-runs. This spec covers the MCP shim, not the API.
- **Choosing a run's profile.** Which profile an agent run is launched with is the
  agent-runs spec; here only what each profile exposes and that it's enforced
  shim-side.
- **dex's MCP server.** `dex mcp` is a separate shim this one mirrors in shape;
  the dex toolset is the dex project's concern.
- **Multi-repo / owner-repo override in one session.** A session is scoped to a
  single resolved repo; cross-repo addressing is out of scope here.
- **A second source of truth.** The shim adds no behavior beyond the REST API;
  any rule (validation, identity stamping, the open data plane) is the server's,
  not the shim's.

## Checklist

- [x] `mgit mcp` serves the toolset over stdio (modelcontextprotocol/go-sdk)
- [x] Stateless stdio↔REST proxy; tools wrap the same /api endpoints as the CLI
- [x] Repo from git remotes / MOONGIT_SERVER; auth via MOONGIT_TOKEN; single-repo scope
- [x] Tool errors surfaced as structured output with the server's message
- [x] 16 tools across issue / review / pipeline / agent groups
- [x] `full` (default) and `review` profiles; review = read + review_* + issue_comment
- [x] Profile enforced at registration (disallowed tools never advertised)
- [x] Profile mapping owned by mgit; only the profile name crosses the wire
- [ ] Verified against the code by the verify workflow (flip to `living`)
