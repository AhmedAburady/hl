// Package reconcile derives the desired DNS state from annotated Caddyfile site
// blocks and computes/applies the changes needed to make a Technitium zone match.
// It is pure logic independent of the CLI: callers supply parsed sites, config,
// and a client.
package reconcile

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/AhmedAburady/hl/internal/caddy"
	"github.com/AhmedAburady/hl/internal/technitium"
)

// Desired is one DNS record the Caddyfile declares should exist.
type Desired struct {
	Domain string // FQDN, e.g. dsm.synology.com
	Zone   string
	Type   technitium.RecordType
	Value  string
	TTL    int
}

// DeriveDesired turns annotated sites into desired records. Sites without a DNS
// directive are skipped. A site whose directive cannot be fully resolved is an
// error.
func DeriveDesired(sites []caddy.Site) ([]Desired, error) {
	var out []Desired
	for _, s := range sites {
		if !s.DNS.Present {
			continue
		}
		d, err := Resolve(s.DNS)
		if err != nil {
			return nil, fmt.Errorf("site %s: %w", s.Host, err)
		}
		out = append(out, d)
	}
	return out, nil
}

// Resolve turns a single DNS annotation into a desired record. The Caddyfile is
// the sole source of truth: zone and value must be declared in the directive
// (there are no config defaults). Type is taken from type= or inferred from the
// value (IPv4 => A, else CNAME).
func Resolve(a caddy.DNSAnnotation) (Desired, error) {
	if a.Zone == "" {
		return Desired{}, fmt.Errorf("no zone: set zone= in the directive")
	}
	if a.Value == "" {
		return Desired{}, fmt.Errorf("no value: set value= in the directive")
	}
	typ, err := resolveType(a)
	if err != nil {
		return Desired{}, err
	}
	return Desired{
		Domain: fqdn(a.Name, a.Zone),
		Zone:   a.Zone,
		Type:   typ,
		Value:  a.Value,
		TTL:    a.TTL,
	}, nil
}

// resolveType determines the record type: explicit type= wins; otherwise it is
// inferred from the value (IPv4 => A, else CNAME).
func resolveType(a caddy.DNSAnnotation) (technitium.RecordType, error) {
	if a.Type != "" {
		switch technitium.RecordType(a.Type) {
		case technitium.TypeA, technitium.TypeCNAME:
			return technitium.RecordType(a.Type), nil
		default:
			return "", fmt.Errorf("unsupported type %q (want A or CNAME)", a.Type)
		}
	}
	if isIPv4(a.Value) {
		return technitium.TypeA, nil
	}
	// A non-IPv4 value that still parses as an IP is IPv6 (AAAA), which this tool
	// does not support; fail fast rather than send it as a bogus CNAME target.
	if net.ParseIP(a.Value) != nil {
		return "", fmt.Errorf("value %q is an IPv6 address (AAAA records are not supported); use type=A/CNAME", a.Value)
	}
	return technitium.TypeCNAME, nil
}

func fqdn(name, zone string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "@" {
		return zone
	}
	if strings.EqualFold(name, zone) || strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(zone)) {
		return name // already fully qualified
	}
	return name + "." + zone
}

func isIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// Action is one change in a Plan.
type Action struct {
	Kind   string // "create", "update", or "delete"
	Domain string
	Zone   string
	Type   technitium.RecordType
	Value  string
	TTL    int
}

// Plan is the set of changes to reconcile actual DNS state with desired.
type Plan struct {
	Create []Action
	Update []Action
	Delete []Action
}

// Empty reports whether the plan has no changes.
func (p Plan) Empty() bool {
	return len(p.Create)+len(p.Update)+len(p.Delete) == 0
}

