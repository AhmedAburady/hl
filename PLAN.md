# homelab-dns — Implementation Plan

A Go 1.26 CLI for a homelab that, in one command:

1. Adds/updates a `reverse_proxy` site block in a **local** Caddyfile (source of
   truth, hand-editable), pushes it to the Caddy host over SSH, and reloads Caddy.
2. Adds a matching **A** or **CNAME** record to a Technitium DNS zone via its HTTP API.

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

Module: `github.com/AhmedAburady/homelab-dns`, `go 1.26.4`. Binary: `homelab-dns`.

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
    add.go                  flagship: caddy block + push/reload + dns record
    caddy.go                caddy add | sync | list
    dns.go                  dns add | list | login
    config.go               config init | show
  internal/
    config/config.go        viper struct + load/save/init
    caddy/caddyfile.go      parse + upsert reverse_proxy block (idempotent)
    caddy/deploy.go         sftp push local file -> remote + backup + reload
    technitium/client.go    createToken, addRecord, listRecords (A/CNAME)
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
- `CreateToken(ctx, user, pass, name) (token, error)` —
  `GET /api/user/createToken`.
- `AddRecord(ctx, AddRecordRequest)` —
  `/api/zones/records/add?domain=&zone=&type=A&ipAddress=` or
  `type=CNAME&cname=` plus `ttl`, `overwrite`, `comments`.
- `ListRecords(ctx, zone, domain)` — `/api/zones/records/get?listZone=true`.
- Response envelope `{status, errorMessage}`; non-`ok` => typed error.

### 5.4 `internal/caddy`
- `UpsertReverseProxy(content, host, upstream string, force bool) (out string, changed bool, err error)`
  - Brace-depth parser finds top-level site blocks + their address labels.
  - Match block whose address equals `host` (scheme/port-normalized).
  - **Found**: update the single simple `reverse_proxy` line in place
    (preserve other directives); insert one if absent. Multiple/block-form
    `reverse_proxy` => error unless `--force` (force replaces whole block).
  - **Not found**: append a canonical block.
- `ListHosts(content) []string` — for `caddy list`.
- `WriteLocalFile(path, content)` — timestamped `.bak` then write.
- `Deploy(ctx, cfg.Caddy)` — backup remote (`cp <path> <path>.hldns.bak`),
  sftp push, run `reload_cmd`; on failure restore backup and return remote output.

### 5.5 `internal/prompt`
- `huh` forms: missing `host`/`target` for `add`; `user`/`pass` for `dns login`.

---

## 6. Commands

- **`add <host> <target>`** (flagship) — e.g. `add app.home.lab 192.168.1.50:8080`
  - upstream = `--scheme`/config scheme + target (unless target already has scheme).
  - upsert local Caddyfile → (unless `--no-deploy`) push + reload →
    (unless `--no-dns`) add DNS record.
  - DNS: `--dns-type A|CNAME` (default CNAME), `--dns-value`
    (default `cname_target` / `a_value`), `--zone` (default `default_zone`),
    `--ttl`. Flags: `--scheme`, `--no-dns`, `--no-deploy`, `--force`.
  - Missing positional args => huh prompt.
- **`caddy add <host> <target>`** — Caddyfile edit + push + reload only.
- **`caddy sync`** — push current local file + reload.
- **`caddy list`** — list configured hosts from local Caddyfile.
- **`dns add <domain>`** — add one A/CNAME record (`--type`,`--value`,`--zone`,`--ttl`,`--overwrite`).
- **`dns list`** — list records in `--zone`.
- **`dns login`** — `createToken`, save to config (`--user`,`--pass` or prompt).
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
  - `caddyfile_test.go` — insert / update / idempotency / multi-host / force / conflict.
  - `technitium_test.go` — URL/query building + envelope handling via `httptest`.
- Manual smoke: `config init`, `dns login`, `add app.home.lab 192.168.1.50:8080`
  against the real Technitium + Caddy host.
