package sshx

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Target struct {
	Host        string
	User        string
	Port        int
	KeyPath     string
	AgentSocket string
}

func (t Target) addr() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

func (t Target) hostArg() string {
	if t.User != "" {
		return t.User + "@" + t.Host
	}
	return t.Host
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

func sshArgs(t Target) []string {
	args := []string{
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "LogLevel=ERROR",
	}
	if t.KeyPath != "" {
		args = append(args, "-i", expand(t.KeyPath))
		if t.AgentSocket == "" {
			args = append(args, "-o", "IdentitiesOnly=yes")
		}
	}
	if t.AgentSocket != "" {
		args = append(args, "-o", "IdentityAgent="+expand(t.AgentSocket))
	}
	if t.Port != 0 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	return append(args, t.hostArg())
}

func Run(ctx context.Context, t Target, cmd string) (string, error) {
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, "ssh", append(sshArgs(t), cmd)...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		detail := strings.TrimRight(stdout.String()+stderr.String(), "\n")
		return detail, fmt.Errorf("ssh %s: %w", t.addr(), err)
	}
	return stdout.String(), nil
}
