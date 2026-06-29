package cmd

import (
	"testing"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/AhmedAburady/hl/internal/ui"
)

func site(host, name, zone string, present bool) caddy.Site {
	s := caddy.Site{Host: host}
	if present {
		s.DNS = caddy.DNSAnnotation{Name: name, Zone: zone, Value: "1.2.3.4", Present: true}
	}
	return s
}

func action(domain string) reconcile.Action {
	return reconcile.Action{Domain: domain, Type: technitium.TypeA, Value: "1.2.3.4"}
}

func rowFor(rows []ui.StatusRow, host string) ui.StatusRow {
	for _, r := range rows {
		if r.Host == host {
			return r
		}
	}
	return ui.StatusRow{}
}

func TestBuildStatusRows(t *testing.T) {
	local := []caddy.Site{
		site("synced.example.com", "synced", "example.com", true),
		site("missing.example.com", "missing", "example.com", true),
		site("drift.example.com", "drift", "example.com", true),
		site("conflict.example.com", "conflict", "example.com", true),
		site("undeployed.example.com", "undeployed", "example.com", true),
		site("nodns.example.com", "", "", false),
	}
	remote := []caddy.Site{
		site("synced.example.com", "synced", "example.com", true),
		site("missing.example.com", "missing", "example.com", true),
		site("drift.example.com", "drift", "example.com", true),
		site("conflict.example.com", "conflict", "example.com", true),
		site("nodns.example.com", "", "", false),
		site("remoteonly.example.com", "remoteonly", "example.com", true),
	}
	plan := reconcile.Plan{
		Create:   []reconcile.Action{action("missing.example.com")},
		Update:   []reconcile.Action{action("drift.example.com")},
		Delete:   []reconcile.Action{action("orphan.example.com")},
		Conflict: []reconcile.Action{action("conflict.example.com")},
	}

	untracked := []string{"untracked.example.com"}

	rows := buildStatusRows(local, remote, plan, untracked, true, true)

	cases := []struct {
		host              string
		local, dns, caddy ui.Mark
	}{
		{"synced.example.com", ui.MarkOK, ui.MarkOK, ui.MarkOK},
		{"missing.example.com", ui.MarkOK, ui.MarkMissing, ui.MarkOK},
		{"drift.example.com", ui.MarkOK, ui.MarkDrift, ui.MarkOK},
		{"conflict.example.com", ui.MarkOK, ui.MarkConflict, ui.MarkOK},
		{"undeployed.example.com", ui.MarkOK, ui.MarkOK, ui.MarkMissing},
		{"nodns.example.com", ui.MarkOK, ui.MarkNA, ui.MarkOK},
		{"remoteonly.example.com", ui.MarkNA, ui.MarkNA, ui.MarkOK},
		{"orphan.example.com", ui.MarkNA, ui.MarkMissing, ui.MarkNA},
		{"untracked.example.com", ui.MarkNA, ui.MarkUntracked, ui.MarkNA},
	}
	for _, tc := range cases {
		r := rowFor(rows, tc.host)
		if r.Host != tc.host {
			t.Errorf("%s: row not found", tc.host)
			continue
		}
		if r.Local != tc.local || r.DNS != tc.dns || r.Caddy != tc.caddy {
			t.Errorf("%s: got LOCAL=%v DNS=%v CADDY=%v; want LOCAL=%v DNS=%v CADDY=%v",
				tc.host, r.Local, r.DNS, r.Caddy, tc.local, tc.dns, tc.caddy)
		}
	}

	for i := 1; i < len(rows); i++ {
		if reconcile.NameKey(rows[i-1].Host) > reconcile.NameKey(rows[i].Host) {
			t.Errorf("rows not sorted: %q before %q", rows[i-1].Host, rows[i].Host)
		}
	}
}

func TestBuildStatusRowsRemoteUnknown(t *testing.T) {
	local := []caddy.Site{site("a.example.com", "a", "example.com", true)}
	rows := buildStatusRows(local, nil, reconcile.Plan{}, nil, false, true)
	if got := rowFor(rows, "a.example.com").Caddy; got != ui.MarkUnknown {
		t.Errorf("CADDY with remoteOK=false: got %v, want MarkUnknown", got)
	}
}

func TestBuildStatusRowsDNSUnknown(t *testing.T) {
	local := []caddy.Site{
		site("a.example.com", "a", "example.com", true),
		site("plain.example.com", "", "", false),
	}
	rows := buildStatusRows(local, nil, reconcile.Plan{}, nil, true, false)
	if got := rowFor(rows, "a.example.com").DNS; got != ui.MarkUnknown {
		t.Errorf("DNS with dnsKnown=false: got %v, want MarkUnknown", got)
	}
	if got := rowFor(rows, "plain.example.com").DNS; got != ui.MarkNA {
		t.Errorf("DNS for no-intent host: got %v, want MarkNA", got)
	}
}
