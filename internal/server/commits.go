package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// commitFormat lays out one commit per `git log` record using control-byte
// separators so neither field values nor multi-line bodies can break parsing:
// fields are joined with US (0x1f) and each record is terminated with RS
// (0x1e). Order matches parseCommits: SHA, short SHA, author, email, ISO date,
// subject, body.
const commitFormat = "%H%x1f%h%x1f%an%x1f%ae%x1f%aI%x1f%s%x1f%b%x1e"

// defaultCommitsPerPage / maxCommitsPerPage bound the commit-list page size.
const (
	defaultCommitsPerPage = 30
	maxCommitsPerPage     = 100
	// treeCommitWorkers caps concurrent `git log -1` probes when annotating a
	// directory listing, so a wide directory can't fork an unbounded number of
	// git processes at once.
	treeCommitWorkers = 16
)

// handleCommits returns a page of commit history on the default branch, newest
// first, optionally filtered to commits touching ?path=. Pagination is
// ?page= (1-based) and ?per_page=. An unborn repo returns an empty list.
func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	p, err := cleanTreePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	perPage := clampInt(parseIntDefault(r.URL.Query().Get("per_page"), defaultCommitsPerPage), 1, maxCommitsPerPage)
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}

	ref := headRef(r.Context(), repoDir)
	out := api.CommitList{Ref: ref, Path: p, Commits: []api.Commit{}}

	if !hasCommits(r.Context(), repoDir) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Fetch one extra so we can report HasMore without a second count query.
	skip := (page - 1) * perPage
	args := []string{
		"log",
		"--format=" + commitFormat,
		"--max-count=" + strconv.Itoa(perPage+1),
		"--skip=" + strconv.Itoa(skip),
		"HEAD",
	}
	if p != "" {
		args = append(args, "--", p)
	}

	raw, err := gitOutput(r.Context(), repoDir, args...)
	if err != nil {
		writeError(w, http.StatusNotFound, "path not found: "+p)
		return
	}

	commits := parseCommits(raw)
	if len(commits) > perPage {
		out.HasMore = true
		commits = commits[:perPage]
	}
	out.Commits = commits
	writeJSON(w, http.StatusOK, out)
}

// handleTreeCommits annotates the directory listing at ?path= with commit
// context: the last commit touching each immediate child, the directory's own
// latest commit, and the total commit count on the branch. The UI overlays
// this on the (faster) tree listing, so per-file lookups live here rather than
// slowing handleTree.
func (s *Server) handleTreeCommits(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	p, err := cleanTreePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ref := headRef(r.Context(), repoDir)
	out := api.TreeCommits{Ref: ref, Path: p, Entries: map[string]api.Commit{}}

	if !hasCommits(r.Context(), repoDir) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	out.Total = countCommits(r.Context(), repoDir, p)
	out.Latest = lastCommit(r.Context(), repoDir, p)

	// Immediate children of the listed tree (basenames).
	treeish := "HEAD:" + p
	raw, err := gitOutput(r.Context(), repoDir, "ls-tree", "--name-only", "-z", treeish)
	if err != nil {
		writeError(w, http.StatusNotFound, "path not found: "+p)
		return
	}

	var children []string
	for name := range strings.SplitSeq(string(raw), "\x00") {
		if name == "" {
			continue
		}
		full := name
		if p != "" {
			full = p + "/" + name
		}
		children = append(children, full)
	}

	// Probe each child's last commit concurrently, bounded by a worker pool.
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, treeCommitWorkers)
	)
	for _, full := range children {
		wg.Add(1)
		sem <- struct{}{}
		go func(full string) {
			defer wg.Done()
			defer func() { <-sem }()
			if c := lastCommit(r.Context(), repoDir, full); c != nil {
				mu.Lock()
				out.Entries[full] = *c
				mu.Unlock()
			}
		}(full)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, out)
}

// lastCommit returns the most recent commit touching path (the whole repo when
// path is empty), or nil if none / on error.
func lastCommit(ctx context.Context, repoDir, path string) *api.Commit {
	args := []string{"log", "-1", "--format=" + commitFormat, "HEAD"}
	if path != "" {
		args = append(args, "--", path)
	}
	raw, err := gitOutput(ctx, repoDir, args...)
	if err != nil {
		return nil
	}
	commits := parseCommits(raw)
	if len(commits) == 0 {
		return nil
	}
	return &commits[0]
}

// countCommits returns the number of commits reachable from HEAD, scoped to
// path when set. Returns 0 on error.
func countCommits(ctx context.Context, repoDir, path string) int {
	args := []string{"rev-list", "--count", "HEAD"}
	if path != "" {
		args = append(args, "--", path)
	}
	raw, err := gitOutput(ctx, repoDir, args...)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return n
}

// parseCommits decodes a `git log --format=commitFormat` stream. Records are
// RS-terminated and fields US-separated (see commitFormat). Malformed records
// are skipped.
func parseCommits(raw []byte) []api.Commit {
	var commits []api.Commit
	for rec := range strings.SplitSeq(string(raw), "\x1e") {
		// git separates records with a newline after the format; strip it so
		// the leading field of the next record is clean.
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x1f")
		if len(fields) < 7 {
			continue
		}
		c := api.Commit{
			SHA:      fields[0],
			ShortSHA: fields[1],
			Author:   fields[2],
			Email:    fields[3],
			Subject:  fields[5],
			Body:     strings.TrimRight(fields[6], "\n"),
		}
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[4])); err == nil {
			c.Date = t
		}
		commits = append(commits, c)
	}
	return commits
}

// parseIntDefault parses s as an int, returning def when empty or invalid.
func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// clampInt clamps n to [lo, hi].
func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
