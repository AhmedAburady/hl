package caddy

import (
	"context"
	"encoding/base64"
	"errors"
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

// ErrValidate marks a deploy or check that aborted because the staged Caddyfile
// failed validation; the live remote config is left untouched. The accompanying
// output string carries the validator's human-readable reason.
var ErrValidate = errors.New("caddyfile validation failed")

// remoteTarget builds the SSH target and the sudo prefix (empty for root) from
// the remote config, rejecting a missing host.
func remoteTarget(remote config.Remote) (sshx.Target, string, error) {
	if remote.Host == "" {
		return sshx.Target{}, "", fmt.Errorf("caddy.remote.host is not configured")
	}
	t := sshx.Target{Host: remote.Host, User: remote.User, Port: remote.Port, KeyPath: remote.Key, AgentSocket: remote.AgentSocket}
	if t.User == "" {
		t.User = "root"
	}
	sudo := ""
	if t.User != "root" {
		sudo = "sudo "
	}
	return t, sudo, nil
}

// stageCmd renders a shell command that ensures the remote directory exists and
// writes content (base64 over the wire) to the staging path, without touching
// the live Caddyfile.
func stageCmd(sudo, rp, staged string, content []byte) string {
	encoded := base64.StdEncoding.EncodeToString(content)
	return fmt.Sprintf(
		"%smkdir -p %s && printf %%s %s | base64 -d | %stee %s >/dev/null",
		sudo, shellQuote(filepath.Dir(rp)),
		shellQuote(encoded), sudo, shellQuote(staged),
	)
}

// validateCmd renders the validator command for the staged file. A `{file}`
// placeholder is substituted; otherwise the path is appended as `--config
// <file>`. The command is prefixed with sudo when the SSH user is not root.
func validateCmd(cmd, sudo, staged string) string {
	v := strings.TrimSpace(cmd)
	if strings.Contains(v, "{file}") {
		v = strings.ReplaceAll(v, "{file}", shellQuote(staged))
	} else {
		v = v + " --config " + shellQuote(staged)
	}
	if sudo != "" && !strings.HasPrefix(v, "sudo ") {
		v = sudo + v
	}
	return v
}

// Validate stages the local Caddyfile to a temp path on the remote host and runs
// the configured validator against it, leaving the live file untouched, then
// removes the temp file. It returns the validator's output (the reason on
// failure) and ErrValidate when the file is rejected. A blank validate_cmd
// disables the check and returns ("", nil).
func Validate(ctx context.Context, cfg config.Caddy) (string, error) {
	remote := cfg.Remote
	if strings.TrimSpace(remote.ValidateCmd) == "" {
		return "", nil
	}
	t, sudo, err := remoteTarget(remote)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(expandTilde(cfg.LocalFile))
	if err != nil {
		return "", fmt.Errorf("read local %s: %w", cfg.LocalFile, err)
	}
	rp := remote.RemotePath
	staged := rp + ".hldns.new"
	if out, err := sshx.Run(ctx, t, stageCmd(sudo, rp, staged, content)); err != nil {
		return out, fmt.Errorf("stage %s: %w", staged, err)
	}
	out, verr := sshx.Run(ctx, t, validateCmd(remote.ValidateCmd, sudo, staged))
	_, _ = sshx.Run(ctx, t, fmt.Sprintf("%srm -f %s", sudo, shellQuote(staged)))
	if verr != nil {
		return out, ErrValidate
	}
	return out, nil
}

// Deploy writes the local Caddyfile to the remote host, reloads Caddy, and
// restores the previous remote file if the reload fails. The file is written by
// piping base64 through `tee` rather than SFTP, so a non-root SSH user can
// elevate with sudo to reach privileged paths like /etc/caddy (passwordless
// sudo required on the host).
//
// The new file is first staged beside the live one and validated (unless
// validate_cmd is blank); a validation failure aborts with ErrValidate and the
// live config is never touched.
func Deploy(ctx context.Context, cfg config.Caddy) (string, error) {
	remote := cfg.Remote
	t, sudo, err := remoteTarget(remote)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(expandTilde(cfg.LocalFile))
	if err != nil {
		return "", fmt.Errorf("read local %s: %w", cfg.LocalFile, err)
	}

	rp := remote.RemotePath
	backup := rp + ".hldns.bak"
	staged := rp + ".hldns.new"

	// Stage the new file beside the live one; the live Caddyfile stays put until
	// the staged copy validates.
	if out, err := sshx.Run(ctx, t, stageCmd(sudo, rp, staged, content)); err != nil {
		return out, fmt.Errorf("stage %s: %w", staged, err)
	}

	// Validate the staged file. On failure the live config is never changed.
	if strings.TrimSpace(remote.ValidateCmd) != "" {
		if out, err := sshx.Run(ctx, t, validateCmd(remote.ValidateCmd, sudo, staged)); err != nil {
			_, _ = sshx.Run(ctx, t, fmt.Sprintf("%srm -f %s", sudo, shellQuote(staged)))
			return out, ErrValidate
		}
	}

	// Promote: back up the live file, then move the staged copy into place.
	promote := fmt.Sprintf(
		"(%scp -f %s %s 2>/dev/null || true) && %smv -f %s %s",
		sudo, shellQuote(rp), shellQuote(backup),
		sudo, shellQuote(staged), shellQuote(rp),
	)
	if out, err := sshx.Run(ctx, t, promote); err != nil {
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
