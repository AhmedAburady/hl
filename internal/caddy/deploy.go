package caddy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// maxLocalBackups caps how many timestamped copies are kept in backups/.
const maxLocalBackups = 2

// WriteLocalFile backs up the existing file into backups/ then writes content.
func WriteLocalFile(path, content string) error {
	path = expandTilde(path)
	if existing, err := os.ReadFile(path); err == nil {
		if err := backupLocalFile(path, existing); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// backupLocalFile copies data into backups/ under a unique timestamped name, then prunes to maxLocalBackups.
func backupLocalFile(path string, data []byte) error {
	dir := filepath.Join(filepath.Dir(path), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create backup dir %s: %w", dir, err)
	}
	name := filepath.Base(path) + "." + time.Now().Format("20060102-150405")
	bak := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(bak); errors.Is(err, fs.ErrNotExist) {
			break
		}
		bak = filepath.Join(dir, fmt.Sprintf("%s.%d", name, i))
	}
	if err := os.WriteFile(bak, data, 0o600); err != nil {
		return fmt.Errorf("backup %s: %w", bak, err)
	}
	pruneBackups(dir, filepath.Base(path), maxLocalBackups)
	return nil
}

// pruneBackups deletes all but the most recent keep backups of base in dir (lexical sort is oldest-first).
func pruneBackups(dir, base string, keep int) {
	matches, err := filepath.Glob(filepath.Join(dir, base+".*"))
	if err != nil || len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keep] {
		_ = os.Remove(old)
	}
}

// contentSHA256 returns the hex SHA-256 of s, formatted to match `sha256sum`
// output so the local Caddyfile can be compared against the remote file's hash
// without transferring the remote file.
func contentSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// readRemoteCmd renders the command that prints the remote Caddyfile to stdout,
// elevated with sudo for a non-root user so root-owned paths like
// /etc/caddy/Caddyfile remain readable.
func readRemoteCmd(sudo, rp string) string {
	return fmt.Sprintf("%scat %s", sudo, shellQuote(rp))
}

// remoteSHACmd renders the command that prints the remote Caddyfile's SHA-256.
// It swallows errors so a missing file yields empty output (parsed as "no hash")
// rather than a remote-command failure.
func remoteSHACmd(sudo, rp string) string {
	return fmt.Sprintf("%ssha256sum %s 2>/dev/null", sudo, shellQuote(rp))
}

// parseSHA pulls the hash (first field) out of `sha256sum` output. ok is false
// when the output is empty — the file did not exist or could not be read.
func parseSHA(out string) (sum string, ok bool) {
	if f := strings.Fields(out); len(f) > 0 {
		return f[0], true
	}
	return "", false
}

