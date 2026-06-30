package cmd

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/config"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/spf13/cobra"
)

type check struct {
	label  string
	ok     bool
	reason string
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Health check: Caddy running/reachable and Technitium reachable/resolving",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			var caddyRes, techRes check
			if err := ui.WithSpinner(c.Context(), "Checking status", func(context.Context) error {
				var wg sync.WaitGroup
				wg.Go(func() { caddyRes = caddyCheck(c, cfg) })
				wg.Go(func() { techRes = technitiumCheck(c, cfg) })
				wg.Wait()
				return nil
			}); err != nil {
				return err
			}
			checks := []check{caddyRes, techRes}
			width := 0
			for _, ck := range checks {
				if len(ck.label) > width {
					width = len(ck.label)
				}
			}
			ok := true
			for _, ck := range checks {
				out(c, "%s", ui.CheckLine(ck.label, width, ck.ok, ck.reason))
				ok = ok && ck.ok
			}
			if !ok {
				return ErrReported
			}
			return nil
		},
	}
	return cmd
}

func caddyCheck(c *cobra.Command, cfg *config.Config) check {
	label := "Caddy"
	active, status, err := caddy.ServiceActive(c.Context(), cfg.Caddy.Remote, "caddy")
	switch {
	case err != nil:
		return check{label, false, fmt.Sprintf("unreachable: %v", err)}
	case !active:
		return check{label, false, fmt.Sprintf("reachable but %s", status)}
	default:
		return check{label, true, ""}
	}
}

func technitiumCheck(c *cobra.Command, cfg *config.Config) check {
	label := "Technitium"
	cl, err := technitiumClient(c, cfg)
	if err != nil {
		return check{label, false, fmt.Sprintf("not configured: %v", err)}
	}
	if _, err := cl.ListManagedZones(c.Context()); err != nil {
		return check{label, false, fmt.Sprintf("API unreachable: %v", err)}
	}
	name := firstManagedFQDN(cfg)
	if name == "" {
		return check{label, true, ""}
	}
	host := technitiumDNSHost(cfg.Technitium.URL)
	if host == "" {
		return check{label, true, ""}
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(host, "53"))
		},
	}
	addrs, rerr := resolver.LookupHost(ctx, name)
	if rerr != nil || len(addrs) == 0 {
		return check{label, false, fmt.Sprintf("API up but Technitium did not resolve %s: %v", name, rerr)}
	}
	return check{label, true, ""}
}

func technitiumDNSHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func firstManagedFQDN(cfg *config.Config) string {
	content, err := caddy.ReadLocalFile(cfg.Caddy.LocalFile)
	if err != nil {
		return ""
	}
	sites, err := caddy.ParseSites(content)
	if err != nil {
		return ""
	}
	for _, s := range sites {
		if !s.DNS.Present {
			continue
		}
		if d, err := reconcile.Resolve(s.DNS); err == nil && !isWildcard(d.Domain) {
			return d.Domain
		}
	}
	return ""
}
