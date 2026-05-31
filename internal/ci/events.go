package ci

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is one entry in a job's append-only JSONL event stream — the contract
// every CI consumer shares: the runner produces it, storage mirrors status
// from it, the API replays + tails it, the UI renders it. Seq is 1-based and
// monotonic within a job's stream so an SSE client can resume via
// Last-Event-ID; Time is unix milliseconds. The stream shape mirrors mooncake's
// verified event stream (issue #14).
type Event struct {
	Seq  int            `json:"seq"`
	Type string         `json:"type"`
	Time int64          `json:"time"`
	Data map[string]any `json:"data,omitempty"`
}

// Event types — the mooncake-shaped stream the runner emits per job.
const (
	EventRunStarted    = "run.started"    // {total_steps}
	EventPlanLoaded    = "plan.loaded"    // {total_steps}
	EventStepStarted   = "step.started"   // {step_id, name, action, ...}
	EventStepStdout    = "step.stdout"    // {step_id, stream, line, line_number}
	EventStepStderr    = "step.stderr"    // {step_id, stream, line, line_number}
	EventStepSkipped   = "step.skipped"   // {step_id, reason}
	EventStepCompleted = "step.completed" // {step_id, duration_ms, changed, result{...}}
	EventRunCompleted  = "run.completed"
	EventRunFailed     = "run.failed"
)

// Agent event types — the live transcript of an agent run, layered on the same
// append-only stream as the CI events above. A turn (one claude invocation) is
// bracketed by agent.turn.started/completed; between them, each claude
// stream-json line becomes one agent.message carrying the parsed object
// verbatim under Data (schema-tolerant — the renderer keys on the inner
// "type", and nothing is dropped if Claude's schema evolves). A line that
// won't parse as JSON is preserved as agent.raw. See #76.
const (
	EventAgentTurnStarted   = "agent.turn.started"   // {turn, prompt}
	EventAgentMessage       = "agent.message"        // {claude: <stream-json object>}
	EventAgentRaw           = "agent.raw"            // {line} — unparseable stdout
	EventAgentTurnCompleted = "agent.turn.completed" // {turn, status, num_turns, duration_ms, cost_usd}
)

// EventLogPath returns the on-disk path for a job's event stream:
//
//	<root>/ci/<owner>/<repo>/<run>/<job>.events.jsonl
//
// Logs live on disk rather than in the DB to keep the write-hot database lean;
// the file is durable and the API replays/tails it.
func EventLogPath(root, owner, repo string, runNumber int, job string) string {
	return filepath.Join(RunLogDir(root, owner, repo, runNumber), job+".events.jsonl")
}

// RunLogDir is the directory holding a run's per-job event logs — the parent of
// every job's EventLogPath. Retention drops a pruned run's logs by removing
// this directory wholesale.
func RunLogDir(root, owner, repo string, runNumber int) string {
	return filepath.Join(root, "ci", owner, repo, strconv.Itoa(runNumber))
}

// EventLog is an append-only writer for one job's event stream. It is safe for
// concurrent Append calls (the runner streams stdout/stderr lines from a job
// while emitting step lifecycle events).
type EventLog struct {
	mu  sync.Mutex
	f   *os.File
	seq int
}

// OpenEventLog opens (creating parent dirs) the append-only stream for a job.
// Seq numbering continues after any existing complete events, so re-opening a
// stream after a crash keeps Seq monotonic. owner/repo/job are validated as
// safe single path components to prevent traversal, since job names come from
// the repo-authored mgitci.yml.
func OpenEventLog(root, owner, repo string, runNumber int, job string) (*EventLog, error) {
	for _, c := range []string{owner, repo, job} {
		if err := safeComponent(c); err != nil {
			return nil, err
		}
	}
	path := EventLogPath(root, owner, repo, runNumber, job)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	seq, err := countEvents(path)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &EventLog{f: f, seq: seq}, nil
}

// Append assigns the next Seq, stamps the time, and writes the event as one
// JSON line. It returns the written event (with Seq/Time filled in).
func (l *EventLog) Append(eventType string, data map[string]any) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ev := Event{Seq: l.seq + 1, Type: eventType, Time: time.Now().UnixMilli(), Data: data}
	b, err := json.Marshal(ev)
	if err != nil {
		return Event{}, err
	}
	if _, err := l.f.Write(append(b, '\n')); err != nil {
		return Event{}, err
	}
	l.seq++
	return ev, nil
}

// Close flushes and closes the underlying file.
func (l *EventLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

// ReadEvents reads all complete events from byteOffset to the end of the file,
// returning the decoded events and the offset just past the last complete
// line. A partial trailing line (the writer mid-append) is left unread so a
// tailer can re-read it once complete. A missing file yields no events and the
// unchanged offset — the stream simply hasn't started yet. This is the
// primitive the SSE endpoint uses for replay (offset 0) and live tail (poll
// from the last returned offset).
func ReadEvents(path string, byteOffset int64) (events []Event, nextOffset int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, byteOffset, nil
		}
		return nil, byteOffset, err
	}
	defer f.Close()

	if _, err := f.Seek(byteOffset, io.SeekStart); err != nil {
		return nil, byteOffset, err
	}
	offset := byteOffset
	r := bufio.NewReader(f)
	for {
		line, rerr := r.ReadBytes('\n')
		if rerr == io.EOF {
			// No terminating newline: incomplete line, stop before it.
			break
		}
		if rerr != nil {
			return events, offset, rerr
		}
		var ev Event
		if e := json.Unmarshal(bytes.TrimSpace(line), &ev); e != nil {
			return events, offset, fmt.Errorf("corrupt event at offset %d: %w", offset, e)
		}
		events = append(events, ev)
		offset += int64(len(line))
	}
	return events, offset, nil
}

// countEvents counts newline-terminated lines (complete events), ignoring any
// partial trailing line — matching ReadEvents' notion of a complete event.
func countEvents(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	n := 0
	r := bufio.NewReader(f)
	for {
		_, rerr := r.ReadBytes('\n')
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, rerr
		}
		n++
	}
	return n, nil
}

// safeComponent rejects strings that aren't a single safe path segment, so a
// repo-authored job name can't escape the CI log root.
func safeComponent(s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("unsafe path component %q", s)
	}
	return nil
}
