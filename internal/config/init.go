package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// InitValues holds the answers gathered by `hl config init` and rendered into a
// complete config file. All fields are present so the written file never omits a
// setting the user needs to fill.
type InitValues struct {
	URL         string
	Token       string
	LocalFile   string
	RemoteHost  string
	RemoteUser  string
	RemotePort  int
	SSHKey      string // set for key-file auth; empty otherwise
	AgentSocket string // set for ssh-agent auth; empty falls back to $SSH_AUTH_SOCK
	RemotePath  string
	ReloadCmd   string
	ManagedTag  string
}

// SuggestedAgentSocket returns a stable ssh-agent socket to show as a hint
// during onboarding: the platform's 1Password agent socket. It is only a
// placeholder — leaving agent_socket empty makes hl fall back to $SSH_AUTH_SOCK
// at run time, which is the right default for the macOS built-in agent (whose
// socket lives under /var/run/com.apple.launchd.*/Listeners and rotates every
// login, so it must never be persisted).
func SuggestedAgentSocket() string {
	if runtime.GOOS == "darwin" {
		return "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
	}
	return "~/.1password/agent.sock"
}

// DefaultInitValues returns the starting point for the init wizard / template.
func DefaultInitValues() InitValues {
	return InitValues{
		URL:        "http://localhost:5380",
		Token:      "",
		LocalFile:  DefaultLocalFile(),
		RemoteHost: "",
		RemoteUser: "root",
		RemotePort: 22,
		SSHKey:     "",
		RemotePath: "/etc/caddy/Caddyfile",
		ReloadCmd:  "systemctl restart caddy",
		ManagedTag: "managed-by:hl",
	}
}

// Render produces a complete, grouped config file from v. Every string value is
// emitted quoted so user input containing YAML-significant characters (a colon,
// a leading #, *, [, {, ~, or an IPv6 host like [fd00::1]) round-trips intact.
func Render(v InitValues) string {
	return fmt.Sprintf(`technitium:
  url: %q
  token: %q

caddy:
  local_file: %q
  managed_tag: %q
  remote:
    host: %q
    user: %q
    port: %d
    key: %q
    agent_socket: %q
    remote_path: %q
    reload_cmd: %q
`,
		v.URL, v.Token, v.LocalFile, v.ManagedTag,
		v.RemoteHost, v.RemoteUser, v.RemotePort, v.SSHKey, v.AgentSocket, v.RemotePath, v.ReloadCmd)
}

// Write writes content to path (creating parent dirs), overwriting any existing
// file. Use Exists to decide whether to confirm an overwrite first.
func Write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// Exists reports whether a config file is already present at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
