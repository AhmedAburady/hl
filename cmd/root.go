// Package cmd wires the cobra command tree for hl.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/AhmedAburady/hl/internal/config"
	"github.com/charmbracelet/colorprofile"
	"github.com/spf13/cobra"
)

var configPath string

// ErrReported signals that a command already presented its failure to the user
// in the styled UI format and exited non-zero; the top-level error handler
// suppresses any further error rendering for it.
var ErrReported = errors.New("reported")

// Root returns the root command for the CLI.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "hl",
		Short: "Manage homelab Caddy reverse proxies and Technitium DNS records",
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "",
		"path to config file (default ~/.config/hl/config.yaml)")

	root.AddCommand(newSyncCmd(), newPullCmd(), newValidateCmd(), newStatusCmd(), newListCmd(), newConfigCmd())
	return root
}

var loadedCfg *config.Config

func loadCfg() (*config.Config, error) {
	if loadedCfg != nil {
		return loadedCfg, nil
	}
	c, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	loadedCfg = c
	return c, nil
}

var stdoutWriter io.Writer = colorprofile.NewWriter(os.Stdout, os.Environ())

func out(cmd *cobra.Command, format string, args ...any) {
	w := cmd.OutOrStdout()
	if w == os.Stdout {
		w = stdoutWriter
	}
	fmt.Fprintf(w, format+"\n", args...)
}
