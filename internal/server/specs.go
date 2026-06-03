package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/dex"
	"github.com/alehatsman/moongit/internal/specs"
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
	if !hasCommits(r.Context(), repoDir) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Recursively list blobs under specs/. "<ref>:specs" addresses that tree;
	// when specs/ is absent the treeish doesn't resolve — that's an empty list,
	// not an error. Names are relative to specs/.
	raw, err := gitOutput(r.Context(), repoDir, "ls-tree", "-r", "-z", "--name-only", ref+":"+specsDir)
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}

	for rel := range strings.SplitSeq(string(raw), "\x00") {
		if rel == "" || !strings.EqualFold(path.Ext(rel), ".md") {
			continue
		}
		full := specsDir + "/" + rel
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

// handleGetSpec returns one spec's content and parsed structure. The path is
// the trailing {path...} segment, taken relative to specs/ (so the URL
// .../specs/ci/pipeline.md reads specs/ci/pipeline.md). It is a dedicated
// handler rather than a /blob wrapper because the response carries the parsed
// frontmatter, sections, and checklist the renderer and truth gutter consume —
// not just bytes. ?ref= selects the branch like the other read handlers.
func (s *Server) handleGetSpec(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusOK, specContent(ref, full, content))
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

// specSearchOverFetch is how many dex hits to request before filtering to the
// specs/ corpus. dex's semantic search takes no path scope (see #213), so we
// over-fetch and keep the specs/ hits — generous enough that a query with a few
// spec matches buried among code hits still surfaces them.
const specSearchOverFetch = 50

// specSearchMaxHits caps the spec hits returned after filtering.
const specSearchMaxHits = 20

type specSearchRequest struct {
	Query string `json:"query"`
}

// handleSearchSpecs runs a dex semantic search scoped to the specs/ corpus: it
// over-fetches from dex, keeps only specs/ hits, and attributes each to its
// enclosing spec section (via the parser's line ranges). The spec corpus is the
// differentiator — no other SDD tool wires semantic search over specs. Mirrors
// the intel handlers' dex-availability semantics (503 unconfigured, 404
// un-indexed, 502 transport error).
func (s *Server) handleSearchSpecs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	if !s.dex.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "dex integration not configured")
		return
	}

	var req specSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	proj, err := s.dex.ResolveProject(r.Context(), repo)
	if errors.Is(err, dex.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, "repo is not indexed by dex")
		return
	}
	if err != nil {
		s.logger.Error("dex resolve project", "err", err)
		writeError(w, http.StatusBadGateway, "dex unreachable: "+err.Error())
		return
	}

	res, err := s.dex.Search(r.Context(), proj.ID, req.Query, specSearchOverFetch)
	if err != nil {
		s.logger.Error("dex search", "err", err)
		writeError(w, http.StatusBadGateway, "dex search failed: "+err.Error())
		return
	}

	ref := headRef(r.Context(), repoDir)
	out := api.SpecSearchResult{Query: req.Query, Hits: []api.SpecSearchHit{}}
	sections := map[string][]specs.Section{} // spec path -> parsed sections, cached
	for _, h := range res.Hits {
		if !strings.HasPrefix(h.Path, specsDir+"/") {
			continue
		}
		if len(out.Hits) >= specSearchMaxHits {
			break
		}
		out.Hits = append(out.Hits, api.SpecSearchHit{
			Path:    h.Path,
			Section: s.specSectionAt(r.Context(), repoDir, ref, h.Path, h.StartLine, sections),
			Line:    h.StartLine,
			Snippet: h.Content,
			Score:   h.Score,
		})
	}
	writeJSON(w, http.StatusOK, out)
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
