package caddy

import "os"

// LocalFileExists reports whether the local Caddyfile at path (with ~ expanded)
// already exists.
func LocalFileExists(path string) bool {
	_, err := os.Stat(expandTilde(path))
	return err == nil
}

// ResolvePath returns path with a leading ~ expanded to the home directory, for
// displaying the concrete location a path resolves to.
func ResolvePath(path string) string {
	return expandTilde(path)
}

// StarterCaddyfile is the commented template written by `hl config init` when no
// Caddyfile exists yet. It documents the DNS annotation format and shows a
// commented example so the file is valid (no active site blocks) until the user
// adds real ones.
func StarterCaddyfile() string {
	return `# hl Caddyfile — the single source of truth for Caddy and DNS.
#
# Each top-level block is a Caddy site (a reverse proxy). To also manage a DNS
# record for it, put an annotation on the comment line directly above the block:
#
#   # <name> zone=<zone> value=<target> [type=A|CNAME] [ttl=<seconds>]
#
#     name   record short name (leftmost label), e.g. dsm        (required)
#     zone   authoritative zone, e.g. example.com                (required)
#     value  A-record IP or CNAME target                         (required)
#     type   optional; inferred from value (IPv4 => A, else CNAME)
#     ttl    optional; seconds (omit to use the server default)
#
# A block without an annotation is deployed to Caddy but left out of DNS.
# Preview with 'hl status'; deploy + reconcile DNS with 'hl sync'.
#
# Example — uncomment and edit, or add your own:
#
# # dsm zone=example.com value=192.168.1.10
# dsm.example.com {
#     reverse_proxy 127.0.0.1:5000
# }
`
}
