package storage

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// seedTokenDB spins up a migrated temp DB with one token, returning the writer
// handle and the token's id.
func seedTokenDB(t *testing.T) (db *sql.DB, tokenID int64) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "ssh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := Migrate(d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	tok, err := CreateToken(d, "alice", "mgt_test_alice")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return d, tok.ID
}

// genAuthorizedKey returns a fresh ed25519 public key as an authorized_keys
// line plus its SHA256 fingerprint.
func genAuthorizedKey(t *testing.T, comment string) (line, fingerprint string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	line = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return line, ssh.FingerprintSHA256(sshPub)
}

func TestAddAndListSSHKey(t *testing.T) {
	db, tokenID := seedTokenDB(t)
	line, fp := genAuthorizedKey(t, "laptop")

	key, err := AddSSHKey(db, tokenID, line, "")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	if key.Fingerprint != fp {
		t.Errorf("fingerprint = %q, want %q", key.Fingerprint, fp)
	}
	if key.Comment != "laptop" {
		t.Errorf("comment = %q, want the key's own trailing comment %q", key.Comment, "laptop")
	}
	if key.TokenName != "alice" {
		t.Errorf("token name = %q, want alice", key.TokenName)
	}

	keys, err := ListSSHKeys(db, tokenID)
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Fingerprint != fp {
		t.Fatalf("ListSSHKeys = %+v, want one key with fp %q", keys, fp)
	}
}

func TestAddSSHKeyExplicitCommentWins(t *testing.T) {
	db, tokenID := seedTokenDB(t)
	line, _ := genAuthorizedKey(t, "key-comment")

	key, err := AddSSHKey(db, tokenID, line, "explicit label")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	if key.Comment != "explicit label" {
		t.Errorf("comment = %q, want the explicit label", key.Comment)
	}
}

func TestAddSSHKeyInvalid(t *testing.T) {
	db, tokenID := seedTokenDB(t)
	if _, err := AddSSHKey(db, tokenID, "not a key", ""); !errors.Is(err, ErrInvalidSSHKey) {
		t.Fatalf("err = %v, want ErrInvalidSSHKey", err)
	}
}

func TestAddSSHKeyDuplicate(t *testing.T) {
	db, tokenID := seedTokenDB(t)
	line, _ := genAuthorizedKey(t, "")
	if _, err := AddSSHKey(db, tokenID, line, ""); err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	if _, err := AddSSHKey(db, tokenID, line, ""); !errors.Is(err, ErrSSHKeyExists) {
		t.Fatalf("err = %v, want ErrSSHKeyExists", err)
	}
}

func TestLookupTokenBySSHKey(t *testing.T) {
	db, tokenID := seedTokenDB(t)
	line, fp := genAuthorizedKey(t, "")
	if _, err := AddSSHKey(db, tokenID, line, ""); err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}

	tok, err := LookupTokenBySSHKey(db, fp)
	if err != nil {
		t.Fatalf("LookupTokenBySSHKey: %v", err)
	}
	if tok.Name != "alice" {
		t.Errorf("identity = %q, want alice", tok.Name)
	}

	if _, err := LookupTokenBySSHKey(db, "SHA256:unknown"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown fingerprint err = %v, want ErrNotFound", err)
	}
}

func TestLookupTokenBySSHKeyRejectsRevoked(t *testing.T) {
	db, tokenID := seedTokenDB(t)
	line, fp := genAuthorizedKey(t, "")
	if _, err := AddSSHKey(db, tokenID, line, ""); err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	if err := RevokeToken(db, "alice"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	// A revoked token's key must stop authenticating, indistinguishable from
	// an unknown key.
	if _, err := LookupTokenBySSHKey(db, fp); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked-token key err = %v, want ErrNotFound", err)
	}
}

func TestDeleteSSHKeyScopedToOwner(t *testing.T) {
	db, aliceID := seedTokenDB(t)
	bob, err := CreateToken(db, "bob", "mgt_test_bob")
	if err != nil {
		t.Fatalf("CreateToken bob: %v", err)
	}
	line, _ := genAuthorizedKey(t, "")
	key, err := AddSSHKey(db, aliceID, line, "")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}

	// Bob can't delete Alice's key — looks like a 404 to him.
	if err := DeleteSSHKey(db, key.ID, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner delete err = %v, want ErrNotFound", err)
	}
	// Alice can.
	if err := DeleteSSHKey(db, key.ID, aliceID); err != nil {
		t.Errorf("owner delete: %v", err)
	}
	if err := DeleteSSHKey(db, key.ID, aliceID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}
