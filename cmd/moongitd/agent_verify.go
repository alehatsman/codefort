package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/specs"
	"github.com/alehatsman/moongit/internal/storage"
)

// verifyFinalize bundles what finalizeVerifyRun needs to close out a spec-verify
// run (keeps the call site in executeAgentRun tidy).
type verifyFinalize struct {
	run         storage.CIRun
	jobID       int64
	owner, name string
	workDir     string
	specContent string // the spec as checked out (frontmatter + body)
	finalText   string // the agent's final assistant message
	turnStatus  string // the turn's status string (success/failed/…)
}

// finalizeVerifyRun closes out a one-shot spec-verify run: parse the agent's
// JSON classification, record it (history) and stamp it (frontmatter on a
// branch), summarize it on the transcript, then tear down. A parse failure
// finalizes the run RunFailed with a diagnostic — the verdict is the agent's
// JSON, so unreadable output is a failed verify, not an infra error.
func (r *ciRunner) finalizeVerifyRun(parent context.Context, f verifyFinalize) {
	log := r.logger.With("kind", "spec-verify", "run", f.run.Number, "spec", f.run.SpecPath)

	res, err := parseVerifyResult(f.finalText)
	if err != nil {
		log.Error("parse verify output", "err", err)
		r.emitVerifySummary(f, "⚠️ Verify run produced no parseable result: "+err.Error())
		r.finishJob(f.jobID, storage.JobError, nil)
		r.tearDownAgent(f.run.ID, f.jobID, f.workDir)
		r.finish(f.run, storage.RunFailed)
		return
	}

	// Recompute alignment from the markers (authoritative) rather than trusting
	// the agent's arithmetic; fall back to its reported value if it gave no
	// verifiable markers.
	alignment := res.Alignment
	if len(res.Markers) > 0 {
		alignment = specs.ComputeAlignment(res.Markers)
		res.Alignment = alignment
	}

	specID := strings.TrimSuffix(filepath.Base(f.run.SpecPath), filepath.Ext(f.run.SpecPath))
	if spec, perr := specs.Parse(f.run.SpecPath, []byte(f.specContent)); perr == nil && spec.Frontmatter.ID != "" {
		specID = spec.Frontmatter.ID
	}

	resultJSON, _ := json.Marshal(res)
	if _, err := storage.RecordVerification(r.db, storage.SpecVerification{
		RepoID:    f.run.RepoID,
		SpecID:    specID,
		SpecPath:  f.run.SpecPath,
		CommitSHA: f.run.CommitSHA,
		Alignment: alignment,
		Result:    string(resultJSON),
		Verifier:  agentCommentAuthor,
	}); err != nil {
		log.Error("record verification", "err", err)
	}

	// Stamp last_verified + alignment into the spec's frontmatter and commit it
	// to a branch (no push — the deliverable is the branch + a later PR, same as
	// the issue-agent handoff). The agent was read-only, so the only change in
	// the workspace is this stamp.
	branch, commit := r.stampVerifyBranch(parent, f, specID, alignment, log)

	driftCount := 0
	for _, m := range res.Markers {
		if m.Marker == specs.MarkerDrifted {
			driftCount++
		}
	}
	summary := fmt.Sprintf("✅ Verified %s against %s: alignment %.0f%%, %d drifted line(s).",
		f.run.SpecPath, short(f.run.CommitSHA), alignment*100, driftCount)
	if branch != "" {
		summary += fmt.Sprintf(" Stamp on branch `%s` (%s).", branch, short(commit))
	}
	r.emitVerifySummary(f, summary)

	zero := 0
	r.finishJob(f.jobID, storage.JobSuccess, &zero)
	r.tearDownAgent(f.run.ID, f.jobID, f.workDir)
	r.finish(f.run, storage.RunSuccess)
	log.Info("spec-verify run finished", "alignment", alignment, "drifted", driftCount, "branch", branch)
}

// stampVerifyBranch writes the stamped spec into the workspace and commits it to
// refs/heads/spec-verify/<id> in the bare repo. Returns ("", "") when nothing
// changed or the commit failed (logged, non-fatal — the record + transcript
// still stand).
func (r *ciRunner) stampVerifyBranch(parent context.Context, f verifyFinalize, specID string, alignment float64, log *slog.Logger) (branch, commit string) {
	date := time.Now().UTC().Format("2006-01-02")
	stamped := specs.Stamp([]byte(f.specContent), date, alignment)
	if err := os.WriteFile(filepath.Join(f.workDir, f.run.SpecPath), stamped, 0o644); err != nil {
		log.Error("write stamped spec", "err", err)
		return "", ""
	}
	branch = "spec-verify/" + branchSafe(specID)
	bareRepo := filepath.Join(r.cfg.ReposDir, f.owner, f.name+".git")
	msg := fmt.Sprintf("docs(specs): stamp %s — alignment %.2f, verified %s\n", f.run.SpecPath, alignment, date)
	c, changed, err := materializeAgentBranch(parent, f.run.ID, bareRepo, f.run.CommitSHA, f.workDir, "refs/heads/"+branch, agentCommentAuthor, msg)
	if err != nil {
		log.Error("stamp materialize branch", "err", err)
		return "", ""
	}
	if !changed {
		return "", "" // already stamped with the same values
	}
	return branch, c
}

// emitVerifySummary appends a one-line summary to the run's transcript. The verify
// run has no issue to comment on, so the run page is where the result lands; the
// elog was closed after the turn, so reopen to append (best-effort).
func (r *ciRunner) emitVerifySummary(f verifyFinalize, line string) {
	elog, err := ci.OpenEventLog(r.cfg.DataDir, f.owner, f.name, f.run.Number, agentJobName)
	if err != nil {
		return
	}
	defer elog.Close()
	r.emit(elog, ci.EventAgentRaw, map[string]any{"line": line})
}

// jsonBlockRe extracts the contents of the first ```json fenced block.
var jsonBlockRe = regexp.MustCompile("(?s)```json\\s*(.*?)```")

// parseVerifyResult pulls the structured classification out of the agent's final
// message: a ```json fenced block if present, else the first {...} object. It
// tolerates surrounding prose so a slightly chatty agent still parses.
func parseVerifyResult(text string) (specs.VerificationResult, error) {
	var raw string
	if m := jsonBlockRe.FindStringSubmatch(text); m != nil {
		raw = m[1]
	} else if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			raw = text[i : j+1]
		}
	}
	if strings.TrimSpace(raw) == "" {
		return specs.VerificationResult{}, fmt.Errorf("no JSON object in agent output")
	}

	var parsed struct {
		Alignment float64 `json:"alignment"`
		Markers   []struct {
			Line   int    `json:"line"`
			Text   string `json:"text"`
			Marker string `json:"marker"`
			Note   string `json:"note"`
		} `json:"markers"`
		Conflicts []string `json:"conflicts"`
		Notes     string   `json:"notes"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return specs.VerificationResult{}, fmt.Errorf("unmarshal verify JSON: %w", err)
	}

	out := specs.VerificationResult{
		Alignment: parsed.Alignment,
		Conflicts: parsed.Conflicts,
		Notes:     parsed.Notes,
	}
	for _, m := range parsed.Markers {
		out.Markers = append(out.Markers, specs.LineMarker{
			Line: m.Line, Text: m.Text, Marker: specs.Marker(m.Marker), Note: m.Note,
		})
	}
	return out, nil
}

// branchSafe reduces a spec id to a git-ref-safe segment.
var branchUnsafeRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func branchSafe(id string) string {
	s := branchUnsafeRe.ReplaceAllString(id, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "spec"
	}
	return s
}
