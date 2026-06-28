package config

import (
	"context"
	"testing"
)

func TestResolveSecret_Literal(t *testing.T) {
	got, err := ResolveSecret(context.Background(), "plain-token")
	if err != nil || got != "plain-token" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolveSecret_Env(t *testing.T) {
	t.Setenv("HL_TEST_TOKEN", "from-env")
	got, err := ResolveSecret(context.Background(), "${HL_TEST_TOKEN}")
	if err != nil || got != "from-env" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolveSecret_TrimsWhitespaceAndQuotes(t *testing.T) {
	t.Setenv("HL_TEST_TOKEN", "from-env")
	// A literal kept as-is once trimmed.
	if got, err := ResolveSecret(context.Background(), `  "plain-token"  `); err != nil || got != "plain-token" {
		t.Fatalf("quoted literal: got %q, %v", got, err)
	}
	// Quoted ${VAR} still expands.
	if got, err := ResolveSecret(context.Background(), `"${HL_TEST_TOKEN}"`); err != nil || got != "from-env" {
		t.Fatalf("quoted env: got %q, %v", got, err)
	}
}

func TestTrimQuotes(t *testing.T) {
	cases := map[string]string{
		`"op://a/b/c"`: "op://a/b/c",
		`'x'`:          "x",
		`plain`:        "plain",
		`"unbalanced`:  `"unbalanced`,
		`""`:           "",
	}
	for in, want := range cases {
		if got := trimQuotes(in); got != want {
			t.Errorf("trimQuotes(%q) = %q, want %q", in, got, want)
		}
	}
}
