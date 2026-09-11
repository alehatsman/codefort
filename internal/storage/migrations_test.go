package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// applyMigrationsThrough applies migrations 1..n (1-indexed, inclusive) to db,
// letting a test exercise a specific migration's before/after state without
// running the whole ladder.
func applyMigrationsThrough(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
		    version    INTEGER PRIMARY KEY,
		    applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := applyMigration(db, i+1, migrations[i]); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
}

// Migration 29 removes the mooncake-agent execution model: any surviving
// 'mooncake-agent' rows backfill to 'claude-edit', and the now-dead
// mooncake_allow_shell column is dropped. Applied pre-29 state (through
// migration 28, where the column and value still exist) so the test exercises
// the migration's actual before/after, not just the end schema.
func TestMigration29RemovesMooncakeAgentModel(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "migrate29.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	applyMigrationsThrough(t, db, 28)

	if _, err := db.Exec(`
		INSERT INTO users(id, name) VALUES (1, 'alice');
		INSERT INTO repos(id, owner_id, name) VALUES (1, 1, 'repo');
		INSERT INTO ci_runs(repo_id, number, kind, execution_model, mooncake_allow_shell, commit_sha, ref, event)
		VALUES (1, 1, 'agent', 'mooncake-agent', 1, 'deadbeef', 'refs/heads/main', 'agent');
		INSERT INTO ci_runs(repo_id, number, kind, execution_model, mooncake_allow_shell, commit_sha, ref, event)
		VALUES (1, 2, 'agent', 'claude-edit', 0, 'deadbeef', 'refs/heads/main', 'agent');
	`); err != nil {
		t.Fatalf("seed pre-migration rows: %v", err)
	}

	if err := applyMigration(db, 29, migrations[28]); err != nil {
		t.Fatalf("apply migration 29: %v", err)
	}

	rows, err := db.Query(`SELECT number, execution_model FROM ci_runs ORDER BY number`)
	if err != nil {
		t.Fatalf("query ci_runs: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var number int
		var model string
		if err := rows.Scan(&number, &model); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, model)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	want := []string{"claude-edit", "claude-edit"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("execution_model after migration = %v, want %v (mooncake-agent backfilled, claude-edit untouched)", got, want)
	}

	if _, err := db.Query(`SELECT mooncake_allow_shell FROM ci_runs LIMIT 1`); err == nil {
		t.Error("mooncake_allow_shell column still queryable after migration 29, want it dropped")
	}
}
