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
	p := BuildPlan(nil, actual, tag, false)
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
	p := BuildPlan(d, nil, tag, false)
	if len(p.Create) != 1 || p.Empty() {
		t.Fatalf("got %+v", p)
	}
}

func TestBuildPlan_NoChangeWhenMatching(t *testing.T) {
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1"}}
	actual := []technitium.Record{rec("a.home.lab", "A", "1.1.1.1", tag)}
	p := BuildPlan(d, actual, tag, false)
	if !p.Empty() {
		t.Fatalf("expected no changes, got %+v", p)
	}
}

func TestBuildPlan_UpdateOnValueDrift(t *testing.T) {
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "2.2.2.2"}}
	actual := []technitium.Record{rec("a.home.lab", "A", "1.1.1.1", tag)}
	p := BuildPlan(d, actual, tag, false)
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
	p := BuildPlan(d, actual, tag, false)
	if len(p.Delete) != 1 || p.Delete[0].Domain != "managed.home.lab" {
		t.Fatalf("expected only managed orphan deleted, got %+v", p.Delete)
	}
}

func TestBuildPlan_TTLZeroIsNotDrift(t *testing.T) {
	// Desired TTL 0 (server default) must not flag an update against the
	// server-reported TTL, or every sync would re-update the record forever.
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1", TTL: 0}}
	actual := []technitium.Record{{Name: "a.home.lab", Type: "A", TTL: 3600, Comments: tag, RData: map[string]any{"ipAddress": "1.1.1.1"}}}
	if p := BuildPlan(d, actual, tag, false); !p.Empty() {
		t.Fatalf("ttl=0 should not be a change, got %+v", p)
	}
}

