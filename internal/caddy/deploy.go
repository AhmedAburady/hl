package caddy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AhmedAburady/homelab-dns/internal/config"
	"github.com/AhmedAburady/homelab-dns/internal/sshx"
)

// ReadLocalFile reads the local Caddyfile, returning an empty string if it
// does not yet exist.
func ReadLocalFile(path string) (string, error) {
	data, err := os.ReadFile(path)
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

// Deploy pushes the local Caddyfile to the remote host, reloads Caddy, and
// restores the previous remote file if the reload fails.
func Deploy(ctx context.Context, cfg config.Caddy) (string, error) {
	remote := cfg.Remote
	if remote.Host == "" {
		return "", fmt.Errorf("caddy.remote.host is not configured")
	}
	t := sshx.Target{Host: remote.Host, User: remote.User, Port: remote.Port, KeyPath: remote.Key}
	if t.User == "" {
		t.User = "root"
	}

	// 1. Back up the current remote file.
	backup := remote.RemotePath + ".hldns.bak"
	backupCmd := fmt.Sprintf("cp -f %s %s 2>/dev/null || true", shellQuote(remote.RemotePath), shellQuote(backup))
	if _, err := sshx.Run(ctx, t, backupCmd); err != nil {
		return "", fmt.Errorf("remote backup: %w", err)
	}

	// 2. Push the new file.
	if err := sshx.PushFile(ctx, t, cfg.LocalFile, remote.RemotePath); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}

	// 3. Reload.
	reload := remote.ReloadCmd
	if reload == "" {
		reload = "caddy reload --config " + shellQuote(remote.RemotePath)
	}
	out, err := sshx.Run(ctx, t, reload)
	if err != nil {
		// 4. Restore on failure.
		_, _ = sshx.Run(ctx, t, fmt.Sprintf("mv -f %s %s", shellQuote(backup), shellQuote(remote.RemotePath)))
		return out, fmt.Errorf("reload failed (restored previous config): %w", err)
	}
	return out, nil
}

func shellQuote(s string) string {
	return "'" + string([]byte(s)) + "'"
}
