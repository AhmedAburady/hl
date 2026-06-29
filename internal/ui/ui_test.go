package ui

import (
	"strings"
	"testing"
)

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
