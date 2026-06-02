package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/alehatsman/moongit/internal/storage"
)

// extTokenName and extFingerprint key the authenticated identity into the SSH
// connection's Permissions.Extensions, set by the publickey callback and read
// back when running the git service so the push identity matches the Bearer
// path.
const (
	extTokenName   = "moongit-token-name"
	extFingerprint = "moongit-fingerprint"
)

// ServeSSH runs the git SSH transport on addr until ctx is cancelled. It is
// opt-in: cmd/moongitd only calls it when MOONGIT_SSH_ADDR is set, so the
// default deployment stays a single HTTP port. Authentication is publickey
// only — every connection must present a registered key (HTTP remains the open
// path); there is no anonymous SSH. Blocks until the listener closes.
func (s *Server) ServeSSH(ctx context.Context, addr string) error {
	cfg, err := s.sshServerConfig()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ssh listen %s: %w", addr, err)
	}
	s.logger.Info("ssh: listening", "addr", ln.Addr().String())
	return s.acceptSSH(ctx, ln, cfg)
}

// sshServerConfig loads the host key and builds the publickey-only server
// config. Split out from ServeSSH so tests can drive a listener directly.
func (s *Server) sshServerConfig() (*ssh.ServerConfig, error) {
	signer, err := s.loadOrCreateHostKey()
	if err != nil {
		return nil, fmt.Errorf("ssh host key: %w", err)
	}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fp := ssh.FingerprintSHA256(key)
			tok, err := storage.LookupTokenBySSHKey(s.rdb, fp)
			if errors.Is(err, storage.ErrNotFound) {
				return nil, fmt.Errorf("unknown public key")
			}
			if err != nil {
				s.logger.Error("ssh: key lookup", "err", err)
				return nil, fmt.Errorf("internal error")
			}
			// Record usage off the auth critical path, like TouchToken.
			storage.TouchSSHKey(s.db, fp)
			return &ssh.Permissions{Extensions: map[string]string{
				extTokenName:   tok.Name,
				extFingerprint: fp,
			}}, nil
		},
	}
	cfg.AddHostKey(signer)
	return cfg, nil
}

// acceptSSH runs the accept loop on ln, serving each connection in its own
// goroutine, until ctx is cancelled (which closes ln and unblocks Accept).
func (s *Server) acceptSSH(ctx context.Context, ln net.Listener, cfg *ssh.ServerConfig) error {
	// Close the listener on shutdown so Accept unblocks and we return cleanly.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // shutdown
			}
			// A transient Accept error (fd exhaustion under load, ECONNABORTED)
			// is recoverable — log and retry after a short backoff rather than
			// tearing down the whole SSH transport (and, via listenErr, the
			// daemon) for the rest of the process lifetime. The listener only
			// dies for good on shutdown, handled by the ctx.Err() check above.
			// Mirrors net/http.Server.Serve's temporary-error backoff.
			s.logger.Error("ssh: accept (retrying)", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		go s.handleSSHConn(ctx, conn, cfg)
	}
}

// handleSSHConn completes the SSH handshake and services the connection's
// channels. Each connection runs in its own goroutine.
func (s *Server) handleSSHConn(ctx context.Context, nConn net.Conn, cfg *ssh.ServerConfig) {
	defer nConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		// Failed auth / handshake — common and not worth error-level noise.
		s.logger.Debug("ssh: handshake", "remote", nConn.RemoteAddr().String(), "err", err)
		return
	}
	defer sshConn.Close()

	identity := sshConn.Permissions.Extensions[extTokenName]
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			s.logger.Error("ssh: accept channel", "err", err)
			continue
		}
		go s.handleSSHSession(ctx, ch, chReqs, identity)
	}
}

