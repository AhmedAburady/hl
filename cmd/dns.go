package cmd

import (
	"fmt"
	"sort"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func newListCmd() *cobra.Command {
	var (
		zone string
		all  bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Technitium DNS records (hl-managed by default; --all for every record)",
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

			local := localDeclaredFQDNs(cfg)
			out(c, "%s", ui.Info("local: %s    dns: %s", cfg.Caddy.LocalFile, cfg.Technitium.URL))

			type zoneResult struct {
				recs []technitium.Record
				err  error
			}
			results := make([]zoneResult, len(zones))
			var g errgroup.Group
			g.SetLimit(8)
			for i, z := range zones {
				g.Go(func() error {
					recs, err := cl.ListRecords(c.Context(), z, "")
					results[i] = zoneResult{recs, err}
					return nil
				})
			}
			_ = g.Wait()

			var records []technitium.Record
			var failed int
			for i, z := range zones {
				if results[i].err != nil {
					failed++
					out(c, "%s", ui.Warn("zone %s: %v", z, results[i].err))
					continue
				}
				for _, r := range results[i].recs {
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

			out(c, "%s", ui.RenderRecords(records, all, tag, local))
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

func localDeclaredFQDNs(cfg *config.Config) map[string]bool {
	set := map[string]bool{}
	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return set
	}
	sites, err := caddy.ParseSites(content)
	if err != nil {
		return set
	}
	for _, s := range sites {
		if !s.DNS.Present {
			continue
		}
		if d, err := reconcile.Resolve(s.DNS); err == nil {
			set[reconcile.NameKey(d.Domain)] = true
		}
	}
	return set
}
