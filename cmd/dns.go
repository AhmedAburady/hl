package cmd

import (
	"errors"
	"fmt"

	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/spf13/cobra"
)

func newDNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: "Inspect Technitium DNS records",
	}
	cmd.AddCommand(newDNSListCmd())
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
			if zone == "" {
				return errors.New("no zone: pass --zone")
			}
			cl, err := technitiumClient(c, cfg)
			if err != nil {
				return err
			}
			records, err := cl.ListRecords(c.Context(), zone, "")
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
	cmd.Flags().StringVar(&zone, "zone", "", "DNS zone (required)")
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
