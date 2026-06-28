package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ResolveSecret turns a configured secret value into its literal form:
//   - an "op://vault/item/field" reference is read via the 1Password CLI (`op read`)
//   - a value containing "${VAR}" is expanded from the environment
//   - any other value is returned unchanged
//
// This lets the Technitium token be stored as a literal, an env reference, or a
// 1Password reference without a login step.
func ResolveSecret(ctx context.Context, s string) (string, error) {
	// Be forgiving of a value pasted with surrounding whitespace or quotes so a
	// token entered as "op://..." is still recognized as a reference, not a
	// literal that gets sent to the server verbatim.
	s = trimQuotes(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(s, "op://"):
		out, err := exec.CommandContext(ctx, "op", "read", "--no-newline", s).Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return "", fmt.Errorf("op read %s: %s", s, strings.TrimSpace(string(ee.Stderr)))
			}
			return "", fmt.Errorf("op read %s: %w", s, err)
		}
		v := strings.TrimSpace(string(out))
		if v == "" {
			return "", fmt.Errorf("op read %s returned empty value", s)
		}
		return v, nil
	case strings.Contains(s, "${"):
		return os.Expand(s, os.Getenv), nil
	default:
		return s, nil
	}
}

// trimQuotes removes one matching pair of surrounding single or double quotes.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if c := s[0]; (c == '"' || c == '\'') && s[len(s)-1] == c {
			return s[1 : len(s)-1]
		}
	}
	return s
}
