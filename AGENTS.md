# AGENTS.md

This is the **hl** repository: a Go 1.26 CLI that adds Caddy `reverse_proxy`
blocks to a local Caddyfile, deploys them over SSH, and adds matching A/CNAME
records to a Technitium DNS zone. The invoked command is `hl`; the Go module is
`github.com/AhmedAburady/hl`.

Key facts:
- Build the runnable binary as `hl`: `go build -o hl .`. Compile-all: `go build ./...`.
- Verify before finishing: `go vet ./...` and `go test ./...`. Modernize with `go fix ./...` (safe, analysis-based).
- Entry point is `main.go` → `cmd.Root()` → `fang.Execute` (Charm Fang on top of Cobra). All commands are `newXxxCmd()` factories in `cmd/` registered in `cmd/root.go`.
- Domain logic is in `internal/`: `config` (Viper), `caddy` (Caddyfile parser + SSH/SFTP deploy), `technitium` (HTTP API client), `sshx` (SSH dial/run/push), `prompt` (Huh forms).
- Config lives at `~/.config/hl/config.yaml` (or `$XDG_CONFIG_HOME/hl/config.yaml` if set); env override prefix is `HLDNS_` (dots → underscores). Token is set by `hl dns login` and persisted via `config.SetToken`.

## Task

Most changes are either a new/changed subcommand in `cmd/` or a change to one
of the `internal/` packages. Prefer reusing the existing helpers
(`caddy.UpsertReverseProxy`, `caddy.Deploy`, `technitium.Client.AddRecord`,
`sshx.Run`/`sshx.PushFile`) over reimplementing them. Keep the CLI flags-driven
with Huh prompts only as a fallback for missing required values.

## Requirements

1. **Adding a subcommand.** Create a `newXxxCmd()` factory in the relevant
   `cmd/*.go` file, register it in the parent command's `AddCommand(...)`, and
   load config via `loadCfg()` (cached per process). Do not introduce a second
   config-loading path.

2. **Caddyfile editing stays idempotent.** All Caddyfile mutations go through
   `internal/caddy/caddyfile.go` (`UpsertReverseProxy` / `ListHosts`), which
   parses top-level site blocks by brace depth (ignoring braces inside quotes
   and `#` comments). If you change the block format or parser behavior, update
   `internal/caddy/caddyfile_test.go` to cover insert / update / idempotent /
   force / nested-block cases.

3. **DNS records are A and CNAME only.** `addDNSRecord` (in `cmd/add.go`)
   validates the type and `technitium.Client.AddRecord` sends `ipAddress` for A
   and `cname` for CNAME. Do not add other record types without extending both
   the validation and the client, plus a test in `internal/technitium/client_test.go`.

4. **Deploy is push-then-reload with rollback.** `caddy.Deploy` backs up the
   remote file, SFTP-pushes the local file, runs `reload_cmd`, and restores the
   backup on reload failure. Never edit the remote Caddyfile in place; the local
   file is the source of truth.

5. **Never log or print the Technitium token.** `config show` redacts it to
   `<set>`. Keep it that way.

6. **Use Go 1.26 idioms.** Prefer `errors.AsType[T]` over `errors.As`
   (already used for `*fs.PathError` and `*knownhosts.KeyError`), context-aware
   dialing via `net.Dialer.DialContext`, `log/slog` for diagnostics, and
   `strings.Cut`. Run `go fix ./...` before finishing.

7. **Verify before finishing.** Run `go build ./...`, `go vet ./...`, and
   `go test ./...` after any code change. Fix all failures.

## Constraints

- Do not change the SSH host-key policy: unknown hosts are TOFU-accepted with a
  `slog.Warn`, but a known-hosts **mismatch must be rejected**. Do not switch to
  `ssh.InsecureIgnoreHostKey`.
- Do not rename the invoked command away from `hl` or the module path
  `github.com/AhmedAburady/hl` unless the user explicitly asks.
- Do not add Bubble Tea / full-TUI behavior; this tool is command-driven. Huh
  prompts are only for missing required inputs.
- Do not widen scope to the Caddy Admin API; this tool edits a local Caddyfile
  and deploys over SSH by design.

## Examples

### Add a new subcommand, e.g. `hl dns delete`

Given: "add a command to delete a DNS record"
Expected approach:
1. Add `DeleteRecord(ctx, zone, domain, rtype, value)` to `internal/technitium/client.go`, calling `GET /api/zones/records/delete` and checking the `status=="ok"` envelope like `AddRecord` does.
2. Add a test in `internal/technitium/client_test.go` using `httptest.NewServer` that asserts the path and query params and handles the envelope.
3. Add `newDNSDeleteCmd()` in `cmd/dns.go` and register it in `newDNSCmd()`'s `AddCommand(...)`. Reuse `loadCfg()` and `technitium.New`.
4. Run `go build ./...`, `go vet ./...`, `go test ./...`. Fix all failures.

## Reference

- `PLAN.md` — the original design plan and per-component behavior.
- `README.md` — human-facing install and usage docs.
- Config schema and env vars: see `internal/config/config.go` (`setDefaults`,
  `Remote`/`Caddy`/`Technitium` structs) — the canonical source for field names.
- Technitium API: `internal/technitium/client.go` (AddRecord → `/api/zones/records/add`,
  CreateToken → `/api/user/createToken`, ListRecords → `/api/zones/records/get`).
- SSH/SFTP: `internal/sshx/sshx.go` (`Run`, `PushFile`, `dial`).
