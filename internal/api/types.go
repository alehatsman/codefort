// Package api defines the JSON wire types shared by the moongitd server and
// the moongit client. Anything serialized over HTTP between them lives here.
package api

import "time"

// IssueState is the lifecycle state of an issue. The set is closed — see
// Valid for the enumeration. Any-to-any transitions are allowed.
type IssueState string

const (
	IssueTodo       IssueState = "todo"
	IssueInProgress IssueState = "in_progress"
	IssueDone       IssueState = "done"
	IssueClosed     IssueState = "closed"
)

// Valid reports whether s is one of the known issue states.
func (s IssueState) Valid() bool {
	switch s {
	case IssueTodo, IssueInProgress, IssueDone, IssueClosed:
		return true
	}
	return false
}

// AllIssueStates is the canonical list, suitable for help text and clients
// that want to enumerate without hardcoding.
var AllIssueStates = []IssueState{IssueTodo, IssueInProgress, IssueDone, IssueClosed}

// IssueSort selects the ordering of a ListIssues result. The set is closed —
// see Valid. The zero value ("") means the default, IssueSortNewest.
type IssueSort string

const (
	IssueSortNewest          IssueSort = "newest"           // by issue number, descending (default)
	IssueSortOldest          IssueSort = "oldest"           // by issue number, ascending
	IssueSortRecentlyUpdated IssueSort = "recently-updated" // by last-updated time, descending
)

// Valid reports whether s is one of the known sort orders. The empty string is
// not Valid — callers treat "" as "unset, use the default" before validating.
func (s IssueSort) Valid() bool {
	switch s {
	case IssueSortNewest, IssueSortOldest, IssueSortRecentlyUpdated:
		return true
	}
	return false
}

// AllIssueSorts is the canonical list, suitable for clients enumerating the
// sort options without hardcoding.
var AllIssueSorts = []IssueSort{IssueSortNewest, IssueSortOldest, IssueSortRecentlyUpdated}

type Issue struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Author    string     `json:"author"`
	State     IssueState `json:"state"`
	Assignee  *string    `json:"assignee"`   // nil = unassigned. Explicit null in JSON.
	ClaimedAt *time.Time `json:"claimed_at"` // when Assignee took the issue; nil when unassigned
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CreateIssueRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Author string `json:"-"` // populated server-side from token
}

// UpdateIssueRequest is a partial update: every field is optional, and only
// the ones present (non-nil) are changed. An all-nil request is a no-op the
// server rejects. State-only requests stay wire-compatible with older clients
// that sent {"state": "..."}.
type UpdateIssueRequest struct {
	State *IssueState `json:"state,omitempty"`
	Title *string     `json:"title,omitempty"`
	Body  *string     `json:"body,omitempty"`
}

// ClaimRequest atomically takes ownership of an issue. The server stamps
// the assignee from the authenticated token's name; the request body
// only carries an optional state transition. Succeeds only if the issue
// is currently unassigned (409 otherwise).
type ClaimRequest struct {
	Assignee string     `json:"-"` // populated server-side from token; ignored on the wire
	State    IssueState `json:"state,omitempty"`
}

type Comment struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issue_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateCommentRequest struct {
	Author string `json:"-"` // populated server-side from token
	Body   string `json:"body"`
}

