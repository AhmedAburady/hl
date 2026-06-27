package cmd

import (
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/prompt"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var (
		scheme   string
		zone     string
		ttl      int
		dnsType  string
		dnsValue string
		noDNS    bool
		noDeploy bool
		noPrune  bool
		noSync   bool
		force    bool
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "add [host] [target]",
		Short: "Add a reverse_proxy block with a DNS annotation, then sync",
		Long: `add writes (or updates) a reverse_proxy site block in the local Caddyfile
and, unless --no-dns, a DNS directive comment above it. It then runs the same
deploy + DNS reconcile as 'hl sync'. The Caddyfile remains the single source of
truth; this command is just a convenient way to author an annotated block.`,
		Args: cobra.MaximumNArgs(2),
		Example: `
  hl add app.home.lab 192.168.1.50:8080
  hl add app.home.lab 192.168.1.50:8080 --dns-type A --dns-value 192.168.1.10
  hl add app.home.lab 192.168.1.50:8080 --no-dns`,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			var host, target string
			if len(args) > 0 {
				host = args[0]
			}
			if len(args) > 1 {
				target = args[1]
			}
			if host == "" || target == "" {
				a, err := prompt.ForAdd(host, target)
				if err != nil {
					return err
				}
				host, target = a.Host, a.Target
			}

			upstream := buildUpstream(target, scheme, cfg.Caddy.TargetScheme)

			content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
			if err != nil {
				return err
			}
			updated, res, err := caddy.UpsertReverseProxy(content, host, upstream, force)
			if err != nil {
				return err
			}

			if !noDNS {
				ann, err := buildAnnotation(host, zone, ttl, dnsType, dnsValue, *cfg)
				if err != nil {
					return err
				}
				updated, err = caddy.UpsertDNSAnnotation(updated, host, ann)
				if err != nil {
					return err
				}
			}

			if dryRun {
				if res.Changed {
					out(c, "[dry-run] would set %s -> %s", host, upstream)
				} else {
					out(c, "[dry-run] %s already up to date", host)
				}
				if noDNS {
					return nil
				}
				return reconcileDNS(c, cfg, updated, true, noPrune)
			}

			if err := caddy.WriteLocalFile(cfg.Caddy.LocalFile, updated); err != nil {
				return err
			}
			out(c, "Updated local Caddyfile: %s -> %s", host, upstream)

			if noSync {
				return nil
			}
			return runSync(c, cfg, syncOpts{noDeploy: noDeploy, noDNS: noDNS, noPrune: noPrune})
		},
	}
	cmd.Flags().StringVar(&scheme, "scheme", "", "upstream scheme (http/https) when target has none")
	cmd.Flags().StringVar(&zone, "zone", "", "DNS zone (default technitium.default_zone)")
	cmd.Flags().IntVar(&ttl, "ttl", 0, "DNS record TTL in seconds (0 = server default)")
	cmd.Flags().StringVar(&dnsType, "dns-type", "", "DNS record type: A or CNAME (default inferred)")
	cmd.Flags().StringVar(&dnsValue, "dns-value", "", "DNS record value (default caddy.cname_target / caddy.a_value)")
	cmd.Flags().BoolVar(&noDNS, "no-dns", false, "do not write a DNS annotation or reconcile DNS")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "skip the Caddy deploy step")
	cmd.Flags().BoolVar(&noPrune, "no-prune", false, "do not delete managed DNS records absent from the Caddyfile")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "edit the local Caddyfile only; do not deploy or touch DNS")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing block-form reverse_proxy")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing, deploying, or modifying DNS")
	return cmd
}

// buildAnnotation constructs the DNS directive to persist above the block. It
// resolves type and zone via config defaults (so the annotation is explicit and
// self-describing) but only records value/ttl when the user set them, leaving
// the rest to inherit config defaults at sync time. Resolving metadata does not
// require a value, so a value-less annotation (value defaulted at sync) is fine.
func buildAnnotation(host, zone string, ttl int, dnsType, dnsValue string, cfg config.Config) (caddy.DNSAnnotation, error) {
	a := caddy.DNSAnnotation{
		Type:    strings.ToUpper(dnsType),
		Zone:    zone,
		Value:   dnsValue,
		TTL:     ttl,
		Present: true,
	}
	typ, zoneR, err := reconcile.ResolveMeta(a, cfg)
	if err != nil {
		return caddy.DNSAnnotation{}, err
	}
	a.Name = shortName(host, zoneR)
	a.Type = string(typ)
	a.Zone = zoneR
	return a, nil
}

// shortName derives the DNS record's short name from the Caddy host by stripping
// the resolved zone suffix (so a.b.example.com in zone example.com yields "a.b").
// It falls back to the first label when the host is not within the zone, and "@"
// when the host equals the zone apex.
func shortName(host, zone string) string {
	h := strings.TrimSuffix(strings.TrimSpace(host), ".")
	z := strings.TrimSuffix(strings.TrimSpace(zone), ".")
	if z != "" {
		if strings.EqualFold(h, z) {
			return "@"
		}
		if strings.HasSuffix(strings.ToLower(h), "."+strings.ToLower(z)) {
			return h[:len(h)-len(z)-1]
		}
	}
	if before, _, ok := strings.Cut(h, "."); ok {
		return before
	}
	return h
}

func buildUpstream(target, schemeFlag, cfgScheme string) string {
	if strings.Contains(target, "://") {
		return target
	}
	s := schemeFlag
	if s == "" {
		s = cfgScheme
	}
	if s == "" {
		s = "http"
	}
	return s + "://" + target
}
