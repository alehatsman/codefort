package storage

import (
	"database/sql"
	"errors"
)

// SpecVerification is one recorded verify pass over a spec (see migration 19).
// Result is the specs.VerificationResult marshaled to JSON; the caller owns
// (un)marshaling so storage stays free of the specs package.
type SpecVerification struct {
	ID        int64
	RepoID    int64
	SpecID    string
	SpecPath  string
	CommitSHA string
	Alignment float64
	Result    string
	Verifier  string
	CreatedAt int64
}

const specVerificationColumns = "id, repo_id, spec_id, spec_path, commit_sha, alignment, result, verifier, created_at"

func scanSpecVerification(row interface{ Scan(...any) error }) (SpecVerification, error) {
	var v SpecVerification
	err := row.Scan(&v.ID, &v.RepoID, &v.SpecID, &v.SpecPath, &v.CommitSHA, &v.Alignment, &v.Result, &v.Verifier, &v.CreatedAt)
	return v, err
}

// RecordVerification inserts a verify pass and returns the stored row.
func RecordVerification(db *sql.DB, v SpecVerification) (SpecVerification, error) {
	result := v.Result
	if result == "" {
		result = "{}"
	}
	row := db.QueryRow(`
		INSERT INTO spec_verifications(repo_id, spec_id, spec_path, commit_sha, alignment, result, verifier)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING `+specVerificationColumns, v.RepoID, v.SpecID, v.SpecPath, v.CommitSHA, v.Alignment, result, v.Verifier)
	return scanSpecVerification(row)
}

// LatestVerification returns the most recent verify pass for a spec id, or
// ErrNotFound when the spec has never been verified.
func LatestVerification(db *sql.DB, repoID int64, specID string) (SpecVerification, error) {
	row := db.QueryRow(`
		SELECT `+specVerificationColumns+`
		FROM spec_verifications
		WHERE repo_id = ? AND spec_id = ?
		ORDER BY id DESC LIMIT 1`, repoID, specID)
	v, err := scanSpecVerification(row)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// ListVerifications returns a spec's verify history, newest first, capped at
// limit (<=0 means a default of 50).
func ListVerifications(db *sql.DB, repoID int64, specID string, limit int) ([]SpecVerification, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT `+specVerificationColumns+`
		FROM spec_verifications
		WHERE repo_id = ? AND spec_id = ?
		ORDER BY id DESC LIMIT ?`, repoID, specID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SpecVerification, 0)
	for rows.Next() {
		v, err := scanSpecVerification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