// ReadRemoteFile reads the remote Caddyfile over SSH (cat, prefixed with sudo for
// a non-root user). A missing remote file surfaces as an error.
func ReadRemoteFile(ctx context.Context, remote config.Remote) (string, error) {
	t, sudo, err := remoteTarget(remote)
	if err != nil {
		return "", err
	}
	out, err := sshx.Run(ctx, t, readRemoteCmd(sudo, remote.RemotePath))
	if err != nil {
		return out, fmt.Errorf("read remote %s: %w", remote.RemotePath, err)
	}
	return out, nil
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

// stagedPath returns a per-process staging path beside rp. The pid + timestamp
// suffix keeps concurrent `hl sync` runs from clobbering each other's candidate
// file.
func stagedPath(rp string) string {
	return fmt.Sprintf("%s.hldns.%d-%d.new", rp, os.Getpid(), time.Now().UnixNano())
}

// hasConfigFlag reports whether cmd already passes its own --config/-c, in which
// case appending another would be ambiguous.
func hasConfigFlag(cmd string) bool {
	for f := range strings.FieldsSeq(cmd) {
		if f == "--config" || f == "-c" || strings.HasPrefix(f, "--config=") || strings.HasPrefix(f, "-c=") {
			return true
		}
	}
	return false
}

// validateCmd renders the validator command for the staged file. A `{file}`
// placeholder is substituted; otherwise the path is appended as `--config
// <file>`. A command that already sets its own --config but has no placeholder is
// rejected, since a second --config would validate the wrong file. The command is
// prefixed with sudo when the SSH user is not root.
func validateCmd(cmd, sudo, staged string) (string, error) {
	v := strings.TrimSpace(cmd)
	switch {
	case strings.Contains(v, "{file}"):
		v = strings.ReplaceAll(v, "{file}", shellQuote(staged))
	case hasConfigFlag(v):
		return "", fmt.Errorf("validate_cmd sets its own --config; use the {file} placeholder so hl can point it at the staged Caddyfile")
	default:
		v = v + " --config " + shellQuote(staged)
	}
	if sudo != "" && !strings.HasPrefix(v, "sudo ") {
		v = sudo + v
	}
	return v, nil
}

const validateRCMarker = "__hl_validate_rc:"

// runValidator runs vcmd against the staged file and classifies the outcome.
// rejected is true only when the validator ran and reported the file invalid; a
// non-nil error means the validator could not run at all (missing binary, bad
// flag, sudo/transport failure) — a different problem the user fixes differently.
// The validator's exit status is captured via a trailing marker so a non-zero
// exit does not surface as an SSH transport error.
func runValidator(ctx context.Context, t sshx.Target, vcmd string) (output string, rejected bool, err error) {
	wrapped := "{ " + vcmd + "; } 2>&1; printf '\\n" + validateRCMarker + "%d\\n' \"$?\""
	out, err := sshx.Run(ctx, t, wrapped)
	if err != nil {
		// The marker never ran: this is a transport/shell failure, not a verdict.
		return out, false, err
	}
	body, rc, ok := extractRC(out)
	if !ok {
		return out, false, fmt.Errorf("validator produced no exit status")
	}
	switch rc {
	case 0:
		return body, false, nil
	case 126, 127:
		return body, false, fmt.Errorf("validator command could not run (exit %d) — is it installed on the host and runnable under the SSH user?", rc)
	default:
		return body, true, nil
	}
}

// extractRC pulls the trailing exit-status marker out of combined output,
// returning the output without that line, the parsed code, and whether a marker
// was found.
func extractRC(out string) (body string, rc int, ok bool) {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if rest, found := strings.CutPrefix(line, validateRCMarker); found {
			if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
				body = strings.Join(append(lines[:i], lines[i+1:]...), "\n")
				return strings.TrimRight(body, "\n"), n, true
			}
		}
	}
	return out, 0, false
}

// Validate stages the local Caddyfile to a temp path on the remote host and runs
// the configured validator against it, leaving the live file untouched, then
// removes the temp file. It returns the validator's output (the reason on
// failure) and ErrValidate when the file is rejected. A failure to run the
// validator itself is returned as an ordinary error, not ErrValidate. A blank
// validate_cmd disables the check and returns ("", nil).
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
	staged := stagedPath(rp)
	vcmd, err := validateCmd(remote.ValidateCmd, sudo, staged)
	if err != nil {
		return "", err
	}
	if out, err := sshx.Run(ctx, t, stageCmd(sudo, rp, staged, content)); err != nil {
		return out, fmt.Errorf("stage %s: %w", staged, err)
	}
	out, rejected, rerr := runValidator(ctx, t, vcmd)
	_, _ = sshx.Run(ctx, t, fmt.Sprintf("%srm -f %s", sudo, shellQuote(staged)))
	if rerr != nil {
		return out, rerr
	}
	if rejected {
		return out, ErrValidate
	}
	return out, nil
}

