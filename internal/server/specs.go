package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/specs"
	"github.com/alehatsman/codefort/internal/storage"
)

// specsDir is the conventional in-repo directory holding markdown specs.
const specsDir = "specs"

// handleListSpecs lists the markdown specs under specs/ on the selected ref,
// each with its parsed frontmatter metadata. It mirrors the read-handler shape
// in tree.go: ?ref= selects the branch (default branch otherwise). A repo with
// no specs/ directory (or no commits) returns an empty list, not a 404, so the
// Specs tab can render its empty state.
func (s *Server) handleListSpecs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}
	ref, ok := s.resolveRef(w, r, repoDir)
	if !ok {
		return
	}

	out := api.SpecList{Ref: ref, Specs: []api.SpecListItem{}}
	for _, full := range s.specBlobPaths(r.Context(), repoDir, ref) {
		content, err := gitOutput(r.Context(), repoDir, "cat-file", "blob", ref+":"+full)
		if err != nil {
			s.logger.Error("read spec", "repo", repoDir, "path", full, "err", err)
			continue
		}
		out.Specs = append(out.Specs, specListItem(full, content))
	}

	sort.Slice(out.Specs, func(i, j int) bool {
		return out.Specs[i].Path < out.Specs[j].Path
	})
	writeJSON(w, http.StatusOK, out)
}

// specBlobPaths returns the repo-relative paths of every .md blob under specs/
// on the ref, in git's listing order. An unborn repo or a missing specs/ dir
// yields nil (callers render an empty list, not an error).
func (s *Server) specBlobPaths(ctx context.Context, repoDir, ref string) []string {
	if !hasCommits(ctx, repoDir) {
		return nil
	}
	// "<ref>:specs" addresses that tree; when specs/ is absent the treeish
	// doesn't resolve — nil, not an error. Names are relative to specs/.
	raw, err := gitOutput(ctx, repoDir, "ls-tree", "-r", "-z", "--name-only", ref+":"+specsDir)
	if err != nil {
		return nil
	}
	var paths []string
	for rel := range strings.SplitSeq(string(raw), "\x00") {
		if rel == "" || !strings.EqualFold(path.Ext(rel), ".md") {
			continue
		}
		paths = append(paths, specsDir+"/"+rel)
	}
	return paths
}

