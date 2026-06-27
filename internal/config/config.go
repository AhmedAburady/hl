package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Remote struct {
	Host        string `mapstructure:"host"`
	User        string `mapstructure:"user"`
	Port        int    `mapstructure:"port"`
	Key         string `mapstructure:"key"`
	AgentSocket string `mapstructure:"agent_socket"`
	RemotePath  string `mapstructure:"remote_path"`
	ReloadCmd   string `mapstructure:"reload_cmd"`
}

type Caddy struct {
	LocalFile  string `mapstructure:"local_file"`
	Remote     Remote `mapstructure:"remote"`
	ManagedTag string `mapstructure:"managed_tag"`
}

type Technitium struct {
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

type Config struct {
	Technitium Technitium `mapstructure:"technitium"`
	Caddy      Caddy      `mapstructure:"caddy"`

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

func setDefaults(v *viper.Viper) {
	v.SetDefault("technitium.url", "http://localhost:5380")
	v.SetDefault("caddy.local_file", DefaultLocalFile())
	v.SetDefault("caddy.managed_tag", "managed-by:hl")
	v.SetDefault("caddy.remote.port", 22)
	v.SetDefault("caddy.remote.remote_path", "/etc/caddy/Caddyfile")
	v.SetDefault("caddy.remote.reload_cmd", "caddy reload --config /etc/caddy/Caddyfile")
}

func newViper(path string) *viper.Viper {
	v := viper.New()
	setDefaults(v)
	v.SetEnvPrefix("HLDNS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	return v
}

// Load reads the config file at path (or the default path when empty). A
// missing file is not an error; defaults and environment variables are used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	v := newViper(path)
	if err := v.ReadInConfig(); err != nil {
		// A missing file is fine (defaults + env apply); anything else — including
		// a permission-denied on an existing file — is a real error.
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	c := &Config{path: path}
	if err := v.Unmarshal(c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}

func (c *Config) Path() string { return c.path }
