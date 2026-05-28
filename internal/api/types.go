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
	Assignee  *string    `json:"assignee"` // nil = unassigned. Explicit null in JSON.
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CreateIssueRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Author string `json:"-"` // populated server-side from token
}

type UpdateIssueRequest struct {
	State IssueState `json:"state"`
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
