package ui

import (
	"strings"
	"testing"

	"github.com/AhmedAburady/hl/internal/technitium"
)

func TestRenderRecordsNumberedAndLocal(t *testing.T) {
	records := []technitium.Record{
		{Name: "declared.example.com", Type: "A", TTL: 300, RData: map[string]any{"ipAddress": "1.2.3.4"}, Zone: "example.com"},
		{Name: "orphan.example.com", Type: "A", TTL: 300, RData: map[string]any{"ipAddress": "5.6.7.8"}, Zone: "example.com"},
	}
	local := map[string]bool{"declared.example.com": true}

	out := RenderRecords(records, false, "managed-by:hl", local)

	for _, want := range []string{"#", "NAME", "L", "declared.example.com", "orphan.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderRecords output missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Errorf("expected numbered rows in output:\n%s", out)
	}
}

func TestRenderStatusHeadersAndMarks(t *testing.T) {
	rows := []StatusRow{
		{Host: "a.example.com", Local: MarkOK, DNS: MarkOK, Caddy: MarkOK},
		{Host: "b.example.com", Local: MarkOK, DNS: MarkMissing, Caddy: MarkUnknown},
	}
	out := RenderStatus(rows)
	for _, want := range []string{"#", "HOST", "L", "DNS", "CA", "✓", "✗", "?", "a.example.com", "b.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderStatus output missing %q\n%s", want, out)
		}
	}
}