func TestBuildPlan_ExplicitTTLDrift(t *testing.T) {
	d := []Desired{{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1", TTL: 300}}
	actual := []technitium.Record{{Name: "a.home.lab", Type: "A", TTL: 3600, Comments: tag, RData: map[string]any{"ipAddress": "1.1.1.1"}}}
	if p := BuildPlan(d, actual, tag, false); len(p.Update) != 1 {
		t.Fatalf("explicit ttl drift should update, got %+v", p)
	}
}

func TestBuildPlan_DedupesDuplicateDesired(t *testing.T) {
	d := []Desired{
		{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1"},
		{Domain: "a.home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.1.1.1"},
	}
	if p := BuildPlan(d, nil, tag, false); len(p.Create) != 1 {
		t.Fatalf("expected a single create, got %+v", p.Create)
	}
}

func TestBuildPlan_DeleteCarriesZone(t *testing.T) {
	actual := []technitium.Record{{Name: "orphan.home.lab", Type: "A", Zone: "home.lab", Comments: tag, RData: map[string]any{"ipAddress": "9.9.9.9"}}}
	p := BuildPlan(nil, actual, tag, false)
	if len(p.Delete) != 1 || p.Delete[0].Zone != "home.lab" {
		t.Fatalf("delete action should carry the record zone, got %+v", p.Delete)
	}
}

func TestBuildPlan_CnameTrailingDotEquivalence(t *testing.T) {
	d := []Desired{{Domain: "x.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab"}}
	actual := []technitium.Record{rec("x.home.lab", "CNAME", "caddy.home.lab.", tag)}
	p := BuildPlan(d, actual, tag, false)
	if !p.Empty() {
		t.Fatalf("trailing-dot difference should not be a change: %+v", p)
	}
}

func TestBuildPlan_ConflictWithUnmanagedSameType(t *testing.T) {
	d := []Desired{{Domain: "app.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab."}}
	actual := []technitium.Record{rec("app.home.lab", "CNAME", "other.home.lab.", "")} // untagged
	p := BuildPlan(d, actual, tag, false)
	if len(p.Conflict) != 1 || len(p.Create) != 0 {
		t.Fatalf("expected a conflict and no create, got %+v", p)
	}
}

func TestBuildPlan_AdoptOverwritesSameType(t *testing.T) {
	d := []Desired{{Domain: "app.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab."}}
	actual := []technitium.Record{rec("app.home.lab", "CNAME", "other.home.lab.", "")}
	p := BuildPlan(d, actual, tag, true)
	if len(p.Conflict) != 0 || len(p.Create) != 1 || len(p.Delete) != 0 {
		t.Fatalf("adopt of same-type should create (overwrite) with no delete, got %+v", p)
	}
}

func TestBuildPlan_AdoptCrossTypeStaysConflict(t *testing.T) {
	// Desired CNAME where an untagged A exists: hl must NOT delete the unmanaged A
	// to force the create (destructive + non-atomic). It stays a conflict.
	d := []Desired{{Domain: "app.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab."}}
	actual := []technitium.Record{rec("app.home.lab", "A", "9.9.9.9", "")}
	p := BuildPlan(d, actual, tag, true)
	if len(p.Conflict) != 1 || len(p.Create) != 0 || len(p.Delete) != 0 {
		t.Fatalf("adopt must not delete an unmanaged cross-type record, got %+v", p)
	}
}

func TestBuildPlan_AdoptRefusesNonSameTypeBlocker(t *testing.T) {
	// A desired CNAME collides with an untagged TXT (a type hl cannot manage).
	// Even with adopt this must stay a conflict, never a create that the server
	// would reject for CNAME exclusivity.
	d := []Desired{{Domain: "app.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab."}}
	actual := []technitium.Record{{Name: "app.home.lab", Type: "TXT", RData: map[string]any{"text": "x"}}}
	p := BuildPlan(d, actual, tag, true)
	if len(p.Conflict) != 1 || len(p.Create) != 0 || len(p.Delete) != 0 {
		t.Fatalf("adopt must refuse a non-same-type blocker, got %+v", p)
	}
}

func TestBuildPlan_ConflictProtectsManagedFromPrune(t *testing.T) {
	// app has a managed A (ours) plus an unmanaged TXT. The annotation switches
	// the desired type to CNAME -> conflict (TXT blocks it). The managed A must
	// NOT be pruned, or the host loses DNS with no replacement applied.
	d := []Desired{{Domain: "app.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab."}}
	actual := []technitium.Record{
		rec("app.home.lab", "A", "1.1.1.1", tag),                                // managed
		{Name: "app.home.lab", Type: "TXT", RData: map[string]any{"text": "x"}}, // unmanaged blocker
	}
	p := BuildPlan(d, actual, tag, false)
	if len(p.Conflict) != 1 || len(p.Delete) != 0 {
		t.Fatalf("conflict must protect the managed record from prune, got %+v", p)
	}
}

func TestBuildPlan_NoConflictForDistinctName(t *testing.T) {
	d := []Desired{{Domain: "new.home.lab", Zone: "home.lab", Type: technitium.TypeCNAME, Value: "caddy.home.lab."}}
	actual := []technitium.Record{rec("other.home.lab", "CNAME", "x.home.lab.", "")}
	p := BuildPlan(d, actual, tag, false)
	if len(p.Conflict) != 0 || len(p.Create) != 1 {
		t.Fatalf("unrelated unmanaged record should not conflict, got %+v", p)
	}
}

func TestBuildPlan_ApexACoexistsWithUnmanagedNS(t *testing.T) {
	// An A at the apex must not conflict with the zone's own NS/SOA records.
	d := []Desired{{Domain: "home.lab", Zone: "home.lab", Type: technitium.TypeA, Value: "1.2.3.4"}}
	actual := []technitium.Record{
		{Name: "home.lab", Type: "NS", RData: map[string]any{"nameServer": "ns1.home.lab"}},
		{Name: "home.lab", Type: "SOA", RData: map[string]any{}},
	}
	p := BuildPlan(d, actual, tag, false)
	if len(p.Conflict) != 0 || len(p.Create) != 1 {
		t.Fatalf("apex A should not conflict with NS/SOA, got %+v", p)
	}
}
