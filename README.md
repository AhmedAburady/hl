# hl — homelab Caddy + Technitium DNS CLI

`hl` is a small Go CLI that treats your **local Caddyfile as the single source of
truth** for both reverse proxies and DNS. Each site block declares its DNS intent
in a comment directly above it, and one command keeps everything in sync:

```
# dsm type=CNAME zone=synology.com value=caddy.home.lab.
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
#    On a terminal this is an interactive wizard (DNS URL, token, Caddy host, SSH
#    auth, paths) and asks before overwriting an existing file. When stdin is not
#    a terminal it writes a complete template you can edit by hand; if a config
#    already exists it errors instead of clobbering (pass --force to overwrite).
#    Lives at ~/.config/hl/config.yaml (or $XDG_CONFIG_HOME/hl/config.yaml).

# 2. Create an API token in the Technitium web UI
#    Administration → Sessions → Create Token. Use it as technitium.token —
#    a literal, an op://vault/item/field reference, or ${ENV_VAR} (see below).

# 3. Check the effective config (token is redacted)
hl config show
```

### Config file reference

```yaml
technitium:
  url: http://dns.home:5380       # Technitium web API base URL
  token: ""                       # API token from the Technitium UI;
                                  # literal, ${ENV_VAR}, or op://vault/item/field

caddy:
  local_file: ~/.config/hl/Caddyfile   # source of truth (~ is expanded)
  managed_tag: managed-by:hl      # ownership tag written to records hl manages
  remote:
    host: caddy.home              # SSH host for the Caddy server
    user: root                    # SSH user (defaults to root)
    port: 22
    key: ~/.ssh/id_ed25519        # private key; leave empty to use ssh-agent
    agent_socket: ""              # ssh-agent socket; empty falls back to $SSH_AUTH_SOCK
    remote_path: /etc/caddy/Caddyfile
    reload_cmd: "caddy reload --config /etc/caddy/Caddyfile"
```

There are no DNS defaults in config: the Caddyfile is the sole source of truth, so
each managed block must declare its `zone` and `value` in its annotation.

The token is resolved at use time: a plain string is used as-is, a value
containing `${VAR}` is expanded from the environment, and an `op://vault/item/field`
reference is read from 1Password via the `op` CLI (which must be installed and
signed in). There is no login step — create the token once in the Technitium UI.

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
# dsm type=CNAME zone=synology.com value=caddy.home.lab.
dsm.synology.com {
	reverse_proxy 127.0.0.1:5003
}
```

| Key | Required | Meaning |
| --- | --- | --- |
| `<name>` (leading word) | yes | record short name; with `zone` forms the FQDN |
| `zone` | yes | authoritative zone (no config default — fail if absent) |
| `value` | yes | record value (no config default — fail if absent) |
| `type` | no | `A` or `CNAME`; inferred from value (IPv4 ⇒ A, else CNAME) |
| `ttl` | no | TTL in seconds (0 = server default) |

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

You edit the Caddyfile directly — add a block, add its annotation — then run
`hl sync`. There is no `add` command: the file is the source of truth.

### All commands

| Command | What it does |
| --- | --- |
| `hl sync` | Deploy the Caddyfile and reconcile DNS from annotations |
| `hl status` | Show hosts + the pending DNS plan (read-only) |
| `hl dns list` | List records in a zone (`--zone`, required) |
| `hl config init` | Create the config file (interactive wizard on a TTY, template otherwise) |
| `hl config show` | Print effective config (token redacted) |

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