// CodeComment is a comment anchored to a line range of a file on a branch.
// StartLine/EndLine are 1-based and inclusive. Snippet is the referenced
// source lines, populated server-side on list (empty on create) so reviewers
// see the code without a separate fetch. CommitSha is the ref's HEAD when the
// comment was made — context for whether the lines have since drifted.
type CodeComment struct {
	ID        int64     `json:"id"`
	RepoID    int64     `json:"repo_id"`
	Ref       string    `json:"ref"`
	Path      string    `json:"path"`
	StartLine int       `json:"start_line"`
	EndLine   int       `json:"end_line"`
	CommitSha string    `json:"commit_sha,omitempty"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	Resolved  bool      `json:"resolved"`
	Snippet   string    `json:"snippet,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateCodeCommentRequest anchors a new comment to Path's StartLine..EndLine
// on Ref. Author and the ref's commit SHA are stamped server-side.
type CreateCodeCommentRequest struct {
	Ref       string `json:"ref"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Body      string `json:"body"`
	Author    string `json:"-"` // populated server-side from token
	CommitSha string `json:"-"` // populated server-side from the ref's HEAD
}

// UpdateCodeCommentRequest is a partial update of a code comment. Only the
// resolved flag is mutable; body edits are out of scope (delete + recreate).
type UpdateCodeCommentRequest struct {
	Resolved *bool `json:"resolved,omitempty"`
}

// RefList is the branch listing for a repo: every local branch plus the name
// of the default one, so a client can preselect it.
type RefList struct {
	Default  string   `json:"default"`
	Branches []string `json:"branches"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// Repo is the public view of a registered repository for UI/CLI clients.
type Repo struct {
	ID          int64     `json:"id"`
	Owner       string    `json:"owner"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	OpenIssues  int       `json:"open_issues"`
	TotalIssues int       `json:"total_issues"`
	CIEnabled   bool      `json:"ci_enabled"`
	// CIStatus / CINumber describe the repo's most recent CI run (highest run
	// number), omitted when the repo has no runs. They let the repos list show
	// a CI status icon linking to that run.
	CIStatus string `json:"ci_status,omitempty"`
	CINumber int    `json:"ci_number,omitempty"`
	// OpenPulls / OpenReviews / ActiveAgents are at-a-glance counts for the repos
	// list metric grid: open PRs, unresolved code-review comments, and agent runs
	// in a non-terminal state. Always emitted (a zero is a real "none", not absent).
	OpenPulls    int `json:"open_pulls"`
	OpenReviews  int `json:"open_reviews"`
	ActiveAgents int `json:"active_agents"`
}

// UpdateRepoRequest is a partial update of a repo's settings. Only non-nil
// fields are changed; an all-nil request is rejected.
type UpdateRepoRequest struct {
	CIEnabled *bool `json:"ci_enabled,omitempty"`
}

// CreateRepoRequest provisions a new repository: a bare git repo on disk
// plus its database registration.
type CreateRepoRequest struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// TreeEntry is one item in a repository directory listing.
type TreeEntry struct {
	Name string `json:"name"`           // basename, relative to the listed dir
	Path string `json:"path"`           // full path from the repo root
	Type string `json:"type"`           // "blob" (file) | "tree" (dir)
	Size int64  `json:"size,omitempty"` // bytes for blobs; 0 for trees
}

// Tree is a directory listing at Path on a ref. Entries are ordered
// directories-first, then alphabetically.
type Tree struct {
	Ref     string      `json:"ref"`     // branch name, e.g. "main"
	Path    string      `json:"path"`    // "" for the repo root
	Entries []TreeEntry `json:"entries"` // empty for an unborn (no commits) repo
}

// Blob is a single file's contents at Path on a ref. For binary files or
// files larger than the server's display cap, Content is empty and the
// flag (Binary / TooLarge) says why.
type Blob struct {
	Ref      string `json:"ref"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"too_large"`
	Content  string `json:"content"` // text; empty when Binary or TooLarge
}

// Commit is a single commit's metadata. Subject is the first line of the
// message; Body is the rest (empty for single-line messages).
type Commit struct {
	SHA      string    `json:"sha"`
	ShortSHA string    `json:"short_sha"`
	Subject  string    `json:"subject"`
	Body     string    `json:"body,omitempty"`
	Author   string    `json:"author"`
	Email    string    `json:"email"`
	Date     time.Time `json:"date"`
	// Branch is a single human-friendly label for which branch the commit is
	// on: the default branch if it contains the commit, else the first local
	// branch that does. Lossy by design (a commit can live on several
	// branches). Only populated where a branch hint is useful — the issue
	// commits list and the single-commit view — so it's omitempty elsewhere.
	Branch string `json:"branch,omitempty"`
}

// CommitList is a page of commit history on a ref, newest first, optionally
// filtered to those touching Path. HasMore is true when another page exists.
type CommitList struct {
	Ref     string   `json:"ref"`            // branch name, e.g. "main"
	Path    string   `json:"path,omitempty"` // "" for whole-repo history
	Commits []Commit `json:"commits"`        // empty for an unborn repo
	HasMore bool     `json:"has_more"`
}

// TreeCommits annotates a directory listing with commit context: the last
// commit touching each immediate child (keyed by full path), the dir's own
// latest commit, and the total commit count on the branch. Powers the
// GitHub-style latest-commit bar and per-file "last changed" columns.
type TreeCommits struct {
	Ref     string            `json:"ref"`
	Path    string            `json:"path,omitempty"`
	Total   int               `json:"total"`   // total commits on the branch (scoped to Path when set)
	Latest  *Commit           `json:"latest"`  // last commit touching Path; nil for an unborn repo
	Entries map[string]Commit `json:"entries"` // child full path -> last commit touching it
}

// DiffLine is one line within a hunk. Old/New are 1-based line numbers in the
// pre-/post-image; the side that doesn't carry the line is 0 (context lines
// carry both, an add carries only New, a del only Old). Text is the line body
// without the leading +/-/space marker.
type DiffLine struct {
	Kind string `json:"kind"` // "context" | "add" | "del"
	Old  int    `json:"old"`
	New  int    `json:"new"`
	Text string `json:"text"`
}

// DiffHunk is a contiguous run of context/changed lines, introduced by an @@
// header in the unified patch. Header is the text trailing the second @@ (the
// enclosing function/section git prints), empty when absent.
type DiffHunk struct {
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

// DiffFile is the diff for a single path. For a rename OldPath != NewPath; for
// an add OldPath is "", for a delete NewPath is "". Binary files carry counts
// of 0 and no hunks.
type DiffFile struct {
	OldPath   string     `json:"old_path"`
	NewPath   string     `json:"new_path"`
	Status    string     `json:"status"` // "added" | "modified" | "deleted" | "renamed"
	Binary    bool       `json:"binary"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Hunks     []DiffHunk `json:"hunks"`
}

// CommitDetail is a single commit's metadata plus its diff against the first
// parent (the empty tree for a root commit, the first parent for a merge),
// parsed into structured per-file hunks for the side-by-side diff view.
// Additions/Deletions are the totals across Files. Truncated is set when the
// diff exceeded the server's line budget and some hunks were dropped.
type CommitDetail struct {
	Commit    Commit     `json:"commit"`
	Parents   []string   `json:"parents"`
	Files     []DiffFile `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Truncated bool       `json:"truncated"`
}

// Compare is the difference of Head relative to Base, computed as the
// three-dot (merge-base) diff git uses for pull requests: the changes Head
// introduces since the two branches diverged. MergeBase is the SHA the diff is
// taken against ("" when the histories are unrelated, in which case the diff is
// the whole Head tree). Ahead/Behind count commits Head/Base has that the other
// does not. Commits are the Ahead commits (Base..Head), newest first.
// Additions/Deletions total across Files; Truncated mirrors CommitDetail.
type Compare struct {
	Base      string     `json:"base"`       // base branch short name
	Head      string     `json:"head"`       // head branch short name
	MergeBase string     `json:"merge_base"` // SHA the diff is taken against; "" if unrelated
	Ahead     int        `json:"ahead"`      // commits on Head not on Base
	Behind    int        `json:"behind"`     // commits on Base not on Head
	Commits   []Commit   `json:"commits"`    // Base..Head, newest first
	Files     []DiffFile `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Truncated bool       `json:"truncated"`
}

// PRState is the lifecycle state of a pull request. The set is closed — see
// Valid. A PR is born "open"; it becomes "merged" via the merge endpoint or
// "closed" when abandoned without merging.
type PRState string

const (
	PROpen   PRState = "open"
	PRMerged PRState = "merged"
	PRClosed PRState = "closed"
)

// Valid reports whether s is one of the known PR states.
func (s PRState) Valid() bool {
	switch s {
	case PROpen, PRMerged, PRClosed:
		return true
	}
	return false
}

// AllPRStates is the canonical list, for clients enumerating without hardcoding.
var AllPRStates = []PRState{PROpen, PRMerged, PRClosed}

// PullRequest pairs a head branch with a base branch for review and merge.
// Number is per-repo (like issues). MergedAt is non-nil only once the PR is
// merged. Review comments are not embedded here — see PullRequestDetail.
type PullRequest struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	BaseRef   string     `json:"base_ref"`
	HeadRef   string     `json:"head_ref"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Author    string     `json:"author"`
	State     PRState    `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	MergedAt  *time.Time `json:"merged_at"` // nil unless State == merged
	// MergeBaseSHA/MergeHeadSHA are the base/head branch tips frozen at merge
	// time, used to reproduce the pre-merge compare on the detail endpoint (a
	// merged head is contained in base, so the live-ref diff is empty). Server-
	// internal: set only for PRs merged after migration 16, never serialized.
	MergeBaseSHA string `json:"-"`
	MergeHeadSHA string `json:"-"`
}

// PullRequestDetail is a PR plus the head-vs-base compare (PR 1) and the code
// review comments anchored to its head branch. Compare is best-effort: for a
// merged PR it's reproduced from the tips frozen at merge time, for an open PR
// from the live branches; if the needed commits are gone it carries only
// Base/Head with zero diff.
type PullRequestDetail struct {
	PullRequest
	Compare  Compare       `json:"compare"`
	Comments []CodeComment `json:"comments"`
}

// CreatePullRequest opens a PR from Head into Base. Author is stamped
// server-side from the token; Base/Head must name existing local branches.
type CreatePullRequest struct {
	Base   string `json:"base"`
	Head   string `json:"head"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Author string `json:"-"` // populated server-side from token
}

// UpdatePullRequest is a partial update: only non-nil fields change. State may
// move to "closed" (abandon) or back to "open" (reopen); transitioning to
// "merged" is rejected here — that goes through the merge endpoint (#81).
type UpdatePullRequest struct {
	Title *string  `json:"title,omitempty"`
	Body  *string  `json:"body,omitempty"`
	State *PRState `json:"state,omitempty"`
}

// MergeMethod selects how a PR is merged. "merge" (default) always creates a
// merge commit; "ff-only" fast-forwards the base ref and fails if the branches
// have diverged.
type MergeMethod string

const (
	MergeCommitMethod MergeMethod = "merge"
	MergeFFOnlyMethod MergeMethod = "ff-only"
)

// Valid reports whether m is a known merge method. The empty string is treated
// as the default (merge) by the handler before validating.
func (m MergeMethod) Valid() bool {
	return m == MergeCommitMethod || m == MergeFFOnlyMethod
}

// MergeRequest is the body of the merge endpoint. An empty Method means the
// default (merge commit).
type MergeRequest struct {
	Method MergeMethod `json:"method,omitempty"`
}

// MergeResult is returned on a successful merge: the PR (now state=merged) plus
// the resulting base-ref tip and whether it was a fast-forward.
type MergeResult struct {
	PullRequest
	MergeCommit string `json:"merge_commit"` // base ref tip after the merge
	FastForward bool   `json:"fast_forward"`
}

// MergeConflictResponse is the 409 body when a merge can't proceed cleanly: the
// human message plus the conflicting paths (empty for non-conflict 409s such as
// "not fast-forwardable" or a concurrent base move).
type MergeConflictResponse struct {
	Error     string   `json:"error"`
	Conflicts []string `json:"conflicts,omitempty"`
}

// CIRun is the public view of a CI run. Number is the per-repo run number
// (the address clients use); the internal DB id is not exposed.
type CIRun struct {
	Number             int        `json:"number"`
	Kind               string     `json:"kind"`                           // "ci" | "agent"
	IssueNumber        *int       `json:"issue_number,omitempty"`         // the issue an agent run serves
	ExecutionModel     string     `json:"execution_model,omitempty"`      // agent model: "claude-edit" | "mooncake-agent"
	MooncakeAllowShell bool       `json:"mooncake_allow_shell,omitempty"` // mooncake-agent run allowed to use shell/cmd (#110)
	ToolProfile        string     `json:"tool_profile,omitempty"`         // mgit MCP toolset slice: "full" | "review" (#184)
	CommitSHA          string     `json:"commit_sha"`
	CommitMsg          string     `json:"commit_msg,omitempty"`
	CommitAuthor       string     `json:"commit_author,omitempty"`
	Ref                string     `json:"ref"`
	Event              string     `json:"event"`
	Trigger            string     `json:"trigger,omitempty"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
}

// SpawnAgentRequest starts an agent run for an issue. Ref is the base the agent
// checks out and branches from (optional; defaults to the repo's HEAD). Model
// selects the execution model ("claude-edit" | "mooncake-agent"); empty uses
// the server's configured default (#110).
type SpawnAgentRequest struct {
	Ref   string `json:"ref,omitempty"`
	Model string `json:"model,omitempty"`
	// AllowShell, for the mooncake-agent model, drops the default shell/cmd
	// denial for this run so the agent's plan may run shell commands (#110).
	AllowShell bool `json:"allow_shell,omitempty"`
	// ToolProfile scopes which mgit MCP tools the run sees ("full" | "review");
	// empty defaults to "full". "review" yields a read-only review agent (#184).
	ToolProfile string `json:"tool_profile,omitempty"`
}

// TriggerCIRunRequest starts a CI run for an arbitrary ref (branch, tag, or
// commit SHA) without a git push. The server resolves Ref to a commit against
// the bare repo and enqueues a run with event "manual".
type TriggerCIRunRequest struct {
	Ref string `json:"ref"`
}

// CIJob is one job within a run, addressed by Name (which is also the key in
// the per-job event-stream path).
type CIJob struct {
	Name       string     `json:"name"`
	Needs      []string   `json:"needs,omitempty"`
	Status     string     `json:"status"`
	ExitCode   *int       `json:"exit_code"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// CIRunDetail is a run plus its jobs, for the run-detail view. For an agent run
// it also carries the conversation's follow-up turns (the issue body is turn 1
// and isn't listed here), so the UI can show queued/in-flight messages that
// haven't reached the event stream yet.
type CIRunDetail struct {
	CIRun
	Jobs  []CIJob     `json:"jobs"`
	Turns []AgentTurn `json:"turns,omitempty"`
}

// RepoRef identifies the repo a globally-listed item belongs to. The
// cross-repo aggregate endpoints (/api/issues, /api/pulls, /api/runs) attach it
// to every row so a fleet-wide list view can link each entry back to its repo's
// detail route. Number stays per-repo, so a row is addressed by Repo + Number.
type RepoRef struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// IssueWithRepo is an Issue plus its owning repo, returned by GET /api/issues.
type IssueWithRepo struct {
	Issue
	Repo RepoRef `json:"repo"`
}

// PullRequestWithRepo is a PullRequest plus its owning repo, returned by
// GET /api/pulls.
type PullRequestWithRepo struct {
	PullRequest
	Repo RepoRef `json:"repo"`
}

// CIRunWithRepo is a CIRun plus its owning repo, returned by GET /api/runs.
type CIRunWithRepo struct {
	CIRun
	Repo RepoRef `json:"repo"`
}

// AgentTurn is one human follow-up message in an agent run's conversation.
type AgentTurn struct {
	Seq        int        `json:"seq"`
	Author     string     `json:"author"`
	Body       string     `json:"body"`
	Status     string     `json:"status"` // pending | running | done | error
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// CreateAgentTurnRequest queues a follow-up turn (a message to the agent) on an
// agent run that's awaiting input (or running — it queues behind the current
// turn).
type CreateAgentTurnRequest struct {
	Text string `json:"text"`
}

// AgentSettings is the operator-facing agent config. The Claude token is
// write-only — the API reports only whether one is set, never its value.
// EnvFallbackSet reports whether a server-env credential
// (MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN or MOONGIT_AGENT_ANTHROPIC_API_KEY) is
// configured, so the UI can distinguish "no auth at all" from "authenticating
// via the env fallback" when no Settings token is set. ExecutionModel is the
// default model new agent runs use when they don't pick one at spawn ("" means
// the server falls back to its built-in default). LLMBaseURL is the operator-set
// ANTHROPIC_BASE_URL override (not a secret — returned as-is). AuthTokenSet, like
// ClaudeTokenSet, is write-only: the API reports only whether an
// ANTHROPIC_AUTH_TOKEN gateway bearer is configured, never its value.
type AgentSettings struct {
	ClaudeTokenSet bool   `json:"claude_oauth_token_set"`
	EnvFallbackSet bool   `json:"claude_token_env_fallback"`
	ExecutionModel string `json:"execution_model,omitempty"`
	LLMBaseURL     string `json:"llm_base_url,omitempty"`
	AuthTokenSet   bool   `json:"anthropic_auth_token_set"`
}

// UpdateAgentSettingsRequest sets global agent config. A nil pointer leaves a
// field unchanged; an empty string clears it; any other value sets it.
type UpdateAgentSettingsRequest struct {
	ClaudeToken        *string `json:"claude_oauth_token"`
	ExecutionModel     *string `json:"execution_model"`
	LLMBaseURL         *string `json:"llm_base_url"`
	AnthropicAuthToken *string `json:"anthropic_auth_token"`
}

// Event is one entry in the outbound fleet event feed (#73), as streamed by the
// GET /api/events SSE endpoint. Seq is the global monotonic id echoed back as
// Last-Event-ID on reconnect; Time is unix milliseconds (matching ci.Event).
// Type is dotted (e.g. "issue.claimed", "ci.run.finished", "push"). Repo is the
// "owner/name" slug, empty for non-repo events. Actor is the token name (or
// pusher) that caused it. Data is the type-specific detail payload.
type Event struct {
	Seq   int64          `json:"seq"`
	Type  string         `json:"type"`
	Time  int64          `json:"time"`
	Repo  string         `json:"repo,omitempty"`
	Actor string         `json:"actor,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// Token represents an API token's metadata. The plaintext token itself
// is never returned over the API except once, in CreatedToken at creation
// time — list/lookup never expose it.
type Token struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// CreateTokenRequest mints a new API token under the given name. Names are
// unique; identity in every claim/comment is the token's name.
type CreateTokenRequest struct {
	Name string `json:"name"`
}

// CreatedToken is returned once, by POST /api/tokens. It carries the
// plaintext Secret alongside the metadata — this is the only time the
// plaintext is ever sent over the wire, mirroring the `moongitd token`
// CLI. The caller must surface it immediately; it's unrecoverable after.
type CreatedToken struct {
	Token
	Secret string `json:"secret"`
}

// SSHKey is a registered SSH public key for the git SSH transport. It belongs
// to a token (TokenName is that token's name — the push/pull identity). The
// public half is not a secret, so unlike tokens it's returned in full on every
// read. Fingerprint is the SHA256 form (e.g. "SHA256:abc…").
type SSHKey struct {
	ID          int64      `json:"id"`
	TokenName   string     `json:"token_name"`
	Fingerprint string     `json:"fingerprint"`
	Comment     string     `json:"comment,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

// CreateSSHKeyRequest registers an SSH public key against the requesting
// token. PublicKey is a single authorized_keys line (e.g. "ssh-ed25519 AAAA…
// comment"). Comment is optional and defaults to the key's own trailing
// comment when omitted.
type CreateSSHKeyRequest struct {
	PublicKey string `json:"public_key"`
	Comment   string `json:"comment,omitempty"`
}

// SpecListItem is one in-repo spec's metadata in a SpecList. Path is
// repo-relative (e.g. "specs/ssh-transport.md"); the remaining fields come
// from the spec's optional frontmatter, with ID and Title defaulted from the
// path/heading when absent. Alignment is a pointer so "never verified" (null)
// is distinct from "0% aligned".
type SpecListItem struct {
	Path         string   `json:"path"`
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Status       string   `json:"status,omitempty"`
	Owners       []string `json:"owners,omitempty"`
	Covers       []string `json:"covers,omitempty"`
	LastVerified string   `json:"last_verified,omitempty"`
	Alignment    *float64 `json:"alignment,omitempty"`
}

// SpecList is the response for GET /api/repos/{owner}/{repo}/specs: every
// markdown spec under specs/ on the selected ref, with parsed metadata. Specs
// is non-nil and empty (not null) when the repo has no specs/ directory.
type SpecList struct {
	Ref   string         `json:"ref"`
	Specs []SpecListItem `json:"specs"`
}

// SpecSection is one heading in a spec body and the markdown beneath it (up to
// the next heading of the same or shallower level, so a section subsumes its
// subsections). Line is the 1-based, file-absolute line of the heading — for
// the editor and truth-gutter to deep-link to a section.
type SpecSection struct {
	Title string `json:"title"`
	Level int    `json:"level"`
	Body  string `json:"body"`
	Line  int    `json:"line"`
}

// SpecChecklistItem is a GitHub-style task-list line found in a spec body. Line
// is 1-based and file-absolute.
type SpecChecklistItem struct {
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
	Line    int    `json:"line"`
}

// SpecContent is the response for GET /api/repos/{owner}/{repo}/specs/{path}:
// one spec's metadata plus its content, both raw (Content — the whole file,
// frontmatter included, for the editor) and structured (Body without the
// frontmatter, Sections, Checklist — for rendering and the truth gutter).
// Sections and Checklist are non-nil (empty, not null) when the body has none.
type SpecContent struct {
	Ref          string              `json:"ref"`
	Path         string              `json:"path"`
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Status       string              `json:"status,omitempty"`
	Owners       []string            `json:"owners,omitempty"`
	Covers       []string            `json:"covers,omitempty"`
	LastVerified string              `json:"last_verified,omitempty"`
	Alignment    *float64            `json:"alignment,omitempty"`
	Content      string              `json:"content"`
	Body         string              `json:"body"`
	Sections     []SpecSection       `json:"sections"`
	Checklist    []SpecChecklistItem `json:"checklist"`
	// Verification is the spec's latest verify pass (nil when never verified),
	// surfaced so the read view can render the truth gutter + result panel.
	Verification *SpecVerification `json:"verification,omitempty"`
}

// SpecVerificationMarker is one line's verify verdict for the truth gutter:
// Line is 1-based file-absolute; Marker is aligned|drifted|unverifiable|unspecced.
type SpecVerificationMarker struct {
	Line   int    `json:"line"`
	Text   string `json:"text,omitempty"`
	Marker string `json:"marker"`
	Note   string `json:"note,omitempty"`
}

// SpecVerification is a spec's latest recorded verify pass for the read view:
// the alignment, per-line markers, conflicts, when/what it ran against, and
// whether the spec or its governed code has changed since (Stale).
type SpecVerification struct {
	Alignment  float64                  `json:"alignment"`
	Markers    []SpecVerificationMarker `json:"markers"`
	Conflicts  []string                 `json:"conflicts,omitempty"`
	Notes      string                   `json:"notes,omitempty"`
	VerifiedAt string                   `json:"verified_at"`
	Commit     string                   `json:"commit"`
	Stale      bool                     `json:"stale"`
}

// SpecSearchHit is one semantic-search match within the spec corpus. Path is
// repo-relative; Section is the enclosing spec heading (the nearest heading at
// or before Line), empty when none precedes it. Line is the match's 1-based
// start line, Snippet the matched text, Score dex's relevance (higher = closer).
type SpecSearchHit struct {
	Path    string  `json:"path"`
	Section string  `json:"section,omitempty"`
	Line    int     `json:"line"`
	Snippet string  `json:"snippet,omitempty"`
	Score   float32 `json:"score"`
}

// SpecSearchResult is the response for POST .../specs/search: dex semantic
// search scoped to the specs/ corpus. Hits is non-nil and empty (not null)
// when nothing matches.
type SpecSearchResult struct {
	Query string          `json:"query"`
	Hits  []SpecSearchHit `json:"hits"`
}

// WriteSpecRequest is the body of PUT .../specs/{path}: commit spec content to a
// feature branch. Branch defaults to "spec/<filename-stem>" and is never the
// repo's default branch (specs land via a PR, not a direct push to main). Base
// is the branch the target is created from when it doesn't yet exist (defaults
// to the repo's default branch); committing onto an existing branch ignores it.
type WriteSpecRequest struct {
	Content string `json:"content"`
	Message string `json:"message,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Base    string `json:"base,omitempty"`
}

// WriteSpecResult reports where a spec write landed so the UI can offer a PR.
// Created is true when the commit started a new branch (vs. extending one).
type WriteSpecResult struct {
	Branch  string `json:"branch"`
	Commit  string `json:"commit"`
	Created bool   `json:"created"`
}

// SpecDriftItem is one spec's deterministic (non-LLM) drift status: whether the
// code it governs has changed since it was last verified. This is the backstop
// that gates the agent pass — it never classifies *how* code drifted, only
// whether a re-verify is warranted. Status is one of:
//   - "uncovered": no covers[] globs, so drift can't be checked deterministically
//   - "unverified": has covers[] but was never verified (no baseline) → candidate
//   - "stale": a governed path changed since the baseline commit → candidate
//   - "fresh": no governed path changed since the baseline → skip the agent
type SpecDriftItem struct {
	Path         string   `json:"path"`
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Covers       []string `json:"covers,omitempty"`
	Base         string   `json:"base,omitempty"`          // baseline commit (last verified), when known
	Changed      []string `json:"changed,omitempty"`       // governed paths changed since base (when stale)
	LastVerified string   `json:"last_verified,omitempty"` // date from the verification record
}

// SpecDriftReport is the response for GET .../specs/drift: the deterministic
// drift status of every spec on the ref. The "unverified" + "stale" items are
// the stale-candidate set the verify agent pass should run over.
type SpecDriftReport struct {
	Ref   string          `json:"ref"`
	Specs []SpecDriftItem `json:"specs"`
}

// VerifySpecRequest is the body of POST .../specs/verify: kick off a verify
// agent run for one spec. Ref selects the commit to verify against (default
// branch when empty).
type VerifySpecRequest struct {
	Path string `json:"path"`
	Ref  string `json:"ref,omitempty"`
}