// handleSSHSession serves a single session channel. It accepts exactly one
// "exec" request carrying a git-upload-pack / git-receive-pack command, passes
// through GIT_PROTOCOL for protocol-v2, runs the service, and replies with the
// exit status. pty/shell and any non-git command are rejected.
func (s *Server) handleSSHSession(ctx context.Context, ch ssh.Channel, reqs <-chan *ssh.Request, identity string) {
	defer ch.Close()

	gitProtocol := ""
	for req := range reqs {
		switch req.Type {
		case "env":
			// Honor GIT_PROTOCOL (enables protocol v2); ignore the rest.
			var env struct{ Name, Value string }
			if ssh.Unmarshal(req.Payload, &env) == nil && env.Name == "GIT_PROTOCOL" {
				gitProtocol = env.Value
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				return
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			code := s.runGitOverSSH(ctx, ch, payload.Command, identity, gitProtocol)
			sendExitStatus(ch, code)
			return
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

// runGitOverSSH parses and runs a git service command, wiring the channel as
// the service's stdio. Returns the exit code to report to the client (1 on any
// rejection so the client surfaces a clean failure). The command is parsed
// without a shell.
func (s *Server) runGitOverSSH(ctx context.Context, ch ssh.Channel, command, identity, gitProtocol string) uint32 {
	service, owner, repo, err := parseGitSSHCommand(command)
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "moongit: %s\n", err)
		return 1
	}
	repoDir, err := repoPath(s.cfg.ReposDir, owner, repo)
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "moongit: %s\n", err)
		return 1
	}
	if _, err := os.Stat(repoDir); err != nil {
		fmt.Fprintf(ch.Stderr(), "moongit: repository not found\n")
		return 1
	}

	cmd := exec.CommandContext(ctx, "git", strings.TrimPrefix(service, "git-"), repoDir)
	cmd.Stdin = ch
	cmd.Stdout = ch
	cmd.Stderr = ch.Stderr()
	cmd.Env = os.Environ()
	if gitProtocol != "" {
		cmd.Env = append(cmd.Env, "GIT_PROTOCOL="+gitProtocol)
	}
	// On push, hand the post-receive hook the same notification env the HTTP
	// path injects — the pusher here is the authenticated key's token name.
	if service == "git-receive-pack" {
		cmd.Env = append(cmd.Env,
			"MOONGIT_CI_URL="+s.ciURL,
			"MOONGIT_CI_SECRET="+s.ciSecret,
			"MOONGIT_CI_REPO="+owner+"/"+strings.TrimSuffix(repo, ".git"),
			"MOONGIT_CI_PUSHER="+identity,
		)
	}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return uint32(exitErr.ExitCode())
		}
		s.logger.Error("ssh: git service failed", "service", service, "repo", repoDir, "err", err)
		return 1
	}
	return 0
}

// parseGitSSHCommand extracts the service verb and owner/repo from a git SSH
// exec command such as `git-upload-pack 'owner/repo.git'`. Only the two git
// pack services are accepted; anything else (a shell, an arbitrary binary) is
// rejected. The path is single-token and de-quoted; owner/repo validation and
// traversal defense are left to repoPath.
func parseGitSSHCommand(command string) (service, owner, repo string, err error) {
	command = strings.TrimSpace(command)
	verb, rest, found := strings.Cut(command, " ")
	if !found {
		return "", "", "", fmt.Errorf("unsupported command")
	}
	if _, ok := validServices[verb]; !ok {
		return "", "", "", fmt.Errorf("unsupported command")
	}
	// git quotes the path with single quotes; strip surrounding quotes and any
	// leading slash, then require exactly owner/repo.
	path := strings.TrimSpace(rest)
	path = strings.Trim(path, "'\"")
	path = strings.TrimPrefix(path, "/")
	o, r, found := strings.Cut(path, "/")
	if !found || o == "" || r == "" || strings.Contains(r, "/") {
		return "", "", "", fmt.Errorf("malformed repository path")
	}
	return verb, o, r, nil
}

// sendExitStatus sends git's expected exit-status reply on the channel.
func sendExitStatus(ch ssh.Channel, code uint32) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
}

// loadOrCreateHostKey loads the persisted SSH host key, generating a fresh
// ed25519 key (persisted 0600) on first run so the server's host identity is
// stable across restarts.
func (s *Server) loadOrCreateHostKey() (ssh.Signer, error) {
	path := s.cfg.SSHHostKey
	if data, err := os.ReadFile(path); err == nil {
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse host key %s: %w", path, err)
		}
		return signer, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read host key %s: %w", path, err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write host key %s: %w", path, err)
	}
	s.logger.Info("ssh: generated host key", "path", path)
	return ssh.ParsePrivateKey(pemBytes)
}
