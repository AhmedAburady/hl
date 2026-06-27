package config

import (
	"path/filepath"
	"testing"
)

func TestRenderIsLoadable(t *testing.T) {
	v := DefaultInitValues()
	v.URL = "http://dns:5380"
	v.Token = "op://Vault/tech/token"
	v.RemoteHost = "caddy.lan"
	v.SSHKey = "~/.ssh/id_ed25519"

	dir := t.TempDir()
	path := filepath.Join(dir, "hl", "config.yaml")
	if Exists(path) {
		t.Fatal("path should not exist yet")
	}
	if err := Write(path, Render(v)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !Exists(path) {
		t.Fatal("path should exist after write")
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Technitium.URL != "http://dns:5380" || cfg.Technitium.Token != "op://Vault/tech/token" {
		t.Fatalf("technitium round-trip wrong: %+v", cfg.Technitium)
	}
	if cfg.Caddy.Remote.Host != "caddy.lan" || cfg.Caddy.Remote.Key != "~/.ssh/id_ed25519" {
		t.Fatalf("remote round-trip wrong: %+v", cfg.Caddy.Remote)
	}
	if cfg.Caddy.ManagedTag != "managed-by:hl" || cfg.Caddy.Remote.Port != 22 {
		t.Fatalf("defaults round-trip wrong: %+v", cfg.Caddy)
	}
}

// TestRenderYAMLSafe ensures values with YAML-significant characters survive a
// Render -> Load round-trip intact rather than corrupting the file.
func TestRenderYAMLSafe(t *testing.T) {
	v := DefaultInitValues()
	v.RemoteHost = "[fd00::1]"                  // IPv6 host: a YAML flow sequence unquoted
	v.URL = "http://dns:5380#frag"              // '#' starts a comment unquoted
	v.RemotePath = "/etc/caddy/Caddyfile: main" // ': ' is a mapping unquoted
	v.ReloadCmd = "caddy reload && echo *done*" // leading '*' on a token is an alias
	v.ManagedTag = "managed-by:hl"

	dir := t.TempDir()
	path := filepath.Join(dir, "hl", "config.yaml")
	if err := Write(path, Render(v)); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Caddy.Remote.Host != "[fd00::1]" {
		t.Fatalf("host corrupted: %q", cfg.Caddy.Remote.Host)
	}
	if cfg.Technitium.URL != "http://dns:5380#frag" {
		t.Fatalf("url corrupted: %q", cfg.Technitium.URL)
	}
	if cfg.Caddy.Remote.RemotePath != "/etc/caddy/Caddyfile: main" {
		t.Fatalf("remote_path corrupted: %q", cfg.Caddy.Remote.RemotePath)
	}
	if cfg.Caddy.Remote.ReloadCmd != "caddy reload && echo *done*" {
		t.Fatalf("reload_cmd corrupted: %q", cfg.Caddy.Remote.ReloadCmd)
	}
}
