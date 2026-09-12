package storage

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/alehatsman/codefort/internal/api"
	"golang.org/x/crypto/bcrypt"
)

// ErrUserExists is returned when the requested username is already taken.
var ErrUserExists = errors.New("username already exists")

// ErrBadCredentials is returned by LoginUser when the password is wrong or the
// user does not exist. Deliberately indistinct to prevent username enumeration.
var ErrBadCredentials = errors.New("invalid username or password")

// ErrNoPassword is returned when a user has no password set (admin-provisioned)
// and a password-based login is attempted.
var ErrNoPassword = errors.New("user has no password set; use a token instead")

// RegisterUser creates a new user account with a bcrypt-hashed password, then
// mints a bearer token for the new account. Returns the populated User, the
// api.Token metadata, and the plaintext token (shown once; store it). The
// caller must surface the plaintext immediately — only the hash is stored.
func RegisterUser(db *sql.DB, username, password string) (api.User, api.Token, string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return api.User{}, api.Token{}, "", errors.New("username is required")
	}
	if len(password) < 8 {
		return api.User{}, api.Token{}, "", errors.New("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return api.User{}, api.Token{}, "", err
	}

	tx, err := db.Begin()
	if err != nil {
		return api.User{}, api.Token{}, "", err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO users(name, password_hash) VALUES (?, ?)`,
		username, string(hash),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return api.User{}, api.Token{}, "", ErrUserExists
		}
		return api.User{}, api.Token{}, "", err
	}
	userID, _ := res.LastInsertId()

	plain, err := GenerateTokenString()
	if err != nil {
		return api.User{}, api.Token{}, "", err
	}
	hashed := HashToken(plain)
	tokenRes, err := tx.Exec(
		`INSERT INTO tokens(name, hashed_token, user_id) VALUES (?, ?, ?)`,
		username, hashed, userID,
	)
	if err != nil {
		return api.User{}, api.Token{}, "", err
	}
	tokenID, _ := tokenRes.LastInsertId()

	if err := tx.Commit(); err != nil {
		return api.User{}, api.Token{}, "", err
	}

	user := api.User{ID: userID, Name: username, CreatedAt: time.Now().UTC()}
	tok := api.Token{ID: tokenID, Name: username, CreatedAt: time.Now().UTC()}
	return user, tok, plain, nil
}

// LoginUser validates a username/password pair and mints a new token for the
// session. Each call produces a fresh token; the caller stores it like any
// other mgt_ token.
func LoginUser(db *sql.DB, username, password string) (api.Token, string, error) {
	var userID int64
	var hashStr sql.NullString
	err := db.QueryRow(
		`SELECT id, password_hash FROM users WHERE name = ?`, username,
	).Scan(&userID, &hashStr)
	if errors.Is(err, sql.ErrNoRows) {
		return api.Token{}, "", ErrBadCredentials
	}
	if err != nil {
		return api.Token{}, "", err
	}
	if !hashStr.Valid || hashStr.String == "" {
		return api.Token{}, "", ErrNoPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hashStr.String), []byte(password)); err != nil {
		return api.Token{}, "", ErrBadCredentials
	}

	plain, err := GenerateTokenString()
	if err != nil {
		return api.Token{}, "", err
	}
	hashed := HashToken(plain)
	res, err := db.Exec(
		`INSERT INTO tokens(name, hashed_token, user_id) VALUES (?, ?, ?)`,
		username+"-session", hashed, userID,
	)
	if err != nil {
		return api.Token{}, "", err
	}
	tokenID, _ := res.LastInsertId()
	tok := api.Token{ID: tokenID, Name: username, CreatedAt: time.Now().UTC()}
	return tok, plain, nil
}

// GetUser returns a user's public profile by username. Returns ErrNotFound
// when the user does not exist.
func GetUser(db *sql.DB, username string) (api.User, error) {
	var u api.User
	var createdAt int64
	err := db.QueryRow(
		`SELECT id, name, created_at FROM users WHERE name = ?`, username,
	).Scan(&u.ID, &u.Name, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	u.CreatedAt = time.Unix(createdAt, 0).UTC()
	return u, nil
}
