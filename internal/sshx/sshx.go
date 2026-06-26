package sshx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Target struct {
	Host    string
	User    string
	Port    int
	KeyPath string
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

func authMethods(keyPath string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if keyPath != "" {
		data, err := os.ReadFile(expand(keyPath))
		if err != nil {
			return nil, fmt.Errorf("read key %s: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse key %s: %w", keyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
		}
	}
	if len(methods) == 0 {
		return nil, errors.New("no SSH auth methods: set caddy.remote.key or run ssh-agent")
	}
	return methods, nil
}

func hostKeyCallback() ssh.HostKeyCallback {
	home, err := os.UserHomeDir()
	if err != nil {
		return ssh.InsecureIgnoreHostKey()
	}
	cb, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		slog.Warn("known_hosts unavailable, skipping host key verification", "err", err)
		return ssh.InsecureIgnoreHostKey()
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		if ke, ok := errors.AsType[*knownhosts.KeyError](err); ok && len(ke.Want) == 0 {
			slog.Warn("host key not in known_hosts, accepting on first use", "host", hostname)
			return nil
		}
		return err
	}
}

func dial(ctx context.Context, t Target) (*ssh.Client, error) {
	methods, err := authMethods(t.KeyPath)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            t.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback(),
		Timeout:         15 * time.Second,
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

// PushFile uploads local file content to remotePath via SFTP.
func PushFile(ctx context.Context, t Target, localPath, remotePath string) error {
	client, err := dial(ctx, t)
	if err != nil {
		return err
	}
	defer client.Close()

	sc, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("sftp client: %w", err)
	}
	defer sc.Close()

	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local %s: %w", localPath, err)
	}
	defer src.Close()

	dst, err := sc.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote %s: %w", remotePath, err)
	}
	defer dst.Close()

	if _, err := dst.ReadFrom(src); err != nil {
		return fmt.Errorf("upload %s: %w", remotePath, err)
	}
	return nil
}
