# hl — homelab Caddy + Technitium DNS CLI

`hl` is a small Go CLI that treats your **local Caddyfile as the single source of
truth** for both reverse proxies and DNS. Each site block declares its DNS intent
in a comment directly above it, and one command keeps everything in sync:

```
# dsm type=CNAME zone=synology.com
dsm.synology.com {
	reverse_proxy 127.0.0.1:5003
}
```

`hl sync` then:

1. Pushes the Caddyfile to your Caddy host over SSH and reloads Caddy.
2. Reconciles Technitium DNS so the zone holds exactly the records declared by the
   annotations — creating, updating, and **pruning** records as the file changes.

Because the file is the source of truth, hand-edit it freely and re-run `hl sync`;
`hl status` previews the plan without changing anything. Only records `hl` created
(tagged via Technitium's per-record comment) are ever modified or deleted — your
hand-made records are never touched.

## Requirements

- Go 1.26 or newer
- SSH access to the host running Caddy (key-based or ssh-agent)
- A [Technitium DNS Server](https://technitium.com/dns/) with its HTTP API enabled

## Install

The command is invoked as `hl`.

**Install with Go** (puts `hl` on your `$GOPATH/bin`):

```sh
go install github.com/AhmedAburady/hl@latest
```

**Or build from source:**

```sh
git clone https://github.com/AhmedAburady/hl.git
cd hl
go build -o hl .
sudo mv hl /usr/local/bin/   # or anywhere on your PATH
```

Verify:

```sh
hl --version
hl --help
```

## First-time setup

```sh
# 1. Create the config file
hl config init

# 2. Edit it with your details (path is printed by the command above)
#    Lives at ~/.config/hl/config.yaml on both macOS and Linux
#    (or $XDG_CONFIG_HOME/hl/config.yaml if XDG_CONFIG_HOME is set)

# 3. Create and save a Technitium API token
hl dns login
#    Prompts for your Technitium admin user + password (and a 2FA code if
#    your account has 2FA enabled), then saves a non-expiring token.
#    Non-interactive: hl dns login --user admin --pass 'secret' --totp 123456

# 4. Check the effective config (token is redacted)
hl config show
```

### Config file reference

```yaml
technitium:
  url: http://dns.home:5380       # Technitium web API base URL
  token: ""                       # set automatically by `hl dns login`
  default_zone: home.lab          # zone used when --zone is omitted

caddy:
  local_file: /Users/you/HomeLab/caddy/Caddyfile   # source of truth
  target_scheme: http             # default upstream scheme when target has none
  cname_target: caddy.home.lab.   # default value for CNAME records
  a_value: 192.168.1.10           # default value for A records
  managed_tag: managed-by:hl      # ownership tag written to records hl manages
  remote:
    host: caddy.home              # SSH host for the Caddy server
    user: root                    # SSH user (defaults to root)
    port: 22
    key: ~/.ssh/id_ed25519        # private key; leave empty to use ssh-agent
    remote_path: /etc/caddy/Caddyfile
    reload_cmd: "caddy reload --config /etc/caddy/Caddyfile"
```

Any value can be overridden with an environment variable prefixed `HLDNS_`, with
dots replaced by underscores. Examples:

| Env var | Overrides |
| --- | --- |
| `HLDNS_TECHNITIUM_TOKEN` | `technitium.token` |
| `HLDNS_TECHNITIUM_URL` | `technitium.url` |
| `HLDNS_CADDY_REMOTE_HOST` | `caddy.remote.host` |
| `HLDNS_CADDY_LOCAL_FILE` | `caddy.local_file` |

## DNS annotations

A site block is managed for DNS by a directive comment in the group **directly
above** it. The first token is the record's short name; the rest are `key=value`
attributes:

```caddyfile
# dsm type=CNAME zone=synology.com
dsm.synology.com {
	reverse_proxy 127.0.0.1:5003
}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `<name>` (leading word) | — | record short name; with `zone` forms the FQDN |
| `type` | inferred from value (IPv4 ⇒ A, else CNAME) | `A` or `CNAME` |
| `zone` | `technitium.default_zone` | authoritative zone |
| `value` | `caddy.a_value` / `caddy.cname_target` | record value |
| `ttl` | 0 (server default) | TTL in seconds |

A block with no such directive is left out of DNS entirely (Caddy-only). Ordinary
comments (no `key=value`) are ignored, so keep prose notes on their own and avoid
`=` in a comment placed directly above a block.

## Usage

### `hl sync` — make the world match the Caddyfile

```sh
hl sync              # deploy Caddy, then reconcile DNS from annotations
hl sync --dry-run    # show the plan; change nothing
hl sync --no-dns     # deploy Caddy only
hl sync --no-deploy  # reconcile DNS only
hl sync --no-prune   # never delete managed records absent from the file
```

### `hl status` — preview without changing anything

```sh
hl status
```

Lists the hosts in your Caddyfile and the pending DNS plan (`+` create, `~`
update, `-` delete) — a read-only `sync --dry-run` that never deploys.

### `hl add` — scaffold an annotated block, then sync

```sh
hl add app.home.lab 192.168.1.50:8080
```

Writes the `reverse_proxy` block **and** its `# app type=… zone=…` directive into
the local Caddyfile, then runs the `sync` flow. It's just a convenient editor; the
same result comes from hand-editing the file and running `hl sync`.

| Flag | Default | Description |
| --- | --- | --- |
| `--scheme` | from config (`http`) | upstream scheme when the target has none |
| `--zone` | `technitium.default_zone` | zone written into the annotation |
| `--ttl` | 0 | TTL written into the annotation |
| `--dns-type` | inferred | `A` or `CNAME` |
| `--dns-value` | from config | pins a value override into the annotation |
| `--no-dns` | false | don't write an annotation or reconcile DNS |
| `--no-deploy` | false | skip the Caddy deploy step |
| `--no-prune` | false | don't delete managed records absent from the file |
| `--no-sync` | false | edit the local Caddyfile only; don't deploy or touch DNS |
| `--force` | false | overwrite an existing block-form `reverse_proxy` |
| `--dry-run` | false | show the plan without writing, deploying, or modifying DNS |
| `-c, --config` | default path | path to a config file |

### All commands

| Command | What it does |
| --- | --- |
| `hl sync` | Deploy the Caddyfile and reconcile DNS from annotations |
| `hl status` | Show hosts + the pending DNS plan (read-only) |
| `hl add [host] [target]` | Scaffold an annotated block, then sync |
| `hl dns list` | List records in a zone (`--zone`) |
| `hl dns login` | Create a Technitium API token and save it |
| `hl config init` | Write a default config file |
| `hl config show` | Print effective config (token redacted) |

`hl dns login` flags: `--user`, `--pass`, `--totp` (2FA code, only if the
account has 2FA enabled), `--token-name` (label for the token in Technitium's
Administration → Sessions list, default `hl`). The token is non-expiring, so
a 2FA code is needed only once at login.

> **Migration from earlier versions:** the separate write commands `hl dns add`,
> `hl caddy add`, `hl caddy sync`, and `hl caddy list` were removed. Annotate your
> blocks and use `hl sync`; preview with `hl status`. The first reconcile is
> effectively additive — existing records are only adopted/pruned once they carry
> the `managed-by:hl` tag, which `hl` sets on records it creates.

## Safety

- The local Caddyfile is backed up to a timestamped `.bak` before every write.
- Before pushing, the remote file is copied to `<path>.hldns.bak`. If the
  reload fails, the previous remote file is restored automatically.
- SSH host keys are checked against `~/.ssh/known_hosts`. An unknown host is
  accepted on first use with a warning; a **mismatch is rejected**.
- The Technitium token is never printed (`hl config show` shows `<set>`).
- DNS reconcile only ever updates or deletes records carrying the `managed_tag`
  comment that `hl` sets on records it creates. Records you made by hand (and
  infrastructure records like NS/MX/SOA) are never touched. Preview with
  `hl status` and use `--no-prune` to suppress deletions.

## Development

```sh
go build ./...     # compile everything
go build -o hl .   # build the runnable binary
go vet ./...       # lint
go test ./...      # unit tests (Caddyfile parser, annotations, reconcile, client)
go fix ./...       # apply safe Go modernizations
```

Module: `github.com/AhmedAburady/hl`, Go 1.26. Internal packages live
under `internal/` (`config`, `caddy`, `technitium`, `reconcile`, `sshx`,
`prompt`); the Cobra command tree is in `cmd/`. See `AGENTS.md` for contributor
guidance.
