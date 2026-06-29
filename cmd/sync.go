package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
)

// syncOpts controls the shared deploy + DNS reconcile flow.
type syncOpts struct {
	dryRun     bool
	noDeploy   bool
	noDNS      bool
	noPrune    bool
	noValidate bool
	adopt      bool
	force      bool
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
	cmd.Flags().BoolVar(&o.noValidate, "no-validate", false, "skip validating the Caddyfile on the remote host before deploying")
	cmd.Flags().BoolVar(&o.adopt, "adopt", false, "overwrite existing records not managed by hl (take ownership)")
	cmd.Flags().BoolVar(&o.force, "force", false, "deploy and reload even when the remote Caddyfile already matches (also revives a stopped Caddy)")
	return cmd
}

// runSync deploys the local Caddyfile (unless disabled) and reconciles DNS from
// its annotations (unless disabled).
func runSync(c *cobra.Command, cfg *config.Config, o syncOpts) error {
	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return err
	}

	// A per-run copy of the Caddy config so --no-validate can disable the remote
	// check without mutating the cached config.
	caddyCfg := cfg.Caddy
	if o.noValidate {
		caddyCfg.Remote.ValidateCmd = ""
	}

	if !o.noDeploy {
		if o.dryRun {
			out(c, "%s", ui.Info("[dry-run] would deploy %s to %s and reload", cfg.Caddy.LocalFile, cfg.Caddy.Remote.Host))
			if !o.noValidate && strings.TrimSpace(caddyCfg.Remote.ValidateCmd) != "" {
				out(c, "%s", ui.Info("[dry-run] run 'hl validate' to check the Caddyfile on the host"))
			}
		} else {
			var (
				deployOut string
				changed   bool
			)
			err := ui.WithSpinner(c.Context(), fmt.Sprintf("deploying to %s…", cfg.Caddy.Remote.Host), func(ctx context.Context) error {
				var e error
				deployOut, changed, e = caddy.Deploy(ctx, caddyCfg, o.force)
				return e
			})
			if err != nil {
				if errors.Is(err, caddy.ErrValidate) {
					out(c, "%s", ui.Warn("Caddyfile is invalid — nothing deployed (live config untouched)."))
					if s := strings.TrimSpace(deployOut); s != "" {
						out(c, "%s", ui.Detail(s))
					}
					return ErrReported
				}
				// Surface the remote output — the actual reason the deploy failed.
				out(c, "%s", ui.Warn("Deploy failed: %v", err))
				if s := strings.TrimSpace(deployOut); s != "" {
					out(c, "%s", ui.Detail(s))
				}
				return ErrReported
			}
			if !changed {
				out(c, "%s", ui.OK("Caddy already up to date — nothing to deploy (use --force to redeploy)."))
			} else {
				out(c, "%s", ui.OK("Validated and deployed — Caddy reloaded."))
				if s := strings.TrimSpace(deployOut); s != "" {
					out(c, "%s", ui.Detail(s))
				}
			}
		}
	}

	if !o.noDNS {
		if err := reconcileDNS(c, cfg, content, o.dryRun, o.noPrune, o.adopt); err != nil {
			return err
		}
	}
	return nil
}

