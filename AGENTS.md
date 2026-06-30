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
- Domain logic is in `internal/`: `config` (native YAML via `go.yaml.in/yaml/v3`, defaults + `HLDNS_*` env overrides, `ResolveSecret` for token), `caddy` (Caddyfile parser, DNS annotations, SSH deploy via base64|tee+sudo), `technitium` (HTTP API client), `reconcile` (desired-state derivation + diff/apply — the heart of sync), `sshx` (runs remote commands by shelling out to the system `ssh` binary).
- DNS reconcile is ownership-scoped: records `hl` creates carry the `caddy.managed_tag` comment (default `managed-by:hl`); only tagged records are ever updated or pruned. The one exception is `hl sync --adopt`, which overwrites an existing **same-type** untagged record to take ownership of it (a single atomic `AddRecord`); it never deletes an untagged record of a different type. Without `--adopt`, an annotation that collides with an untagged record is reported as a conflict and skipped (and its name is protected from pruning). Do not widen untagged-record mutation beyond this.
- Config lives at `~/.config/hl/config.yaml` (or `$XDG_CONFIG_HOME/hl/config.yaml` if set); env override prefix is `HLDNS_` (dots → underscores). The Technitium API token is created once in the web UI and stored in `technitium.token`; `config.ResolveSecret` resolves it at use time (literal, `${ENV}`, or an `op://` 1Password reference via `op read`). There is no login command.

## Task

Most changes are either a new/changed subcommand in `cmd/` or a change to one
of the `internal/` packages. Prefer reusing the existing helpers
(`caddy.ParseSites`,
`caddy.Deploy`, `reconcile.DeriveDesired`/`BuildPlan`/`Apply`,
`technitium.Client.AddRecord`/`DeleteRecord`, the shared `runSync`/`reconcileDNS`
in `cmd/sync.go`, `sshx.Run`) over reimplementing them. The CLI is
flags-driven and non-interactive, except `hl config init`, which runs an onboarding
wizard over stdin when attached to a TTY (`term.IsTerminal`) and writes a complete
template otherwise.

## Requirements

1. **Adding a subcommand.** Create a `newXxxCmd()` factory in the relevant
   `cmd/*.go` file, register it in the parent command's `AddCommand(...)`, and
   load config via `loadCfg()` (cached per process). Do not introduce a second
   config-loading path.

2. **Caddyfiles are read-only to hl.** The CLI never edits a Caddyfile in
   place; the local file is the source of truth, authored by the user, and
   deployed whole. All reads go through `caddy.ParseSites`. The parser finds
   top-level site blocks by brace depth (ignoring braces inside quotes and `#`
   comments) and detects a DNS directive as the comment directly above a block
   whose first token is a bare word and which contains ≥1 `key=value`. If you
   change the block/annotation format, update `caddyfile_test.go` /
   `annotation_test.go` to cover the parse cases (hosts/upstreams, upstream
   forms, nested-block, with-directive, prose-comment, unknown-key,
   multiple-directives). Do not reintroduce a Caddyfile write/mutation path
   unless a command actually needs one.

3. **DNS records are A and CNAME only.** Type resolution lives in
   `reconcile.Resolve`; `technitium.Client.AddRecord`/`DeleteRecord` send
   `ipAddress` for A and `cname` for CNAME. Do not add other record types without
   extending the reconciler, the client, and the tests in
   `internal/technitium/client_test.go` + `internal/reconcile/reconcile_test.go`.

4. **Deploy is write-then-reload with rollback.** `caddy.Deploy` backs up the
   remote file, writes the local file over SSH (base64 piped to `tee`, prefixed
   with `sudo` when the SSH user is not root — no SFTP, so privileged paths like
   /etc/caddy work; the host needs passwordless sudo), runs `reload_cmd`
   (default `systemctl restart caddy` — restart, not `caddy reload`, so a stopped
   Caddy is brought back up; reload only works against a running admin API). A
   failed reload restores the backup *and* re-runs `reload_cmd`; a failed or
   interrupted **write** also restores the backup (`restoreBackup` runs on a
   `context.WithoutCancel` context so a Ctrl-C still rolls back). Never edit the
   remote Caddyfile in place; the local file is the source of truth.

5. **Never log or print the Technitium token.** `config show` redacts it to
   `<set>`. Keep it that way.

6. **Use Go 1.26 idioms.** Prefer `errors.AsType[T]` over `errors.As`
   (used for `*fs.PathError` and `*exec.ExitError`) and `strings.Cut`.
   Run `go fix ./...` before finishing.

7. **Verify before finishing.** Run `go build ./...`, `go vet ./...`, and
   `go test ./...` after any code change. Fix all failures.

## Constraints

- SSH runs by shelling out to the system `ssh` binary (`internal/sshx`), the
  sanctioned pattern (it matches the user's other apps and lets a 1Password agent
  authorize once); do not reintroduce `golang.org/x/crypto/ssh`. Keep
  `StrictHostKeyChecking=accept-new` (TOFU: pin an unknown host on first use,
  reject a **changed** key) — do not weaken it (`no`/`accept`) or disable host-key
  checking, and do not have the app write `known_hosts` itself.
- Do not rename the invoked command away from `hl` or the module path
  `github.com/AhmedAburady/hl` unless the user explicitly asks.
- Do not add Bubble Tea / full-TUI behavior; the tool is flags-driven. The only
  interactive surfaces are `hl config init`'s stdin onboarding wizard and the
  transient progress spinner (`internal/ui.WithSpinner`, `charm.land/huh/v2/spinner`),
  both TTY-gated. The spinner is a deliberate exception, not a license for a TUI.
- Do not widen scope to the Caddy Admin API; this tool edits a local Caddyfile
  and deploys over SSH by design.

## Examples

### Add a new annotation attribute, e.g. `proxy=false`

Given: "let a block opt out of the reverse-proxy upstream check"
Expected approach:
1. Add the key to `annotationKeys` and the parse switch in `internal/caddy/annotation.go`, plus a field on `DNSAnnotation`.
2. Cover detection in `internal/caddy/annotation_test.go`.
3. Thread it into `reconcile.Resolve`/`Desired` if it affects the record, with a case in `internal/reconcile/reconcile_test.go`.
4. Run `go build ./...`, `go vet ./...`, `go test ./...`. Fix all failures.

## Reference

- `README.md` — human-facing install and usage docs, including the annotation grammar.
- Config schema and env vars: see `internal/config/config.go` (`defaultConfig`,
  `applyEnv`, `Remote`/`Caddy`/`Technitium` structs) — the canonical source for
  field names and the `HLDNS_*` override names.
- Technitium API: `internal/technitium/client.go` (AddRecord → `/api/zones/records/add`,
  DeleteRecord → `/api/zones/records/delete`, ListRecords → `/api/zones/records/get`,
  ListZones → `/api/zones/list`). Auth is a UI-created API token (Bearer); no login.
- DNS reconcile: `internal/reconcile/reconcile.go` (`DeriveDesired`, `Resolve`,
  `BuildPlan`, `Plan.Apply`); CLI glue in `cmd/sync.go` (`runSync`, `reconcileDNS`).
- SSH: `internal/sshx/sshx.go` (`Run` shells out to the system `ssh` binary with key/agent auth and `StrictHostKeyChecking=accept-new`); deploy writes files via `caddy.Deploy` (base64|tee+sudo).
