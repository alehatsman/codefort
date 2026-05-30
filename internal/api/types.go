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

// CIRun is the public view of a CI run. Number is the per-repo run number
// (the address clients use); the internal DB id is not exposed.
type CIRun struct {
	Number     int        `json:"number"`
	CommitSHA  string     `json:"commit_sha"`
	Ref        string     `json:"ref"`
	Event      string     `json:"event"`
	Trigger    string     `json:"trigger,omitempty"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// CIJob is one job within a run, addressed by Name (which is also the key in
// the per-job event-stream path).
type CIJob struct {
	Name       string     `json:"name"`
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

// Token represents an API token's metadata. The plaintext token itself
// is never returned over the API — it's only shown once at creation time
// by the moongitd CLI.
type Token struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}
