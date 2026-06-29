package cmd

import (
	"sort"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var noRemote bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the drift between the local Caddyfile, Technitium DNS, and the Caddy host",
		Long: `status compares the three places a host can live — the local Caddyfile
(LOCAL), the records in Technitium (DNS), and the Caddyfile deployed on the Caddy
host (CADDY) — and prints a per-host matrix of where they agree or drift, followed
by the DNS changes a sync would make. It deploys and modifies nothing.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			return runStatus(c, cfg, noRemote)
		},
	}
	cmd.Flags().BoolVar(&noRemote, "no-remote", false, "skip the SSH fetch of the remote Caddyfile (CADDY column shows ?)")
	return cmd
}

func runStatus(c *cobra.Command, cfg *config.Config, noRemote bool) error {
	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return err
	}
	localSites, err := caddy.ParseSites(content)
	if err != nil {
		return err
	}

	desired, plan, actual, _, dnsSkip, dnsReason, dnsErr := computeDNSPlan(c, cfg, content, false, false)
	if dnsErr != nil {
		out(c, "%s", ui.Warn("DNS: could not build plan: %v", dnsErr))
	}
	dnsKnown := dnsErr == nil && !dnsSkip
	untracked := untrackedFQDNs(actual, cfg.Caddy.ManagedTag)

	var remoteSites []caddy.Site
	remoteOK := false
	if !noRemote {
		rc, rerr := caddy.ReadRemoteFile(c.Context(), cfg.Caddy.Remote)
		switch {
		case rerr != nil:
			out(c, "%s", ui.Warn("CADDY: could not read remote Caddyfile: %v", rerr))
		default:
			rs, perr := caddy.ParseSites(rc)
			if perr != nil {
				out(c, "%s", ui.Warn("CADDY: could not parse remote Caddyfile: %v", perr))
			} else {
				remoteSites, remoteOK = rs, true
			}
		}
	}

	if len(localSites) == 0 && len(remoteSites) == 0 && len(plan.Delete) == 0 && len(untracked) == 0 {
		out(c, "%s", ui.Info("No site blocks in %s", cfg.Caddy.LocalFile))
	} else {
		out(c, "%s", ui.Heading("Hosts"))
		out(c, "%s", ui.Info("L = %s    DNS = %s    CA = %s", cfg.Caddy.LocalFile, cfg.Technitium.URL, cfg.Caddy.Remote.Host))
		rows := buildStatusRows(localSites, remoteSites, plan, untracked, remoteOK, dnsKnown)
		out(c, "%s", ui.RenderStatus(rows))
		if leg := ui.StatusLegend(rows); leg != "" {
			out(c, "%s", leg)
		}
	}

	out(c, "")
	switch {
	case dnsErr != nil:
	case dnsSkip:
		out(c, "%s", ui.Info("DNS: %s", dnsReason))
	default:
		printDNSPlan(c, plan, len(desired), true)
	}
	return nil
}

func buildStatusRows(local, remote []caddy.Site, plan reconcile.Plan, untracked []string, remoteOK, dnsKnown bool) []ui.StatusRow {
	create := actionNameSet(plan.Create)
	update := actionNameSet(plan.Update)
	conflict := actionNameSet(plan.Conflict)

	type agg struct {
		host      string
		inLocal   bool
		inRemote  bool
		hasDNS    bool
		fqdn      string
		orphan    bool
		untracked bool
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
		get(reconcile.NameKey(s.Host), s.Host).inRemote = true
	}
	for _, ac := range plan.Delete {
		get(reconcile.NameKey(ac.Domain), ac.Domain).orphan = true
	}
	for _, fq := range untracked {
		get(reconcile.NameKey(fq), fq).untracked = true
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
		case !a.hasDNS && a.untracked:
			row.DNS = ui.MarkUntracked
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

func untrackedFQDNs(actual []technitium.Record, tag string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range actual {
		if r.Comments == tag {
			continue
		}
		if r.Type != string(technitium.TypeA) && r.Type != string(technitium.TypeCNAME) {
			continue
		}
		key := reconcile.NameKey(r.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r.Name)
	}
	return out
}
