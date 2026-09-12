package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

func TestParseGitSSHCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantErr     bool
		service     string
		owner, repo string
	}{
		{name: "upload-pack quoted", command: "git-upload-pack 'alice/proj.git'", service: "git-upload-pack", owner: "alice", repo: "proj.git"},
		{name: "receive-pack quoted", command: "git-receive-pack 'alice/proj.git'", service: "git-receive-pack", owner: "alice", repo: "proj.git"},
		{name: "leading slash", command: "git-upload-pack '/alice/proj.git'", service: "git-upload-pack", owner: "alice", repo: "proj.git"},
		{name: "double quotes", command: `git-upload-pack "alice/proj"`, service: "git-upload-pack", owner: "alice", repo: "proj"},
		{name: "no quotes", command: "git-upload-pack alice/proj.git", service: "git-upload-pack", owner: "alice", repo: "proj.git"},
		{name: "unknown verb rejected", command: "rm -rf /", wantErr: true},
		{name: "extra path segment rejected", command: "git-upload-pack 'a/b/c'", wantErr: true},
		{name: "no path rejected", command: "git-upload-pack", wantErr: true},
		{name: "empty owner rejected", command: "git-upload-pack '/proj.git'", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, owner, repo, err := parseGitSSHCommand(tt.command)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got service=%q owner=%q repo=%q", service, owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if service != tt.service || owner != tt.owner || repo != tt.repo {
				t.Errorf("got (%q,%q,%q), want (%q,%q,%q)", service, owner, repo, tt.service, tt.owner, tt.repo)
			}
		})
	}
}

func TestSSHGitTransportEndToEnd(t *testing.T) {
	requireBinaries(t, "ssh", "git")

	const owner, repo = "alice", "proj"
	db := migratedDB(t)
	if _, err := storage.EnsureRepo(db, owner, repo); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	reposDir := t.TempDir()
	bare := filepath.Join(reposDir, owner, repo+".git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v: %s", err, out)
	}

	tok, err := storage.CreateToken(db, "alice", "cf_alice")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	idFile, fingerprint := writeClientKey(t, db, tok.ID)

	port := startSSHServer(t, db, reposDir)
	env := gitSSHEnv(idFile)
	url := fmt.Sprintf("ssh://git@127.0.0.1:%s/%s/%s.git", port, owner, repo)

	// Clone exercises upload-pack auth + service (empty repo is fine).
	cloneDir := filepath.Join(t.TempDir(), "clone")
	if out, err := runEnv(env, "", "git", "clone", url, cloneDir); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	// Commit + push exercises receive-pack.
	mustGit(t, env, cloneDir, "config", "user.email", "alice@example.com")
	mustGit(t, env, cloneDir, "config", "user.name", "alice")
	if err := os.WriteFile(filepath.Join(cloneDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mustGit(t, env, cloneDir, "add", ".")
	mustGit(t, env, cloneDir, "commit", "-q", "-m", "init")
	if out, err := runEnv(env, cloneDir, "git", "push", "-q", "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}

	// The push landed in the bare repo.
	if out, err := runEnv(nil, bare, "git", "log", "--oneline"); err != nil {
		t.Fatalf("log: %v\n%s", err, out)
	} else if !strings.Contains(out, "init") {
		t.Fatalf("push didn't land; bare log = %q", out)
	}

	// Auth recorded usage on the key (last_used_at is set asynchronously, but
	// the identity resolves either way — assert the mapping holds).
	if got, err := storage.LookupTokenBySSHKey(db, fingerprint); err != nil {
		t.Fatalf("LookupTokenBySSHKey: %v", err)
	} else if got.Name != "alice" {
		t.Errorf("identity = %q, want alice", got.Name)
	}
}

func TestSSHRejectsUnregisteredKey(t *testing.T) {
	requireBinaries(t, "ssh", "git")

	db := migratedDB(t)
	if _, err := storage.EnsureRepo(db, "alice", "proj"); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	idFile := writeUnregisteredKey(t)

	port := startSSHServer(t, db, t.TempDir())
	// BatchMode + publickey-only so ssh fails fast instead of prompting.
	env := append(gitSSHEnv(idFile), "")
	env[len(env)-1] = "GIT_SSH_COMMAND=ssh -i " + idFile +
		" -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null" +
		" -o PreferredAuthentications=publickey -o BatchMode=yes"
	url := fmt.Sprintf("ssh://git@127.0.0.1:%s/alice/proj.git", port)
	if out, err := runEnv(env, "", "git", "clone", url, filepath.Join(t.TempDir(), "clone")); err == nil {
		t.Fatalf("clone with unregistered key succeeded, want auth failure: %s", out)
	}
}

// --- helpers ---

func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "ssh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// startSSHServer brings up the transport on an ephemeral port against db, with
// repos rooted at reposDir, and returns the port. It tears down on test end.
func startSSHServer(t *testing.T, db *sql.DB, reposDir string) string {
	t.Helper()
	s := &Server{
		cfg:    &config.Config{ReposDir: reposDir, SSHHostKey: filepath.Join(t.TempDir(), "hostkey")},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	cfg, err := s.sshServerConfig()
	if err != nil {
		t.Fatalf("sshServerConfig: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.acceptSSH(ctx, ln, cfg)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

func gitSSHEnv(idFile string) []string {
	return append(os.Environ(),
		"GIT_SSH_COMMAND=ssh -i "+idFile+" -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
		// Don't let the developer's global/system git config bleed in.
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
}

// writeClientKey generates an ed25519 keypair, registers the public half
// against tokenID, writes the private half as an OpenSSH identity file, and
// returns (idFile, fingerprint).
func writeClientKey(t *testing.T, db *sql.DB, tokenID int64) (idFile, fingerprint string) {
	t.Helper()
	pub, priv := genKeypair(t)
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if _, err := storage.AddSSHKey(db, tokenID, line, "test"); err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	return writeIdentity(t, priv), ssh.FingerprintSHA256(sshPub)
}

func writeUnregisteredKey(t *testing.T) string {
	t.Helper()
	_, priv := genKeypair(t)
	return writeIdentity(t, priv)
}

func genKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// writeIdentity writes priv as an OpenSSH-format identity file (the format the
// ssh client expects for ed25519 keys) and returns its path.
func writeIdentity(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return path
}

func requireBinaries(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("%s not available", n)
		}
	}
}

func mustGit(t *testing.T, env []string, dir string, args ...string) {
	t.Helper()
	if out, err := runEnv(env, dir, "git", args...); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runEnv(env []string, dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if env != nil {
		cmd.Env = env
	}
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
