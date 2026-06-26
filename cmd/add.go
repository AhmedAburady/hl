package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/prompt"
	"github.com/AhmedAburady/hl/internal/technitium"
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
		force    bool
		comments string
	)
	cmd := &cobra.Command{
		Use:   "add [host] [target]",
		Short: "Add a reverse proxy and matching DNS record",
		Args:  cobra.MaximumNArgs(2),
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

			// 1. Edit local Caddyfile.
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

			// 2. Deploy over SSH.
			if !noDeploy {
				out(c, "Deploying to %s ...", cfg.Caddy.Remote.Host)
				deployOut, err := caddy.Deploy(c.Context(), cfg.Caddy)
				if err != nil {
					return fmt.Errorf("deploy: %w", err)
				}
				out(c, "Caddy reloaded.")
				if strings.TrimSpace(deployOut) != "" {
					out(c, "reload output: %s", strings.TrimSpace(deployOut))
				}
			}

			// 3. DNS record.
			if !noDNS {
				if err := addDNSRecord(c, cfg, host, zone, ttl, dnsType, "CNAME", dnsValue, comments, force); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scheme, "scheme", "", "upstream scheme (http/https) when target has none")
	cmd.Flags().StringVar(&zone, "zone", "", "DNS zone (default technitium.default_zone)")
	cmd.Flags().IntVar(&ttl, "ttl", 0, "DNS record TTL in seconds (0 = server default)")
	cmd.Flags().StringVar(&dnsType, "dns-type", "", "DNS record type: A or CNAME (default CNAME)")
	cmd.Flags().StringVar(&dnsValue, "dns-value", "", "DNS record value (default caddy.cname_target / caddy.a_value)")
	cmd.Flags().BoolVar(&noDNS, "no-dns", false, "skip adding a DNS record")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "edit local Caddyfile only; do not push/reload")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing reverse_proxy block or DNS record")
	cmd.Flags().StringVar(&comments, "comments", "", "comments for the DNS record")
	return cmd
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

// addDNSRecord resolves defaults and adds the DNS record via Technitium. An
// empty dnsType falls back to defaultType ("A" or "CNAME").
func addDNSRecord(c *cobra.Command, cfg *config.Config, host, zone string, ttl int, dnsType, defaultType, dnsValue, comments string, force bool) error {
	if cfg.Technitium.Token == "" {
		return errors.New("technitium.token is not set; run `hl dns login` first")
	}
	if cfg.Technitium.URL == "" {
		return errors.New("technitium.url is not configured")
	}

	rt := strings.ToUpper(dnsType)
	if rt == "" {
		rt = strings.ToUpper(defaultType)
	}
	if rt != "A" && rt != "CNAME" {
		return fmt.Errorf("invalid record type %q (want A or CNAME)", dnsType)
	}

	if dnsValue == "" {
		switch technitium.RecordType(rt) {
		case technitium.TypeA:
			dnsValue = cfg.Caddy.AValue
		case technitium.TypeCNAME:
			dnsValue = cfg.Caddy.CnameTarget
		}
	}
	if dnsValue == "" {
		return fmt.Errorf("no --dns-value and no default configured for %s record; set caddy.cname_target / caddy.a_value or pass --dns-value", rt)
	}
	z := zone
	if z == "" {
		z = cfg.Technitium.DefaultZone
	}
	if z == "" {
		return errors.New("no zone: set technitium.default_zone in config or pass --zone")
	}

	cl := technitium.New(cfg.Technitium.URL, cfg.Technitium.Token)
	req := technitium.AddRecordRequest{
		Domain:    host,
		Zone:      z,
		Type:      technitium.RecordType(rt),
		Value:     dnsValue,
		TTL:       ttl,
		Overwrite: force,
		Comments:  comments,
	}
	if err := cl.AddRecord(c.Context(), req); err != nil {
		return fmt.Errorf("add DNS record: %w", err)
	}
	out(c, "Added DNS %s record: %s -> %s (zone %s)", rt, host, dnsValue, z)
	return nil
}
