package storage

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/alehatsman/moongit/internal/api"
)

// ErrSSHKeyExists is returned by AddSSHKey when the same public key (by
// fingerprint) is already registered — keys are globally unique so a
// fingerprint maps to exactly one identity at auth time.
var ErrSSHKeyExists = errors.New("ssh key already registered")

// ErrInvalidSSHKey is returned by AddSSHKey when the supplied text isn't a
// parseable authorized_keys line.
var ErrInvalidSSHKey = errors.New("invalid ssh public key")

// AddSSHKey registers an authorized_keys line against a token. It parses and
// canonicalizes the key (so stored public_key is the normalized form), derives
// the SHA256 fingerprint used for auth lookup, and falls back to the key's own
// trailing comment when the caller passes none. Returns ErrInvalidSSHKey for
// unparseable input and ErrSSHKeyExists when the fingerprint is already taken.
func AddSSHKey(db *sql.DB, tokenID int64, publicKeyLine, comment string) (api.SSHKey, error) {
	pub, keyComment, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKeyLine))
	if err != nil {
		return api.SSHKey{}, ErrInvalidSSHKey
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = strings.TrimSpace(keyComment)
	}
	fingerprint := ssh.FingerprintSHA256(pub)
	// Store the normalized authorized_keys form (type + base64), comment aside.
	normalized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))

	res, err := db.Exec(
		`INSERT INTO ssh_keys(token_id, fingerprint, public_key, comment) VALUES (?, ?, ?, ?)`,
		tokenID, fingerprint, normalized, comment,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return api.SSHKey{}, ErrSSHKeyExists
		}
		return api.SSHKey{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return api.SSHKey{}, err
	}
	return getSSHKeyByID(db, id)
}

// ListSSHKeys returns a token's registered keys, newest first. The public half
// is not a secret, so the full key material is included.
func ListSSHKeys(db *sql.DB, tokenID int64) ([]api.SSHKey, error) {
	rows, err := db.Query(`
		SELECT k.id, t.name, k.fingerprint, k.comment, k.created_at, k.last_used_at
		FROM ssh_keys k JOIN tokens t ON t.id = k.token_id
		WHERE k.token_id = ?
		ORDER BY k.id DESC
	`, tokenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]api.SSHKey, 0)
	for rows.Next() {
		k, err := scanSSHKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// DeleteSSHKey removes a key by id, scoped to its owning token so one token
// can't delete another's key. Returns ErrNotFound when the id doesn't exist or
// belongs to a different token.
func DeleteSSHKey(db *sql.DB, id, tokenID int64) error {
	res, err := db.Exec(`DELETE FROM ssh_keys WHERE id = ? AND token_id = ?`, id, tokenID)
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

// LookupTokenBySSHKey resolves a fingerprint to its owning token's identity for
// SSH publickey auth. It rejects keys whose token has been revoked (mirroring
// LookupToken), and returns ErrNotFound for an unknown fingerprint — the caller
// can't distinguish the two, so a removed key and a revoked token both just
// fail to authenticate. Pure read; pair with TouchSSHKey to record usage.
func LookupTokenBySSHKey(db *sql.DB, fingerprint string) (api.Token, error) {
	var t api.Token
	var created int64
	var lastUsed, revoked sql.NullInt64
	err := db.QueryRow(`
		SELECT t.id, t.name, t.created_at, t.last_used_at, t.revoked_at
		FROM ssh_keys k JOIN tokens t ON t.id = k.token_id
		WHERE k.fingerprint = ?
	`, fingerprint).Scan(&t.ID, &t.Name, &created, &lastUsed, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return api.Token{}, ErrNotFound
	}
	if err != nil {
		return api.Token{}, err
	}
	if revoked.Valid {
		// Token revoked — the key no longer authenticates.
		return api.Token{}, ErrNotFound
	}
	t.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		ts := time.Unix(lastUsed.Int64, 0).UTC()
		t.LastUsedAt = &ts
	}
	return t, nil
}

// TouchSSHKey records that a key was just used to authenticate. Best-effort and
// off the auth critical path, like TouchToken. Runs on the writer pool.
func TouchSSHKey(db *sql.DB, fingerprint string) {
	_, _ = db.Exec(`UPDATE ssh_keys SET last_used_at = strftime('%s','now') WHERE fingerprint = ?`, fingerprint)
}

func getSSHKeyByID(db *sql.DB, id int64) (api.SSHKey, error) {
	row := db.QueryRow(`
		SELECT k.id, t.name, k.fingerprint, k.comment, k.created_at, k.last_used_at
		FROM ssh_keys k JOIN tokens t ON t.id = k.token_id
		WHERE k.id = ?
	`, id)
	return scanSSHKey(row)
}

// rowScanner abstracts *sql.Row and *sql.Rows so scanSSHKey serves both.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSSHKey(row rowScanner) (api.SSHKey, error) {
	var k api.SSHKey
	var created int64
	var lastUsed sql.NullInt64
	if err := row.Scan(&k.ID, &k.TokenName, &k.Fingerprint, &k.Comment, &created, &lastUsed); err != nil {
		return api.SSHKey{}, err
	}
	k.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		ts := time.Unix(lastUsed.Int64, 0).UTC()
		k.LastUsedAt = &ts
	}
	return k, nil
}
