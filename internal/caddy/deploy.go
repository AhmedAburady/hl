package caddy

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/sshx"
)

// expandTilde resolves a leading ~ to the user's home directory so config
// paths like ~/.config/hl/Caddyfile work with os file operations.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// ReadLocalFile reads the local Caddyfile, returning an empty string if it
// does not yet exist.
func ReadLocalFile(path string) (string, error) {
	data, err := os.ReadFile(expandTilde(path))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// WriteLocalFile writes content to the local Caddyfile after making a
// timestamped backup of the existing file.
func WriteLocalFile(path, content string) error {
	path = expandTilde(path)
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		bak := path + "." + time.Now().Format("20060102-150405") + ".bak"
		if err := os.WriteFile(bak, existing, 0o600); err != nil {
			return fmt.Errorf("backup %s: %w", bak, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// Deploy writes the local Caddyfile to the remote host, reloads Caddy, and
// restores the previous remote file if the reload fails. The file is written by
// piping base64 through `tee` rather than SFTP, so a non-root SSH user can
// elevate with sudo to reach privileged paths like /etc/caddy (passwordless
// sudo required on the host).
func Deploy(ctx context.Context, cfg config.Caddy) (string, error) {
	remote := cfg.Remote
	if remote.Host == "" {
		return "", fmt.Errorf("caddy.remote.host is not configured")
	}
	t := sshx.Target{Host: remote.Host, User: remote.User, Port: remote.Port, KeyPath: remote.Key, AgentSocket: remote.AgentSocket}
	if t.User == "" {
		t.User = "root"
	}
	sudo := ""
	if t.User != "root" {
		sudo = "sudo "
	}

	content, err := os.ReadFile(expandTilde(cfg.LocalFile))
	if err != nil {
		return "", fmt.Errorf("read local %s: %w", cfg.LocalFile, err)
	}

	rp := remote.RemotePath
	backup := rp + ".hldns.bak"
	encoded := base64.StdEncoding.EncodeToString(content)

	// One round-trip: ensure the dir, back up the current file, then write the
	// new one decoded from base64 (keeps any content intact over the wire).
	writeCmd := fmt.Sprintf(
		"%smkdir -p %s && (%scp -f %s %s 2>/dev/null || true) && printf %%s %s | base64 -d | %stee %s >/dev/null",
		sudo, shellQuote(filepath.Dir(rp)),
		sudo, shellQuote(rp), shellQuote(backup),
		shellQuote(encoded), sudo, shellQuote(rp),
	)
	if out, err := sshx.Run(ctx, t, writeCmd); err != nil {
		return out, fmt.Errorf("write %s: %w", rp, err)
	}

	reload := remote.ReloadCmd
	if reload == "" {
		reload = "systemctl restart caddy"
	}
	if sudo != "" && !strings.HasPrefix(strings.TrimSpace(reload), "sudo ") {
		reload = sudo + reload
	}
	out, err := sshx.Run(ctx, t, reload)
	if err != nil {
		// Restore the backup and re-run reload so a failed restart still leaves
		// Caddy up on the good config.
		_, _ = sshx.Run(ctx, t, fmt.Sprintf("%smv -f %s %s", sudo, shellQuote(backup), shellQuote(rp)))
		_, _ = sshx.Run(ctx, t, reload)
		return out, fmt.Errorf("reload failed (restored previous config): %w", err)
	}
	return out, nil
}

// shellQuote single-quotes s for safe use in a remote shell command, escaping
// any embedded single quotes (the '\” idiom) so a value cannot break out.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
