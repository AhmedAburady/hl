package cmd

import (
	"fmt"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/spf13/cobra"
)

func newCaddyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "caddy",
		Short: "Manage the local Caddyfile and deploy it over SSH",
	}
	cmd.AddCommand(newCaddyAddCmd(), newCaddySyncCmd(), newCaddyListCmd())
	return cmd
}

func newCaddyAddCmd() *cobra.Command {
	var (
		scheme   string
		force    bool
		noDeploy bool
	)
	cmd := &cobra.Command{
		Use:   "add [host] [target]",
		Short: "Add or update a reverse_proxy block and deploy",
		Args:  cobra.ExactArgs(2),
		Example: `
  hl caddy add app.home.lab 192.168.1.50:8080
  hl caddy add app.home.lab 192.168.1.50:8080 --no-deploy`,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			host, target := args[0], args[1]
			upstream := buildUpstream(target, scheme, cfg.Caddy.TargetScheme)

			content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
			if err != nil {
				return err
			}
			updated, res, err := caddy.UpsertReverseProxy(content, host, upstream, force)
			if err != nil {
				return err
			}
			if res.Changed {
				if err := caddy.WriteLocalFile(cfg.Caddy.LocalFile, updated); err != nil {
					return err
				}
				out(c, "Updated local Caddyfile: %s -> %s", host, upstream)
			} else {
				out(c, "Local Caddyfile already up to date for %s", host)
			}
			if noDeploy {
				return nil
			}
			out(c, "Deploying to %s ...", cfg.Caddy.Remote.Host)
			deployOut, err := caddy.Deploy(c.Context(), cfg.Caddy)
			if err != nil {
				return fmt.Errorf("deploy: %w", err)
			}
			out(c, "Caddy reloaded.")
			if s := strings.TrimSpace(deployOut); s != "" {
				out(c, "reload output: %s", s)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scheme, "scheme", "", "upstream scheme (http/https) when target has none")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing reverse_proxy block")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "edit local Caddyfile only; do not push/reload")
	return cmd
}

func newCaddySyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Push the current local Caddyfile to the remote host and reload",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			out(c, "Deploying to %s ...", cfg.Caddy.Remote.Host)
			deployOut, err := caddy.Deploy(c.Context(), cfg.Caddy)
			if err != nil {
				return fmt.Errorf("deploy: %w", err)
			}
			out(c, "Caddy reloaded.")
			if s := strings.TrimSpace(deployOut); s != "" {
				out(c, "reload output: %s", s)
			}
			return nil
		},
	}
	return cmd
}

func newCaddyListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured hosts from the local Caddyfile",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
			if err != nil {
				return err
			}
			hosts := caddy.ListHosts(content)
			if len(hosts) == 0 {
				out(c, "No site blocks found in %s", cfg.Caddy.LocalFile)
				return nil
			}
			out(c, "Configured hosts in %s:", cfg.Caddy.LocalFile)
			for _, h := range hosts {
				out(c, "  %s", h)
			}
			return nil
		},
	}
	return cmd
}
