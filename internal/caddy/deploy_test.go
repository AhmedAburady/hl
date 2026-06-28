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

func TestValidateCmd(t *testing.T) {
	cases := []struct {
		name              string
		cmd, sudo, staged string
		want              string
	}{
		{
			name:   "appends config when no placeholder",
			cmd:    "caddy adapt --adapter caddyfile",
			staged: "/etc/caddy/Caddyfile.hldns.new",
			want:   `caddy adapt --adapter caddyfile --config '/etc/caddy/Caddyfile.hldns.new'`,
		},
		{
			name:   "substitutes file placeholder",
			cmd:    "caddy validate --config {file} --adapter caddyfile",
			staged: "/etc/caddy/Caddyfile.hldns.new",
			want:   `caddy validate --config '/etc/caddy/Caddyfile.hldns.new' --adapter caddyfile`,
		},
		{
			name:   "prefixes sudo for non-root",
			cmd:    "caddy adapt --adapter caddyfile",
			sudo:   "sudo ",
			staged: "/etc/caddy/Caddyfile.hldns.new",
			want:   `sudo caddy adapt --adapter caddyfile --config '/etc/caddy/Caddyfile.hldns.new'`,
		},
		{
			name:   "does not double-prefix sudo",
			cmd:    "sudo caddy adapt",
			sudo:   "sudo ",
			staged: "/x",
			want:   `sudo caddy adapt --config '/x'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateCmd(tc.cmd, tc.sudo, tc.staged)
			if err != nil {
				t.Fatalf("validateCmd() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("validateCmd() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateCmdRejectsOwnConfig(t *testing.T) {
	if _, err := validateCmd("caddy adapt --config /etc/caddy/Caddyfile", "", "/x"); err == nil {
		t.Fatal("expected error when validate_cmd sets its own --config without {file}")
	}
	// With the placeholder it is allowed even though --config is present.
	if _, err := validateCmd("caddy adapt --config {file}", "", "/x"); err != nil {
		t.Fatalf("placeholder form should be accepted: %v", err)
	}
}

func TestExtractRC(t *testing.T) {
	body, rc, ok := extractRC("line one\nline two\n" + validateRCMarker + "127\n")
	if !ok || rc != 127 {
		t.Fatalf("extractRC rc = %d ok = %v, want 127 true", rc, ok)
	}
	if body != "line one\nline two" {
		t.Errorf("extractRC body = %q, want stripped of marker", body)
	}
	if _, _, ok := extractRC("no marker here"); ok {
		t.Error("extractRC reported ok with no marker present")
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
