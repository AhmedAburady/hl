package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Remote struct {
	Host        string `yaml:"host"`
	User        string `yaml:"user"`
	Port        int    `yaml:"port"`
	Key         string `yaml:"key"`
	AgentSocket string `yaml:"agent_socket"`
	RemotePath  string `yaml:"remote_path"`
	ReloadCmd   string `yaml:"reload_cmd"`
	ValidateCmd string `yaml:"validate_cmd"`
}

type Caddy struct {
	LocalFile  string `yaml:"local_file"`
	Remote     Remote `yaml:"remote"`
	ManagedTag string `yaml:"managed_tag"`
}

type Technitium struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type Config struct {
	Technitium Technitium `yaml:"technitium"`
	Caddy      Caddy      `yaml:"caddy"`

	path string
}

// DefaultPath returns the config file path: $XDG_CONFIG_HOME/hl/config.yaml if
// XDG_CONFIG_HOME is set, otherwise ~/.config/hl/config.yaml on both macOS and
// Linux.
func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join("hl", "config.yaml")
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hl", "config.yaml")
}

// DefaultLocalFile returns the default Caddyfile path: a "Caddyfile" sitting
// next to the config file (e.g. ~/.config/hl/Caddyfile), expressed with a
// leading ~ when it lives under the home directory so it reads cleanly and
// stays portable across machines.
func DefaultLocalFile() string {
	dir := filepath.Dir(DefaultPath())
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel, "Caddyfile")
		}
	}
	return filepath.Join(dir, "Caddyfile")
}

func defaultConfig() Config {
	return Config{
		Technitium: Technitium{
			URL: "http://localhost:5380",
		},
		Caddy: Caddy{
			LocalFile:  DefaultLocalFile(),
			ManagedTag: "managed-by:hl",
			Remote: Remote{
				Port:        22,
				RemotePath:  "/etc/caddy/Caddyfile",
				ReloadCmd:   "systemctl restart caddy",
				ValidateCmd: "caddy adapt --adapter caddyfile",
			},
		},
	}
}

// Load reads the config file at path (or the default path when empty), starting
// from built-in defaults and layering the file then HLDNS_* environment
// variables on top. A missing file is not an error; defaults and environment
// variables still apply.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	c := defaultConfig()
	c.path = path

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	applyEnv(&c)
	return &c, nil
}

func applyEnv(c *Config) {
	setStr := func(env string, dst *string) {
		if v, ok := os.LookupEnv(env); ok {
			*dst = v
		}
	}
	setStr("HLDNS_TECHNITIUM_URL", &c.Technitium.URL)
	setStr("HLDNS_TECHNITIUM_TOKEN", &c.Technitium.Token)
	setStr("HLDNS_CADDY_LOCAL_FILE", &c.Caddy.LocalFile)
	setStr("HLDNS_CADDY_MANAGED_TAG", &c.Caddy.ManagedTag)
	setStr("HLDNS_CADDY_REMOTE_HOST", &c.Caddy.Remote.Host)
	setStr("HLDNS_CADDY_REMOTE_USER", &c.Caddy.Remote.User)
	setStr("HLDNS_CADDY_REMOTE_KEY", &c.Caddy.Remote.Key)
	setStr("HLDNS_CADDY_REMOTE_AGENT_SOCKET", &c.Caddy.Remote.AgentSocket)
	setStr("HLDNS_CADDY_REMOTE_REMOTE_PATH", &c.Caddy.Remote.RemotePath)
	setStr("HLDNS_CADDY_REMOTE_RELOAD_CMD", &c.Caddy.Remote.ReloadCmd)
	setStr("HLDNS_CADDY_REMOTE_VALIDATE_CMD", &c.Caddy.Remote.ValidateCmd)
	if v, ok := os.LookupEnv("HLDNS_CADDY_REMOTE_PORT"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.Caddy.Remote.Port = n
		}
	}
}

func (c *Config) Path() string { return c.path }
