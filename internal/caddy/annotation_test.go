package caddy

import "testing"

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

