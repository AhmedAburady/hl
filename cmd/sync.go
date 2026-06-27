package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/spf13/cobra"
)

// syncOpts controls the shared deploy + DNS reconcile flow.
type syncOpts struct {
	dryRun   bool
	noDeploy bool
	noDNS    bool
	noPrune  bool
}

func newSyncCmd() *cobra.Command {
	var o syncOpts
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Deploy the local Caddyfile and reconcile DNS from its annotations",
		Long: `sync makes the world match the local Caddyfile: it pushes the file to
the Caddy host and reloads, then reconciles Technitium DNS so the zone holds
exactly the records declared by the site-block annotations (creating, updating,
and pruning hl-managed records).`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			return runSync(c, cfg, o)
		},
	}
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "show what would change without deploying or modifying DNS")
	cmd.Flags().BoolVar(&o.noDeploy, "no-deploy", false, "skip the Caddy deploy; reconcile DNS only")
	cmd.Flags().BoolVar(&o.noDNS, "no-dns", false, "deploy Caddy only; skip DNS reconcile")
	cmd.Flags().BoolVar(&o.noPrune, "no-prune", false, "do not delete managed DNS records absent from the Caddyfile")
	return cmd
}

// runSync deploys the local Caddyfile (unless disabled) and reconciles DNS from
// its annotations (unless disabled).
func runSync(c *cobra.Command, cfg *config.Config, o syncOpts) error {
	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return err
	}

	if !o.noDeploy {
		if o.dryRun {
			out(c, "[dry-run] would deploy %s to %s and reload", cfg.Caddy.LocalFile, cfg.Caddy.Remote.Host)
		} else {
			out(c, "Deploying to %s ...", cfg.Caddy.Remote.Host)
			deployOut, err := caddy.Deploy(c.Context(), cfg.Caddy)
			if err != nil {
				return fmt.Errorf("deploy: %w", err)
			}
			out(c, "Caddy reloaded.")
			if s := strings.TrimSpace(deployOut); s != "" {
				out(c, "reload output: %s", s)
			}
		}
	}

	if !o.noDNS {
		if err := reconcileDNS(c, cfg, content, o.dryRun, o.noPrune); err != nil {
			return err
		}
	}
	return nil
}

// reconcileDNS derives desired records from the Caddyfile content and brings the
// Technitium zone(s) into line, printing the plan. With dryRun it only prints.
func reconcileDNS(c *cobra.Command, cfg *config.Config, content string, dryRun, noPrune bool) error {
	sites, err := caddy.ParseSites(content)
	if err != nil {
		return err
	}
	desired, err := reconcile.DeriveDesired(sites)
	if err != nil {
		return err
	}
	if len(desired) == 0 {
		// Never prune from an empty or missing Caddyfile: with no site blocks at
		// all this is almost certainly a misconfiguration, and reconciling would
		// delete every managed record. Removing annotations from a file that still
		// has site blocks IS a legitimate "drop these records" signal, so that
		// case (sites present, token set) falls through to reconcile below.
		if strings.TrimSpace(content) == "" || len(sites) == 0 {
			out(c, "DNS: no site blocks in %s; skipping reconcile (not pruning)", cfg.Caddy.LocalFile)
			return nil
		}
		// No annotations and no token: nothing to manage, and we won't force the
		// user to configure a token just to be told there is nothing to do.
		if cfg.Technitium.Token == "" {
			out(c, "DNS: no managed records declared in %s", cfg.Caddy.LocalFile)
			return nil
		}
	}
	cl, err := technitiumClient(c, cfg)
	if err != nil {
		return err
	}
	actual, err := listManagedRecords(c, cl)
	if err != nil {
		return err
	}

	plan := reconcile.BuildPlan(desired, actual, cfg.Caddy.ManagedTag)
	if noPrune {
		plan.Delete = nil
	}

	if plan.Empty() {
		out(c, "DNS: up to date (%d managed records)", len(desired))
		return nil
	}
	out(c, "DNS plan:")
	out(c, "%s", plan.String())
	if dryRun {
		return nil
	}
	if err := plan.Apply(c.Context(), cl, cfg.Caddy.ManagedTag); err != nil {
		return err
	}
	out(c, "DNS reconciled (%d created, %d updated, %d deleted).", len(plan.Create), len(plan.Update), len(plan.Delete))
	return nil
}

// technitiumClient builds an authenticated client, resolving the configured
// token (literal, ${ENV}, or an op:// 1Password reference) at the point of use.
func technitiumClient(c *cobra.Command, cfg *config.Config) (*technitium.Client, error) {
	if cfg.Technitium.URL == "" {
		return nil, errors.New("technitium.url is not configured")
	}
	if cfg.Technitium.Token == "" {
		return nil, errors.New("technitium.token is not set (Technitium UI API token; literal, ${ENV}, or op://...)")
	}
	token, err := config.ResolveSecret(c.Context(), cfg.Technitium.Token)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("technitium.token %q resolved to an empty value", cfg.Technitium.Token)
	}
	return technitium.New(cfg.Technitium.URL, token), nil
}

// listManagedRecords fetches records across every authoritative zone on the
// server. Scanning all zones (rather than only those currently referenced by the
// Caddyfile) ensures a managed record is still pruned after the block that
// created it changes zones or is removed entirely.
func listManagedRecords(c *cobra.Command, cl *technitium.Client) ([]technitium.Record, error) {
	zones, err := cl.ListPrimaryZones(c.Context())
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	sort.Strings(zones)

	var all []technitium.Record
	for _, z := range zones {
		recs, err := cl.ListRecords(c.Context(), z, "")
		if err != nil {
			return nil, fmt.Errorf("list zone %s: %w", z, err)
		}
		all = append(all, recs...)
	}
	return all, nil
}
