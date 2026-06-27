# AGENTS.md

This is the **hl** repository: a Go 1.26 CLI that treats a local Caddyfile as the
single source of truth for Caddy reverse proxies **and** DNS. Each site block
declares its DNS intent in a `# <name> key=value` comment directly above it;
`hl sync` deploys the Caddyfile over SSH and reconciles a Technitium zone (create/
update/delete) to match. The invoked command is `hl`; the Go module is
`github.com/AhmedAburady/hl`.

Key facts:
- Build the runnable binary as `hl`: `go build -o hl .`. Compile-all: `go build ./...`.
- Verify before finishing: `go vet ./...` and `go test ./...`. Modernize with `go fix ./...` (safe, analysis-based).
- Entry point is `main.go` → `cmd.Root()` → `fang.Execute` (Charm Fang on top of Cobra). All commands are `newXxxCmd()` factories in `cmd/` registered in `cmd/root.go`.
- Domain logic is in `internal/`: `config` (Viper), `caddy` (Caddyfile parser, DNS annotations, SSH/SFTP deploy), `technitium` (HTTP API client), `reconcile` (desired-state derivation + diff/apply — the heart of sync), `sshx` (SSH dial/run/push), `prompt` (Huh forms).
- DNS reconcile is ownership-scoped: records `hl` creates carry the `caddy.managed_tag` comment (default `managed-by:hl`); only tagged records are ever updated or pruned. Never widen this to untagged records.
- Config lives at `~/.config/hl/config.yaml` (or `$XDG_CONFIG_HOME/hl/config.yaml` if set); env override prefix is `HLDNS_` (dots → underscores). Token is set by `hl dns login` (which passes an optional `--totp` for 2FA accounts and mints a non-expiring token) and persisted via `config.SetToken`.

## Task

Most changes are either a new/changed subcommand in `cmd/` or a change to one
of the `internal/` packages. Prefer reusing the existing helpers
(`caddy.UpsertReverseProxy`, `caddy.UpsertDNSAnnotation`, `caddy.ParseSites`,
`caddy.Deploy`, `reconcile.DeriveDesired`/`BuildPlan`/`Apply`,
`technitium.Client.AddRecord`/`DeleteRecord`, the shared `runSync`/`reconcileDNS`
in `cmd/sync.go`, `sshx.Run`/`sshx.PushFile`) over reimplementing them. Keep the
CLI flags-driven with Huh prompts only as a fallback for missing required values.

## Requirements

1. **Adding a subcommand.** Create a `newXxxCmd()` factory in the relevant
   `cmd/*.go` file, register it in the parent command's `AddCommand(...)`, and
   load config via `loadCfg()` (cached per process). Do not introduce a second
   config-loading path.

2. **Caddyfile editing stays idempotent.** All Caddyfile mutations go through
   `internal/caddy` (`UpsertReverseProxy`, `UpsertDNSAnnotation`); reads go
   through `ParseSites`. The parser finds top-level site blocks by brace depth
   (ignoring braces inside quotes and `#` comments) and detects a DNS directive
   as the comment directly above a block whose first token is a bare word and
   which contains ≥1 `key=value`. If you change the block/annotation format,
   update `caddyfile_test.go` / `annotation_test.go` to cover insert / update /
   idempotent / force / nested-block / prose-comment / round-trip cases.

3. **DNS records are A and CNAME only.** Type resolution lives in
   `reconcile.Resolve`; `technitium.Client.AddRecord`/`DeleteRecord` send
   `ipAddress` for A and `cname` for CNAME. Do not add other record types without
   extending the reconciler, the client, and the tests in
   `internal/technitium/client_test.go` + `internal/reconcile/reconcile_test.go`.

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

### Add a new annotation attribute, e.g. `proxy=false`

Given: "let a block opt out of the reverse-proxy upstream check"
Expected approach:
1. Add the key to `annotationKeys` and the parse switch in `internal/caddy/annotation.go`, plus a field on `DNSAnnotation`, and emit it in `formatDNSAnnotation`.
2. Cover detection + round-trip in `internal/caddy/annotation_test.go`.
3. Thread it into `reconcile.Resolve`/`Desired` if it affects the record, with a case in `internal/reconcile/reconcile_test.go`.
4. Surface a matching flag on `hl add` if it should be authorable from the CLI.
5. Run `go build ./...`, `go vet ./...`, `go test ./...`. Fix all failures.

## Reference

- `PLAN.md` — the original design plan and per-component behavior.
- `README.md` — human-facing install and usage docs, including the annotation grammar.
- Config schema and env vars: see `internal/config/config.go` (`setDefaults`,
  `Remote`/`Caddy`/`Technitium` structs) — the canonical source for field names.
- Technitium API: `internal/technitium/client.go` (AddRecord → `/api/zones/records/add`,
  DeleteRecord → `/api/zones/records/delete`, CreateToken → `/api/user/createToken`,
  ListRecords → `/api/zones/records/get`).
- DNS reconcile: `internal/reconcile/reconcile.go` (`DeriveDesired`, `Resolve`,
  `BuildPlan`, `Plan.Apply`); CLI glue in `cmd/sync.go` (`runSync`, `reconcileDNS`).
- SSH/SFTP: `internal/sshx/sshx.go` (`Run`, `PushFile`, `dial`).
