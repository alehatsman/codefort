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
}

// PullRequestDetail is a PR plus the head-vs-base compare (PR 1) and the code
// review comments anchored to its head branch. Compare is best-effort: if a
// branch has since been deleted it carries only Base/Head with zero diff.
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
	Number       int        `json:"number"`
	CommitSHA    string     `json:"commit_sha"`
	CommitMsg    string     `json:"commit_msg,omitempty"`
	CommitAuthor string     `json:"commit_author,omitempty"`
	Ref          string     `json:"ref"`
	Event        string     `json:"event"`
	Trigger      string     `json:"trigger,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
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

// CIRunDetail is a run plus its jobs, for the run-detail view.
type CIRunDetail struct {
	CIRun
	Jobs []CIJob `json:"jobs"`
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
