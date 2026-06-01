package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// eventsPollInterval is how often handleEvents polls the events table for new
// rows once it has drained the backlog — the live-tail cadence. It mirrors the
// per-job CI stream's sub-second poll (handleCIJobEvents) rather than the
// runner's slower queue poll, so subscribed agents see fleet activity promptly.
const eventsPollInterval = time.Second

// eventsBatch caps how many rows each replay/tail read pulls, so a large
// backlog streams in chunks instead of one unbounded query.
const eventsBatch = 500

// handleEvents streams the outbound fleet event feed (#73) as Server-Sent
// Events: it replays the events table from the resume point, then live-tails
// new rows until the client disconnects. Unlike the per-job CI stream this feed
// is long-lived and never self-closes. Clients resume after a drop via the
// Last-Event-ID header (or ?last_event_id=); only events with a higher Seq are
// sent. Optional ?repo=owner/name scopes to one repo and ?types=a,b filters by
// event type. ?once=true replays the available backlog and closes instead of
// tailing — the snapshot mode the CLI's `--once` uses.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// Optional repo filter: resolve owner/name to an id up front so an unknown
	// repo is a clean 404 rather than a silently empty stream.
	var repoID int64
	if rs := strings.TrimSpace(r.URL.Query().Get("repo")); rs != "" {
		owner, name, found := strings.Cut(rs, "/")
		if !found || owner == "" || name == "" {
			writeError(w, http.StatusBadRequest, "repo must be owner/name")
			return
		}
		id, err := storage.LookupRepo(s.rdb, owner, strings.TrimSuffix(name, ".git"))
		if err != nil {
			writeError(w, http.StatusNotFound, "repo not registered: "+rs)
			return
		}
		repoID = id
	}

	// Optional type filter. Empty set means "all types".
	types := parseTypeFilter(r.URL.Query().Get("types"))

	// Snapshot mode: replay the backlog, then close rather than live-tail.
	once := r.URL.Query().Get("once") == "true"

	lastSeq := int64(parseLastEventID(r))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // don't let a proxy buffer the stream
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		batch, err := storage.ListEventsSince(s.rdb, lastSeq, repoID, eventsBatch)
		if err != nil {
			s.logger.Error("events list", "err", err)
			return
		}
		for _, ev := range batch {
			lastSeq = ev.Seq
			if len(types) > 0 && !types[ev.Type] {
				continue
			}
			if err := writeEventSSE(w, toAPIEvent(ev)); err != nil {
				return // client gone
			}
		}
		flusher.Flush()

		// A full batch means there's likely more backlog — keep draining
		// without sleeping. A short read means we've caught up.
		if len(batch) == eventsBatch {
			continue
		}
		// Snapshot mode is done once the backlog is drained; tail mode waits
		// for more.
		if once {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(eventsPollInterval):
			// Heartbeat: keeps intermediaries from idling the connection out and
			// surfaces a client disconnect promptly on the next write.
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// parseTypeFilter turns a comma-separated ?types= list into a set. Blank
// entries are dropped; an empty result means no filter (all types pass).
func parseTypeFilter(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	set := map[string]bool{}
	for t := range strings.SplitSeq(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = true
		}
	}
	return set
}

// toAPIEvent converts a stored event to its wire form, decoding the JSON
// payload into Data and joining the repo slug. A corrupt payload degrades to
// nil Data rather than dropping the event — the seq/type still flow.
func toAPIEvent(e storage.Event) api.Event {
	out := api.Event{
		Seq:   e.Seq,
		Type:  e.Type,
		Time:  e.CreatedAt.UnixMilli(),
		Actor: e.Actor,
	}
	if e.Owner != "" && e.Repo != "" {
		out.Repo = e.Owner + "/" + e.Repo
	}
	if e.Payload != "" {
		_ = json.Unmarshal([]byte(e.Payload), &out.Data)
	}
	return out
}

// writeEventSSE frames one event as an SSE message: Seq is the event id (echoed
// back as Last-Event-ID on reconnect), Type is the event name, and the
// JSON-encoded event is the data payload.
func writeEventSSE(w http.ResponseWriter, ev api.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, data)
	return err
}

// emitRunQueued records a ci.run.queued event for a freshly enqueued run. The
// terminal counterpart (ci.run.finished) is emitted by the in-process runner,
// which lives outside this package and writes the event directly.
func (s *Server) emitRunQueued(repoID int64, run storage.CIRun) {
	s.emit("ci.run.queued", repoID, run.Trigger, map[string]any{
		"number": run.Number,
		"ref":    run.Ref,
		"event":  run.Event,
		"sha":    run.CommitSHA,
	})
}

// emit appends a best-effort event to the outbound feed. A failure is logged
// but never propagated: the feed is a notification side-channel, so a write
// error must not fail the mutation that triggered it. data is marshaled to the
// stored JSON payload; a nil data yields "{}".
func (s *Server) emit(eventType string, repoID int64, actor string, data map[string]any) {
	var payload string
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			s.logger.Error("event emit marshal", "type", eventType, "err", err)
			return
		}
		payload = string(b)
	}
	if _, err := storage.AppendEvent(s.db, eventType, repoID, actor, payload); err != nil {
		s.logger.Error("event emit", "type", eventType, "err", err)
	}
}
