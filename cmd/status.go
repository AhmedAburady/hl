package cmd

import (
	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var noPrune bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local Caddy hosts and the pending DNS reconcile plan",
		Long: `status reads the local Caddyfile and prints the configured hosts plus the
DNS changes a sync would make (create/update/delete), without deploying or
modifying anything.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
			if err != nil {
				return err
			}
			sites, err := caddy.ParseSites(content)
			if err != nil {
				return err
			}

			if len(sites) == 0 {
				out(c, "No site blocks in %s", cfg.Caddy.LocalFile)
			} else {
				out(c, "Hosts in %s:", cfg.Caddy.LocalFile)
				for _, s := range sites {
					dns := "no dns"
					if s.DNS.Present {
						dns = "dns: " + s.DNS.Name
						if s.DNS.Type != "" {
							dns += " " + s.DNS.Type
						}
					}
					out(c, "  %-32s -> %-24s (%s)", s.Host, s.Upstream, dns)
				}
			}
			out(c, "")
			return reconcileDNS(c, cfg, content, true, noPrune)
		},
	}
	cmd.Flags().BoolVar(&noPrune, "no-prune", false, "exclude managed-record deletions from the plan")
	return cmd
}
