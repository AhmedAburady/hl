package ui

import (
	"strings"
	"testing"
)

func TestRenderRecordsHeadersMarksAndGrouping(t *testing.T) {
	rows := []RecordRow{
		{Zone: "example.com", Record: "grafana.example.com", Value: "1.2.3.4", Proxy: "10.0.0.1:8080", Local: MarkOK, DNS: MarkOK, Remote: MarkOK},
		{Zone: "example.com", Record: "wiki.example.com", Value: "1.2.3.4", Proxy: "", Local: MarkOK, DNS: MarkMissing, Remote: MarkUnknown},
		{Zone: "other.com", Record: "vault.other.com", Value: "5.6.7.8", Proxy: "10.0.0.2:3000", Local: MarkNA, DNS: MarkOK, Remote: MarkNA},
	}
	out := RenderRecords(rows)
	for _, want := range []string{
		"#", "RECORD", "VALUE", "ADDRESS", "L", "DNS", "RE",
		"example.com", "other.com",
		"grafana", "wiki", "vault",
		"10.0.0.1:8080", "1.2.3.4",
		"✓", "✗", "?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderRecords output missing %q\n%s", want, out)
		}
	}
	for _, absent := range []string{"grafana.example.com", "wiki.example.com", "vault.other.com"} {
		if strings.Contains(out, absent) {
			t.Errorf("table should show short names, not full FQDN %q\n%s", absent, out)
		}
	}
}

func TestShortName(t *testing.T) {
	cases := []struct {
		record, zone, want string
	}{
		{"affine.at3ch.com", "at3ch.com", "affine"},
		{"at3ch.com", "at3ch.com", "@"},
		{"a.b.at3ch.com", "at3ch.com", "a.b"},
		{"Affine.AT3CH.com", "at3ch.com", "Affine"},
		{"other.net", "at3ch.com", "other.net"},
		{"affine.at3ch.com", "", "affine.at3ch.com"},
	}
	for _, c := range cases {
		if got := shortName(c.record, c.zone); got != c.want {
			t.Errorf("shortName(%q, %q) = %q, want %q", c.record, c.zone, got, c.want)
		}
	}
}
