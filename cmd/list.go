package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var (
		noRemote bool
		zone     string
		format   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed DNS records grouped by zone, with drift across local, DNS, and the deployed Caddy",
		Long: `list prints the managed DNS records grouped by zone — record name, value,
and reverse-proxy address — plus where each agrees or drifts: declared locally
(L), present in Technitium (DNS), and on the deployed Caddy host (RE). It covers
only the records hl manages and changes nothing.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			return runList(c, cfg, noRemote, zone, format)
		},
	}
	cmd.Flags().BoolVar(&noRemote, "no-remote", false, "skip the SSH fetch of the remote Caddyfile (RE column shows ?)")
	cmd.Flags().StringVar(&zone, "zone", "", "show only records in this DNS zone (e.g. example.com)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table or json")
	return cmd
}

func runList(c *cobra.Command, cfg *config.Config, noRemote bool, zone, format string) error {
	if format != "table" && format != "json" {
		return fmt.Errorf("invalid --format %q (want table or json)", format)
	}
	jsonOut := format == "json"
	note := func(s string) {
		if jsonOut {
			fmt.Fprintln(c.ErrOrStderr(), s)
		} else {
			out(c, "%s", s)
		}
	}

	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return err
	}
	localSites, err := caddy.ParseSites(content)
	if err != nil {
		return err
	}

	var (
		plan      reconcile.Plan
		dnsSkip   bool
		dnsReason string
		dnsErr    error
	)
	_ = ui.WithSpinner(c.Context(), "reading DNS records…", func(context.Context) error {
		_, plan, _, _, dnsSkip, dnsReason, dnsErr = computeDNSPlan(c, cfg, content, false, false)
		return dnsErr
	})
	if err := c.Context().Err(); err != nil {
		return err
	}
	if dnsErr != nil {
		note(ui.Warn("DNS: could not build plan: %v", dnsErr))
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
		if err := c.Context().Err(); err != nil {
			return err
		}
		switch {
		case rerr != nil:
			note(ui.Warn("RE: could not read remote Caddyfile: %v", rerr))
		default:
			rs, perr := caddy.ParseSites(rc)
			if perr != nil {
				note(ui.Warn("RE: could not parse remote Caddyfile: %v", perr))
			} else {
				remoteSites, remoteOK = rs, true
			}
		}
	}

	rows := buildRecordRows(localSites, remoteSites, plan, remoteOK, dnsKnown)
	if zone != "" {
		want := reconcile.NameKey(zone)
		kept := rows[:0]
		for _, r := range rows {
			if reconcile.NameKey(r.Zone) == want {
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	if jsonOut {
		return writeRecordsJSON(c, rows)
	}

	if len(rows) == 0 {
		if zone != "" {
			out(c, "%s", ui.Info("No managed records in zone %s", zone))
		} else {
			out(c, "%s", ui.Info("No managed records in %s", cfg.Caddy.LocalFile))
		}
	} else {
		out(c, "%s", ui.RenderRecords(rows))
		if leg := ui.RecordLegend(rows); leg != "" {
			out(c, "")
			out(c, "%s", leg)
		}
	}

	if dnsSkip {
		out(c, "")
		out(c, "%s", ui.Info("DNS: %s", dnsReason))
	}

	out(c, "")
	out(c, "%s", ui.Info("%-3s = %s", "L", cfg.Caddy.LocalFile))
	out(c, "%s", ui.Info("%-3s = %s", "DNS", cfg.Technitium.URL))
	out(c, "%s", ui.Info("%-3s = %s", "RE", cfg.Caddy.Remote.Host))
	return nil
}

func writeRecordsJSON(c *cobra.Command, rows []ui.RecordRow) error {
	type record struct {
		Zone    string `json:"zone"`
		Record  string `json:"record"`
		Value   string `json:"value"`
		Address string `json:"address"`
		Local   string `json:"local"`
		DNS     string `json:"dns"`
		Remote  string `json:"remote"`
	}
	items := make([]record, 0, len(rows))
	for _, r := range rows {
		items = append(items, record{
			Zone:    r.Zone,
			Record:  r.Record,
			Value:   r.Value,
			Address: r.Proxy,
			Local:   r.Local.Label(),
			DNS:     r.DNS.Label(),
			Remote:  r.Remote.Label(),
		})
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	out(c, "%s", string(b))
	return nil
}

func isWildcard(host string) bool {
	return strings.Contains(host, "*")
}

func buildRecordRows(local, remote []caddy.Site, plan reconcile.Plan, remoteOK, dnsKnown bool) []ui.RecordRow {
	create := actionNameSet(plan.Create)
	update := actionNameSet(plan.Update)
	conflict := actionNameSet(plan.Conflict)

	remoteHosts := map[string]bool{}
	for _, s := range remote {
		if isWildcard(s.Host) {
			continue
		}
		remoteHosts[reconcile.NameKey(s.Host)] = true
	}

	dnsMark := func(fqdn string) ui.Mark {
		if !dnsKnown {
			return ui.MarkUnknown
		}
		switch k := reconcile.NameKey(fqdn); {
		case conflict[k]:
			return ui.MarkConflict
		case create[k]:
			return ui.MarkMissing
		case update[k]:
			return ui.MarkDrift
		default:
			return ui.MarkOK
		}
	}

	seen := map[string]bool{}
	var rows []ui.RecordRow

	for _, s := range local {
		if isWildcard(s.Host) || !s.DNS.Present {
			continue
		}
		d, err := reconcile.Resolve(s.DNS)
		if err != nil {
			continue
		}
		seen[reconcile.NameKey(d.Domain)] = true

		var rem ui.Mark
		switch {
		case !remoteOK:
			rem = ui.MarkUnknown
		case remoteHosts[reconcile.NameKey(s.Host)]:
			rem = ui.MarkOK
		default:
			rem = ui.MarkMissing
		}

		rows = append(rows, ui.RecordRow{
			Zone:   d.Zone,
			Record: d.Domain,
			Value:  d.Value,
			Proxy:  s.Upstream,
			Local:  ui.MarkOK,
			DNS:    dnsMark(d.Domain),
			Remote: rem,
		})
	}

	for _, a := range plan.Delete {
		if isWildcard(a.Domain) || seen[reconcile.NameKey(a.Domain)] {
			continue
		}
		seen[reconcile.NameKey(a.Domain)] = true
		dns := ui.MarkOK
		if !dnsKnown {
			dns = ui.MarkUnknown
		}
		rows = append(rows, ui.RecordRow{
			Zone:   a.Zone,
			Record: a.Domain,
			Value:  a.Value,
			Local:  ui.MarkNA,
			DNS:    dns,
			Remote: ui.MarkNA,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Zone != rows[j].Zone {
			return rows[i].Zone < rows[j].Zone
		}
		return reconcile.NameKey(rows[i].Record) < reconcile.NameKey(rows[j].Record)
	})
	return rows
}

func actionNameSet(as []reconcile.Action) map[string]bool {
	m := make(map[string]bool, len(as))
	for _, a := range as {
		m[reconcile.NameKey(a.Domain)] = true
	}
	return m
}
