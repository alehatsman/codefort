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
	"github.com/alehatsman/moongit/internal/ci"
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
	case "ci":
		return runCI(args[1:])
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
    moongit issue edit    <number> [--title <t>] [--body <b>] [--state <s>]
    moongit issue set-state <number> <todo|in_progress|done|closed>
    moongit issue claim   <number> [--state s]
    moongit issue unclaim <number>
    moongit issue delete  <number> [--yes]
    moongit issue comment <number> --body <b>

    moongit ci validate  [path]   (defaults to ./mgitci.yml)

Identity: the server stamps author/assignee from the name of the token
in MOONGIT_TOKEN. Mint a token with "moongitd token create <name>" and
export MOONGIT_TOKEN=mgt_... before running the client.

Run inside a git checkout whose 'origin' remote points at a moongit
server. The target repo is parsed from the remote URL.
`)
}

func runIssue(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongit issue <create|list|show|edit|set-state|claim|unclaim|delete|comment>")
	}
	switch args[0] {
	case "create":
		return runIssueCreate(args[1:])
	case "list":
		return runIssueList(args[1:])
	case "show":
		return runIssueShow(args[1:])
	case "edit":
		return runIssueEdit(args[1:])
	case "set-state":
		return runIssueSetState(args[1:])
	case "claim":
		return runIssueClaim(args[1:])
	case "unclaim":
		return runIssueUnclaim(args[1:])
	case "delete":
		return runIssueDelete(args[1:])
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

	payload, err := json.Marshal(api.UpdateIssueRequest{State: &state})
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

// runIssueEdit applies a partial update to an issue's title, body, and/or
// state. Only the flags actually passed are sent — fs.Visit distinguishes an
// explicitly-empty --body from an omitted one, so editing the title never
// clobbers the body and vice versa.
func runIssueEdit(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: moongit issue edit <number> [--title <t>] [--body <b>] [--state <s>]")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	fs := flag.NewFlagSet("issue edit", flag.ContinueOnError)
	title := fs.String("title", "", "new title")
	body := fs.String("body", "", "new body")
	stateFlag := fs.String("state", "", "new state (todo|in_progress|done|closed)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra args: %v", fs.Args())
	}

	// Build a partial request from only the flags the user actually set.
	var req api.UpdateIssueRequest
	seen := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	if seen["title"] {
		req.Title = title
	}
	if seen["body"] {
		req.Body = body
	}
	if seen["state"] {
		state := api.IssueState(*stateFlag)
		if !state.Valid() {
			return fmt.Errorf("invalid state %q (want one of: %v)", *stateFlag, api.AllIssueStates)
		}
		req.State = &state
	}
	if req.Title == nil && req.Body == nil && req.State == nil {
		return errors.New("nothing to edit: pass at least one of --title, --body, --state")
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(req)
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
	fmt.Printf("#%d  %s  [%s]\n", iss.Number, iss.Title, iss.State)
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

func runIssueDelete(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: moongit issue delete <number> [--yes]")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	fs := flag.NewFlagSet("issue delete", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	fs.BoolVar(yes, "y", false, "skip the confirmation prompt (shorthand)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra args: %v", fs.Args())
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}

	if !*yes {
		fmt.Printf("Delete issue #%d and all its comments? This cannot be undone. [y/N]: ", num)
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("aborted")
			return nil
		}
	}

	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/issues/%d", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodDelete, endpoint, nil, "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	fmt.Printf("#%d  deleted\n", num)
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

// runCI dispatches `moongit ci <subcommand>`. CI subcommands are local-only
// (no server round-trip): they operate on the mgitci.yml in the working copy.
func runCI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongit ci validate [path]")
	}
	switch args[0] {
	case "validate":
		return runCIValidate(args[1:])
	default:
		return fmt.Errorf("unknown ci subcommand: %s", args[0])
	}
}

// runCIValidate parses and validates an mgitci.yml locally, reporting the
// jobs and their dependencies on success. It hits no server — it's the
// authoring-time check before pushing a pipeline.
func runCIValidate(args []string) error {
	fs := flag.NewFlagSet("ci validate", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "mgitci.yml"
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	} else if fs.NArg() > 1 {
		return fmt.Errorf("unexpected extra args: %v", fs.Args()[1:])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pipeline, err := ci.Parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	fmt.Printf("%s: ok — %d job(s)\n", path, len(pipeline.Jobs))
	for _, name := range pipeline.JobNames() {
		job := pipeline.Jobs[name]
		needs := ""
		if len(job.Needs) > 0 {
			needs = " (needs: " + strings.Join(job.Needs, ", ") + ")"
		}
		fmt.Printf("  %s — %d step(s)%s\n", name, len(job.Steps), needs)
	}
	return nil
}

// target describes the moongit server + repo derived from `git remote`.
type target struct {
	server string // scheme://host[:port]
	owner  string
	repo   string
}

// discoverTarget derives the moongit server URL and owner/repo from the
// current git checkout's remotes. It prefers a dedicated `moongit` remote
// (the code mirror) so the server URL + owner/repo come straight from it,
// and falls back to `origin` for checkouts that only have their upstream
// configured. MOONGIT_SERVER overrides the derived server host — required
// when the chosen remote is SSH (no http base URL to derive).
func discoverTarget() (target, error) {
	remote, err := gitRemoteURL("moongit")
	if err != nil {
		remote, err = gitRemoteURL("origin")
		if err != nil {
			return target{}, fmt.Errorf("read git remote 'moongit' or 'origin': %w (run inside a checkout of the target repo)", err)
		}
	}
	t, err := parseRemote(remote)
	if err != nil {
		return target{}, err
	}
	if override := os.Getenv("MOONGIT_SERVER"); override != "" {
		t.server = strings.TrimRight(override, "/")
	}
	if t.server == "" {
		return target{}, fmt.Errorf("remote %q has no http(s) host; add a `moongit` http remote or set MOONGIT_SERVER", remote)
	}
	return t, nil
}

// parseRemote extracts owner/repo from any git remote form (http(s), ssh://,
// or scp-like git@host:owner/repo). For http(s) it also derives the server
// base URL; ssh/scp forms leave server empty so the caller supplies
// MOONGIT_SERVER.
func parseRemote(remote string) (target, error) {
	remote = strings.TrimSpace(remote)

	// ssh://[user@]host[:port]/owner/repo(.git)
	if strings.HasPrefix(remote, "ssh://") {
		u, err := url.Parse(remote)
		if err != nil {
			return target{}, fmt.Errorf("parse remote URL %q: %w", remote, err)
		}
		owner, repo, err := splitOwnerRepo(u.Path)
		if err != nil {
			return target{}, err
		}
		return target{owner: owner, repo: repo}, nil
	}

	// scp-like: [user@]host:owner/repo(.git) — has a colon, no "://".
	if !strings.Contains(remote, "://") && strings.Contains(remote, ":") {
		_, path, _ := strings.Cut(remote, ":")
		owner, repo, err := splitOwnerRepo(path)
		if err != nil {
			return target{}, err
		}
		return target{owner: owner, repo: repo}, nil
	}

	u, err := url.Parse(remote)
	if err != nil {
		return target{}, fmt.Errorf("parse remote URL %q: %w", remote, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return target{}, fmt.Errorf("unsupported remote scheme %q", u.Scheme)
	}
	owner, repo, err := splitOwnerRepo(u.Path)
	if err != nil {
		return target{}, err
	}
	return target{server: u.Scheme + "://" + u.Host, owner: owner, repo: repo}, nil
}

// splitOwnerRepo trims a leading slash and a trailing ".git" from a remote
// path and splits it into <owner>/<repo>.
func splitOwnerRepo(p string) (owner, repo string, err error) {
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	owner, repo, ok := strings.Cut(p, "/")
	if !ok || owner == "" || repo == "" {
		return "", "", fmt.Errorf("remote path %q is not <owner>/<repo>(.git)", p)
	}
	return owner, repo, nil
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