// BuildPlan compares desired records against the actual records, considering
// only actual records tagged as managed (Comments == tag) for update/delete.
// Records the tool did not create are never touched.
func BuildPlan(desired []Desired, actual []technitium.Record, tag string) Plan {
	managed := map[string]technitium.Record{}
	for _, r := range actual {
		if r.Comments != tag {
			continue
		}
		if t := technitium.RecordType(r.Type); t != technitium.TypeA && t != technitium.TypeCNAME {
			continue
		}
		managed[recordKey(r.Name, technitium.RecordType(r.Type))] = r
	}

	var p Plan
	seen := map[string]bool{}
	for _, d := range desired {
		key := recordKey(d.Domain, d.Type)
		if seen[key] {
			continue // duplicate desired record (e.g. two blocks, same name+type)
		}
		seen[key] = true
		cur, ok := managed[key]
		act := Action{Kind: "create", Domain: d.Domain, Zone: d.Zone, Type: d.Type, Value: d.Value, TTL: d.TTL}
		if !ok {
			p.Create = append(p.Create, act)
			continue
		}
		// A desired TTL of 0 means "server default"; the server reports its own
		// value back, so only flag TTL drift when an explicit TTL was requested.
		ttlDrift := d.TTL > 0 && cur.TTL != d.TTL
		if !sameValue(d.Type, cur.Value(), d.Value) || ttlDrift {
			act.Kind = "update"
			p.Update = append(p.Update, act)
		}
	}
	// Managed records with no desired counterpart are pruned.
	for key, r := range managed {
		if seen[key] {
			continue
		}
		p.Delete = append(p.Delete, Action{
			Kind: "delete", Domain: r.Name, Zone: r.Zone,
			Type: technitium.RecordType(r.Type), Value: r.Value(), TTL: r.TTL,
		})
	}
	sortActions(p.Create)
	sortActions(p.Update)
	sortActions(p.Delete)
	return p
}

func recordKey(name string, t technitium.RecordType) string {
	return strings.ToLower(strings.TrimSuffix(name, ".")) + "|" + string(t)
}

// sameValue compares record values, normalizing the trailing dot for CNAME
// targets (Technitium may store them with or without it).
func sameValue(t technitium.RecordType, a, b string) bool {
	if t == technitium.TypeCNAME {
		return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
	}
	return a == b
}

func sortActions(a []Action) {
	sort.Slice(a, func(i, j int) bool { return a[i].Domain < a[j].Domain })
}

// Apply executes the plan against the server. Deletes run first so that a record
// whose type changed (e.g. A -> CNAME) has its old form removed before the new
// one is created — Technitium rejects a CNAME that coexists with another record
// at the same name. Create and update then use an overwriting, tagged AddRecord.
// It stops at the first error.
func (p Plan) Apply(ctx context.Context, cl *technitium.Client, tag string) error {
	for _, a := range p.Delete {
		req := technitium.DeleteRecordRequest{Domain: a.Domain, Zone: a.Zone, Type: a.Type, Value: a.Value}
		if err := cl.DeleteRecord(ctx, req); err != nil {
			return fmt.Errorf("delete %s %s: %w", a.Type, a.Domain, err)
		}
	}
	for _, a := range append(append([]Action{}, p.Create...), p.Update...) {
		req := technitium.AddRecordRequest{
			Domain: a.Domain, Zone: a.Zone, Type: a.Type,
			Value: a.Value, TTL: a.TTL, Overwrite: true, Comments: tag,
		}
		if err := cl.AddRecord(ctx, req); err != nil {
			return fmt.Errorf("%s %s %s: %w", a.Kind, a.Type, a.Domain, err)
		}
	}
	return nil
}

// String renders the plan as a human-readable diff.
func (p Plan) String() string {
	if p.Empty() {
		return "DNS: no changes"
	}
	var b strings.Builder
	for _, a := range p.Create {
		fmt.Fprintf(&b, "  + %-6s %-32s %s\n", a.Type, a.Domain, a.Value)
	}
	for _, a := range p.Update {
		fmt.Fprintf(&b, "  ~ %-6s %-32s %s\n", a.Type, a.Domain, a.Value)
	}
	for _, a := range p.Delete {
		fmt.Fprintf(&b, "  - %-6s %-32s %s\n", a.Type, a.Domain, a.Value)
	}
	return strings.TrimRight(b.String(), "\n")
}
