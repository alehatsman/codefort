package server

import (
	"context"
	"net/http"
	"regexp"
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

	ref, ok := s.resolveRef(w, r, repoDir)
	if !ok {
		return
	}
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
		ref,
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

// handleIssueCommits returns commits whose message references this issue,
// newest first, across all branches. A commit references issue N when its
// message contains "#N" on a non-digit boundary (GitHub convention), so "#12"
// does not match "#123". Returns an empty list for an unborn repo or no match.
func (s *Server) handleIssueCommits(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	out := []api.Commit{}
	if !hasCommits(r.Context(), repoDir) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	// The leading '#' anchors the left boundary (a digit before N would not be
	// preceded by '#'); the trailing class rejects a longer number.
	pattern := "#" + strconv.Itoa(num) + "([^0-9]|$)"
	raw, err := gitOutput(r.Context(), repoDir,
		"log", "--all", "-E", "--grep="+pattern,
		"--format="+commitFormat,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git log failed")
		return
	}
	if commits := parseCommits(raw); len(commits) > 0 {
		def := headRef(r.Context(), repoDir)
		for i := range commits {
			commits[i].Branch = primaryBranch(r.Context(), repoDir, commits[i].SHA, def)
		}
		out = commits
	}
	writeJSON(w, http.StatusOK, out)
}

// shaPattern bounds the {sha} path segment to hex so it can't smuggle git
// options (a leading dash) or refspecs into the plumbing commands below. Short
// (abbreviated) shas down to 4 chars are allowed; git resolves them.
var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// hunkHeaderRe parses a unified-diff hunk header: "@@ -old,n +new,m @@ trailer".
// The line counts are optional (a single-line hunk omits them); the trailer is
// the enclosing function/section git prints after the second @@.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$`)

// maxDiffLines caps the total number of diff lines parsed across all files in
// one commit, so a pathological commit can't blow up the response. When the
// budget is hit, remaining lines are dropped and CommitDetail.Truncated is set.
const maxDiffLines = 20000

// handleCommit returns one commit's metadata plus its diff against the first
// parent — the first parent for a merge, the empty tree for a root commit —
// parsed into structured per-file hunks for the side-by-side diff view.
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	sha := r.PathValue("sha")
	if !shaPattern.MatchString(sha) {
		writeError(w, http.StatusBadRequest, "invalid commit sha")
		return
	}
	if !hasCommits(r.Context(), repoDir) {
		writeError(w, http.StatusNotFound, "commit not found: "+sha)
		return
	}

	raw, err := gitOutput(r.Context(), repoDir, "log", "-1", "--format="+commitFormat, sha)
	if err != nil {
		writeError(w, http.StatusNotFound, "commit not found: "+sha)
		return
	}
	commits := parseCommits(raw)
	if len(commits) == 0 {
		writeError(w, http.StatusNotFound, "commit not found: "+sha)
		return
	}

	commits[0].Branch = primaryBranch(r.Context(), repoDir, sha, headRef(r.Context(), repoDir))

	parents := commitParents(r.Context(), repoDir, sha)

	// Diff against the first parent (--root for a parentless commit). The
	// explicit two-tree form makes a merge diff against its first parent rather
	// than producing the empty default merge diff.
	args := []string{"diff-tree", "--no-commit-id", "-p", "-r", "-M", "--no-color"}
	if len(parents) == 0 {
		args = append(args, "--root", sha)
	} else {
		args = append(args, parents[0], sha)
	}
	patch, err := gitOutput(r.Context(), repoDir, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "diff failed")
		return
	}

	files, truncated := parseUnifiedDiff(patch, maxDiffLines)
	detail := api.CommitDetail{
		Commit:    commits[0],
		Parents:   parents,
		Files:     files,
		Truncated: truncated,
	}
	for _, f := range files {
		detail.Additions += f.Additions
		detail.Deletions += f.Deletions
	}
	writeJSON(w, http.StatusOK, detail)
}

