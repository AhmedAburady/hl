package cmd

import (
	"fmt"
	"sort"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
)

func newDNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: "Inspect Technitium DNS records",
	}
	cmd.AddCommand(newDNSListCmd())
	return cmd
}

func newDNSListCmd() *cobra.Command {
	var (
		zone string
		all  bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List hl-managed records (use --all for every record in the zone)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}

			var zones []string
			if zone != "" {
				zones = []string{zone}
			} else {
				zones, err = zonesFromCaddyfile(cfg)
				if err != nil {
					return err
				}
				if len(zones) == 0 {
					out(c, "%s", ui.Info("No --zone given and no DNS zones declared in %s.", cfg.Caddy.LocalFile))
					out(c, "%s", ui.Info("Pass --zone <zone> to list a specific zone."))
					return nil
				}
			}

			cl, err := technitiumClient(c, cfg)
			if err != nil {
				return err
			}
			tag := cfg.Caddy.ManagedTag

			// One unreadable/absent zone (e.g. an annotation for a zone not yet
			// created) must not hide the others — warn and keep going.
			var records []technitium.Record
			var failed int
			for _, z := range zones {
				recs, err := cl.ListRecords(c.Context(), z, "")
				if err != nil {
					failed++
					out(c, "%s", ui.Warn("zone %s: %v", z, err))
					continue
				}
				for _, r := range recs {
					if all || r.Comments == tag {
						records = append(records, r)
					}
				}
			}

			if len(records) == 0 {
				switch {
				case failed == len(zones):
					return fmt.Errorf("no zones could be listed (%d failed)", failed)
				case all:
					out(c, "%s", ui.Info("No records found."))
				default:
					out(c, "%s", ui.Info("No hl-managed records (tagged %q). Pass --all to list every record.", tag))
				}
				return nil
			}

			out(c, "%s", ui.RenderRecords(records, all, tag))
			if all {
				out(c, "%s", ui.Info("● = managed by hl (%s)", tag))
			}
			if failed > 0 {
				out(c, "%s", ui.Warn("%d of %d zone(s) could not be listed (see warnings above)", failed, len(zones)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&zone, "zone", "", "DNS zone (defaults to zones in the Caddyfile)")
	cmd.Flags().BoolVar(&all, "all", false, "list all records, not just hl-managed ones")
	return cmd
}

// zonesFromCaddyfile returns the distinct, sorted set of zones declared in the
// local Caddyfile's DNS annotations.
func zonesFromCaddyfile(cfg *config.Config) ([]string, error) {
	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return nil, err
	}
	sites, err := caddy.ParseSites(content)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, s := range sites {
		if s.DNS.Present && s.DNS.Zone != "" {
			set[s.DNS.Zone] = true
		}
	}
	zones := make([]string, 0, len(set))
	for z := range set {
		zones = append(zones, z)
	}
	sort.Strings(zones)
	return zones, nil
}
