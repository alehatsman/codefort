package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

// getSpec calls handleGetSpec for the spec at relPath (relative to specs/) and
// decodes the response. It mirrors getJSON but also sets the {path...} value.
func getSpec(t *testing.T, s *Server, relPath, query string) (api.SpecContent, int) {
	t.Helper()
	target := "/api/repos/" + cOwner + "/" + cRepo + "/specs/" + relPath
	if query != "" {
		target += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	req.SetPathValue("path", relPath)
	rr := httptest.NewRecorder()
	s.handleGetSpec(rr, req)
	var out api.SpecContent
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
		}
	}
	return out, rr.Code
}

func TestHandleGetSpec(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/ssh-transport.md", `---
id: ssh-transport
status: living
owners: [aleh]
covers: ["internal/ssh/**"]
alignment: 0.91
---
# SSH Transport
## Intent
Git over SSH.
## Checklist
- [x] publickey auth
- [ ] key rotation
`)
	done()

	out, code := getSpec(t, s, "ssh-transport.md", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Path != "specs/ssh-transport.md" || out.ID != "ssh-transport" {
		t.Errorf("path/id = %q/%q", out.Path, out.ID)
	}
	if out.Title != "SSH Transport" || out.Status != "living" {
		t.Errorf("title/status = %q/%q", out.Title, out.Status)
	}
	if out.Alignment == nil || *out.Alignment != 0.91 {
		t.Errorf("alignment = %v", out.Alignment)
	}
	// Content is the whole raw file (frontmatter included); Body drops it.
	if !strings.Contains(out.Content, "---") || !strings.Contains(out.Content, "# SSH Transport") {
		t.Errorf("Content missing raw markdown: %q", out.Content)
	}
	if strings.Contains(out.Body, "id: ssh-transport") {
		t.Errorf("Body should not contain frontmatter: %q", out.Body)
	}
	// Sections: H1 + Intent + Checklist.
	if len(out.Sections) != 3 {
		t.Fatalf("got %d sections, want 3: %+v", len(out.Sections), out.Sections)
	}
	if out.Sections[1].Title != "Intent" || out.Sections[1].Body != "Git over SSH." {
		t.Errorf("Intent section = %+v", out.Sections[1])
	}
	// Checklist: one checked, one not.
	if len(out.Checklist) != 2 {
		t.Fatalf("got %d checklist items, want 2", len(out.Checklist))
	}
	if !out.Checklist[0].Checked || out.Checklist[0].Text != "publickey auth" {
		t.Errorf("checklist[0] = %+v", out.Checklist[0])
	}
	if out.Checklist[1].Checked {
		t.Errorf("checklist[1] should be unchecked: %+v", out.Checklist[1])
	}
}

func TestHandleGetSpecFoldered(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/ci/pipeline.md", "# Pipeline\n## Intent\nCI DAG.\n")
	done()

	out, code := getSpec(t, s, "ci/pipeline.md", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Path != "specs/ci/pipeline.md" || out.Title != "Pipeline" {
		t.Errorf("path/title = %q/%q", out.Path, out.Title)
	}
}

func TestHandleGetSpecNotFound(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/real.md", "# Real\n")
	done()

	if _, code := getSpec(t, s, "ghost.md", ""); code != http.StatusNotFound {
		t.Errorf("missing spec: status = %d, want 404", code)
	}
}

func TestHandleGetSpecDirectoryIs400(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/ci/pipeline.md", "# Pipeline\n")
	done()

	// "ci" resolves to a tree, not a blob.
	if _, code := getSpec(t, s, "ci", ""); code != http.StatusBadRequest {
		t.Errorf("directory path: status = %d, want 400", code)
	}
}

func TestHandleGetSpecTraversalRejected(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/real.md", "# Real\n")
	done()

	if _, code := getSpec(t, s, "../README.md", ""); code != http.StatusBadRequest {
		t.Errorf("traversal: status = %d, want 400", code)
	}
}

func TestHandleGetSpecBadRef(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/real.md", "# Real\n")
	done()

	if _, code := getSpec(t, s, "real.md", "ref=nonexistent"); code != http.StatusNotFound {
		t.Errorf("bad ref: status = %d, want 404", code)
	}
}

func TestHandleGetSpecMalformedFrontmatter(t *testing.T) {
	// Broken frontmatter still loads (raw content + path-derived title) so the
	// editor can open and fix it.
	s, write, done := newSpecsTestServer(t)
	write("specs/broken.md", "---\nid: [unterminated\n---\n# Broken\n")
	done()

	out, code := getSpec(t, s, "broken.md", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Title != "broken" {
		t.Errorf("title = %q, want broken", out.Title)
	}
	if !strings.Contains(out.Content, "unterminated") {
		t.Errorf("raw content should be returned: %q", out.Content)
	}
	if len(out.Sections) != 0 {
		t.Errorf("malformed spec should have no parsed sections: %+v", out.Sections)
	}
}
