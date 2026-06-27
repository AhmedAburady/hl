package cmd

import (
	"errors"
	"fmt"

	"github.com/AhmedAburady/hl/internal/prompt"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/spf13/cobra"
)

func newDNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: "Manage Technitium DNS records",
	}
	cmd.AddCommand(newDNSAddCmd(), newDNSListCmd(), newDNSLoginCmd())
	return cmd
}

func newDNSAddCmd() *cobra.Command {
	var (
		rtype     string
		value     string
		zone      string
		ttl       int
		overwrite bool
		comments  string
	)
	cmd := &cobra.Command{
		Use:   "add [domain]",
		Short: "Add an A or CNAME record to a Technitium zone",
		Args:  cobra.MaximumNArgs(1),
		Example: `
  hl dns add app.home.lab --type CNAME --value caddy.home.lab.
  hl dns add app.home.lab --type A --value 192.168.1.10 --overwrite`,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			domain := ""
			if len(args) > 0 {
				domain = args[0]
			}
			if domain == "" || value == "" || zone == "" {
				domain, value, zone, err = prompt.ForDNSAdd(domain, value, zone)
				if err != nil {
					return err
				}
			}
			return addDNSRecord(c, cfg, domain, zone, ttl, rtype, "A", value, comments, overwrite)
		},
	}
	cmd.Flags().StringVar(&rtype, "type", "", "record type: A or CNAME (default A)")
	cmd.Flags().StringVar(&value, "value", "", "record value (IP for A, target for CNAME)")
	cmd.Flags().StringVar(&zone, "zone", "", "DNS zone (default technitium.default_zone)")
	cmd.Flags().IntVar(&ttl, "ttl", 0, "TTL in seconds (0 = server default)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace existing record set for this type")
	cmd.Flags().StringVar(&comments, "comments", "", "comments for the record")
	return cmd
}

func newDNSListCmd() *cobra.Command {
	var zone string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List records in a Technitium zone",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if cfg.Technitium.Token == "" {
				return errors.New("technitium.token is not set; run `hl dns login` first")
			}
			z := zone
			if z == "" {
				z = cfg.Technitium.DefaultZone
			}
			if z == "" {
				return errors.New("no zone: set technitium.default_zone in config or pass --zone")
			}
			cl := technitium.New(cfg.Technitium.URL, cfg.Technitium.Token)
			records, err := cl.ListRecords(c.Context(), z, "")
			if err != nil {
				return fmt.Errorf("list records: %w", err)
			}
			out(c, "%-32s %-6s %-6s %s", "NAME", "TYPE", "TTL", "VALUE")
			for _, r := range records {
				out(c, "%-32s %-6s %-6d %s", r.Name, r.Type, r.TTL, recordValue(r))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&zone, "zone", "", "DNS zone (default technitium.default_zone)")
	return cmd
}

func newDNSLoginCmd() *cobra.Command {
	var (
		user string
		pass string
		totp string
		name string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Create a Technitium API token and save it to config",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if user == "" || pass == "" {
				a, err := prompt.ForLogin(user, pass, totp, name)
				if err != nil {
					return err
				}
				user, pass, totp, name = a.User, a.Pass, a.TOTP, a.Name
			}
			cl := technitium.New(cfg.Technitium.URL, "")
			token, err := cl.CreateToken(c.Context(), user, pass, totp, name)
			if err != nil {
				return fmt.Errorf("create token: %w", err)
			}
			if err := cfg.SetToken(token); err != nil {
				return fmt.Errorf("save token: %w", err)
			}
			out(c, "Saved Technitium token to %s", cfg.Path())
			return nil
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "Technitium admin user")
	cmd.Flags().StringVar(&pass, "pass", "", "Technitium admin password")
	cmd.Flags().StringVar(&totp, "totp", "", "2FA code; required if the account has 2FA enabled (prompted only when --user/--pass are omitted)")
	cmd.Flags().StringVar(&name, "token-name", "", "name for the created token (default hl)")
	return cmd
}

func recordValue(r technitium.Record) string {
	if r.RData == nil {
		return ""
	}
	for _, k := range []string{"ipAddress", "cname", "nameServer", "exchange", "text", "target"} {
		if v, ok := r.RData[k]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}