// emptyTreeSHA is git's well-known SHA-1 hash of the empty tree, used as the
// diff base when two branches share no history (no merge base) so the compare
// still renders the whole head tree as additions.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// handleCompare returns the three-dot diff of head relative to base: the
// changes head introduces since merge-base(base, head), plus the ahead/behind
// commit counts and the list of commits base..head. This is the read-only
// foundation of the PR workflow (#70) and is useful on its own as a compare
// view. No PR object is created.
func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	base := strings.TrimSpace(r.URL.Query().Get("base"))
	head := strings.TrimSpace(r.URL.Query().Get("head"))
	if base == "" || head == "" {
		writeError(w, http.StatusBadRequest, "base and head are required")
		return
	}
	// Validate both against refs/heads — the same injection guard resolveRef
	// uses, since the values are interpolated into revision args below.
	if !branchExists(r.Context(), repoDir, base) {
		writeError(w, http.StatusNotFound, "branch not found: "+base)
		return
	}
	if !branchExists(r.Context(), repoDir, head) {
		writeError(w, http.StatusNotFound, "branch not found: "+head)
		return
	}

	out, err := computeCompare(r.Context(), repoDir, base, head)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "diff failed")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// computeCompare builds the three-dot compare of head relative to base. Callers
// must validate base/head against refs/heads first (the values are interpolated
// into the revision args). Commits and Files are always non-nil so the JSON
// renders arrays. An error is returned only if the diff itself fails; the
// ahead/behind/commit probes degrade silently to zero/empty.
func computeCompare(ctx context.Context, repoDir, base, head string) (api.Compare, error) {
	out := api.Compare{Base: base, Head: head, Commits: []api.Commit{}, Files: []api.DiffFile{}}

	// merge-base(base, head): the point the diff is taken against. Missing means
	// unrelated histories — diff the whole head tree against the empty tree.
	if mb, err := gitOutput(ctx, repoDir, "merge-base", base, head); err == nil {
		out.MergeBase = strings.TrimSpace(string(mb))
	}
	diffBase := out.MergeBase
	if diffBase == "" {
		diffBase = emptyTreeSHA
	}

	// ahead/behind: `rev-list --left-right --count base...head` prints
	// "<behind>\t<ahead>" (left = base-only, right = head-only).
	if raw, err := gitOutput(ctx, repoDir, "rev-list", "--left-right", "--count", base+"..."+head); err == nil {
		if f := strings.Fields(string(raw)); len(f) == 2 {
			out.Behind, _ = strconv.Atoi(f[0])
			out.Ahead, _ = strconv.Atoi(f[1])
		}
	}

	// Commits head has that base does not (base..head), newest first.
	if raw, err := gitOutput(ctx, repoDir, "log", "--format="+commitFormat, base+".."+head); err == nil {
		if commits := parseCommits(raw); len(commits) > 0 {
			out.Commits = commits
		}
	}

	patch, err := gitOutput(ctx, repoDir, "diff-tree", "--no-commit-id", "-p", "-r", "-M", "--no-color", diffBase, head)
	if err != nil {
		return out, err
	}
	files, truncated := parseUnifiedDiff(patch, maxDiffLines)
	if len(files) > 0 {
		out.Files = files
	}
	out.Truncated = truncated
	for _, f := range files {
		out.Additions += f.Additions
		out.Deletions += f.Deletions
	}
	return out, nil
}

