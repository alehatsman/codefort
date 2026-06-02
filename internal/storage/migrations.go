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

	// 6: humane CI IO. Freeze each run's commit subject + author at enqueue
	// (a SHA alone has no human meaning in the UI), and record each job's
	// `needs` so the run-detail view can draw the DAG instead of a flat tab
	// list. needs is a JSON array of job names ('' for a root job).
	`
	ALTER TABLE ci_runs ADD COLUMN commit_msg    TEXT NOT NULL DEFAULT '';
	ALTER TABLE ci_runs ADD COLUMN commit_author TEXT NOT NULL DEFAULT '';
	ALTER TABLE ci_jobs ADD COLUMN needs         TEXT NOT NULL DEFAULT '';
	`,

	// 7: code review comments. A comment anchored to a line range of a file,
	// bound to a branch (ref) so a review is scoped to the code as it stands
	// on that branch. commit_sha freezes the ref's HEAD at comment time so a
	// later reader can tell whether the lines have since drifted. resolved
	// drives the open/done lifecycle. Author is the token name, as elsewhere.
	`
	CREATE TABLE IF NOT EXISTS code_comments (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    repo_id     INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
	    ref         TEXT NOT NULL,
	    path        TEXT NOT NULL,
	    start_line  INTEGER NOT NULL,
	    end_line    INTEGER NOT NULL,
	    commit_sha  TEXT NOT NULL DEFAULT '',
	    author      TEXT NOT NULL,
	    body        TEXT NOT NULL,
	    resolved    INTEGER NOT NULL DEFAULT 0,
	    created_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);
	CREATE INDEX IF NOT EXISTS idx_code_comments_repo ON code_comments(repo_id, ref);
	CREATE INDEX IF NOT EXISTS idx_code_comments_path ON code_comments(repo_id, ref, path);
	`,

	// 8: pull requests. A lightweight PR object pairing a head branch with a
	// base branch; number is per-repo and monotonic like issues/ci_runs. state
	// is open|merged|closed. merged_at is set only when a PR is merged (the
	// merge endpoint, #81); it stays NULL for open and plain-closed PRs.
	// Review threads are NOT stored here — they reuse code_comments anchored to
	// head_ref (api.CodeComment carries Ref), so there is no PR-comment table.
	`
	CREATE TABLE IF NOT EXISTS pull_requests (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    repo_id    INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
	    number     INTEGER NOT NULL,
	    base_ref   TEXT NOT NULL,
	    head_ref   TEXT NOT NULL,
	    title      TEXT NOT NULL,
	    body       TEXT NOT NULL DEFAULT '',
	    author     TEXT NOT NULL,
	    state      TEXT NOT NULL DEFAULT 'open',
	    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    merged_at  INTEGER,
	    UNIQUE (repo_id, number)
	);
	CREATE INDEX IF NOT EXISTS idx_pulls_repo  ON pull_requests(repo_id);
	CREATE INDEX IF NOT EXISTS idx_pulls_state ON pull_requests(state);
	`,

	// 9: SSH public keys for the git SSH transport. Each key is registered
	// against a token (ON DELETE CASCADE: revoking-then-deleting a token would
	// drop its keys, though tokens are only ever soft-revoked today). The key's
	// token name is the push/pull identity, exactly like the Bearer path — one
	// identity primitive, two credentials. fingerprint is the SHA256 form
	// (ssh.FingerprintSHA256) and is the unique lookup key during auth;
	// public_key stores the full authorized_keys line for display + re-parse.
	`
	CREATE TABLE IF NOT EXISTS ssh_keys (
	    id           INTEGER PRIMARY KEY AUTOINCREMENT,
	    token_id     INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
	    fingerprint  TEXT NOT NULL UNIQUE,
	    public_key   TEXT NOT NULL,
	    comment      TEXT NOT NULL DEFAULT '',
	    created_at   INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    last_used_at INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_ssh_keys_token ON ssh_keys(token_id);
	`,

	// 10: agent runs. An issue-spawned Claude agent reuses the entire CI run
	// spine (event log, SSE, container isolation, lifecycle), modeled as a
	// synthetic single-job run. kind distinguishes a normal pipeline run
	// ('ci', the default for every pre-existing row) from an agent run
	// ('agent'); issue_number links an agent run to the issue it works (NULL
	// for CI runs). See #74.
	`
	ALTER TABLE ci_runs ADD COLUMN kind         TEXT NOT NULL DEFAULT 'ci';
	ALTER TABLE ci_runs ADD COLUMN issue_number INTEGER;
	CREATE INDEX IF NOT EXISTS idx_ci_runs_kind ON ci_runs(kind);
	`,

	// 11: agent turns. An agent run is a conversation: turn 1 is the issue body
	// (executed inline, not stored), and each later human message is a row here
	// dispatched as its own claude --resume turn while the run sits in
	// awaiting_input between turns. seq is per-run and monotonic. status mirrors
	// the dispatch lifecycle; claimed_at is the dispatcher lease so a crashed
	// dispatch can be retried, same pattern as ci_runs. See #76.
	`
	CREATE TABLE IF NOT EXISTS agent_turns (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    run_id      INTEGER NOT NULL REFERENCES ci_runs(id) ON DELETE CASCADE,
	    seq         INTEGER NOT NULL,
	    author      TEXT NOT NULL,
	    body        TEXT NOT NULL,
	    status      TEXT NOT NULL DEFAULT 'pending',
	    claimed_at  INTEGER,
	    created_at  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	    started_at  INTEGER,
	    finished_at INTEGER,
	    UNIQUE (run_id, seq)
	);
	CREATE INDEX IF NOT EXISTS idx_agent_turns_run    ON agent_turns(run_id);
	CREATE INDEX IF NOT EXISTS idx_agent_turns_status ON agent_turns(status);
	`,

	// 12: server settings — a generic key/value store for operator-set config
	// that shouldn't require a restart (e.g. the global agent Claude token,
	// #106). Values may be secrets; the API surface that reads them must be
	// write-only (never echo a secret back).
	`
	CREATE TABLE IF NOT EXISTS settings (
	    key        TEXT PRIMARY KEY,
	    value      TEXT NOT NULL,
	    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);
	`,

	// 13: per-run agent execution model (#110). An agent run executes via
	// one of the pluggable execution models ('claude-edit' = claude edits
	// files directly; 'mooncake-pilot' = mooncake plans+applies actions).
	// Chosen at spawn, stored here. CI runs ignore it; the default keeps
	// existing rows on the original model.
	`
	ALTER TABLE ci_runs ADD COLUMN execution_model TEXT NOT NULL DEFAULT 'claude-edit';
	`,

	// 14: per-run "allow shell" override for the mooncake-pilot model (#110).
	// The default policy denies shell/cmd; checking "allow shell" at spawn
	// drops that denial for this run only. 0 = deny (safe default), 1 = allow.
	// Ignored by claude-edit and CI runs.
	`
	ALTER TABLE ci_runs ADD COLUMN pilot_allow_shell INTEGER NOT NULL DEFAULT 0;
	`,

	// 15: outbound event feed (#73). A single append-only log of fleet-visible
	// events — pushes, issue/claim changes, CI run lifecycle — that the
	// GET /api/events SSE endpoint replays + live-tails. id is the global
	// monotonic seq (the SSE event id): gap-tolerant and resume-friendly via
	// Last-Event-ID, the same trick ci_runs.number uses but server-wide instead
	// of per-repo. repo_id is nullable (room for non-repo events) and cascades
	// so a deleted repo drops its events. payload is an opaque JSON blob whose
	// shape depends on type; actor is the token name (or pusher) that caused it.
	// Bounded by the event retention reaper, mirroring CI run retention.
	`
	CREATE TABLE IF NOT EXISTS events (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    type       TEXT NOT NULL,
	    repo_id    INTEGER REFERENCES repos(id) ON DELETE CASCADE,
	    actor      TEXT NOT NULL DEFAULT '',
	    payload    TEXT NOT NULL DEFAULT '{}',
	    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);
	CREATE INDEX IF NOT EXISTS idx_events_repo ON events(repo_id);
	`,

	// 16: freeze a merged PR's compare endpoints. The PR detail diff is computed
	// from the live base/head branch tips; once head is merged into base, head
	// is contained in base, so merge-base(base, head) == head and the diff goes
	// empty — a merged PR renders with no files/commits. Record the two branch
	// tips at merge time so the detail endpoint can reproduce the exact pre-merge
	// compare regardless of where the branches drift (or whether they're deleted)
	// afterward. NULL on open/closed PRs and on PRs merged before this migration.
	`
	ALTER TABLE pull_requests ADD COLUMN merge_base_sha TEXT;
	ALTER TABLE pull_requests ADD COLUMN merge_head_sha TEXT;
	`,

	// 17: adopt mooncake's pilot→agent rename (#75, no back-compat). The
	// execution model formerly 'mooncake-pilot' is now 'mooncake-agent', and
	// the per-run override column pilot_allow_shell becomes mooncake_allow_shell
	// to match. Both rename in place so existing agent runs keep their model +
	// allow-shell flag; claude-edit / CI rows are untouched.
	`
	UPDATE ci_runs SET execution_model = 'mooncake-agent' WHERE execution_model = 'mooncake-pilot';
	ALTER TABLE ci_runs RENAME COLUMN pilot_allow_shell TO mooncake_allow_shell;
	`,

	// 18: per-run agent tool profile (#184). Names the slice of the mgit MCP
	// toolset a run may see ('full' | 'review'); the shim enforces it. 'full'
	// is the column default so existing agent runs and CI rows keep the
	// current full surface.
	`
	ALTER TABLE ci_runs ADD COLUMN tool_profile TEXT NOT NULL DEFAULT 'full';
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
		// Run each migration and its version bump in one transaction. SQLite
		// supports transactional DDL, so a multi-statement migration that fails
		// partway rolls back wholesale rather than leaving a half-applied schema
		// that can't be cleanly re-run (a non-idempotent ALTER would then collide
		// on the next startup).
		if err := applyMigration(db, ver, migrations[i]); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one migration's SQL and records its version atomically.
func applyMigration(db *sql.DB, ver int, sqlText string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migration %d: begin: %w", ver, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(sqlText); err != nil {
		return fmt.Errorf("migration %d: %w", ver, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, ver); err != nil {
		return fmt.Errorf("record migration %d: %w", ver, err)
	}
	return tx.Commit()
}
