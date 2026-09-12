package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alehatsman/codefort/internal/api"
)

// TokenPrefix is prepended to every generated token so they're easy to
// recognize in logs and grep results.
const TokenPrefix = "mgt_"

// ErrTokenExists is returned by CreateToken when the requested name is
// already taken (UNIQUE constraint on the name column).
var ErrTokenExists = errors.New("token name already exists")

// GenerateTokenString returns a fresh plaintext token of the form
// "mgt_<64-hex>". The caller is responsible for sending it to the user
// once — the database only stores the hash.
func GenerateTokenString() (string, error) {
	buf := make([]byte, 32) // 256 bits
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return TokenPrefix + hex.EncodeToString(buf), nil
}

// HashToken returns the canonical hex-encoded SHA-256 of a plaintext
// token, suitable for indexed lookup in the tokens table.
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreateToken inserts a new token, returning the populated api.Token
// metadata. The plaintext is the caller's responsibility to display.
func CreateToken(db *sql.DB, name, plaintext string) (api.Token, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return api.Token{}, errors.New("token name is required")
	}
	hashed := HashToken(plaintext)
	res, err := db.Exec(
		`INSERT INTO tokens(name, hashed_token) VALUES (?, ?)`,
		name, hashed,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return api.Token{}, ErrTokenExists
		}
		return api.Token{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return api.Token{}, err
	}
	return getTokenByID(db, id)
}

// LookupToken finds a token by its plaintext value and reports whether
// it's still active (not revoked). This is a pure read — callers that want
// to record usage call TouchToken separately, so the auth read can run on a
// read-only connection pool off the single writer. Returns ErrNotFound for
// unknown or revoked tokens — the caller doesn't get to distinguish, so
// leaked tokens can't be probed for liveness.
func LookupToken(db *sql.DB, plaintext string) (api.Token, error) {
	hashed := HashToken(plaintext)
	var t api.Token
	var created int64
	var lastUsed, revoked sql.NullInt64
	// LEFT JOIN, not JOIN: tokens.user_id is nullable (admin-provisioned and
	// per-run agent tokens have no account), and those must still authenticate.
	var userName sql.NullString
	err := db.QueryRow(`
		SELECT tokens.id, tokens.name, tokens.created_at,
		       tokens.last_used_at, tokens.revoked_at, users.name
		FROM tokens LEFT JOIN users ON users.id = tokens.user_id
		WHERE tokens.hashed_token = ?
	`, hashed).Scan(&t.ID, &t.Name, &created, &lastUsed, &revoked, &userName)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if revoked.Valid {
		return t, ErrNotFound
	}
	t.UserName = userName.String
	t.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		ts := time.Unix(lastUsed.Int64, 0).UTC()
		t.LastUsedAt = &ts
	}
	return t, nil
}

// TouchToken bumps last_used_at for a token. Best-effort — the error is
// ignored, since a usage-tracking miss isn't worth failing auth over. Runs
// on the writer pool.
func TouchToken(db *sql.DB, id int64) {
	_, _ = db.Exec(`UPDATE tokens SET last_used_at = strftime('%s','now') WHERE id = ?`, id)
}

// ListTokens returns active and revoked tokens with all metadata except
// the plaintext (which is unrecoverable by design).
func ListTokens(db *sql.DB) ([]api.Token, error) {
	rows, err := db.Query(`
		SELECT id, name, created_at, last_used_at, revoked_at
		FROM tokens ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make([]api.Token, 0)
	for rows.Next() {
		var t api.Token
		var created int64
		var lastUsed, revoked sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &created, &lastUsed, &revoked); err != nil {
			return nil, err
		}
		t.CreatedAt = time.Unix(created, 0).UTC()
		if lastUsed.Valid {
			ts := time.Unix(lastUsed.Int64, 0).UTC()
			t.LastUsedAt = &ts
		}
		if revoked.Valid {
			ts := time.Unix(revoked.Int64, 0).UTC()
			t.RevokedAt = &ts
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// RevokeToken sets revoked_at on the token with the given name. Idempotent
// only in that a second call updates the timestamp again.
func RevokeToken(db *sql.DB, name string) error {
	res, err := db.Exec(
		`UPDATE tokens SET revoked_at = strftime('%s','now') WHERE name = ?`,
		name,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeTokenByID sets revoked_at on the token with the given id. Returns
// ErrNotFound when no such token exists. The HTTP surface revokes by id
// (stable, URL-safe) rather than by name.
func RevokeTokenByID(db *sql.DB, id int64) error {
	res, err := db.Exec(
		`UPDATE tokens SET revoked_at = strftime('%s','now') WHERE id = ?`,
		id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAgentRunTokens revokes any still-active ephemeral per-run agent tokens
// (named "agent-run-<id>", minted by an agent run and normally revoked on
// finalize). Called at startup so tokens stranded by a crash — their run
// reconciled, but the token never revoked — don't linger valid. Returns the
// number revoked.
func RevokeAgentRunTokens(db *sql.DB) (int64, error) {
	res, err := db.Exec(
		`UPDATE tokens SET revoked_at = strftime('%s','now')
		   WHERE name LIKE 'agent-run-%' AND revoked_at IS NULL`,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeStaleAgentTokens revokes per-agent session tokens — those named
// "agent#<n>", minted one-per-spawn by the `ce` launcher — whose last
// activity is older than ttl (measured from last_used_at, falling back to
// created_at when never used). They accumulate and are never explicitly
// revoked, so the token reaper sweeps idle ones. Already-revoked rows are
// skipped. Returns the number revoked; no-op when ttl <= 0.
func RevokeStaleAgentTokens(db *sql.DB, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, nil
	}
	res, err := db.Exec(`
		UPDATE tokens
		   SET revoked_at = strftime('%s','now')
		 WHERE revoked_at IS NULL
		   AND name LIKE 'agent#%'
		   AND COALESCE(last_used_at, created_at) <= strftime('%s','now') - ?
	`, int64(ttl.Seconds()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountActiveTokens reports how many non-revoked tokens exist. Used at
// server startup to warn when zero tokens are configured.
func CountActiveTokens(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tokens WHERE revoked_at IS NULL`).Scan(&n)
	return n, err
}

func getTokenByID(db *sql.DB, id int64) (api.Token, error) {
	var t api.Token
	var created int64
	var lastUsed, revoked sql.NullInt64
	err := db.QueryRow(`
		SELECT id, name, created_at, last_used_at, revoked_at
		FROM tokens WHERE id = ?
	`, id).Scan(&t.ID, &t.Name, &created, &lastUsed, &revoked)
	if err != nil {
		return t, fmt.Errorf("read created token: %w", err)
	}
	t.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		ts := time.Unix(lastUsed.Int64, 0).UTC()
		t.LastUsedAt = &ts
	}
	if revoked.Valid {
		ts := time.Unix(revoked.Int64, 0).UTC()
		t.RevokedAt = &ts
	}
	return t, nil
}
