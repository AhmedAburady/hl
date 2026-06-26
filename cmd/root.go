// Package cmd wires the cobra command tree for hl.
package cmd

import (
	"fmt"

	"github.com/AhmedAburady/homelab-dns/internal/config"
	"github.com/spf13/cobra"
)

var configPath string

// Root returns the root command for the CLI.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "hl",
		Short: "Manage homelab Caddy reverse proxies and Technitium DNS records",
		Long: `hl adds a reverse_proxy site block to a local Caddyfile, pushes it
to the Caddy host over SSH and reloads Caddy, and adds a matching A or CNAME
record to a Technitium DNS zone.`,
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "",
		"path to config file (default ~/.config/hl/config.yaml)")

	root.AddCommand(newAddCmd(), newCaddyCmd(), newDNSCmd(), newConfigCmd())
	return root
}

var loadedCfg *config.Config

func loadCfg() (*config.Config, error) {
	if loadedCfg != nil {
		return loadedCfg, nil
	}
	c, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	loadedCfg = c
	return c, nil
}

func out(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintf(cmd.OutOrStdout(), format+"\n", args...)
}
