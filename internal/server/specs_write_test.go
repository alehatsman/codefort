package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

func putSpec(t *testing.T, s *Server, relPath, body string) (api.WriteSpecResult, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/repos/alice/proj/specs/"+relPath, strings.NewReader(body))
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	req.SetPathValue("path", relPath)
	rr := httptest.NewRecorder()
	s.handleWriteSpec(rr, req)
	var out api.WriteSpecResult
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
		}
	}
	return out, rr.Code
}

// blobAt reads a file's content from the bare repo at <ref>:<path>.
func blobAt(t *testing.T, s *Server, ref, path string) (string, error) {
	t.Helper()
	bare := filepath.Join(s.cfg.ReposDir, cOwner, cRepo+".git")
	out, err := gitOutput(context.Background(), bare, "cat-file", "blob", ref+":"+path)
	return string(out), err
}

func TestHandleWriteSpecNewBranch(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/existing.md", "# Existing\n")
	done()

	body := `{"content":"# New Spec\n## Intent\nWhy.\n","message":"add new spec"}`
	out, code := putSpec(t, s, "new-spec.md", body)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !out.Created || out.Branch != "spec/new-spec" || out.Commit == "" {
		t.Fatalf("result = %+v", out)
	}

	// The new branch carries the new spec…
	got, err := blobAt(t, s, "spec/new-spec", "specs/new-spec.md")
	if err != nil {
		t.Fatalf("read written spec: %v", err)
	}
	if !strings.Contains(got, "# New Spec") {
		t.Errorf("written content = %q", got)
	}
	// …and the default branch is untouched (no auto-push to main).
	if _, err := blobAt(t, s, "main", "specs/new-spec.md"); err == nil {
		t.Error("spec leaked onto main; must stay on the feature branch")
	}
}

func TestHandleWriteSpecExtendBranchAndUnchanged(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/x.md", "# X\n")
	done()

	// First write creates spec/x.
	first, code := putSpec(t, s, "x.md", `{"content":"# X\nfirst edit\n","branch":"spec/x"}`)
	if code != http.StatusOK || !first.Created {
		t.Fatalf("first write = %+v code=%d", first, code)
	}
	// Second write to the same branch extends it (not created), new commit.
	second, code := putSpec(t, s, "x.md", `{"content":"# X\nsecond edit\n","branch":"spec/x"}`)
	if code != http.StatusOK {
		t.Fatalf("second write status = %d", code)
	}
	if second.Created {
		t.Error("second write should extend the branch, not create it")
	}
	if second.Commit == first.Commit {
		t.Error("second write should produce a new commit")
	}
	// Re-writing identical content is a no-op → 409.
	if _, code := putSpec(t, s, "x.md", `{"content":"# X\nsecond edit\n","branch":"spec/x"}`); code != http.StatusConflict {
		t.Errorf("unchanged write: status = %d, want 409", code)
	}
}

func TestHandleWriteSpecRefusesDefaultBranch(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/x.md", "# X\n")
	done()

	if _, code := putSpec(t, s, "x.md", `{"content":"# X\nedit\n","branch":"main"}`); code != http.StatusBadRequest {
		t.Errorf("write to main: status = %d, want 400", code)
	}
}

func TestHandleWriteSpecValidation(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/x.md", "# X\n")
	done()

	if _, code := putSpec(t, s, "notes.txt", `{"content":"x"}`); code != http.StatusBadRequest {
		t.Errorf("non-md path: status = %d, want 400", code)
	}
	if _, code := putSpec(t, s, "x.md", `not json`); code != http.StatusBadRequest {
		t.Errorf("bad JSON: status = %d, want 400", code)
	}
}
