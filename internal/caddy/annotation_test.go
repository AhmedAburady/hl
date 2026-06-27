package caddy

import (
	"strings"
	"testing"
)

func TestParseSites_BraceInUpstreamArgNotBlockForm(t *testing.T) {
	// A single-line reverse_proxy whose argument contains a brace placeholder
	// must not be mistaken for the block form.
	content := "x.home.lab {\n\treverse_proxy {$UPSTREAM}\n}\n"
	sites, err := ParseSites(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sites[0].Upstream != "{$UPSTREAM}" {
		t.Fatalf("upstream wrong: %q", sites[0].Upstream)
	}
}

func TestUpsertReverseProxy_BraceArgUpdatesInPlace(t *testing.T) {
	content := "x.home.lab {\n\treverse_proxy {$OLD}\n}\n"
	out, _, err := UpsertReverseProxy(content, "x.home.lab", "http://10.0.0.1:80", false)
	if err != nil {
		t.Fatalf("brace-arg single-line should not be treated as block form: %v", err)
	}
	if !strings.Contains(out, "reverse_proxy http://10.0.0.1:80") {
		t.Fatalf("directive not updated:\n%s", out)
	}
}

func TestParseSites_WithDirective(t *testing.T) {
	content := "# dsm type=CNAME zone=synology.com value=caddy.lan. ttl=300\ndsm.synology.com {\n\treverse_proxy 127.0.0.1:5003\n}\n"
	sites, err := ParseSites(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("got %d sites", len(sites))
	}
	d := sites[0].DNS
	if !d.Present || d.Name != "dsm" || d.Type != "CNAME" || d.Zone != "synology.com" || d.Value != "caddy.lan." || d.TTL != 300 {
		t.Fatalf("directive parsed wrong: %+v", d)
	}
}

func TestParseSites_ProseCommentIgnored(t *testing.T) {
	// A prose comment without key=value must not be treated as a directive.
	content := "# home assistant box\nha.home.lab {\n\treverse_proxy 127.0.0.1:8123\n}\n"
	sites, err := ParseSites(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sites[0].DNS.Present {
		t.Fatalf("prose comment wrongly parsed as directive: %+v", sites[0].DNS)
	}
}

func TestParseSites_UnknownKeyErrors(t *testing.T) {
	content := "# dsm type=CNAME zoen=synology.com\ndsm.synology.com {\n\treverse_proxy 127.0.0.1:5003\n}\n"
	if _, err := ParseSites(content); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestParseSites_MultipleDirectivesErrors(t *testing.T) {
	content := "# a type=A\n# b type=CNAME\nx.home.lab {\n\treverse_proxy 127.0.0.1:1\n}\n"
	if _, err := ParseSites(content); err == nil {
		t.Fatal("expected error for two directives")
	}
}

func TestUpsertDNSAnnotation_Insert(t *testing.T) {
	content := "dsm.synology.com {\n\treverse_proxy 127.0.0.1:5003\n}\n"
	out, err := UpsertDNSAnnotation(content, "dsm.synology.com", DNSAnnotation{Name: "dsm", Type: "CNAME", Zone: "synology.com", Present: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sites, err := ParseSites(out)
	if err != nil {
		t.Fatalf("re-parse error: %v", err)
	}
	if d := sites[0].DNS; !d.Present || d.Name != "dsm" || d.Type != "CNAME" || d.Zone != "synology.com" {
		t.Fatalf("round-trip failed: %+v", d)
	}
}

func TestUpsertDNSAnnotation_ReplaceAndPreserveComment(t *testing.T) {
	content := "# keep me\n# dsm type=A\ndsm.synology.com {\n\treverse_proxy 127.0.0.1:5003\n}\n"
	out, err := UpsertDNSAnnotation(content, "dsm.synology.com", DNSAnnotation{Name: "dsm", Type: "CNAME", Zone: "synology.com", Present: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "# keep me") {
		t.Errorf("non-directive comment was lost:\n%s", out)
	}
	sites, _ := ParseSites(out)
	if sites[0].DNS.Type != "CNAME" || sites[0].DNS.Zone != "synology.com" {
		t.Fatalf("directive not replaced: %+v", sites[0].DNS)
	}
}
