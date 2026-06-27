package reconcile

import (
	"testing"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/technitium"
)

func ann(name string, set func(*caddy.DNSAnnotation)) caddy.Site {
	a := caddy.DNSAnnotation{Name: name, Zone: "home.lab", Value: "caddy.home.lab.", Present: true}
	if set != nil {
		set(&a)
	}
	return caddy.Site{Host: name + ".home.lab", DNS: a}
}

func TestDeriveDesired_ExplicitType(t *testing.T) {
	got, err := DeriveDesired([]caddy.Site{ann("dsm", func(a *caddy.DNSAnnotation) { a.Type = "CNAME" })})
	if err != nil {
		t.Fatal(err)
	}
	d := got[0]
	if d.Domain != "dsm.home.lab" || d.Type != technitium.TypeCNAME || d.Value != "caddy.home.lab." || d.Zone != "home.lab" {
		t.Fatalf("got %+v", d)
	}
}

func TestDeriveDesired_InfersTypeFromValue(t *testing.T) {
	got, err := DeriveDesired([]caddy.Site{ann("nas", func(a *caddy.DNSAnnotation) { a.Value = "10.0.0.5" })})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Type != technitium.TypeA {
		t.Fatalf("expected A, got %s", got[0].Type)
	}
}

func TestDeriveDesired_ZoneOverrideAndFQDN(t *testing.T) {
	got, _ := DeriveDesired([]caddy.Site{ann("dsm", func(a *caddy.DNSAnnotation) {
		a.Type = "CNAME"
		a.Zone = "synology.com"
	})})
	if got[0].Domain != "dsm.synology.com" || got[0].Zone != "synology.com" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestDeriveDesired_NoZoneErrors(t *testing.T) {
	_, err := DeriveDesired([]caddy.Site{ann("x", func(a *caddy.DNSAnnotation) { a.Zone = "" })})
	if err == nil {
		t.Fatal("expected zone error")
	}
}

func TestDeriveDesired_NoValueErrors(t *testing.T) {
	_, err := DeriveDesired([]caddy.Site{ann("x", func(a *caddy.DNSAnnotation) { a.Value = "" })})
	if err == nil {
		t.Fatal("expected value error")
	}
}

func TestDeriveDesired_IPv6WithoutTypeErrors(t *testing.T) {
	_, err := DeriveDesired([]caddy.Site{ann("x", func(a *caddy.DNSAnnotation) { a.Value = "2001:db8::1" })})
	if err == nil {
		t.Fatal("expected error inferring type for an IPv6 value")
	}
}

func TestBuildPlan_PrunesAllWhenDesiredEmpty(t *testing.T) {
	// Removing every annotation must still prune the records hl created.
	actual := []technitium.Record{rec("orphan.home.lab", "A", "9.9.9.9", tag)}
	p := BuildPlan(nil, actual, tag)
	if len(p.Delete) != 1 {
		t.Fatalf("expected orphan pruned with empty desired, got %+v", p)
	}
}

func TestDeriveDesired_SkipsUnannotated(t *testing.T) {
	got, err := DeriveDesired([]caddy.Site{{Host: "plain.home.lab"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unannotated site produced records: %+v", got)
	}
}

const tag = "managed-by:hl"

func rec(name, typ, val, comments string) technitium.Record {
	rd := map[string]any{}
	if typ == "A" {
		rd["ipAddress"] = val
	} else {
		rd["cname"] = val
	}
	return technitium.Record{Name: name, Type: typ, RData: rd, Comments: comments}
}

func TestBuildPlan_Create(t *testing.T) {
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1"}}
	p := BuildPlan(d, nil, tag)
	if len(p.Create) != 1 || p.Empty() {
		t.Fatalf("got %+v", p)
	}
}

func TestBuildPlan_NoChangeWhenMatching(t *testing.T) {
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1"}}
	actual := []technitium.Record{rec("a.home.lab", "A", "1.1.1.1", tag)}
	p := BuildPlan(d, actual, tag)
	if !p.Empty() {
		t.Fatalf("expected no changes, got %+v", p)
	}
}

func TestBuildPlan_UpdateOnValueDrift(t *testing.T) {
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "2.2.2.2"}}
	actual := []technitium.Record{rec("a.home.lab", "A", "1.1.1.1", tag)}
	p := BuildPlan(d, actual, tag)
	if len(p.Update) != 1 {
		t.Fatalf("expected update, got %+v", p)
	}
}

func TestBuildPlan_DeletesOnlyManagedOrphans(t *testing.T) {
	d := []Desired{}
	actual := []technitium.Record{
		rec("managed.home.lab", "A", "1.1.1.1", tag),
		rec("manual.home.lab", "A", "9.9.9.9", ""), // not tagged => must be left alone
	}
	p := BuildPlan(d, actual, tag)
	if len(p.Delete) != 1 || p.Delete[0].Domain != "managed.home.lab" {
		t.Fatalf("expected only managed orphan deleted, got %+v", p.Delete)
	}
}

func TestBuildPlan_TTLZeroIsNotDrift(t *testing.T) {
	// Desired TTL 0 (server default) must not flag an update against the
	// server-reported TTL, or every sync would re-update the record forever.
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1", TTL: 0}}
	actual := []technitium.Record{{Name: "a.home.lab", Type: "A", TTL: 3600, Comments: tag, RData: map[string]any{"ipAddress": "1.1.1.1"}}}
	if p := BuildPlan(d, actual, tag); !p.Empty() {
		t.Fatalf("ttl=0 should not be a change, got %+v", p)
	}
}

func TestBuildPlan_ExplicitTTLDrift(t *testing.T) {
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1", TTL: 300}}
	actual := []technitium.Record{{Name: "a.home.lab", Type: "A", TTL: 3600, Comments: tag, RData: map[string]any{"ipAddress": "1.1.1.1"}}}
	if p := BuildPlan(d, actual, tag); len(p.Update) != 1 {
		t.Fatalf("explicit ttl drift should update, got %+v", p)
	}
}

func TestBuildPlan_DedupesDuplicateDesired(t *testing.T) {
	d := []Desired{
		{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1"},
		{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1"},
	}
	if p := BuildPlan(d, nil, tag); len(p.Create) != 1 {
		t.Fatalf("expected a single create, got %+v", p.Create)
	}
}

func TestBuildPlan_DeleteCarriesZone(t *testing.T) {
	actual := []technitium.Record{{Name: "orphan.home.lab", Type: "A", Zone: "home.lab", Comments: tag, RData: map[string]any{"ipAddress": "9.9.9.9"}}}
	p := BuildPlan(nil, actual, tag)
	if len(p.Delete) != 1 || p.Delete[0].Zone != "home.lab" {
		t.Fatalf("delete action should carry the record zone, got %+v", p.Delete)
	}
}

func TestBuildPlan_CnameTrailingDotEquivalence(t *testing.T) {
	d := []Desired{{Domain: "x.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab"}}
	actual := []technitium.Record{rec("x.home.lab", "CNAME", "caddy.home.lab.", tag)}
	p := BuildPlan(d, actual, tag)
	if !p.Empty() {
		t.Fatalf("trailing-dot difference should not be a change: %+v", p)
	}
}
