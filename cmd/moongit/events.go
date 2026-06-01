package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// runEvents tails the server's outbound event feed (#73) — the SSE counterpart
// to polling `issue list` / CI status. It replays from --since (or the start),
// prints one line per event, and live-tails until interrupted, reconnecting on
// a dropped stream and resuming from the last seq. --once replays and exits.
func runEvents(args []string) error {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	repo := fs.String("repo", "", "filter to one repo (owner/name)")
	types := fs.String("types", "", "comma-separated event types to include")
	since := fs.Int64("since", 0, "resume after this event seq (0 = from the start)")
	once := fs.Bool("once", false, "replay available events and exit, without tailing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	t, err := discoverTarget()
	if err != nil {
		return err
	}

	q := url.Values{}
	if *repo != "" {
		q.Set("repo", *repo)
	}
	if *types != "" {
		q.Set("types", *types)
	}
	if *once {
		// Ask the server to close after draining the backlog rather than tail,
		// so the single streamEvents call returns instead of hanging.
		q.Set("once", "true")
	}
	endpoint := t.server + "/api/events"
	if enc := q.Encode(); enc != "" {
		endpoint += "?" + enc
	}

	lastSeq := *since
	for {
		seq, streamErr := streamEvents(endpoint, lastSeq)
		lastSeq = seq
		if *once {
			if errors.Is(streamErr, io.EOF) {
				return nil // drained the backlog cleanly
			}
			return streamErr
		}
		// A clean EOF or a transient network error: reconnect from where we
		// left off. A non-retryable HTTP status surfaces as a hard error.
		if stop, ok := errors.AsType[stopErr](streamErr); ok {
			return stop.err
		}
		if streamErr != nil {
			fmt.Fprintf(os.Stderr, "events: stream dropped (%v); reconnecting…\n", streamErr)
		}
		time.Sleep(time.Second)
	}
}

// stopErr wraps a non-retryable error so the reconnect loop gives up instead of
// retrying (e.g. a 401, or an unknown-repo 404).
type stopErr struct{ err error }

func (e stopErr) Error() string { return e.err.Error() }

// streamEvents opens one SSE connection, prints each event, and returns the
// highest seq seen so a reconnect can resume past it. A non-nil error is the
// reason the stream ended (EOF for a clean server close, a network error for a
// drop, or a stopErr for a fatal HTTP status).
func streamEvents(endpoint string, lastSeq int64) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return lastSeq, stopErr{err}
	}
	req.Header.Set("Accept", "text/event-stream")
	if tok := os.Getenv("MOONGIT_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if lastSeq > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(lastSeq, 10))
	}

	// No client timeout: the stream is intentionally long-lived.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return lastSeq, err // transient — let the caller reconnect
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return lastSeq, stopErr{fmt.Errorf("server returned %d: %s", resp.StatusCode, decodeError(body))}
	}

	// Parse the SSE framing by hand: frames are separated by a blank line, and
	// the data: line carries the JSON-encoded api.Event. Comment lines (": ...",
	// the heartbeat) and id:/event: lines are ignored — Seq lives in the JSON.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var data strings.Builder
	flush := func() {
		if data.Len() == 0 {
			return
		}
		raw := data.String()
		data.Reset()
		var ev api.Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			return
		}
		if ev.Seq > lastSeq {
			lastSeq = ev.Seq
		}
		fmt.Println(formatEvent(ev))
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		// id:/event:/comment lines carry nothing we don't already have in data.
	}
	flush() // a final frame without a trailing blank line
	if err := sc.Err(); err != nil {
		return lastSeq, err
	}
	return lastSeq, io.EOF
}

// formatEvent renders one event as a single compact line:
//
//	HH:MM:SS  type  repo  @actor  summary
func formatEvent(ev api.Event) string {
	ts := time.UnixMilli(ev.Time).Local().Format("15:04:05")
	actor := ev.Actor
	if actor != "" {
		actor = "@" + actor
	}
	parts := []string{ts, ev.Type, ev.Repo, actor}
	if s := eventSummary(ev); s != "" {
		parts = append(parts, s)
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "  ")
}

// eventSummary pulls the most salient detail out of an event's data payload.
func eventSummary(ev api.Event) string {
	d := ev.Data
	if d == nil {
		return ""
	}
	num := func() string {
		if n, ok := d["number"]; ok {
			return fmt.Sprintf("#%v", numStr(n))
		}
		return ""
	}
	switch ev.Type {
	case "push":
		return joinNonEmpty(str(d["ref"]), short(str(d["after"])))
	case "issue.created":
		return joinNonEmpty(num(), str(d["title"]))
	case "issue.claimed", "issue.state_changed":
		return joinNonEmpty(num(), str(d["state"]))
	case "issue.updated", "issue.unclaimed", "issue.commented":
		return num()
	case "ci.run.queued":
		return joinNonEmpty("run "+num(), str(d["ref"]), short(str(d["sha"])))
	case "ci.run.finished":
		return joinNonEmpty("run "+num(), str(d["status"]))
	default:
		return ""
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// numStr renders a JSON number without a spurious ".0" (encoding/json decodes
// numbers as float64).
func numStr(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return str(v)
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}
