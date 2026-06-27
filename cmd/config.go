package cmd

import (
	"fmt"
	"strings"

	"github.com/AhmedAburady/hl/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the hl configuration",
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigShowCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a default config file (fails if one already exists)",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := config.Init(configPath)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			out(c, "Wrote default config to %s", path)
			out(c, "Edit it with your Technitium URL/zone and Caddy SSH details, then run `hl dns login`.")
			return nil
		},
	}
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration (token redacted)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			token := cfg.Technitium.Token
			if token != "" {
				token = "<set>"
			}
			out(c, "config file: %s", cfg.Path())
			out(c, "")
			out(c, "technitium:")
			out(c, "  url:          %s", cfg.Technitium.URL)
			out(c, "  token:        %s", token)
			out(c, "  default_zone: %s", cfg.Technitium.DefaultZone)
			out(c, "caddy:")
			out(c, "  local_file:   %s", cfg.Caddy.LocalFile)
			out(c, "  target_scheme: %s", cfg.Caddy.TargetScheme)
			out(c, "  cname_target:  %s", cfg.Caddy.CnameTarget)
			out(c, "  a_value:       %s", cfg.Caddy.AValue)
			out(c, "  managed_tag:   %s", cfg.Caddy.ManagedTag)
			out(c, "  remote:")
			out(c, "    host:        %s", cfg.Caddy.Remote.Host)
			out(c, "    user:        %s", cfg.Caddy.Remote.User)
			out(c, "    port:        %d", cfg.Caddy.Remote.Port)
			out(c, "    key:         %s", redactKey(cfg.Caddy.Remote.Key))
			out(c, "    remote_path: %s", cfg.Caddy.Remote.RemotePath)
			out(c, "    reload_cmd:  %s", cfg.Caddy.Remote.ReloadCmd)
			return nil
		},
	}
	return cmd
}

func redactKey(k string) string {
	if k == "" {
		return "(ssh-agent)"
	}
	if strings.HasPrefix(k, "~") {
		return k
	}
	return k
}
