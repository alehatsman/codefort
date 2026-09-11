package storage

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// AddRepoMember grants the user named by username a role on the given repo.
// The owner cannot be added as a member (they already have admin rights).
// Role must be "read" or "write". Returns ErrNotFound if the username doesn't
// exist and ErrInvalidInput for an unrecognized role.
func AddRepoMember(db *sql.DB, repoID int64, username, role string) error {
	if role != "read" && role != "write" {
		return ErrInvalidInput
	}
	var userID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE name = ?`, username).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	_, err := db.Exec(`
		INSERT INTO repo_members(repo_id, user_id, role) VALUES (?, ?, ?)
		ON CONFLICT(repo_id, user_id) DO UPDATE SET role = excluded.role
	`, repoID, userID, role)
	return err
}

// RemoveRepoMember revokes the named user's membership on the repo.
// Returns ErrNotFound when the user is not a member.
func RemoveRepoMember(db *sql.DB, repoID int64, username string) error {
	res, err := db.Exec(`
		DELETE FROM repo_members
		WHERE repo_id = ?
		  AND user_id = (SELECT id FROM users WHERE name = ?)
	`, repoID, username)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRepoMembers returns all collaborators on the repo (does not include the
// owner themselves — ownership is checked separately).
func ListRepoMembers(db *sql.DB, repoID int64) ([]api.RepoMember, error) {
	rows, err := db.Query(`
		SELECT users.name, repo_members.role, repo_members.created_at
		FROM repo_members
		JOIN users ON users.id = repo_members.user_id
		WHERE repo_members.repo_id = ?
		ORDER BY repo_members.created_at ASC
	`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []api.RepoMember
	for rows.Next() {
		var m api.RepoMember
		var ts int64
		if err := rows.Scan(&m.Username, &m.Role, &ts); err != nil {
			return nil, err
		}
		m.JoinedAt = time.Unix(ts, 0).UTC()
		members = append(members, m)
	}
	if members == nil {
		members = []api.RepoMember{}
	}
	return members, rows.Err()
}

// CanAccessRepo reports whether the caller (identified by token name) can read
// the repo. Public repos are always readable. Private repos require either
// ownership or an explicit member row.
func CanAccessRepo(db *sql.DB, repoID int64, callerName string) (bool, error) {
	// Check visibility first.
	var vis, ownerName string
	err := db.QueryRow(`
		SELECT repos.visibility, users.name
		FROM repos JOIN users ON users.id = repos.owner_id
		WHERE repos.id = ?
	`, repoID).Scan(&vis, &ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if vis == "public" {
		return true, nil
	}
	// Private — check owner or member.
	if ownerName == callerName {
		return true, nil
	}
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM repo_members
		JOIN users ON users.id = repo_members.user_id
		WHERE repo_members.repo_id = ? AND users.name = ?
	`, repoID, callerName).Scan(&count)
	return count > 0, err
}

// CanWriteRepo reports whether the caller can mutate the repo (create issues,
// push, etc.). Owners always have write access; members need role='write'.
func CanWriteRepo(db *sql.DB, repoID int64, callerName string) (bool, error) {
	var ownerName string
	err := db.QueryRow(`
		SELECT users.name FROM repos JOIN users ON users.id = repos.owner_id
		WHERE repos.id = ?
	`, repoID).Scan(&ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if ownerName == callerName {
		return true, nil
	}
	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM repo_members
		JOIN users ON users.id = repo_members.user_id
		WHERE repo_members.repo_id = ? AND users.name = ? AND role = 'write'
	`, repoID, callerName).Scan(&count)
	return count > 0, err
}

// IsRepoOwner reports whether callerName is the owner of the repo.
func IsRepoOwner(db *sql.DB, repoID int64, callerName string) (bool, error) {
	var ownerName string
	err := db.QueryRow(`
		SELECT users.name FROM repos JOIN users ON users.id = repos.owner_id
		WHERE repos.id = ?
	`, repoID).Scan(&ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return strings.EqualFold(ownerName, callerName), nil
}

// RepoIsPrivate reports whether a repo's visibility is 'private'. Split out
// from CanAccessRepo because the git transport needs the cheap public-repo
// short-circuit before it goes looking for a credential: a public clone must
// stay open, and must not pay for an auth lookup it will ignore.
func RepoIsPrivate(db *sql.DB, repoID int64) (bool, error) {
	var vis string
	err := db.QueryRow(`SELECT visibility FROM repos WHERE id = ?`, repoID).Scan(&vis)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return vis == "private", nil
}
