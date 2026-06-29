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

func TestContentSHA256MatchesSha256sum(t *testing.T) {
	// Known vector: sha256sum of "hello\n" (the trailing newline is significant).
	if got := contentSHA256("hello\n"); got != "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03" {
		t.Errorf("contentSHA256 = %q, want sha256sum of \"hello\\n\"", got)
	}
	if contentSHA256("a") == contentSHA256("b") {
		t.Error("distinct content produced the same hash")
	}
}

func TestReadAndSHACmds(t *testing.T) {
	if got := readRemoteCmd("", "/etc/caddy/Caddyfile"); got != `cat '/etc/caddy/Caddyfile'` {
		t.Errorf("readRemoteCmd root = %q", got)
	}
	if got := readRemoteCmd("sudo ", "/etc/caddy/Caddyfile"); got != `sudo cat '/etc/caddy/Caddyfile'` {
		t.Errorf("readRemoteCmd sudo = %q", got)
	}
	if got := remoteSHACmd("sudo ", "/etc/caddy/Caddyfile"); got != `sudo sha256sum '/etc/caddy/Caddyfile' 2>/dev/null` {
		t.Errorf("remoteSHACmd = %q", got)
	}
}

func TestParseSHA(t *testing.T) {
	sum, ok := parseSHA("5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03  /etc/caddy/Caddyfile\n")
	if !ok || sum != "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03" {
		t.Fatalf("parseSHA = %q ok=%v, want the hash field", sum, ok)
	}
	if _, ok := parseSHA(""); ok {
		t.Error("parseSHA reported ok on empty output (missing file)")
	}
	if _, ok := parseSHA("   \n"); ok {
		t.Error("parseSHA reported ok on whitespace-only output")
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
