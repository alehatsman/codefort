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
	case "review":
		return runReview(args[1:])
	case "pr":
		return runPR(args[1:])
	case "ci":
		return runCI(args[1:])
	case "repo":
		return runRepo(args[1:])
	case "events":
		return runEvents(args[1:])
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
    moongit issue list    [--state s,s] [--assignee a|null] [--query|-q kw] [--limit n]
    moongit issue show    <number>
    moongit issue edit    <number> [--title <t>] [--body <b>] [--state <s>]
    moongit issue set-state <number> <todo|in_progress|done|closed>
    moongit issue claim   <number> [--state s]
    moongit issue unclaim <number>
    moongit issue delete  <number> [--yes]
    moongit issue comment <number> --body <b>

    moongit review list    [--ref <branch>] [--path <p>] [--state open|resolved|all] [--json]
    moongit review create  --path <p> --lines <n|a-b> --body <b> [--ref <branch>]
    moongit review resolve <id>
    moongit review reopen  <id>
    moongit review delete  <id>

    moongit pr create  --base <ref> --head <ref> --title <t> [--body <b>]
    moongit pr list    [--state open|merged|closed|all]
    moongit pr show    <number>
    moongit pr merge   <number> [--ff-only]

    moongit ci validate  [path]   (defaults to ./mgitci.yml)
    moongit ci run       <ref>    (trigger a run for a branch/tag/sha)

    moongit repo delete  <owner>/<name> [--yes]   (irreversible)

    moongit events                (tail the fleet event feed; Ctrl-C to stop)
        [--repo owner/name] [--types a,b] [--since <seq>] [--once]

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
	var query string
	fs.StringVar(&query, "query", "", "filter by keyword in title or body")
	fs.StringVar(&query, "q", "", "shorthand for --query")
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
	if query != "" {
		q.Set("q", query)
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
		_, _ = fmt.Scanln(&answer)
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

// runReview dispatches `moongit review <subcommand>` — the read/triage side of
// the code-review comments anchored to file blocks on a branch.
func runReview(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongit review <list|create|resolve|reopen|delete>")
	}
	switch args[0] {
	case "list":
		return runReviewList(args[1:])
	case "create":
		return runReviewCreate(args[1:])
	case "resolve":
		return runReviewSetResolved(args[1:], true)
	case "reopen":
		return runReviewSetResolved(args[1:], false)
	case "delete":
		return runReviewDelete(args[1:])
	default:
		return fmt.Errorf("unknown review subcommand: %s", args[0])
	}
}

// parseLineSpec parses a review line spec — a single 1-based line ("5") or an
// inclusive range ("5-12") — into start/end. It mirrors the L5 / L5-L12 form
// `review list` prints. Lines are 1-based; a range must be non-decreasing.
func parseLineSpec(spec string) (start, end int, err error) {
	lo, hi, isRange := strings.Cut(spec, "-")
	start, err = strconv.Atoi(strings.TrimSpace(lo))
	if err != nil || start < 1 {
		return 0, 0, fmt.Errorf("invalid --lines %q (want N or A-B, 1-based)", spec)
	}
	if !isRange {
		return start, start, nil
	}
	end, err = strconv.Atoi(strings.TrimSpace(hi))
	if err != nil || end < start {
		return 0, 0, fmt.Errorf("invalid --lines %q (want N or A-B, 1-based)", spec)
	}
	return start, end, nil
}

