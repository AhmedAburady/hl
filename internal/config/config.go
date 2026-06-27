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
	Host       string `mapstructure:"host"`
	User       string `mapstructure:"user"`
	Port       int    `mapstructure:"port"`
	Key        string `mapstructure:"key"`
	RemotePath string `mapstructure:"remote_path"`
	ReloadCmd  string `mapstructure:"reload_cmd"`
}

type Caddy struct {
	LocalFile    string `mapstructure:"local_file"`
	Remote       Remote `mapstructure:"remote"`
	TargetScheme string `mapstructure:"target_scheme"`
	CnameTarget  string `mapstructure:"cname_target"`
	AValue       string `mapstructure:"a_value"`
	ManagedTag   string `mapstructure:"managed_tag"`
}

type Technitium struct {
	URL         string `mapstructure:"url"`
	Token       string `mapstructure:"token"`
	DefaultZone string `mapstructure:"default_zone"`
}

type Config struct {
	Technitium Technitium `mapstructure:"technitium"`
	Caddy      Caddy      `mapstructure:"caddy"`

	v    *viper.Viper
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

func setDefaults(v *viper.Viper) {
	v.SetDefault("technitium.url", "http://localhost:5380")
	v.SetDefault("technitium.default_zone", "")
	v.SetDefault("caddy.local_file", "Caddyfile")
	v.SetDefault("caddy.target_scheme", "http")
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
		if _, ok := errors.AsType[*fs.PathError](err); !ok {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	c := &Config{v: v, path: path}
	if err := v.Unmarshal(c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}

func (c *Config) Path() string { return c.path }

// SetToken persists the Technitium session token to the config file.
func (c *Config) SetToken(token string) error {
	c.Technitium.Token = token
	c.v.Set("technitium.token", token)
	return c.write()
}

func (c *Config) write() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	return c.v.WriteConfigAs(c.path)
}

// Init writes a default config file at path, failing if one already exists.
func Init(path string) (string, error) {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	v := newViper(path)
	if err := v.SafeWriteConfigAs(path); err != nil {
		return "", err
	}
	return path, nil
}
