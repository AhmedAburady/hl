package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Target struct {
	Host        string
	User        string
	Port        int
	KeyPath     string
	AgentSocket string // explicit ssh-agent socket; empty falls back to $SSH_AUTH_SOCK
}

func (t Target) addr() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

func expand(path string) string {
	if path == "" {
		return ""
	}
	if path == "~" || (len(path) >= 2 && path[:2] == "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// authMethods builds the SSH auth methods and a cleanup func the caller must
// invoke once the handshake is done (it closes the ssh-agent connection, if any,
// which must stay open through authentication).
func authMethods(keyPath, agentSocket string) ([]ssh.AuthMethod, func(), error) {
	var methods []ssh.AuthMethod
	cleanup := func() {}
	if keyPath != "" {
		data, err := os.ReadFile(expand(keyPath))
		if err != nil {
			return nil, nil, fmt.Errorf("read key %s: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse key %s: %w", keyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	// Try the configured socket first, then fall back to $SSH_AUTH_SOCK if the
	// configured one is unset or cannot be reached.
	var sockets []string
	if s := expand(agentSocket); s != "" {
		sockets = append(sockets, s)
	}
	if env := os.Getenv("SSH_AUTH_SOCK"); env != "" {
		sockets = append(sockets, env)
	}
	for _, sock := range sockets {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			continue
		}
		ag := agent.NewClient(conn)
		methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
		cleanup = func() { _ = conn.Close() }
		break
	}

	if len(methods) == 0 {
		return nil, nil, errors.New("no SSH auth methods: set caddy.remote.key, caddy.remote.agent_socket, or run ssh-agent")
	}
	return methods, cleanup, nil
}

// knownHostsCallback returns the raw knownhosts checker, or ok == false when no
// known_hosts file is usable (caller then skips verification, as before).
func knownHostsCallback() (ssh.HostKeyCallback, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	cb, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		slog.Warn("known_hosts unavailable, skipping host key verification", "err", err)
		return nil, false
	}
	return cb, true
}

// tofuCallback wraps the knownhosts checker to accept an unknown host on first
// use (with a warning) while still rejecting a genuine key mismatch.
func tofuCallback(cb ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		if ke, ok := errors.AsType[*knownhosts.KeyError](err); ok && len(ke.Want) == 0 {
			if _, dup := warnedHosts.LoadOrStore(hostname, true); !dup {
				slog.Warn("host key not in known_hosts, accepting on first use", "host", hostname)
			}
			return nil
		}
		return err
	}
}

// hostKeyAlgorithms returns the host-key algorithms to request for addr, taken
// from the key types already pinned in known_hosts. This mirrors OpenSSH, which
// offers the type it has on record; without it the Go client can be handed a key
// type that isn't pinned (e.g. ecdsa when only ed25519 is known) and report a
// false "key mismatch". Returns nil for an unknown host so first-use still works.
func hostKeyAlgorithms(cb ssh.HostKeyCallback, addr string) []string {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil
	}
	probe, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil
	}
	var remote net.Addr
	if host, port, err := net.SplitHostPort(addr); err == nil {
		p, _ := strconv.Atoi(port)
		remote = &net.TCPAddr{IP: net.ParseIP(host), Port: p}
	}
	// A throwaway key never matches, so knownhosts reports every key pinned for
	// the host in KeyError.Want; their types are the algorithms to request.
	ke, ok := errors.AsType[*knownhosts.KeyError](cb(addr, remote, probe))
	if !ok || len(ke.Want) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var algos []string
	for _, k := range ke.Want {
		for _, a := range signatureAlgos(k.Key.Type()) {
			if !seen[a] {
				seen[a] = true
				algos = append(algos, a)
			}
		}
	}
	return algos
}

// signatureAlgos maps a pinned host-key type to the signature algorithms a
// client should advertise for it. An RSA key expands to the SHA-2 variants
// modern servers require (plain ssh-rsa is often refused).
func signatureAlgos(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

// warnedHosts dedupes the first-use host-key warning so a single run that opens
// several connections to the same host only warns once.
var warnedHosts sync.Map

func dial(ctx context.Context, t Target) (*ssh.Client, error) {
	methods, cleanup, err := authMethods(t.KeyPath, t.AgentSocket)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	cfg := &ssh.ClientConfig{
		User:    t.User,
		Auth:    methods,
		Timeout: 15 * time.Second,
	}
	if raw, ok := knownHostsCallback(); ok {
		cfg.HostKeyCallback = tofuCallback(raw)
		cfg.HostKeyAlgorithms = hostKeyAlgorithms(raw, t.addr())
	} else {
		cfg.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", t.addr())
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", t.addr(), err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, t.addr(), cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", t.addr(), err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// Run executes a command on the remote host and returns its combined output.
func Run(ctx context.Context, t Target, cmd string) (string, error) {
	client, err := dial(ctx, t)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(out), fmt.Errorf("remote command failed: %w", err)
	}
	return string(out), nil
}
