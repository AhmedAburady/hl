package caddy

import "testing"

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	cases := map[string]string{
		"/etc/caddy/Caddyfile": `'/etc/caddy/Caddyfile'`,
		"a'b":                  `'a'\''b'`,
		"'; rm -rf / #":        `''\''; rm -rf / #'`,
		"":                     `''`,
		"with space":           `'with space'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExpandTilde(t *testing.T) {
	if got := expandTilde("/abs/path"); got != "/abs/path" {
		t.Errorf("absolute path changed: %q", got)
	}
	if got := expandTilde("relative"); got != "relative" {
		t.Errorf("relative path changed: %q", got)
	}
	got := expandTilde("~/x")
	if got == "~/x" || got == "" {
		t.Errorf("tilde not expanded: %q", got)
	}
}
