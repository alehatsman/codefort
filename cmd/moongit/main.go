// Command moongit is the client CLI for the moongit server. It infers the
// target repo from the local git remote, so users run it from inside a working
// copy: `moongit issue create --title "..."`.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "moongit:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "issue":
		return runIssue(args[1:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q (try `moongit help`)", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `moongit — client for the moongit server

USAGE:
    moongit issue create  --title <t> [--body <b>]
    moongit issue list    [--state s,s] [--assignee a|null] [--limit n]
    moongit issue show    <number>
    moongit issue set-state <number> <todo|in_progress|done|closed>
    moongit issue claim   <number> [--state s]
    moongit issue unclaim <number>
    moongit issue comment <number> --body <b>

Identity: the server stamps author/assignee from the name of the token
in MOONGIT_TOKEN. Mint a token with "moongitd token create <name>" and
export MOONGIT_TOKEN=mgt_... before running the client.

Run inside a git checkout whose 'origin' remote points at a moongit
server. The target repo is parsed from the remote URL.
`)
}

func runIssue(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongit issue <create|list|show|set-state|claim|unclaim|comment>")
	}
	switch args[0] {
	case "create":
		return runIssueCreate(args[1:])
	case "list":
		return runIssueList(args[1:])
	case "show":
		return runIssueShow(args[1:])
	case "set-state":
		return runIssueSetState(args[1:])
	case "claim":
		return runIssueClaim(args[1:])
	case "unclaim":
		return runIssueUnclaim(args[1:])
	case "comment":
		return runIssueComment(args[1:])
	default:
		return fmt.Errorf("unknown issue subcommand: %s", args[0])
	}
}


func runIssueCreate(args []string) error {
	fs := flag.NewFlagSet("issue create", flag.ContinueOnError)
	title := fs.String("title", "", "issue title (required)")
	body := fs.String("body", "", "issue body")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return errors.New("--title is required")
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}

	payload, err := json.Marshal(api.CreateIssueRequest{
		Title: *title, Body: *body,
	})
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues", target.server, target.owner, target.repo)
	resp, raw, err := httpDo(http.MethodPost, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}

	var iss api.Issue
	if err := json.Unmarshal(raw, &iss); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("#%d  %s\n", iss.Number, iss.Title)
	fmt.Printf("        by %s, %s\n", iss.Author, iss.CreatedAt.Local().Format(time.RFC3339))
	return nil
}

func runIssueList(args []string) error {
	fs := flag.NewFlagSet("issue list", flag.ContinueOnError)
	state := fs.String("state", "", "filter by state(s), comma-separated (todo,in_progress,done,closed)")
	assignee := fs.String("assignee", "", "filter by assignee; 'null' for unassigned")
	limit := fs.Int("limit", 0, "max results (default 100, max 1000)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target, err := discoverTarget()
	if err != nil {
		return err
	}

	q := url.Values{}
	if *state != "" {
		q.Set("state", *state)
	}
	if *assignee != "" {
		q.Set("assignee", *assignee)
	}
	if *limit > 0 {
		q.Set("limit", strconv.Itoa(*limit))
	}

	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues", target.server, target.owner, target.repo)
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	resp, raw, err := httpDo(http.MethodGet, endpoint, nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var issues []api.Issue
	if err := json.Unmarshal(raw, &issues); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(issues) == 0 {
		fmt.Println("(no issues)")
		return nil
	}
	for _, iss := range issues {
		assignee := "—"
		if iss.Assignee != nil {
			assignee = *iss.Assignee
		}
		fmt.Printf("#%-4d  [%-11s]  @%-20s  %s  — %s\n",
			iss.Number, iss.State, assignee, iss.Title, iss.Author)
	}
	return nil
}

func runIssueShow(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongit issue show <number>")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}
	target, err := discoverTarget()
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues/%d", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodGet, endpoint, nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var iss api.Issue
	if err := json.Unmarshal(raw, &iss); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("#%d  %s\n", iss.Number, iss.Title)
	fmt.Printf("state:    %s\n", iss.State)
	fmt.Printf("author:   %s\n", iss.Author)
	if iss.Assignee != nil {
		fmt.Printf("assignee: %s\n", *iss.Assignee)
	} else {
		fmt.Printf("assignee: (unassigned)\n")
	}
	fmt.Printf("created:  %s\n", iss.CreatedAt.Local().Format(time.RFC3339))
	if iss.Body != "" {
		fmt.Printf("\n%s\n", iss.Body)
	}

	// Comments timeline.
	commentsEndpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues/%d/comments", target.server, target.owner, target.repo, num)
	cresp, craw, cerr := httpDo(http.MethodGet, commentsEndpoint, nil, "")
	if cerr == nil && cresp.StatusCode == http.StatusOK {
		var comments []api.Comment
		if json.Unmarshal(craw, &comments) == nil && len(comments) > 0 {
			fmt.Printf("\nComments (%d):\n", len(comments))
			for _, c := range comments {
				fmt.Printf("  [%s] %s: %s\n", c.CreatedAt.Local().Format(time.RFC3339), c.Author, c.Body)
			}
		}
	}
	return nil
}

func runIssueSetState(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: moongit issue set-state <number> <todo|in_progress|done|closed>")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}
	state := api.IssueState(args[1])
	if !state.Valid() {
		return fmt.Errorf("invalid state %q (want one of: %v)", args[1], api.AllIssueStates)
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}

	payload, err := json.Marshal(api.UpdateIssueRequest{State: state})
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues/%d", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodPatch, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var iss api.Issue
	if err := json.Unmarshal(raw, &iss); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("#%d  %s  → %s\n", iss.Number, iss.Title, iss.State)
	return nil
}

func runIssueClaim(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: moongit issue claim <number> [--state <s>]")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	fs := flag.NewFlagSet("issue claim", flag.ContinueOnError)
	stateFlag := fs.String("state", "", "optional state transition (e.g. in_progress)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra args: %v", fs.Args())
	}
	state := api.IssueState(*stateFlag)
	if state != "" && !state.Valid() {
		return fmt.Errorf("invalid state %q (want one of: %v)", *stateFlag, api.AllIssueStates)
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.ClaimRequest{State: state})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues/%d/claim", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodPost, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("issue #%d already claimed", num)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var iss api.Issue
	if err := json.Unmarshal(raw, &iss); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("#%d  claimed by @%s  [%s]\n", iss.Number, *iss.Assignee, iss.State)
	return nil
}

func runIssueUnclaim(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongit issue unclaim <number>")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}
	target, err := discoverTarget()
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues/%d/unclaim", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodPost, endpoint, nil, "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	fmt.Printf("#%d  unclaimed\n", num)
	return nil
}

func runIssueComment(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: moongit issue comment <number> --body <b> [--author <a>]")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	fs := flag.NewFlagSet("issue comment", flag.ContinueOnError)
	body := fs.String("body", "", "comment body (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra args: %v", fs.Args())
	}
	if strings.TrimSpace(*body) == "" {
		return errors.New("--body is required")
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.CreateCommentRequest{Body: *body})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues/%d/comments", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodPost, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var c api.Comment
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("commented on #%d by %s at %s\n", num, c.Author, c.CreatedAt.Local().Format(time.RFC3339))
	return nil
}

// target describes the moongit server + repo derived from `git remote`.
type target struct {
	server string // scheme://host[:port]
	owner  string
	repo   string
}

// discoverTarget derives the moongit server URL and owner/repo from the
// current git checkout's `origin` remote. MOONGIT_SERVER overrides the
// derived server URL (host part); owner and repo always come from the remote.
func discoverTarget() (target, error) {
	remote, err := gitRemoteURL("origin")
	if err != nil {
		return target{}, fmt.Errorf("read git remote 'origin': %w (run inside a checkout of the target repo)", err)
	}
	t, err := parseRemote(remote)
	if err != nil {
		return target{}, err
	}
	if override := os.Getenv("MOONGIT_SERVER"); override != "" {
		t.server = strings.TrimRight(override, "/")
	}
	return t, nil
}

func parseRemote(remote string) (target, error) {
	remote = strings.TrimSpace(remote)
	if strings.HasPrefix(remote, "git@") || strings.HasPrefix(remote, "ssh://") {
		return target{}, fmt.Errorf("ssh remotes not supported yet; configure an http(s) remote to %s", remote)
	}
	u, err := url.Parse(remote)
	if err != nil {
		return target{}, fmt.Errorf("parse remote URL %q: %w", remote, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return target{}, fmt.Errorf("unsupported remote scheme %q", u.Scheme)
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	owner, repo, ok := strings.Cut(path, "/")
	if !ok || owner == "" || repo == "" {
		return target{}, fmt.Errorf("remote path %q is not <owner>/<repo>(.git)", u.Path)
	}
	return target{server: u.Scheme + "://" + u.Host, owner: owner, repo: repo}, nil
}

func gitRemoteURL(name string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", name)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}


func httpDo(method, urlStr string, body io.Reader, contentType string) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if tok := os.Getenv("MOONGIT_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	return resp, raw, nil
}

func decodeError(raw []byte) string {
	var er api.ErrorResponse
	if json.Unmarshal(raw, &er) == nil && er.Error != "" {
		return er.Error
	}
	return strings.TrimSpace(string(raw))
}
