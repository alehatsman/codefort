package storage

import (
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("not found")

// EnsureUser returns the user id, creating the row if absent.
func EnsureUser(db *sql.DB, name string) (int64, error) {
	if _, err := db.Exec("INSERT OR IGNORE INTO users(name) VALUES (?)", name); err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM users WHERE name = ?", name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// EnsureRepo returns the repo id, creating both the owner user and the repo
// row if absent. Idempotent.
func EnsureRepo(db *sql.DB, owner, name string) (int64, error) {
	userID, err := EnsureUser(db, owner)
	if err != nil {
		return 0, err
	}
	if _, err := db.Exec("INSERT OR IGNORE INTO repos(owner_id, name) VALUES (?, ?)", userID, name); err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM repos WHERE owner_id = ? AND name = ?", userID, name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// LookupRepo returns the repo id or ErrNotFound when the owner/name pair is
// unknown.
func LookupRepo(db *sql.DB, owner, name string) (int64, error) {
	var id int64
	err := db.QueryRow(`
		SELECT repos.id FROM repos
		JOIN users ON users.id = repos.owner_id
		WHERE users.name = ? AND repos.name = ?
	`, owner, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
