package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

func postVerify(t *testing.T, s *Server, body string) (api.CIRun, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alice/proj/specs/verify", strings.NewReader(body))
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	rr := httptest.NewRecorder()
	s.handleVerifySpec(rr, req)
	var out api.CIRun
	if rr.Code == http.StatusAccepted {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
		}
	}
	return out, rr.Code
}

func TestHandleVerifySpec(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/ssh-transport.md", "---\nid: ssh-transport\ncovers: [\"internal/ssh/**\"]\n---\n# SSH\n## Behavior\nWHEN x THEN y.\n")
	done()

	run, code := postVerify(t, s, `{"path":"specs/ssh-transport.md"}`)
	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	if run.Number == 0 {
		t.Fatalf("no run number: %+v", run)
	}

	// The enqueued run is a spec-verify run targeting the spec, read-only.
	repoID, err := storage.EnsureRepo(s.db, cOwner, cRepo)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := storage.GetRun(s.db, repoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Kind != storage.RunKindSpecVerify {
		t.Errorf("kind = %q, want spec-verify", stored.Kind)
	}
	if stored.SpecPath != "specs/ssh-transport.md" {
		t.Errorf("spec_path = %q", stored.SpecPath)
	}
	if stored.ToolProfile != storage.ToolProfileReview {
		t.Errorf("tool_profile = %q, want review (read-only)", stored.ToolProfile)
	}
	if stored.IssueNumber != nil {
		t.Errorf("spec-verify run should have no issue: %v", stored.IssueNumber)
	}
}

func TestHandleVerifySpecValidation(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/real.md", "# Real\n")
	done()

	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing spec", `{"path":"specs/ghost.md"}`, http.StatusNotFound},
		{"not under specs/", `{"path":"README.md"}`, http.StatusBadRequest},
		{"not markdown", `{"path":"specs/x.txt"}`, http.StatusBadRequest},
		{"empty path", `{"path":""}`, http.StatusBadRequest},
		{"bad json", `nope`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, code := postVerify(t, s, tc.body); code != tc.want {
				t.Errorf("status = %d, want %d", code, tc.want)
			}
		})
	}
}
