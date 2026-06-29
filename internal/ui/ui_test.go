package ui

import (
	"strings"
	"testing"
)

func TestRenderRecordsHeadersMarksAndGrouping(t *testing.T) {
	rows := []RecordRow{
		{Zone: "example.com", Record: "a.example.com", Value: "1.2.3.4", Proxy: "10.0.0.1:8080", Local: MarkOK, DNS: MarkOK, Remote: MarkOK},
		{Zone: "example.com", Record: "b.example.com", Value: "1.2.3.4", Proxy: "", Local: MarkOK, DNS: MarkMissing, Remote: MarkUnknown},
		{Zone: "other.com", Record: "c.other.com", Value: "5.6.7.8", Proxy: "10.0.0.2:3000", Local: MarkNA, DNS: MarkOK, Remote: MarkNA},
	}
	out := RenderRecords(rows)
	for _, want := range []string{
		"#", "RECORD", "VALUE", "ADDRESS", "L", "DNS", "RE",
		"example.com", "other.com",
		"a.example.com", "b.example.com", "c.other.com",
		"10.0.0.1:8080", "1.2.3.4",
		"✓", "✗", "?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderRecords output missing %q\n%s", want, out)
		}
	}
}
