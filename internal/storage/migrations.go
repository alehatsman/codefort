package storage

import (
	"database/sql"
	"fmt"
)

// migrations is the ordered list of schema changes. Each is applied exactly
// once, tracked via the schema_version table. APPEND new migrations only —
// never edit a committed migration.
var migrations = []string{
	// 1: initial schema. CREATE ... IF NOT EXISTS so pre-existing DBs that
	// were bootstrapped before the migration framework was introduced can
	// pick up tracking without re-creating anything.
	`
	CREATE TABLE IF NOT EXISTS users (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    name       TEXT NOT NULL UNIQUE,
	    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);
	CREATE TABLE IF NOT EXISTS repos (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	    name       TEXT NOT NULL,
	    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    UNIQUE (owner_id, name)
	);
	CREATE INDEX IF NOT EXISTS idx_repos_owner ON repos(owner_id);
	CREATE TABLE IF NOT EXISTS issues (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    repo_id    INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
	    number     INTEGER NOT NULL,
	    title      TEXT NOT NULL,
	    body       TEXT NOT NULL DEFAULT '',
	    author     TEXT NOT NULL,
	    state      TEXT NOT NULL,
	    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    UNIQUE (repo_id, number)
	);
	CREATE INDEX IF NOT EXISTS idx_issues_repo  ON issues(repo_id);
	CREATE INDEX IF NOT EXISTS idx_issues_state ON issues(state);
	`,

	// 2: agent coordination — assignee column + comments table.
	`
	ALTER TABLE issues ADD COLUMN assignee TEXT;
	CREATE INDEX IF NOT EXISTS idx_issues_assignee ON issues(assignee);
	CREATE TABLE IF NOT EXISTS issue_comments (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
	    author     TEXT NOT NULL,
	    body       TEXT NOT NULL,
	    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);
	CREATE INDEX IF NOT EXISTS idx_issue_comments_issue ON issue_comments(issue_id);
	`,

	// 3: API tokens. Plaintext never stored — only sha256(token).
	// last_used_at is nullable (never-used tokens have NULL).
	// revoked_at is nullable (NULL = active, set = revoked).
	`
	CREATE TABLE IF NOT EXISTS tokens (
	    id            INTEGER PRIMARY KEY AUTOINCREMENT,
	    name          TEXT NOT NULL UNIQUE,
	    hashed_token  TEXT NOT NULL UNIQUE,
	    created_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    last_used_at  INTEGER,
	    revoked_at    INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_tokens_hash ON tokens(hashed_token);
	`,

	// 4: claim leases. claimed_at records when the current assignee took
	// the issue, so an orphaned claim (crashed agent) can expire and be
	// reclaimed. Invariant: claimed_at IS NOT NULL iff assignee IS NOT NULL.
	// Backfill existing claims from updated_at so they carry a real age
	// rather than being instantly stealable post-migration.
	`
	ALTER TABLE issues ADD COLUMN claimed_at INTEGER;
	UPDATE issues SET claimed_at = updated_at WHERE assignee IS NOT NULL;
	`,

	// 5: moongitci — per-repo CI opt-in plus run/job tracking. ci_enabled
	// gates execution (off by default; CI runs untrusted repo code, so it's
	// strictly opt-in). ci_runs.number is per-repo and monotonic like issues.
	// status mirrors the runner lifecycle; claimed_at is the runner lease
	// start (NULL until a runner claims it) so a crashed runner's 'running'
	// row can expire and be re-claimed, same as the issue claim lease.
	`
	ALTER TABLE repos ADD COLUMN ci_enabled INTEGER NOT NULL DEFAULT 0;

	CREATE TABLE IF NOT EXISTS ci_runs (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    repo_id     INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
	    number      INTEGER NOT NULL,
	    commit_sha  TEXT NOT NULL,
	    ref         TEXT NOT NULL,
	    event       TEXT NOT NULL,
	    trigger     TEXT NOT NULL DEFAULT '',
	    status      TEXT NOT NULL DEFAULT 'queued',
	    claimed_at  INTEGER,
	    created_at  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    started_at  INTEGER,
	    finished_at INTEGER,
	    UNIQUE (repo_id, number)
	);
	CREATE INDEX IF NOT EXISTS idx_ci_runs_repo   ON ci_runs(repo_id);
	CREATE INDEX IF NOT EXISTS idx_ci_runs_status ON ci_runs(status);

	CREATE TABLE IF NOT EXISTS ci_jobs (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    run_id      INTEGER NOT NULL REFERENCES ci_runs(id) ON DELETE CASCADE,
	    name        TEXT NOT NULL,
	    status      TEXT NOT NULL DEFAULT 'queued',
	    exit_code   INTEGER,
	    created_at  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    started_at  INTEGER,
	    finished_at INTEGER,
	    UNIQUE (run_id, name)
	);
	CREATE INDEX IF NOT EXISTS idx_ci_jobs_run ON ci_jobs(run_id);
	`,
}

// Migrate brings the database up to the latest schema version. Idempotent —
// already-applied migrations are skipped via the schema_version table.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
		    version    INTEGER PRIMARY KEY,
		    applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		)`); err != nil {
		return err
	}

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return err
	}

	for i := current; i < len(migrations); i++ {
		ver := i + 1
		if _, err := db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", ver, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version(version) VALUES (?)`, ver); err != nil {
			return fmt.Errorf("record migration %d: %w", ver, err)
		}
	}
	return nil
}