// handleGetSpec returns one spec's content and parsed structure. The path is
// the trailing {path...} segment, taken relative to specs/ (so the URL
// .../specs/ci/pipeline.md reads specs/ci/pipeline.md). It is a dedicated
// handler rather than a /blob wrapper because the response carries the parsed
// frontmatter, sections, and checklist the renderer and truth gutter consume —
// not just bytes. ?ref= selects the branch like the other read handlers.
func (s *Server) handleGetSpec(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	rel, err := cleanTreePath(r.PathValue("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if rel == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	full := specsDir + "/" + rel

	ref, ok := s.resolveRef(w, r, repoDir)
	if !ok {
		return
	}
	treeish := ref + ":" + full

	typ, err := gitOutput(r.Context(), repoDir, "cat-file", "-t", treeish)
	if err != nil {
		writeError(w, http.StatusNotFound, "spec not found: "+full)
		return
	}
	switch strings.TrimSpace(string(typ)) {
	case "blob":
		// ok
	case "tree":
		writeError(w, http.StatusBadRequest, "path is a directory: "+full)
		return
	default:
		writeError(w, http.StatusNotFound, "spec not found: "+full)
		return
	}

	sizeOut, err := gitOutput(r.Context(), repoDir, "cat-file", "-s", treeish)
	if err != nil {
		s.logger.Error("spec size", "repo", repoDir, "path", full, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if size, _ := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64); size > maxBlobBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "spec too large")
		return
	}

	content, err := gitOutput(r.Context(), repoDir, "cat-file", "blob", treeish)
	if err != nil {
		s.logger.Error("spec content", "repo", repoDir, "path", full, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := specContent(ref, full, content)
	out.Verification = s.latestVerification(r.Context(), repoDir, repoID, ref, full, content)
	writeJSON(w, http.StatusOK, out)
}

// latestVerification loads a spec's most recent verify pass for the read view,
// or nil when the spec is unparseable or never verified. Stale is true when the
// spec file or its covered code changed since the verified commit — so a spec
// that's drifted since its last green check still looks stale.
func (s *Server) latestVerification(ctx context.Context, repoDir string, repoID int64, ref, full string, content []byte) *api.SpecVerification {
	spec, err := specs.Parse(full, content)
	if err != nil {
		return nil
	}
	rec, err := storage.LatestVerification(s.rdb, repoID, spec.Frontmatter.ID)
	if err != nil {
		return nil // ErrNotFound (never verified) or a read error — show nothing
	}

	var res specs.VerificationResult
	_ = json.Unmarshal([]byte(rec.Result), &res)

	out := &api.SpecVerification{
		Alignment:  rec.Alignment,
		Conflicts:  res.Conflicts,
		Notes:      res.Notes,
		VerifiedAt: time.Unix(rec.CreatedAt, 0).UTC().Format(time.RFC3339),
		Commit:     rec.CommitSHA,
	}
	for _, m := range res.Markers {
		out.Markers = append(out.Markers, api.SpecVerificationMarker{
			Line: m.Line, Text: m.Text, Marker: string(m.Marker), Note: m.Note,
		})
	}

	// Stale when the spec itself or any governed path changed since the verified
	// commit. A failed diff (gone commit) also reads as stale, never falsely fresh.
	globs := append([]string{full}, spec.Frontmatter.Covers...)
	changed, derr := gitDiffGlobs(ctx, repoDir, rec.CommitSHA, ref, globs)
	out.Stale = derr != nil || len(changed) > 0
	return out
}

// specContent parses a spec blob into the full content response. As with the
// list endpoint, a malformed-frontmatter spec is returned best-effort (raw
// content, path-derived title, no structure) so the editor can still open and
// fix it rather than getting a hard error.
func specContent(ref, specPath string, content []byte) api.SpecContent {
	out := api.SpecContent{
		Ref:       ref,
		Path:      specPath,
		Content:   string(content),
		Sections:  []api.SpecSection{},
		Checklist: []api.SpecChecklistItem{},
	}

	spec, err := specs.Parse(specPath, content)
	if err != nil {
		title := strings.TrimSuffix(path.Base(specPath), path.Ext(specPath))
		out.ID, out.Title, out.Body = title, title, string(content)
		return out
	}

	fm := spec.Frontmatter
	out.ID = fm.ID
	out.Title = spec.Title
	out.Status = string(fm.Status)
	out.Owners = fm.Owners
	out.Covers = fm.Covers
	out.LastVerified = fm.LastVerified
	out.Alignment = fm.Alignment
	out.Body = spec.Body
	for _, sec := range spec.Sections {
		out.Sections = append(out.Sections, api.SpecSection{
			Title: sec.Title, Level: sec.Level, Body: sec.Body, Line: sec.Line,
		})
	}
	for _, it := range spec.Checklist {
		out.Checklist = append(out.Checklist, api.SpecChecklistItem{
			Text: it.Text, Checked: it.Checked, Line: it.Line,
		})
	}
	return out
}

// specListItem parses a spec blob into its list metadata. A spec whose
// frontmatter is malformed YAML is still listed — with a path-derived title and
// no metadata — rather than dropped, so a broken spec stays visible (and
// fixable) in the tab instead of silently vanishing.
func specListItem(specPath string, content []byte) api.SpecListItem {
	spec, err := specs.Parse(specPath, content)
	if err != nil {
		title := strings.TrimSuffix(path.Base(specPath), path.Ext(specPath))
		return api.SpecListItem{Path: specPath, ID: title, Title: title}
	}
	fm := spec.Frontmatter
	return api.SpecListItem{
		Path:         spec.Path,
		ID:           fm.ID,
		Title:        spec.Title,
		Status:       string(fm.Status),
		Owners:       fm.Owners,
		Covers:       fm.Covers,
		LastVerified: fm.LastVerified,
		Alignment:    fm.Alignment,
	}
}

// specSectionAt returns the title of the spec heading enclosing line — the
// nearest heading at or before it — or "" when the spec can't be read/parsed or
// nothing precedes the line. Parsed sections are cached per path so a spec with
// several hits is read once.
func (s *Server) specSectionAt(
	ctx context.Context,
	repoDir, ref, specPath string,
	line int,
	cache map[string][]specs.Section,
) string {
	secs, ok := cache[specPath]
	if !ok {
		// Cache even on failure (nil) so a bad spec isn't re-read per hit.
		if content, err := gitOutput(ctx, repoDir, "cat-file", "blob", ref+":"+specPath); err == nil {
			if spec, perr := specs.Parse(specPath, content); perr == nil {
				secs = spec.Sections
			}
		}
		cache[specPath] = secs
	}
	title, best := "", 0
	for _, sec := range secs {
		if sec.Line <= line && sec.Line >= best {
			title, best = sec.Title, sec.Line
		}
	}
	return title
}

// zeroOID is git's "ref must not exist" sentinel for an update-ref CAS — used to
// create a branch atomically (fails if something raced us to create it).
const zeroOID = "0000000000000000000000000000000000000000"

// handleWriteSpec commits spec content to a feature branch and returns the
// branch + commit so the UI can open a PR. The write happens in the bare repo
// without a worktree (hash-object → temp-index tree → commit-tree → update-ref),
// the same worktree-free style as the merge handler. It never writes to the
// repo's default branch — specs reach main via a PR, honoring "never auto-push
// main". Identity comes from the request token, stamped on the commit.
func (s *Server) handleWriteSpec(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	rel, err := cleanTreePath(r.PathValue("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if rel == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if !strings.EqualFold(path.Ext(rel), ".md") {
		writeError(w, http.StatusBadRequest, "spec path must end in .md")
		return
	}
	full := specsDir + "/" + rel

	var req api.WriteSpecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	def := headRef(r.Context(), repoDir)
	base := req.Base
	if base == "" {
		base = def
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = "spec/" + strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	}
	// Specs land via a PR; refuse to commit straight onto the default branch.
	if branch == def {
		writeError(w, http.StatusBadRequest, "refusing to write specs directly to the default branch "+def+"; use a feature branch")
		return
	}

	// The commit's parent is the target branch tip if it exists (extend it),
	// otherwise the base branch tip (start it). created drives the ref CAS below.
	created := !branchExists(r.Context(), repoDir, branch)
	parentRef := "refs/heads/" + branch
	if created {
		if !branchExists(r.Context(), repoDir, base) {
			writeError(w, http.StatusNotFound, "base branch not found: "+base)
			return
		}
		parentRef = "refs/heads/" + base
	}
	parent, err := revParse(r.Context(), repoDir, parentRef)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve branch failed")
		return
	}
	baseTree, err := revParse(r.Context(), repoDir, parent+"^{tree}")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve tree failed")
		return
	}

	blob, err := hashObject(r.Context(), repoDir, []byte(req.Content))
	if err != nil {
		s.logger.Error("spec hash-object", "repo", repoDir, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tree, err := writeTreeWithBlob(r.Context(), repoDir, baseTree, blob, full)
	if err != nil {
		s.logger.Error("spec write-tree", "repo", repoDir, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tree == baseTree {
		writeError(w, http.StatusConflict, "spec content is unchanged")
		return
	}

	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = "docs(specs): update " + full
	}
	commit, err := commitTree(r.Context(), repoDir, tree, msg, identityFromContext(r), []string{parent})
	if err != nil {
		s.logger.Error("spec commit-tree", "repo", repoDir, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// CAS the ref: create-if-absent (zeroOID guard) or extend the existing tip.
	old := parent
	if created {
		old = zeroOID
	}
	if err := updateRef(r.Context(), repoDir, "refs/heads/"+branch, commit, old); err != nil {
		s.logger.Warn("spec update-ref", "repo", repoDir, "branch", branch, "err", err)
		writeError(w, http.StatusConflict, "branch "+branch+" moved during write; retry")
		return
	}

	writeJSON(w, http.StatusOK, api.WriteSpecResult{Branch: branch, Commit: commit, Created: created})
}

// hashObject writes content to the object store as a blob and returns its OID.
func hashObject(ctx context.Context, repoDir string, content []byte) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "hash-object", "-w", "--stdin")
	cmd.Dir = repoDir
	cmd.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("git hash-object: " + err.Error() + ": " + stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// writeTreeWithBlob produces a new tree equal to baseTree but with path set to
// blobOID, using a throwaway index (no worktree touched). Returns the new tree
// OID — equal to baseTree when the blob already matched (no change).
func writeTreeWithBlob(ctx context.Context, repoDir, baseTree, blobOID, path string) (string, error) {
	idx, err := os.CreateTemp("", "codefort-index-*")
	if err != nil {
		return "", err
	}
	idxPath := idx.Name()
	_ = idx.Close()
	defer func() { _ = os.Remove(idxPath) }()

	env := append(os.Environ(), "GIT_INDEX_FILE="+idxPath)
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repoDir
		cmd.Env = env
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, errors.New("git " + args[0] + ": " + err.Error() + ": " + stderr.String())
		}
		return out, nil
	}
	if _, err := run("read-tree", baseTree); err != nil {
		return "", err
	}
	if _, err := run("update-index", "--add", "--cacheinfo", "100644,"+blobOID+","+path); err != nil {
		return "", err
	}
	out, err := run("write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// handleSpecsDrift reports each spec's deterministic drift status — whether the
// code it governs (covers[]) changed since it was last verified. It is the
// non-LLM backstop the research rule demands: never trust the agent alone. The
// "unverified" + "stale" specs are the candidate set the verify agent pass runs
// over; "fresh" specs are skipped (cost control + a deterministic signal).
func (s *Server) handleSpecsDrift(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}
	ref, ok := s.resolveRef(w, r, repoDir)
	if !ok {
		return
	}

	out := api.SpecDriftReport{Ref: ref, Specs: []api.SpecDriftItem{}}
	for _, full := range s.specBlobPaths(r.Context(), repoDir, ref) {
		content, err := gitOutput(r.Context(), repoDir, "cat-file", "blob", ref+":"+full)
		if err != nil {
			s.logger.Error("read spec", "repo", repoDir, "path", full, "err", err)
			continue
		}
		out.Specs = append(out.Specs, s.specDriftItem(r.Context(), repoDir, repoID, ref, full, content))
	}
	sort.Slice(out.Specs, func(i, j int) bool { return out.Specs[i].Path < out.Specs[j].Path })
	writeJSON(w, http.StatusOK, out)
}

// specDriftItem classifies one spec. A spec with no covers[] (or unparseable
// frontmatter) is "uncovered" — drift can't be checked deterministically. With
// covers[] but no prior verification it's "unverified". Otherwise it diffs the
// governed globs between the last-verified commit and ref: any change → "stale"
// (a failed diff, e.g. the baseline commit is gone, also errs toward "stale"
// rather than falsely "fresh"); no change → "fresh".
func (s *Server) specDriftItem(ctx context.Context, repoDir string, repoID int64, ref, full string, content []byte) api.SpecDriftItem {
	item := api.SpecDriftItem{Path: full}
	spec, err := specs.Parse(full, content)
	if err != nil || spec == nil {
		item.ID = strings.TrimSuffix(path.Base(full), path.Ext(full))
		item.Status = "uncovered"
		return item
	}
	item.ID = spec.Frontmatter.ID
	item.Covers = spec.Frontmatter.Covers
	item.LastVerified = spec.Frontmatter.LastVerified
	if len(spec.Frontmatter.Covers) == 0 {
		item.Status = "uncovered"
		return item
	}

	latest, err := storage.LatestVerification(s.rdb, repoID, spec.Frontmatter.ID)
	if errors.Is(err, storage.ErrNotFound) {
		item.Status = "unverified"
		return item
	}
	if err != nil {
		s.logger.Error("latest verification", "repo", repoDir, "spec", spec.Frontmatter.ID, "err", err)
		item.Status = "unverified"
		return item
	}
	item.Base = latest.CommitSHA

	changed, err := gitDiffGlobs(ctx, repoDir, latest.CommitSHA, ref, spec.Frontmatter.Covers)
	if err != nil {
		// Baseline unresolvable (rewritten history, gone commit): can't prove
		// fresh, so flag for re-verify.
		item.Status = "stale"
		return item
	}
	if len(changed) > 0 {
		item.Status = "stale"
		item.Changed = changed
	} else {
		item.Status = "fresh"
	}
	return item
}

// gitDiffGlobs returns the governed paths that changed between base and ref.
// covers[] are matched with git's :(glob) pathspec magic, so a pattern like
// "internal/ssh/**" matches across directories — letting git do the globbing
// rather than re-implementing gitignore semantics.
func gitDiffGlobs(ctx context.Context, repoDir, base, ref string, covers []string) ([]string, error) {
	args := []string{"diff", "--name-only", base + ".." + ref, "--"}
	for _, c := range covers {
		args = append(args, ":(glob)"+c)
	}
	raw, err := gitOutput(ctx, repoDir, args...)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			changed = append(changed, line)
		}
	}
	return changed, nil
}

// handleVerifySpec kicks off a spec-verify agent run for one spec: it resolves
// the spec + the commit to verify against, then enqueues a kind=spec-verify run
// on the agent spine (read-only tool profile, claude-edit). The run streams over
// the same run/job event endpoints; #220 parses its output and stamps the result.
func (s *Server) handleVerifySpec(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	var req api.VerifySpecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	specPath, err := cleanTreePath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if specPath == "" || !strings.HasPrefix(specPath, specsDir+"/") || !strings.EqualFold(path.Ext(specPath), ".md") {
		writeError(w, http.StatusBadRequest, "path must be a .md file under specs/")
		return
	}

	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = headRef(r.Context(), repoDir)
	}
	if strings.HasPrefix(ref, "-") {
		writeError(w, http.StatusBadRequest, "invalid ref")
		return
	}
	// Resolve to an immutable commit (same peel as the issue-agent trigger).
	out, err := gitOutput(r.Context(), repoDir, "rev-parse", "-q", "--verify", ref+"^{commit}")
	sha := strings.TrimSpace(string(out))
	if err != nil || sha == "" {
		writeError(w, http.StatusBadRequest, "cannot resolve ref "+ref)
		return
	}
	// The spec must exist at that commit.
	if _, err := gitOutput(r.Context(), repoDir, "cat-file", "-e", sha+":"+specPath); err != nil {
		writeError(w, http.StatusNotFound, "spec not found: "+specPath)
		return
	}

	msg, author := gitCommitMeta(repoDir, sha)
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		Kind:           storage.RunKindSpecVerify,
		SpecPath:       specPath,
		ExecutionModel: storage.ExecModelClaudeEdit,
		ToolProfile:    storage.ToolProfileReview, // read-only
		CommitSHA:      sha,
		CommitMsg:      msg,
		CommitAuthor:   author,
		Ref:            ref,
		Event:          "spec-verify",
		Trigger:        identityFromContext(r),
	})
	if err != nil {
		s.logger.Error("spec verify enqueue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.logger.Info("spec-verify run spawned", "spec", specPath, "run", run.Number, "ref", ref)
	writeJSON(w, http.StatusAccepted, toAPIRun(run))
}
