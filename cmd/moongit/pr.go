package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// runPR dispatches `moongit pr <subcommand>` — a thin client over the PR data
// plane and merge endpoint, mirroring `moongit issue` / `moongit review`.
func runPR(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongit pr <create|list|show|merge|close|reopen>")
	}
	switch args[0] {
	case "create":
		return runPRCreate(args[1:])
	case "list":
		return runPRList(args[1:])
	case "show":
		return runPRShow(args[1:])
	case "merge":
		return runPRMerge(args[1:])
	case "close":
		return runPRSetState(args[1:], api.PRClosed)
	case "reopen":
		return runPRSetState(args[1:], api.PROpen)
	default:
		return fmt.Errorf("unknown pr subcommand: %s", args[0])
	}
}

func runPRCreate(args []string) error {
	fs := flag.NewFlagSet("pr create", flag.ContinueOnError)
	base := fs.String("base", "", "base branch to merge into (required)")
	head := fs.String("head", "", "head branch with the changes (required)")
	title := fs.String("title", "", "PR title (required)")
	body := fs.String("body", "", "PR body")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *base == "" || *head == "" || strings.TrimSpace(*title) == "" {
		return errors.New("usage: moongit pr create --base <ref> --head <ref> --title <t> [--body <b>]")
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.CreatePullRequest{
		Base: *base, Head: *head, Title: *title, Body: *body,
	})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/pulls", target.server, target.owner, target.repo)
	resp, raw, err := httpDo(http.MethodPost, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("branch not found: %s", decodeError(raw))
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var pr api.PullRequest
	if err := json.Unmarshal(raw, &pr); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("#%d  %s\n", pr.Number, pr.Title)
	fmt.Printf("        %s ← %s, by %s\n", pr.BaseRef, pr.HeadRef, pr.Author)
	return nil
}

func runPRList(args []string) error {
	fs := flag.NewFlagSet("pr list", flag.ContinueOnError)
	state := fs.String("state", "open", "open | merged | closed | all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *state {
	case "open", "merged", "closed", "all":
	default:
		return fmt.Errorf("invalid --state %q (want open|merged|closed|all)", *state)
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/pulls", target.server, target.owner, target.repo)
	if *state != "all" {
		q := url.Values{}
		q.Set("state", *state)
		endpoint += "?" + q.Encode()
	}
	resp, raw, err := httpDo(http.MethodGet, endpoint, nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var pulls []api.PullRequest
	if err := json.Unmarshal(raw, &pulls); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(pulls) == 0 {
		fmt.Println("(no pull requests)")
		return nil
	}
	for _, pr := range pulls {
		fmt.Printf("#%-4d  [%-6s]  %s ← %s  %s  — %s\n",
			pr.Number, pr.State, pr.BaseRef, pr.HeadRef, pr.Title, pr.Author)
	}
	return nil
}

func runPRShow(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongit pr show <number>")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid pull request number: %s", args[0])
	}
	target, err := discoverTarget()
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/pulls/%d", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodGet, endpoint, nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var d api.PullRequestDetail
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	fmt.Printf("#%d  %s  [%s]\n", d.Number, d.Title, d.State)
	fmt.Printf("merge:    %s ← %s\n", d.BaseRef, d.HeadRef)
	fmt.Printf("author:   %s\n", d.Author)
	fmt.Printf("created:  %s\n", d.CreatedAt.Local().Format(time.RFC3339))
	if d.MergedAt != nil {
		fmt.Printf("merged:   %s\n", d.MergedAt.Local().Format(time.RFC3339))
	}
	if d.Body != "" {
		fmt.Printf("\n%s\n", d.Body)
	}

	c := d.Compare
	fmt.Printf("\nCompare: %d commit(s), %d file(s), +%d -%d  (%d ahead, %d behind)\n",
		len(c.Commits), len(c.Files), c.Additions, c.Deletions, c.Ahead, c.Behind)
	if c.Truncated {
		fmt.Println("  (diff truncated)")
	}
	for _, commit := range c.Commits {
		fmt.Printf("  %s  %s\n", commit.ShortSHA, commit.Subject)
	}

	// Open (unresolved) review comments only — the actionable ones.
	var open []api.CodeComment
	for _, cc := range d.Comments {
		if !cc.Resolved {
			open = append(open, cc)
		}
	}
	if len(open) > 0 {
		fmt.Printf("\nOpen comments (%d):\n", len(open))
		for _, cc := range open {
			lines := fmt.Sprintf("L%d", cc.StartLine)
			if cc.EndLine > cc.StartLine {
				lines = fmt.Sprintf("L%d-L%d", cc.StartLine, cc.EndLine)
			}
			fmt.Printf("  #%d  %s:%s  @%s: %s\n", cc.ID, cc.Path, lines, cc.Author, cc.Body)
		}
	}
	return nil
}

func runPRMerge(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: moongit pr merge <number> [--ff-only]")
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid pull request number: %s", args[0])
	}

	fs := flag.NewFlagSet("pr merge", flag.ContinueOnError)
	ffOnly := fs.Bool("ff-only", false, "fast-forward only; fail if the branches have diverged")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra args: %v", fs.Args())
	}

	method := api.MergeCommitMethod
	if *ffOnly {
		method = api.MergeFFOnlyMethod
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.MergeRequest{Method: method})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/pulls/%d/merge", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodPost, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusConflict {
		// Distinguish a content conflict (carries paths) from the other 409s
		// (not fast-forwardable, nothing to merge, already merged).
		var cr api.MergeConflictResponse
		if json.Unmarshal(raw, &cr) == nil && len(cr.Conflicts) > 0 {
			return fmt.Errorf("merge conflict in:\n  %s\nresolve locally and push, then retry",
				strings.Join(cr.Conflicts, "\n  "))
		}
		return errors.New(decodeError(raw))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var res api.MergeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	how := "merge commit"
	if res.FastForward {
		how = "fast-forward"
	}
	sha := res.MergeCommit
	if len(sha) > 12 {
		sha = sha[:12]
	}
	fmt.Printf("#%d  merged (%s) into %s @ %s\n", res.Number, how, res.BaseRef, sha)
	return nil
}

// runPRSetState backs `pr close` (→ closed) and `pr reopen` (→ open): a state
// flip over the PR update endpoint (PATCH). Transitioning to "merged" is not
// reachable here — that's the merge endpoint — and the server owns transition
// validity (e.g. a merged PR can't be reopened), so we just surface its error.
func runPRSetState(args []string, state api.PRState) error {
	verb := "close"
	if state == api.PROpen {
		verb = "reopen"
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: moongit pr %s <number>", verb)
	}
	num, err := strconv.Atoi(args[0])
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid pull request number: %s", args[0])
	}
	if len(args) > 1 {
		return fmt.Errorf("unexpected extra args: %v", args[1:])
	}

	target, err := discoverTarget()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(api.UpdatePullRequest{State: &state})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/repos/%s/%s/pulls/%d", target.server, target.owner, target.repo, num)
	resp, raw, err := httpDo(http.MethodPatch, endpoint, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(raw))
	}
	var pr api.PullRequest
	if err := json.Unmarshal(raw, &pr); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("#%d  %s  [%s]\n", pr.Number, pr.Title, pr.State)
	return nil
}