// commitParents returns the parent SHAs of sha (empty for a root commit).
// `rev-list --parents -n 1` prints "<sha> <parent1> <parent2> ...".
func commitParents(ctx context.Context, repoDir, sha string) []string {
	raw, err := gitOutput(ctx, repoDir, "rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(raw))
	if len(fields) <= 1 {
		return nil
	}
	return fields[1:]
}

// parseUnifiedDiff turns `git diff-tree -p` output into structured per-file
// diffs, tracking old/new line numbers per line so the frontend can render a
// split (side-by-side) view. Parsing stops appending lines once the budget is
// hit and reports truncated=true; file entries are still emitted (just without
// the dropped hunks).
func parseUnifiedDiff(patch []byte, budget int) (files []api.DiffFile, truncated bool) {
	var cur *api.DiffFile
	var hunk *api.DiffHunk
	oldLine, newLine, total := 0, 0, 0

	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for ln := range strings.SplitSeq(string(patch), "\n") {
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			flushFile()
			old, nw := parseDiffGitPaths(ln)
			cur = &api.DiffFile{OldPath: old, NewPath: nw, Status: "modified"}
		case cur == nil:
			// Preamble before the first file header — ignore.
			continue
		case strings.HasPrefix(ln, "@@"):
			flushHunk()
			os, ns, hdr := parseHunkHeader(ln)
			oldLine, newLine = os, ns
			hunk = &api.DiffHunk{Header: hdr}
		case hunk != nil:
			// Inside a hunk: classify the line by its leading marker. A "\ No
			// newline at end of file" marker (and the trailing empty split
			// element) carry no line and leave the counters untouched.
			if ln == "" || ln[0] == '\\' {
				continue
			}
			if total >= budget {
				truncated = true
				continue
			}
			dl := api.DiffLine{Text: ln[1:]}
			switch ln[0] {
			case '+':
				dl.Kind, dl.New = "add", newLine
				newLine++
				cur.Additions++
			case '-':
				dl.Kind, dl.Old = "del", oldLine
				oldLine++
				cur.Deletions++
			default: // ' ' context
				dl.Kind, dl.Old, dl.New = "context", oldLine, newLine
				oldLine++
				newLine++
			}
			hunk.Lines = append(hunk.Lines, dl)
			total++
		case strings.HasPrefix(ln, "new file mode"):
			cur.Status = "added"
		case strings.HasPrefix(ln, "deleted file mode"):
			cur.Status = "deleted"
		case strings.HasPrefix(ln, "rename from "):
			cur.Status = "renamed"
			cur.OldPath = strings.TrimPrefix(ln, "rename from ")
		case strings.HasPrefix(ln, "rename to "):
			cur.Status = "renamed"
			cur.NewPath = strings.TrimPrefix(ln, "rename to ")
		case strings.HasPrefix(ln, "Binary files "):
			cur.Binary = true
		case strings.HasPrefix(ln, "--- "):
			if p := strings.TrimPrefix(ln, "--- "); p != "/dev/null" {
				cur.OldPath = stripDiffPathPrefix(p)
			}
		case strings.HasPrefix(ln, "+++ "):
			if p := strings.TrimPrefix(ln, "+++ "); p != "/dev/null" {
				cur.NewPath = stripDiffPathPrefix(p)
			}
		}
	}
	flushFile()
	return files, truncated
}

// parseHunkHeader extracts the 1-based old/new start lines and the trailing
// section header from an @@ line, defaulting to (1, 1, "") when malformed.
func parseHunkHeader(ln string) (oldStart, newStart int, header string) {
	m := hunkHeaderRe.FindStringSubmatch(ln)
	if m == nil {
		return 1, 1, ""
	}
	oldStart, _ = strconv.Atoi(m[1])
	newStart, _ = strconv.Atoi(m[2])
	return oldStart, newStart, m[3]
}

// parseDiffGitPaths recovers the old/new paths from a "diff --git a/x b/y"
// line. The --- / +++ / rename lines are the authoritative source; this is the
// fallback for entries that lack them (binary or mode-only changes). Paths with
// a literal " b/" substring are ambiguous in this form, but git emits the
// authoritative lines for those cases.
func parseDiffGitPaths(ln string) (old, nw string) {
	s := strings.TrimPrefix(ln, "diff --git ")
	if i := strings.Index(s, " b/"); i >= 0 {
		return stripDiffPathPrefix(s[:i]), stripDiffPathPrefix(s[i+1:])
	}
	return "", ""
}

// stripDiffPathPrefix drops the a/ or b/ prefix git puts on diff paths.
func stripDiffPathPrefix(p string) string {
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
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

	ref, ok := s.resolveRef(w, r, repoDir)
	if !ok {
		return
	}
	out := api.TreeCommits{Ref: ref, Path: p, Entries: map[string]api.Commit{}}

	if !hasCommits(r.Context(), repoDir) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	out.Total = countCommits(r.Context(), repoDir, ref, p)
	out.Latest = lastCommit(r.Context(), repoDir, ref, p)

	// Immediate children of the listed tree (basenames).
	treeish := ref + ":" + p
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
			if c := lastCommit(r.Context(), repoDir, ref, full); c != nil {
				mu.Lock()
				out.Entries[full] = *c
				mu.Unlock()
			}
		}(full)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, out)
}

// lastCommit returns the most recent commit on ref touching path (the whole
// repo when path is empty), or nil if none / on error.
func lastCommit(ctx context.Context, repoDir, ref, path string) *api.Commit {
	args := []string{"log", "-1", "--format=" + commitFormat, ref}
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

// countCommits returns the number of commits reachable from ref, scoped to
// path when set. Returns 0 on error.
func countCommits(ctx context.Context, repoDir, ref, path string) int {
	args := []string{"rev-list", "--count", ref}
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
