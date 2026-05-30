package ci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEventLogPath(t *testing.T) {
	got := EventLogPath("/data", "alice", "repo", 7, "test")
	want := filepath.FromSlash("/data/ci/alice/repo/7/test.events.jsonl")
	if got != want {
		t.Errorf("EventLogPath = %q, want %q", got, want)
	}
}

func TestEventLogAppendAndReplay(t *testing.T) {
	root := t.TempDir()
	log, err := OpenEventLog(root, "alice", "repo", 1, "test")
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	if _, err := log.Append(EventRunStarted, map[string]any{"total_steps": 2}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := log.Append(EventStepStdout, map[string]any{"line": "hello", "line_number": 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := EventLogPath(root, "alice", "repo", 1, "test")
	events, off, err := ReadEvents(path, 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[0].Type != EventRunStarted {
		t.Errorf("event[0] = seq %d type %q, want 1 run.started", events[0].Seq, events[0].Type)
	}
	if events[1].Seq != 2 || events[1].Data["line"] != "hello" {
		t.Errorf("event[1] = seq %d data %v, want 2 line=hello", events[1].Seq, events[1].Data)
	}
	if off <= 0 {
		t.Errorf("nextOffset = %d, want > 0", off)
	}

	// Reading from the end returns nothing and the same offset (tail caught up).
	more, off2, err := ReadEvents(path, off)
	if err != nil || len(more) != 0 || off2 != off {
		t.Errorf("tail-at-end = (%d events, off %d, %v), want (0, %d, nil)", len(more), off2, err, off)
	}
}

func TestReadEventsIncrementalTail(t *testing.T) {
	root := t.TempDir()
	log, _ := OpenEventLog(root, "o", "r", 1, "job")
	path := EventLogPath(root, "o", "r", 1, "job")

	log.Append(EventRunStarted, map[string]any{"total_steps": 1})
	first, off, err := ReadEvents(path, 0)
	if err != nil || len(first) != 1 {
		t.Fatalf("first read = %d events, %v", len(first), err)
	}
	// Append more, then read only from the prior offset — the tail sees just the new event.
	log.Append(EventRunCompleted, nil)
	next, _, err := ReadEvents(path, off)
	if err != nil {
		t.Fatalf("tail read: %v", err)
	}
	if len(next) != 1 || next[0].Type != EventRunCompleted || next[0].Seq != 2 {
		t.Errorf("tail = %+v, want one run.completed seq 2", next)
	}
}

func TestOpenEventLogContinuesSeqAfterReopen(t *testing.T) {
	root := t.TempDir()
	log, _ := OpenEventLog(root, "o", "r", 1, "job")
	log.Append(EventRunStarted, nil)
	log.Close()

	// Re-open the same stream: Seq must continue, not reset.
	log2, err := OpenEventLog(root, "o", "r", 1, "job")
	if err != nil {
		t.Fatalf("re-OpenEventLog: %v", err)
	}
	ev, err := log2.Append(EventRunCompleted, nil)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if ev.Seq != 2 {
		t.Errorf("reopened seq = %d, want 2", ev.Seq)
	}
	log2.Close()
}

func TestReadEventsIgnoresPartialTrailingLine(t *testing.T) {
	root := t.TempDir()
	path := EventLogPath(root, "o", "r", 1, "job")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// One complete event followed by a half-written line (writer mid-append).
	content := `{"seq":1,"type":"run.started","time":1}` + "\n" + `{"seq":2,"type":"step.`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	events, off, err := ReadEvents(path, 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (partial line skipped)", len(events))
	}
	// Offset stops before the partial line so a later read picks it up once complete.
	if int(off) != len(`{"seq":1,"type":"run.started","time":1}`)+1 {
		t.Errorf("offset = %d, want end of first complete line", off)
	}
}

func TestOpenEventLogRejectsUnsafeJobName(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"../escape", "a/b", "..", ""} {
		if _, err := OpenEventLog(root, "o", "r", 1, bad); err == nil {
			t.Errorf("OpenEventLog(job=%q) = nil error, want rejection", bad)
		}
	}
}

func TestSafeComponent(t *testing.T) {
	if err := safeComponent("ok-name_1"); err != nil {
		t.Errorf("safeComponent(valid) = %v", err)
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`} {
		if err := safeComponent(bad); err == nil {
			t.Errorf("safeComponent(%q) = nil, want error", bad)
		}
	}
}
