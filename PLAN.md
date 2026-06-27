# hl — Implementation Plan

A Go 1.26 CLI for a homelab where the **local Caddyfile is the single source of
truth** for both reverse proxies and DNS. Each site block declares its DNS intent
in a `# <name> key=value` comment directly above it. `hl sync`:

1. Pushes the Caddyfile to the Caddy host over SSH and reloads Caddy.
2. Reconciles a Technitium DNS zone (via its HTTP API) to match the annotations —
   creating, updating, and pruning the **A**/**CNAME** records it manages.

DNS reconcile is ownership-scoped via a per-record `managed_tag` comment: only
records `hl` created are ever updated or deleted; hand-made records are untouched.

---

## 1. Stack

| Concern            | Choice                                              |
| ------------------ | --------------------------------------------------- |
| Command framework  | `github.com/spf13/cobra`                            |
| CLI polish         | `github.com/charmbracelet/fang` (help/errors/version/completions) |
| Config             | `github.com/spf13/viper`                            |
| Interactive prompts| `github.com/charmbracelet/huh`                      |
| SSH transport      | `golang.org/x/crypto/ssh` (+ `agent`, `knownhosts`) |
| File push          | `github.com/pkg/sftp`                               |
| HTTP (Technitium)  | stdlib `net/http`                                   |

Module: `github.com/AhmedAburady/hl`, `go 1.26.4`. Binary: `hl` (invoked as `hl`; built with `go build -o hl .`).

---

## 2. Go 1.26 patterns to adopt (BLOCKER requirement)

Researched against the official 1.26 release notes. Patterns applied throughout:

- **`errors.AsType[T](err)`** — replaces `errors.As` everywhere (type-safe, no
  reflection, scoped vars). Used for `*fs.PathError`, `*knownhosts.KeyError`,
  HTTP/API error typing.
- **`new(expr)`** — for optional pointer values where needed (e.g. building
  optional request fields) instead of helper funcs.
- **`context.Context` plumbed end-to-end** via `cmd.Context()` →
  `http.NewRequestWithContext` and context-aware SSH dialing
  (`net.Dialer.DialContext`).
- **`log/slog`** for diagnostics (host-key TOFU warnings, deploy steps).
- **`cmp.Or`** for defaulting flag/config fallbacks.
- Run **`go vet ./...`** and **`go fix ./...`** (modernized, analysis-based) at
  the end to auto-adopt newer idioms (`slices.Contains`, `omitzero`, etc.).

---

## 3. Project layout

```
caddy/                      module root
  go.mod
  PLAN.md
  main.go                   fang.Execute(ctx, cmd.Root())
  cmd/
    root.go                 root cmd, --config, version, wiring
    add.go                  scaffold an annotated block, then sync
    sync.go                 sync (deploy + DNS reconcile); shared runSync/reconcileDNS
    status.go               read-only: hosts + pending DNS plan
    dns.go                  dns list | login
    config.go               config init | show
  internal/
    config/config.go        viper struct + load/save/init
    caddy/caddyfile.go      parse blocks + upsert reverse_proxy; ParseSites read model
    caddy/annotation.go     DNS directive parse/format (# <name> key=value)
    caddy/deploy.go         sftp push local file -> remote + backup + reload
    technitium/client.go    createToken, addRecord, deleteRecord, listRecords (A/CNAME)
    reconcile/reconcile.go  desired-state derivation + diff (BuildPlan) + apply
    sshx/sshx.go            ctx-aware ssh dial (key/agent), run cmd, sftp push
    prompt/prompt.go        huh forms for missing args / login
```

---

## 4. Configuration

Path: `~/.config/hl/config.yaml` (or `$XDG_CONFIG_HOME/hl/config.yaml` if set; override via `--config` or `HLDNS_*` env).
Env binding: `HLDNS_TECHNITIUM_TOKEN`, `HLDNS_CADDY_REMOTE_HOST`, etc.

```yaml
technitium:
  url: http://dns.home:5380
  token: ""              # set by `dns login`
  default_zone: home.lab
caddy:
  local_file: /Users/ahmabora1/HomeLab/caddy/Caddyfile   # source of truth
  target_scheme: http                                    # default upstream scheme
  cname_target: caddy.home.lab.                          # default CNAME value
  a_value: 192.168.1.10                                  # default A record IP
  managed_tag: managed-by:hl                             # DNS ownership tag
  remote:
    host: caddy.home
    user: root
    port: 22
    key: ~/.ssh/id_ed25519        # empty => try ssh-agent
    remote_path: /etc/caddy/Caddyfile
    reload_cmd: "caddy reload --config /etc/caddy/Caddyfile"
```

---

## 5. Components

### 5.1 `internal/config`
- `Load(path)` — viper + defaults + env; missing file ignored via
  `errors.AsType[*fs.PathError]`.
- `SetToken(token)` — persist token (`WriteConfigAs`).
- `Init(path)` — `SafeWriteConfigAs` default file (fails if exists).

### 5.2 `internal/sshx`
- `Target{Host,User,Port,KeyPath}`.
- `dial(ctx, t)` — `net.Dialer.DialContext` → `ssh.NewClientConn`.
  Auth: private key if present, plus ssh-agent (`SSH_AUTH_SOCK`).
  Host keys: verify against `~/.ssh/known_hosts`; unknown host =>
  TOFU accept with `slog.Warn`; **mismatch => reject** (detected via
  `errors.AsType[*knownhosts.KeyError]` + non-empty `Want`).
- `Run(ctx, t, cmd) (string, error)` — run remote command, capture output.
- `PushFile(ctx, t, local, remote)` — sftp upload.

### 5.3 `internal/technitium`
- `New(baseURL, token)`.
- `CreateToken(ctx, user, pass, totp, name) (token, error)` —
  `GET /api/user/createToken`.
- `AddRecord(ctx, AddRecordRequest)` —
  `/api/zones/records/add?domain=&zone=&type=A&ipAddress=` or
  `type=CNAME&cname=` plus `ttl`, `overwrite`, `comments`.
- `DeleteRecord(ctx, DeleteRecordRequest)` —
  `/api/zones/records/delete?domain=&zone=&type=&value=` (+`ipAddress`/`cname`).
- `ListRecords(ctx, zone, domain)` — `/api/zones/records/get?listZone=true`;
  `Record` captures `comments` (the ownership tag) and exposes `Value()`.
- Response envelope `{status, errorMessage}`; non-`ok` => typed error.

### 5.4 `internal/caddy`
- `UpsertReverseProxy(content, host, upstream string, force bool)` — brace-depth
  parser, in-place idempotent edit of the simple `reverse_proxy` line (force
  replaces block-form); appends a canonical block when absent.
- `UpsertDNSAnnotation(content, host, DNSAnnotation)` — insert/replace the
  directive line in the comment group above the block, preserving other comments.
- `ParseSites(content) ([]Site, error)` — read model (`Host`, `Upstream`, `DNS`)
  consumed by sync/status; invalid directive => error.
- `annotation.go` — `parseDirectiveLine`/`parseAnnotation` (detection: first token
  bare + ≥1 recognized `key=value`; unknown key/bad ttl => error) and
  `formatDNSAnnotation`.
- `WriteLocalFile(path, content)` — timestamped `.bak` then write.
- `Deploy(ctx, cfg.Caddy)` — backup remote, sftp push, run `reload_cmd`;
  on failure restore backup and return remote output.

### 5.5 `internal/reconcile`
- `DeriveDesired(sites, cfg)` / `Resolve(ann, cfg)` — annotation → `Desired`
  (FQDN, zone, type, value, ttl), applying config defaults and type inference
  (explicit > value-IPv4 > config-default).
- `BuildPlan(desired, actual, tag)` — diff vs. tagged actual records (by
  name+type), classifying create / update (value or ttl drift) / delete (tagged
  orphan). CNAME trailing dot normalized.
- `Plan.Apply(ctx, client, tag)` — create/update via overwriting `AddRecord`
  tagged with `tag`; delete via `DeleteRecord`. `Plan.String()` renders the diff.

### 5.6 `internal/prompt`
- `huh` forms: missing `host`/`target` for `add`; `user`/`pass`/`totp` for `dns login`.

---

## 6. Commands

- **`sync`** — read local Caddyfile → (unless `--no-deploy`) push + reload →
  (unless `--no-dns`) reconcile DNS via `reconcile.BuildPlan`/`Apply` over the
  desired set + the default zone. Flags: `--dry-run`, `--no-deploy`, `--no-dns`,
  `--no-prune`. Shared helpers `runSync`/`reconcileDNS` live here.
- **`status`** — read-only: list hosts from the local file + print the pending
  DNS plan (`sync --dry-run` that never deploys). Flag: `--no-prune`.
- **`add <host> <target>`** — authoring convenience: `UpsertReverseProxy` +
  (unless `--no-dns`) `UpsertDNSAnnotation` writing resolved `type`/`zone`, then
  the `sync` flow. Flags: `--scheme`, `--zone`, `--ttl`, `--dns-type`,
  `--dns-value`, `--no-dns`, `--no-deploy`, `--no-prune`, `--no-sync`, `--force`,
  `--dry-run`. Missing positional args => huh prompt.
- **`dns list`** — list records in `--zone`.
- **`dns login`** — `createToken`, save non-expiring token to config (`--user`,`--pass`,`--totp`,`--token-name` or prompt; `--totp` only if 2FA enabled).
- **`config init|show`** — scaffold / print effective config (token redacted).

---

## 7. Safety

- Local file: timestamped backup before every write.
- Remote: backup before push; auto-restore on reload failure.
- SSH host-key mismatch rejected; unknown host TOFU-accepted with warning.
- Token never printed (`config show` redacts).

---

## 8. Verification

- `go build ./...`, `go vet ./...`, `go fix -diff ./...` review.
- Unit tests:
  - `caddyfile_test.go` — insert / update / idempotency / multi-host / force / conflict; `ParseSites`.
  - `annotation_test.go` — directive detection, prose-comment skip, unknown-key/multi-directive errors, upsert round-trip.
  - `reconcile_test.go` — `DeriveDesired` defaults/overrides/zone/type inference; `BuildPlan` create/update/delete + tag filtering + CNAME dot equivalence.
  - `client_test.go` — URL/query building, envelope handling, `DeleteRecord`, comments parsing, via `httptest`.
- Manual smoke: `config init`, `dns login`, annotate a block, `hl status` then
  `hl sync` against the real Technitium + Caddy host. Confirm a removed block's
  managed record is pruned while a hand-made record in the zone is untouched.
