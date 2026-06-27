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
