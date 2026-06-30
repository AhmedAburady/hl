package cmd

import (
	"testing"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/AhmedAburady/hl/internal/ui"
)

func site(host, name, zone, upstream string, present bool) caddy.Site {
	s := caddy.Site{Host: host, Upstream: upstream}
	if present {
		s.DNS = caddy.DNSAnnotation{Name: name, Zone: zone, Value: "1.2.3.4", Present: true}
	}
	return s
}

func action(domain, zone string) reconcile.Action {
	return reconcile.Action{Domain: domain, Zone: zone, Type: technitium.TypeA, Value: "9.9.9.9"}
}

func rowFor(rows []ui.RecordRow, record string) (ui.RecordRow, bool) {
	for _, r := range rows {
		if r.Record == record {
			return r, true
		}
	}
	return ui.RecordRow{}, false
}

func TestBuildRecordRows(t *testing.T) {
	local := []caddy.Site{
		site("synced.example.com", "synced", "example.com", "10.0.0.1:8080", true),
		site("missing.example.com", "missing", "example.com", "10.0.0.2:8080", true),
		site("drift.example.com", "drift", "example.com", "10.0.0.3:8080", true),
		site("conflict.example.com", "conflict", "example.com", "10.0.0.4:8080", true),
		site("undeployed.example.com", "undeployed", "example.com", "10.0.0.5:8080", true),
		site("nodns.example.com", "", "", "10.0.0.6:8080", false),
		site("*.example.com", "", "", "", false),
	}
	remote := []caddy.Site{
		site("synced.example.com", "synced", "example.com", "10.0.0.1:8080", true),
		site("missing.example.com", "missing", "example.com", "10.0.0.2:8080", true),
		site("drift.example.com", "drift", "example.com", "10.0.0.3:8080", true),
		site("conflict.example.com", "conflict", "example.com", "10.0.0.4:8080", true),
		site("stale.example.com", "stale", "example.com", "10.0.0.7:8080", true),
	}
	plan := reconcile.Plan{
		Create: []reconcile.Action{action("missing.example.com", "example.com")},
		Update: []reconcile.Action{action("drift.example.com", "example.com")},
		Delete: []reconcile.Action{
			action("orphan.example.com", "example.com"),
			action("stale.example.com", "example.com"),
		},
		Conflict: []reconcile.Action{action("conflict.example.com", "example.com")},
	}

	rows := buildRecordRows(local, remote, plan, true, true)

	cases := []struct {
		record, value, proxy string
		local, dns, remote   ui.Mark
	}{
		{"synced.example.com", "1.2.3.4", "10.0.0.1:8080", ui.MarkOK, ui.MarkOK, ui.MarkOK},
		{"missing.example.com", "1.2.3.4", "10.0.0.2:8080", ui.MarkOK, ui.MarkMissing, ui.MarkOK},
		{"drift.example.com", "1.2.3.4", "10.0.0.3:8080", ui.MarkOK, ui.MarkDrift, ui.MarkOK},
		{"conflict.example.com", "1.2.3.4", "10.0.0.4:8080", ui.MarkOK, ui.MarkConflict, ui.MarkOK},
		{"undeployed.example.com", "1.2.3.4", "10.0.0.5:8080", ui.MarkOK, ui.MarkOK, ui.MarkMissing},
		{"orphan.example.com", "9.9.9.9", "", ui.MarkNA, ui.MarkOK, ui.MarkNA},
		{"stale.example.com", "9.9.9.9", "", ui.MarkNA, ui.MarkOK, ui.MarkOK},
	}
	for _, tc := range cases {
		r, ok := rowFor(rows, tc.record)
		if !ok {
			t.Errorf("%s: row not found", tc.record)
			continue
		}
		if r.Value != tc.value || r.Proxy != tc.proxy || r.Local != tc.local || r.DNS != tc.dns || r.Remote != tc.remote {
			t.Errorf("%s: got value=%q proxy=%q L=%v DNS=%v RE=%v; want value=%q proxy=%q L=%v DNS=%v RE=%v",
				tc.record, r.Value, r.Proxy, r.Local, r.DNS, r.Remote, tc.value, tc.proxy, tc.local, tc.dns, tc.remote)
		}
	}

	if _, ok := rowFor(rows, "nodns.example.com"); ok {
		t.Error("host without a DNS annotation should not appear")
	}
	for _, r := range rows {
		if isWildcard(r.Record) {
			t.Errorf("wildcard leaked into rows: %q", r.Record)
		}
	}
}

func TestBuildRecordRowsGroupedByZone(t *testing.T) {
	local := []caddy.Site{
		site("b.zeta.com", "b", "zeta.com", "10.0.0.1:1", true),
		site("a.alpha.com", "a", "alpha.com", "10.0.0.2:1", true),
		site("c.alpha.com", "c", "alpha.com", "10.0.0.3:1", true),
	}
	rows := buildRecordRows(local, nil, reconcile.Plan{}, true, true)
	want := []string{"a.alpha.com", "c.alpha.com", "b.zeta.com"}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, w := range want {
		if rows[i].Record != w {
			t.Errorf("row %d: got %q, want %q (zones must sort, then records)", i, rows[i].Record, w)
		}
	}
}

func TestBuildRecordRowsRemoteUnknown(t *testing.T) {
	local := []caddy.Site{site("a.example.com", "a", "example.com", "10.0.0.1:1", true)}
	rows := buildRecordRows(local, nil, reconcile.Plan{}, false, true)
	if r, ok := rowFor(rows, "a.example.com"); !ok || r.Remote != ui.MarkUnknown {
		t.Errorf("RE with remoteOK=false: got %v, want MarkUnknown", r.Remote)
	}
}

func TestBuildRecordRowsDNSUnknown(t *testing.T) {
	local := []caddy.Site{site("a.example.com", "a", "example.com", "10.0.0.1:1", true)}
	rows := buildRecordRows(local, nil, reconcile.Plan{}, true, false)
	if r, ok := rowFor(rows, "a.example.com"); !ok || r.DNS != ui.MarkUnknown {
		t.Errorf("DNS with dnsKnown=false: got %v, want MarkUnknown", r.DNS)
	}
}