// validatePreview runs the remote Caddyfile validator and reports the result in
// the styled format, returning ErrReported when the file is rejected. A blank
// validate_cmd (or --no-validate) is a no-op.
func validatePreview(c *cobra.Command, caddyCfg config.Caddy) error {
	if strings.TrimSpace(caddyCfg.Remote.ValidateCmd) == "" {
		return nil
	}
	var vout string
	err := ui.WithSpinner(c.Context(), fmt.Sprintf("validating on %s…", caddyCfg.Remote.Host), func(ctx context.Context) error {
		var e error
		vout, e = caddy.Validate(ctx, caddyCfg)
		return e
	})
	if err != nil {
		if errors.Is(err, caddy.ErrValidate) {
			out(c, "%s", ui.Warn("Caddyfile is invalid."))
			if s := strings.TrimSpace(vout); s != "" {
				out(c, "%s", ui.Detail(s))
			}
			return ErrReported
		}
		out(c, "%s", ui.Warn("Could not validate: %v", err))
		return ErrReported
	}
	out(c, "%s", ui.OK("Caddyfile is valid."))
	return nil
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check the local Caddyfile on the remote host without deploying",
		Long: `validate stages the local Caddyfile to a temporary path on the Caddy
host and runs the configured validator (caddy adapt) against it. The live
configuration is never touched; this only reports whether the file is valid.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.Caddy.Remote.ValidateCmd) == "" {
				out(c, "%s", ui.Info("validation is disabled (caddy.remote.validate_cmd is empty)"))
				return nil
			}
			return validatePreview(c, cfg.Caddy)
		},
	}
}

func computeDNSPlan(c *cobra.Command, cfg *config.Config, content string, noPrune, adopt bool) (desired []reconcile.Desired, plan reconcile.Plan, actual []technitium.Record, cl *technitium.Client, skip bool, reason string, err error) {
	sites, err := caddy.ParseSites(content)
	if err != nil {
		return nil, reconcile.Plan{}, nil, nil, false, "", err
	}
	desired, err = reconcile.DeriveDesired(sites)
	if err != nil {
		return nil, reconcile.Plan{}, nil, nil, false, "", err
	}
	if len(desired) == 0 {
		if strings.TrimSpace(content) == "" || len(sites) == 0 {
			return desired, reconcile.Plan{}, nil, nil, true, fmt.Sprintf("no site blocks in %s; skipping reconcile (not pruning)", cfg.Caddy.LocalFile), nil
		}
		if cfg.Technitium.Token == "" {
			return desired, reconcile.Plan{}, nil, nil, true, fmt.Sprintf("no managed records declared in %s", cfg.Caddy.LocalFile), nil
		}
	}
	cl, err = technitiumClient(c, cfg)
	if err != nil {
		return desired, reconcile.Plan{}, nil, nil, false, "", err
	}
	actual, err = listZoneRecords(c, cl)
	if err != nil {
		return desired, reconcile.Plan{}, nil, nil, false, "", err
	}
	plan = reconcile.BuildPlan(desired, actual, cfg.Caddy.ManagedTag, adopt)
	if noPrune {
		plan.Delete = nil
	}
	return desired, plan, actual, cl, false, "", nil
}

func printDNSPlan(c *cobra.Command, plan reconcile.Plan, managedCount int, dryRun bool) {
	if plan.Empty() && len(plan.Conflict) == 0 {
		out(c, "%s", ui.OK("DNS up to date (%d managed records)", managedCount))
		return
	}
	if dryRun {
		out(c, "%s", ui.Heading("DNS plan (dry-run, nothing applied)"))
	} else {
		out(c, "%s", ui.Heading("DNS changes"))
	}
	out(c, "%s", ui.RenderPlan(plan))
	if len(plan.Conflict) > 0 {
		out(c, "")
		out(c, "%s", ui.Warn("%d record(s) already exist and are not managed by hl. Re-run with --adopt", len(plan.Conflict)))
		out(c, "%s", ui.Info("  to overwrite same-type records; a cross-type collision (e.g. a CNAME"))
		out(c, "%s", ui.Info("  over an existing A or TXT) must be removed by hand first."))
	}
}

// reconcileDNS derives desired records from the Caddyfile content and brings the
// Technitium zone(s) into line, printing the plan. With dryRun it only prints;
// with adopt it overwrites records hl does not already manage.
func reconcileDNS(c *cobra.Command, cfg *config.Config, content string, dryRun, noPrune, adopt bool) error {
	var (
		desired []reconcile.Desired
		plan    reconcile.Plan
		cl      *technitium.Client
		skip    bool
		reason  string
		err     error
	)
	err = ui.WithSpinner(c.Context(), "reading DNS records…", func(context.Context) error {
		var e error
		desired, plan, _, cl, skip, reason, e = computeDNSPlan(c, cfg, content, noPrune, adopt)
		return e
	})
	if err != nil {
		return err
	}
	if skip {
		out(c, "%s", ui.Info("DNS: %s", reason))
		return nil
	}

	printDNSPlan(c, plan, len(desired), dryRun)
	if dryRun {
		return nil
	}
	if plan.Empty() {
		return nil
	}
	if err := ui.WithSpinner(c.Context(), "applying DNS changes…", func(ctx context.Context) error {
		return plan.Apply(ctx, cl, cfg.Caddy.ManagedTag)
	}); err != nil {
		return err
	}
	out(c, "%s", ui.OK("DNS reconciled — %d created, %d updated, %d deleted", len(plan.Create), len(plan.Update), len(plan.Delete)))
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

// listZoneRecords fetches all records across every zone hl can manage. Scanning
// all zones (rather than only those currently referenced by the Caddyfile)
// ensures a managed record is still pruned after the block that created it
// changes zones or is removed entirely, and lets conflict detection see existing
// unmanaged records anywhere.
func listZoneRecords(c *cobra.Command, cl *technitium.Client) ([]technitium.Record, error) {
	zones, err := cl.ListManagedZones(c.Context())
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