// Deploy writes the local Caddyfile to the remote host, reloads Caddy, and
// restores the previous remote file if the reload fails. The file is written by
// piping base64 through `tee` rather than SFTP, so a non-root SSH user can
// elevate with sudo to reach privileged paths like /etc/caddy (passwordless
// sudo required on the host). Writing through the live path with tee preserves
// its inode, mode, ownership, and any symlink at that path.
//
// When validate_cmd is set, the file is first staged to a per-run temp path and
// validated there; a rejected file aborts with ErrValidate and the live config
// is never touched, while a validator that cannot run returns an ordinary error.
//
// Smart skip: unless force is set, Deploy first compares the live remote file's
// SHA-256 with the local content and, when they match, returns changed=false
// without writing or reloading. force bypasses this so the file is always
// rewritten and reload_cmd re-run — the way to bring a stopped Caddy back up on
// an otherwise-unchanged config.
func Deploy(ctx context.Context, cfg config.Caddy, force bool) (output string, changed bool, err error) {
	remote := cfg.Remote
	t, sudo, err := remoteTarget(remote)
	if err != nil {
		return "", false, err
	}

	content, err := os.ReadFile(expandTilde(cfg.LocalFile))
	if err != nil {
		return "", false, fmt.Errorf("read local %s: %w", cfg.LocalFile, err)
	}

	rp := remote.RemotePath

	// Nothing to do when the live file already hashes to the local content. A
	// transport error or missing file falls through to a normal deploy, which
	// surfaces the real problem.
	if !force {
		if out, err := sshx.Run(ctx, t, remoteSHACmd(sudo, rp)); err == nil {
			if sum, ok := parseSHA(out); ok && sum == contentSHA256(string(content)) {
				return "", false, nil
			}
		}
	}

	backup := rp + ".hldns.bak"

	// Validate a staged copy first. The live Caddyfile is never touched until the
	// staged copy passes; the staged file is then removed.
	if strings.TrimSpace(remote.ValidateCmd) != "" {
		staged := stagedPath(rp)
		vcmd, err := validateCmd(remote.ValidateCmd, sudo, staged)
		if err != nil {
			return "", false, err
		}
		if out, err := sshx.Run(ctx, t, stageCmd(sudo, rp, staged, content)); err != nil {
			return out, false, fmt.Errorf("stage %s: %w", staged, err)
		}
		out, rejected, rerr := runValidator(ctx, t, vcmd)
		_, _ = sshx.Run(ctx, t, fmt.Sprintf("%srm -f %s", sudo, shellQuote(staged)))
		if rerr != nil {
			return out, false, rerr
		}
		if rejected {
			return out, false, ErrValidate
		}
	}

	// Promote: back up the live file, then write the new content through the live
	// path with tee (preserving the path object), all in one round-trip.
	promote := fmt.Sprintf(
		"(%scp -f %s %s 2>/dev/null || true) && %s",
		sudo, shellQuote(rp), shellQuote(backup),
		stageCmd(sudo, rp, rp, content),
	)
	if out, err := sshx.Run(ctx, t, promote); err != nil {
		restoreBackup(ctx, t, sudo, backup, rp)
		return out, false, fmt.Errorf("write %s (restored previous config): %w", rp, err)
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
		restoreBackup(ctx, t, sudo, backup, rp)
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		_, _ = sshx.Run(rctx, t, reload)
		cancel()
		return out, false, fmt.Errorf("reload failed (restored previous config): %w", err)
	}
	return out, true, nil
}

func ServiceActive(ctx context.Context, remote config.Remote, service string) (bool, string, error) {
	t, _, err := remoteTarget(remote)
	if err != nil {
		return false, "", err
	}
	out, err := sshx.Run(ctx, t, fmt.Sprintf("systemctl is-active %s", shellQuote(service)))
	status := strings.TrimSpace(out)
	if err == nil {
		return status == "active", status, nil
	}
	switch status {
	case "inactive", "failed", "activating", "deactivating", "reloading", "unknown":
		return false, status, nil
	}
	if status != "" {
		return false, "", errors.New(status)
	}
	return false, "", err
}

func restoreBackup(ctx context.Context, t sshx.Target, sudo, backup, rp string) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	_, _ = sshx.Run(rctx, t, fmt.Sprintf("%smv -f %s %s 2>/dev/null || true", sudo, shellQuote(backup), shellQuote(rp)))
}

// shellQuote single-quotes s for safe use in a remote shell command, escaping
// any embedded single quotes (the '\” idiom) so a value cannot break out.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
