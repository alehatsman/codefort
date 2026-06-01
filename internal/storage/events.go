package storage

import (
	"database/sql"
	"time"
)

// Event is one row in the append-only outbound event feed (#73). Seq is the
// global monotonic id, used as the SSE event id so a client can resume via
// Last-Event-ID. RepoID is the originating repo (0 when the event isn't
// repo-scoped); Owner/Repo are its resolved slug, populated on read so callers
// needn't look it up. Payload is opaque JSON whose shape depends on Type;
// Actor is the token name (or pusher) that caused the event.
type Event struct {
	Seq       int64
	Type      string
	RepoID    int64 // 0 when not repo-scoped
	Owner     string
	Repo      string
	Actor     string
	Payload   string
	CreatedAt time.Time
}

// AppendEvent writes one event to the feed and returns its assigned Seq. An
// empty payload is normalized to "{}" so the column's JSON shape stays valid.
func AppendEvent(db *sql.DB, eventType string, repoID int64, actor, payload string) (int64, error) {
	if payload == "" {
		payload = "{}"
	}
	// repo_id is nullable; pass NULL (not 0) when the event isn't repo-scoped
	// so the FK/index treat it as absent rather than pointing at repo 0.
	var repo any
	if repoID > 0 {
		repo = repoID
	}
	res, err := db.Exec(
		`INSERT INTO events(type, repo_id, actor, payload) VALUES (?, ?, ?, ?)`,
		eventType, repo, actor, payload,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListEventsSince returns events with Seq > sinceSeq in ascending Seq order,
// up to limit rows (default 500 when <= 0). When repoID > 0 only that repo's
// events are returned. The repo slug is resolved via LEFT JOIN, so non-repo
// events come back with empty Owner/Repo rather than dropping out.
func ListEventsSince(db *sql.DB, sinceSeq, repoID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `
		SELECT e.id, e.type, COALESCE(e.repo_id, 0),
		       COALESCE(u.name, ''), COALESCE(rep.name, ''),
		       e.actor, e.payload, e.created_at
		  FROM events e
		  LEFT JOIN repos rep ON rep.id = e.repo_id
		  LEFT JOIN users u   ON u.id   = rep.owner_id
		 WHERE e.id > ?`
	args := []any{sinceSeq}
	if repoID > 0 {
		q += ` AND e.repo_id = ?`
		args = append(args, repoID)
	}
	q += ` ORDER BY e.id LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var created int64
		if err := rows.Scan(&e.Seq, &e.Type, &e.RepoID, &e.Owner, &e.Repo, &e.Actor, &e.Payload, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(created, 0)
		events = append(events, e)
	}
	return events, rows.Err()
}

// PruneEvents bounds the feed to the newest `keep` events by Seq, returning the
// number of rows deleted. keep <= 0 disables pruning. Because ids are monotonic
// and never reused, deleting everything at or below MAX(id)-keep keeps at most
// `keep` rows (slightly fewer if earlier prunes or repo-cascade deletes left
// gaps) — a strict, gap-tolerant upper bound on the table.
func PruneEvents(db *sql.DB, keep int) (int64, error) {
	if keep <= 0 {
		return 0, nil
	}
	res, err := db.Exec(
		`DELETE FROM events WHERE id <= (SELECT COALESCE(MAX(id), 0) - ? FROM events)`,
		keep,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
