package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the hl configuration",
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigShowCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the config file, interactively when possible",
		RunE: func(c *cobra.Command, _ []string) error {
			path := configPath
			if path == "" {
				path = config.DefaultPath()
			}

			if !isInteractive() {
				if config.Exists(path) && !force {
					return fmt.Errorf("config already exists at %s (use --force to overwrite)", path)
				}
				vals := config.DefaultInitValues()
				if err := config.Write(path, config.Render(vals)); err != nil {
					return fmt.Errorf("write config: %w", err)
				}
				out(c, "Wrote config template to %s; edit technitium.token and caddy.remote.host.", path)
				ensureStarterCaddyfile(c, vals.LocalFile)
				return nil
			}

			if config.Exists(path) && !force {
				overwrite, err := confirm(fmt.Sprintf("Config already exists at %s.\nOverwrite it?", path))
				if err != nil {
					return err
				}
				if !overwrite {
					out(c, "Aborted; existing config left unchanged.")
					return nil
				}
			}

			vals := config.DefaultInitValues()
			if err := runInitWizard(&vals); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					out(c, "Aborted.")
					return nil
				}
				return err
			}
			if err := config.Write(path, config.Render(vals)); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			out(c, "Wrote config to %s", path)
			ensureStarterCaddyfile(c, vals.LocalFile)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config without prompting")
	return cmd
}

// ensureStarterCaddyfile writes a documented starter Caddyfile at path when none
// exists, so a new user has an editable example. A failure is non-fatal.
func ensureStarterCaddyfile(c *cobra.Command, path string) {
	if path == "" || caddy.LocalFileExists(path) {
		return
	}
	resolved := caddy.ResolvePath(path)
	if err := caddy.WriteLocalFile(path, caddy.StarterCaddyfile()); err != nil {
		out(c, "warning: could not create starter Caddyfile at %s: %v", resolved, err)
		return
	}
	out(c, "Created a starter Caddyfile at %s (documents how to add hosts).", resolved)
}

// runInitWizard runs a styled form for the essential settings, mutating vals.
func runInitWizard(vals *config.InitValues) error {
	useAgent := vals.SSHKey == ""
	// Prefill the key path only; the agent socket stays empty so it resolves
	// $SSH_AUTH_SOCK at run time (the per-session launchd socket must not be saved).
	if vals.SSHKey == "" {
		vals.SSHKey = "~/.ssh/id_ed25519"
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("hl setup").Description("Technitium DNS + Caddy reverse-proxy details."),
			huh.NewInput().Title("Technitium DNS server URL").Value(&vals.URL),
			huh.NewInput().Title("Technitium API token").
				Description("From the Technitium UI. Literal, ${ENV_VAR}, or op://vault/item/field.").
				EchoMode(huh.EchoModePassword).
				Value(&vals.Token),
		),
		huh.NewGroup(
			huh.NewInput().Title("Caddy server host (SSH)").Placeholder("192.168.1.20").Value(&vals.RemoteHost),
			huh.NewInput().Title("SSH user").Value(&vals.RemoteUser),
			// Two side-by-side pills: [ ssh-agent ] [ ssh key ].
			huh.NewConfirm().Title("SSH authentication").
				Affirmative("ssh-agent").Negative("ssh key").Value(&useAgent),
		),
		// One of the next two groups shows, based on the toggle above.
		huh.NewGroup(
			huh.NewInput().Title("ssh-agent socket").
				Description("Leave empty to use $SSH_AUTH_SOCK. Set a stable path only for a custom agent (e.g. 1Password).").
				Placeholder(config.SuggestedAgentSocket()).
				Value(&vals.AgentSocket),
		).WithHideFunc(func() bool { return !useAgent }),
		huh.NewGroup(
			huh.NewInput().Title("SSH private key path").Value(&vals.SSHKey),
		).WithHideFunc(func() bool { return useAgent }),
		huh.NewGroup(
			huh.NewInput().Title("Local Caddyfile path").Value(&vals.LocalFile),
			huh.NewInput().Title("Remote Caddyfile path").Value(&vals.RemotePath),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if useAgent {
		vals.SSHKey = ""
	} else {
		vals.AgentSocket = ""
	}
	return nil
}

// confirm shows a styled yes/no prompt defaulting to no.
func confirm(title string) (bool, error) {
	var ok bool
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Affirmative("Overwrite").Negative("Cancel").Value(&ok),
	)).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	return ok, err
}

// isInteractive reports whether stdin is a terminal we can prompt on.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration (token redacted)",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			token := cfg.Technitium.Token
			if token != "" {
				token = "<set>"
			}
			out(c, "config file: %s", cfg.Path())
			out(c, "")
			out(c, "technitium:")
			out(c, "  url:          %s", cfg.Technitium.URL)
			out(c, "  token:        %s", token)
			out(c, "caddy:")
			out(c, "  local_file:   %s", cfg.Caddy.LocalFile)
			out(c, "  managed_tag:  %s", cfg.Caddy.ManagedTag)
			out(c, "  remote:")
			out(c, "    host:         %s", cfg.Caddy.Remote.Host)
			out(c, "    user:         %s", cfg.Caddy.Remote.User)
			out(c, "    port:         %d", cfg.Caddy.Remote.Port)
			out(c, "    key:          %s", redactKey(cfg.Caddy.Remote.Key))
			out(c, "    agent_socket: %s", agentSocketDisplay(cfg.Caddy.Remote.AgentSocket))
			out(c, "    remote_path:  %s", cfg.Caddy.Remote.RemotePath)
			out(c, "    reload_cmd:   %s", cfg.Caddy.Remote.ReloadCmd)
			out(c, "    validate_cmd: %s", validateCmdDisplay(cfg.Caddy.Remote.ValidateCmd))
			return nil
		},
	}
	return cmd
}

// validateCmdDisplay shows the validator command, noting when it is disabled.
func validateCmdDisplay(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(disabled)"
	}
	return v
}

func redactKey(k string) string {
	if k == "" {
		return "(ssh-agent)"
	}
	return k
}

// agentSocketDisplay shows the configured ssh-agent socket, noting the
// environment fallback when none is set.
func agentSocketDisplay(s string) string {
	if s == "" {
		return "($SSH_AUTH_SOCK)"
	}
	return s
}
