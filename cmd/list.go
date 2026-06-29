package cmd

import (
	"context"
	"sort"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var noRemote bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Compare the local Caddyfile, Technitium DNS, and the deployed Caddyfile",
		Long: `list compares the three places a managed host can live — the local
Caddyfile (L), the records in Technitium (DNS), and the Caddyfile deployed on the
Caddy host (CA) — and prints a per-host matrix of where they agree or drift. It
covers only the records hl manages and changes nothing.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			return runList(c, cfg, noRemote)
		},
	}
	cmd.Flags().BoolVar(&noRemote, "no-remote", false, "skip the SSH fetch of the remote Caddyfile (CA column shows ?)")
	return cmd
}

func runList(c *cobra.Command, cfg *config.Config, noRemote bool) error {
	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return err
	}
	localSites, err := caddy.ParseSites(content)
	if err != nil {
		return err
	}

	var (
		desired   []reconcile.Desired
		plan      reconcile.Plan
		dnsSkip   bool
		dnsReason string
		dnsErr    error
	)
	_ = ui.WithSpinner(c.Context(), "reading DNS records…", func(context.Context) error {
		desired, plan, _, _, dnsSkip, dnsReason, dnsErr = computeDNSPlan(c, cfg, content, false, false)
		return dnsErr
	})
	if dnsErr != nil {
		out(c, "%s", ui.Warn("DNS: could not build plan: %v", dnsErr))
	}
	dnsKnown := dnsErr == nil && !dnsSkip

	var remoteSites []caddy.Site
	remoteOK := false
	if !noRemote {
		var rc string
		var rerr error
		_ = ui.WithSpinner(c.Context(), "reading remote Caddyfile…", func(ctx context.Context) error {
			rc, rerr = caddy.ReadRemoteFile(ctx, cfg.Caddy.Remote)
			return rerr
		})
		switch {
		case rerr != nil:
			out(c, "%s", ui.Warn("CA: could not read remote Caddyfile: %v", rerr))
		default:
			rs, perr := caddy.ParseSites(rc)
			if perr != nil {
				out(c, "%s", ui.Warn("CA: could not parse remote Caddyfile: %v", perr))
			} else {
				remoteSites, remoteOK = rs, true
			}
		}
	}

	rows := buildStatusRows(localSites, remoteSites, plan, remoteOK, dnsKnown)
	if len(rows) == 0 {
		out(c, "%s", ui.Info("No hosts in %s", cfg.Caddy.LocalFile))
	} else {
		out(c, "%s", ui.Heading("Hosts"))
		out(c, "%s", ui.RenderStatus(rows))
		if leg := ui.StatusLegend(rows); leg != "" {
			out(c, "%s", leg)
		}
	}

	switch {
	case dnsErr != nil:
	case dnsSkip:
		out(c, "")
		out(c, "%s", ui.Info("DNS: %s", dnsReason))
	default:
		out(c, "")
		printDNSPlan(c, plan, len(desired), true)
	}

	out(c, "")
	out(c, "%s", ui.Info("%-3s = %s", "L", cfg.Caddy.LocalFile))
	out(c, "%s", ui.Info("%-3s = %s", "DNS", cfg.Technitium.URL))
	out(c, "%s", ui.Info("%-3s = %s", "CA", cfg.Caddy.Remote.Host))
	return nil
}

func isWildcard(host string) bool {
	return strings.Contains(host, "*")
}

func buildStatusRows(local, remote []caddy.Site, plan reconcile.Plan, remoteOK, dnsKnown bool) []ui.StatusRow {
	create := actionNameSet(plan.Create)
	update := actionNameSet(plan.Update)
	conflict := actionNameSet(plan.Conflict)

	type agg struct {
		host     string
		inLocal  bool
		inRemote bool
		hasDNS   bool
		fqdn     string
		orphan   bool
	}
	order := []string{}
	rows := map[string]*agg{}
	get := func(key, display string) *agg {
		a, ok := rows[key]
		if !ok {
			a = &agg{host: display}
			rows[key] = a
			order = append(order, key)
		}
		return a
	}

	for _, s := range local {
		if isWildcard(s.Host) {
			continue
		}
		a := get(reconcile.NameKey(s.Host), s.Host)
		a.inLocal = true
		if s.DNS.Present {
			a.hasDNS = true
			if d, err := reconcile.Resolve(s.DNS); err == nil {
				a.fqdn = d.Domain
			} else {
				a.fqdn = s.Host
			}
		}
	}
	for _, s := range remote {
		if isWildcard(s.Host) {
			continue
		}
		get(reconcile.NameKey(s.Host), s.Host).inRemote = true
	}
	for _, ac := range plan.Delete {
		if isWildcard(ac.Domain) {
			continue
		}
		get(reconcile.NameKey(ac.Domain), ac.Domain).orphan = true
	}

	sort.Strings(order)
	out := make([]ui.StatusRow, 0, len(order))
	for _, key := range order {
		a := rows[key]
		row := ui.StatusRow{Host: a.host}

		if a.inLocal {
			row.Local = ui.MarkOK
		} else {
			row.Local = ui.MarkNA
		}

		switch {
		case !remoteOK:
			row.Caddy = ui.MarkUnknown
		case a.inRemote:
			row.Caddy = ui.MarkOK
		case a.inLocal:
			row.Caddy = ui.MarkMissing
		default:
			row.Caddy = ui.MarkNA
		}

		switch {
		case a.orphan:
			if dnsKnown {
				row.DNS = ui.MarkMissing
			} else {
				row.DNS = ui.MarkUnknown
			}
		case !a.hasDNS:
			row.DNS = ui.MarkNA
		case !dnsKnown:
			row.DNS = ui.MarkUnknown
		default:
			fk := reconcile.NameKey(a.fqdn)
			switch {
			case conflict[fk]:
				row.DNS = ui.MarkConflict
			case create[fk]:
				row.DNS = ui.MarkMissing
			case update[fk]:
				row.DNS = ui.MarkDrift
			default:
				row.DNS = ui.MarkOK
			}
		}
		out = append(out, row)
	}
	return out
}

func actionNameSet(as []reconcile.Action) map[string]bool {
	m := make(map[string]bool, len(as))
	for _, a := range as {
		m[reconcile.NameKey(a.Domain)] = true
	}
	return m
}
