package config

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "absent.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Technitium.URL != "http://localhost:5380" {
		t.Errorf("url default wrong: %q", cfg.Technitium.URL)
	}
	if cfg.Caddy.ManagedTag != "managed-by:hl" {
		t.Errorf("managed_tag default wrong: %q", cfg.Caddy.ManagedTag)
	}
	if cfg.Caddy.Remote.Port != 22 || cfg.Caddy.Remote.RemotePath != "/etc/caddy/Caddyfile" {
		t.Errorf("remote defaults wrong: %+v", cfg.Caddy.Remote)
	}
	if cfg.Caddy.Remote.ReloadCmd != "systemctl restart caddy" || cfg.Caddy.Remote.ValidateCmd != "caddy adapt --adapter caddyfile" {
		t.Errorf("remote cmd defaults wrong: %+v", cfg.Caddy.Remote)
	}
}

func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := Write(path, "technitium:\n  url: http://dns:5380\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Technitium.URL != "http://dns:5380" {
		t.Errorf("url not loaded: %q", cfg.Technitium.URL)
	}
	if cfg.Caddy.ManagedTag != "managed-by:hl" || cfg.Caddy.Remote.Port != 22 {
		t.Errorf("defaults clobbered by partial file: %+v", cfg.Caddy)
	}
}

func TestLoadEnvOverridesAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := Write(path, Render(DefaultInitValues())); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("HLDNS_TECHNITIUM_TOKEN", "env-token")
	t.Setenv("HLDNS_TECHNITIUM_URL", "http://env:5380")
	t.Setenv("HLDNS_CADDY_REMOTE_HOST", "env.host")
	t.Setenv("HLDNS_CADDY_REMOTE_PORT", "2222")
	t.Setenv("HLDNS_CADDY_REMOTE_VALIDATE_CMD", "true")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Technitium.Token != "env-token" {
		t.Errorf("token override failed: %q", cfg.Technitium.Token)
	}
	if cfg.Technitium.URL != "http://env:5380" {
		t.Errorf("url override failed: %q", cfg.Technitium.URL)
	}
	if cfg.Caddy.Remote.Host != "env.host" {
		t.Errorf("host override failed: %q", cfg.Caddy.Remote.Host)
	}
	if cfg.Caddy.Remote.Port != 2222 {
		t.Errorf("port override failed: %d", cfg.Caddy.Remote.Port)
	}
	if cfg.Caddy.Remote.ValidateCmd != "true" {
		t.Errorf("validate_cmd override failed: %q", cfg.Caddy.Remote.ValidateCmd)
	}
}