// runReviewCreate anchors a new code-review comment to a file's line range on a
// branch. The server validates the path/ref and stamps author + commit SHA; we
// only marshal the request and report the created id. Mirrors runCITrigger.
func runReviewCreate(args []string) error {
	fs := flag.NewFlagSet("review create", flag.ContinueOnError)
	path := fs.String("path", "", "file path to anchor the comment to (required)")
	lines := fs.String("lines", "", "line or inclusive range: N or A-B (required)")
	body := fs.String("body", "", "comment text (required)")
	ref := fs.String("ref", "", "branch to anchor on (defaults to the repo's default branch)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" || *lines == "" || strings.TrimSpace(*body) == "" {
		return errors.New("usage: moongit review create --path <p> --lines <n|a-b> --body <b> [--ref <branch>]")
	}
	start, end, err := parseLineSpec(*lines)
	if err != nil {
		return err
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.CreateCodeCommentRequest{
		Ref:       *ref,
		Path:      *path,
		StartLine: start,
		EndLine:   end,
		Body:      *body,
	})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/code-comments", target.server, target.owner, target.repo)
	resp, raw, err := httpDo(http.MethodPost, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("branch or path not found: %s", decodeError(raw))
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var c api.CodeComment
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	anchor := fmt.Sprintf("L%d", c.StartLine)
	if c.EndLine > c.StartLine {
		anchor = fmt.Sprintf("L%d-L%d", c.StartLine, c.EndLine)
	}
	fmt.Printf("comment #%d created — %s:%s (%s)\n", c.ID, c.Path, anchor, c.Ref)
	return nil
}

func runReviewList(args []string) error {
	fs := flag.NewFlagSet("review list", flag.ContinueOnError)
	ref := fs.String("ref", "", "branch to review (defaults to the repo's default branch)")
	path := fs.String("path", "", "scope to a single file path")
	state := fs.String("state", "open", "open | resolved | all")
	asJSON := fs.Bool("json", false, "emit the raw API JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *state {
	case "open", "resolved", "all":
	default:
		return fmt.Errorf("invalid --state %q (want open|resolved|all)", *state)
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	q := url.Values{}
	if *ref != "" {
		q.Set("ref", *ref)
	}
	if *path != "" {
		q.Set("path", *path)
	}
	q.Set("state", *state)
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/code-comments?%s", target.server, target.owner, target.repo, q.Encode())
	resp, raw, err := httpDo(http.MethodGet, endpoint, nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	if *asJSON {
		fmt.Println(string(raw))
		return nil
	}
	var comments []api.CodeComment
	if err := json.Unmarshal(raw, &comments); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(comments) == 0 {
		fmt.Println("(no comments)")
		return nil
	}
	for _, c := range comments {
		lines := fmt.Sprintf("L%d", c.StartLine)
		if c.EndLine > c.StartLine {
			lines = fmt.Sprintf("L%d-L%d", c.StartLine, c.EndLine)
		}
		flag := "open"
		if c.Resolved {
			flag = "resolved"
		}
		fmt.Printf("#%d  %s:%s  @%s  [%s]  (%s)\n", c.ID, c.Path, lines, c.Author, flag, c.Ref)
		fmt.Printf("    %s\n", strings.ReplaceAll(c.Body, "\n", "\n    "))
		if c.Snippet != "" {
			fmt.Println("    ┄┄┄")
			for _, l := range strings.Split(c.Snippet, "\n") {
				fmt.Printf("    │ %s\n", l)
			}
		}
		fmt.Println()
	}
	return nil
}

func runReviewSetResolved(args []string, resolved bool) error {
	verb, past := "resolve", "resolved"
	if !resolved {
		verb, past = "reopen", "reopened"
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: moongit review %s <id>", verb)
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid comment id: %s", args[0])
	}
	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.UpdateCodeCommentRequest{Resolved: &resolved})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/code-comments/%d", target.server, target.owner, target.repo, id)
	resp, raw, err := httpDo(http.MethodPatch, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	fmt.Printf("comment #%d %s\n", id, past)
	return nil
}

func runReviewDelete(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongit review delete <id>")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid comment id: %s", args[0])
	}
	target, err := discoverTarget()
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/code-comments/%d", target.server, target.owner, target.repo, id)
	resp, raw, err := httpDo(http.MethodDelete, endpoint, nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	fmt.Printf("comment #%d deleted\n", id)
	return nil
}

// runCI dispatches `moongit ci <subcommand>`. CI subcommands are local-only
// (no server round-trip): they operate on the mgitci.yml in the working copy.
func runCI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongit ci <validate|run>")
	}
	switch args[0] {
	case "validate":
		return runCIValidate(args[1:])
	case "run":
		return runCITrigger(args[1:])
	default:
		return fmt.Errorf("unknown ci subcommand: %s", args[0])
	}
}

// runCITrigger starts a CI run for a ref (branch, tag, or commit SHA) without a
// push — the on-demand counterpart to push-driven CI. The server resolves the
// ref against the repo and enqueues a run with event "manual".
func runCITrigger(args []string) error {
	fs := flag.NewFlagSet("ci run", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: moongit ci run <ref>")
	}
	ref := fs.Arg(0)

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.TriggerCIRunRequest{Ref: ref})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/runs", target.server, target.owner, target.repo)
	resp, raw, err := httpDo(http.MethodPost, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusConflict {
		return errors.New("CI is disabled for this repo")
	}
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var run api.CIRun
	if err := json.Unmarshal(raw, &run); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	sha := run.CommitSHA
	if len(sha) > 12 {
		sha = sha[:12]
	}
	fmt.Printf("run #%d queued — %s @ %s [%s]\n", run.Number, run.Ref, sha, run.Status)
	return nil
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
	for _, h := range ci.ToolchainHints(pipeline) {
		fmt.Printf("  warning: job %q runs %q but pins no image: — the default CI image is toolchain-free, "+
			"so this fails at run time with %q not found; set image: to one carrying %s (see ci/Dockerfile.dev)\n",
			h.Job, h.Tool, h.Tool, h.Tool)
	}
	return nil
}

// runRepo dispatches `moongit repo <subcommand>`. Repo-level operations target
// a repo by its explicit <owner>/<name>, not the current checkout's remote —
// you typically delete a repo other than the one you're standing in.
func runRepo(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongit repo <delete>")
	}
	switch args[0] {
	case "delete":
		return runRepoDelete(args[1:])
	default:
		return fmt.Errorf("unknown repo subcommand: %s", args[0])
	}
}

// runRepoDelete deletes a repo and everything under it — issues, runs,
// comments, pulls, and the on-disk git data. Irreversible, so it confirms
// interactively unless --yes is given. The target repo is the explicit
// <owner>/<name> argument; only the server URL is derived from the local
// remote (or MOONGIT_SERVER), so it works from any checkout.
func runRepoDelete(args []string) error {
	// The <owner>/<name> positional comes first; flags are parsed from what
	// follows it. (Go's flag package stops at the first non-flag arg, so a
	// trailing --yes would otherwise be left unparsed.)
	if len(args) < 1 {
		return errors.New("usage: moongit repo delete <owner>/<name> [--yes]")
	}
	owner, repo, err := splitOwnerRepo(args[0])
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("repo delete", flag.ContinueOnError)
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
		fmt.Printf("Delete repo %s/%s and ALL its issues, runs, comments, and git data? This cannot be undone. [y/N]: ", owner, repo)
		var answer string
		_, _ = fmt.Scanln(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("aborted")
			return nil
		}
	}

	endpoint := fmt.Sprintf("%s/api/repos/%s/%s", target.server, owner, repo)
	resp, raw, err := httpDo(http.MethodDelete, endpoint, nil, "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("repo not found: %s/%s", owner, repo)
	}
	// In-flight CI/agent runs block deletion; the server's message names the
	// count, so surface it as-is rather than a bare status code.
	if resp.StatusCode == http.StatusConflict {
		return errors.New(decodeError(raw))
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	fmt.Printf("%s/%s  deleted\n", owner, repo)
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
