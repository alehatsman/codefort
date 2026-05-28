// Package api defines the JSON wire types shared by the moongitd server and
// the moongit client. Anything serialized over HTTP between them lives here.
package api

import "time"

type IssueState string

const (
	IssueOpen   IssueState = "open"
	IssueClosed IssueState = "closed"
)

type Issue struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Author    string     `json:"author"`
	State     IssueState `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CreateIssueRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Author string `json:"author"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
