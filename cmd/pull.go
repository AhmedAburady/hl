package cmd

import (
	"context"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Download the remote Caddyfile to the local file",
		Long: `pull reads the live Caddyfile from the Caddy host over SSH and writes it
to the local file. If the local file already matches the remote, nothing is
written. Otherwise the existing local file is backed up (timestamped) before it
is overwritten — pull is the inverse of 'hl sync's deploy.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			return runPull(c, cfg, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report whether the local file would change without writing it")
	return cmd
}

// runPull fetches the remote Caddyfile and writes it locally when it differs,
// backing up the previous local file. It is a no-op when the two already match.
func runPull(c *cobra.Command, cfg *config.Config, dryRun bool) error {
	var remote string
	err := ui.WithSpinner(c.Context(), "fetching remote Caddyfile…", func(ctx context.Context) error {
		var e error
		remote, e = caddy.ReadRemoteFile(ctx, cfg.Caddy.Remote)
		return e
	})
	if err != nil {
		out(c, "%s", ui.Warn("Pull failed: %v", err))
		if s := strings.TrimSpace(remote); s != "" {
			out(c, "%s", ui.Detail(s))
		}
		return ErrReported
	}

	local, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return err
	}
	if local == remote {
		out(c, "%s", ui.OK("Already up to date — local Caddyfile matches remote"))
		return nil
	}

	if dryRun {
		if caddy.LocalFileExists(cfg.Caddy.LocalFile) {
			out(c, "%s", ui.Info("[dry-run] local %s differs from remote; would overwrite it (previous version backed up)", cfg.Caddy.LocalFile))
		} else {
			out(c, "%s", ui.Info("[dry-run] local %s does not exist; would create it from remote", cfg.Caddy.LocalFile))
		}
		return nil
	}

	if err := caddy.WriteLocalFile(cfg.Caddy.LocalFile, remote); err != nil {
		return err
	}
	out(c, "%s", ui.OK("Pulled remote Caddyfile to %s (previous version backed up)", cfg.Caddy.LocalFile))
	return nil
}
