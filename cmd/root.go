// Package cmd wires the cobra command tree for hl.
package cmd

import (
	"fmt"

	"github.com/AhmedAburady/hl/internal/config"
	"github.com/spf13/cobra"
)

var configPath string

// Root returns the root command for the CLI.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "hl",
		Short: "Manage homelab Caddy reverse proxies and Technitium DNS records",
		Long: `hl treats the local Caddyfile as the single source of truth. Each site
block declares its DNS intent in a comment directly above it; 'hl sync' deploys
the Caddyfile and reconciles Technitium DNS to match.`,
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "",
		"path to config file (default ~/.config/hl/config.yaml)")

	root.AddCommand(newAddCmd(), newSyncCmd(), newStatusCmd(), newDNSCmd(), newConfigCmd())
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
