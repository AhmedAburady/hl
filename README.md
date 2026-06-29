# hl — your homelab's Caddy + Technitium DNS, from one file

<p align="center">
  <img src=".github/assets/banner.jpg" alt="hl — homelab Caddy + Technitium DNS" width="100%" />
</p>

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/AhmedAburady/hl?include_prereleases)](https://github.com/AhmedAburady/hl/releases)

If you run [Caddy](https://caddyserver.com/) for reverse proxies and
[Technitium](https://technitium.com/dns/) for DNS, you know the dance: spin up a
service, edit the Caddyfile, then go poke the DNS server for the matching record.
Two places, by hand, every single time — and sooner or later they drift.

**hl makes your Caddyfile the single source of truth for both.** Declare a
record's DNS intent in a comment right above its site block, run one command, and
the proxy *and* its DNS record come up together:

```
# dsm type=CNAME zone=synology.com value=caddy.home.lab.
dsm.synology.com {
	reverse_proxy 127.0.0.1:5003
}
```

`hl sync` then:

1. Pushes the Caddyfile to your Caddy host over SSH and reloads Caddy.
2. Reconciles Technitium DNS so the zone holds exactly the records your
   annotations declare — creating, updating, and **pruning** as the file changes.

Hand-edit the file whenever you like and re-run `hl sync`; `hl status` previews
the plan first and changes nothing. And it's careful by default: hl only ever
touches records *it* created (tagged via Technitium's per-record comment). Your
hand-made records — and NS/MX/SOA infrastructure — are left strictly alone.

### Why I built it

This started as a scratch for my own itch: running Caddy + Technitium at home, the
copy-the-hostname-into-two-places chore got old fast, and the two kept falling out
of sync. If you're on the same stack, it'll save you the same annoyance. It's
small, it leans on tools you already trust (SSH, your Caddyfile, the Technitium
API), and it treats your config file the way it deserves — as the truth. PRs and
issues from fellow homelabbers welcome.

## Requirements

- Go 1.26 or newer
- SSH access to the host running Caddy (key-based or ssh-agent). If the SSH user
  is not `root`, it needs passwordless `sudo` — hl writes the Caddyfile and
  reloads Caddy via `sudo` so it can reach privileged paths like `/etc/caddy`.
- A [Technitium DNS Server](https://technitium.com/dns/) with its HTTP API enabled

## Install

The command is invoked as `hl`.

**Install with Go** (puts `hl` on your `$GOPATH/bin`):

```sh
go install github.com/AhmedAburady/hl@latest
```

**Or download a pre-built binary** (macOS and Linux) from the
[Releases](https://github.com/AhmedAburady/hl/releases) page:

| Platform | Architecture | Binary |
|---|---|---|
| macOS | Apple Silicon | `hl-darwin-arm64` |
| macOS | Intel | `hl-darwin-amd64` |
| Linux | x64 | `hl-linux-amd64` |
| Linux | ARM64 | `hl-linux-arm64` |

```sh
chmod +x hl-darwin-arm64
sudo mv hl-darwin-arm64 /usr/local/bin/hl   # or anywhere on your PATH
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
    reload_cmd: "systemctl restart caddy"   # run on the host after the file is written (sudo auto-added for non-root)
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
hl sync --adopt      # overwrite existing records hl does not manage
hl sync --force      # redeploy + reload even if the remote file already matches
```

The deploy step is a no-op when the live remote Caddyfile already hashes (SHA-256)
to your local file — sync compares the two before writing, so it never restarts
Caddy for an unchanged config. Pass `--force` to deploy and reload anyway, which
is also how you bring a **stopped** Caddy back up on an otherwise-unchanged file.

hl only ever touches records it created (tagged `managed-by:hl`). If an
annotation would land on a record that already exists and is **not** managed by
hl, it is reported as a conflict (`!`) and skipped, and that name is protected
from pruning. Re-run with `--adopt` to take ownership by overwriting it — but
only when the existing record is the **same type** (a single atomic write). A
cross-type collision (e.g. a CNAME over an existing A or TXT) is never resolved
by deleting the other record; remove it by hand first.

### `hl pull` — bring the remote Caddyfile down to local

```sh
hl pull              # download the live remote Caddyfile to the local file
hl pull --dry-run    # report whether the local file would change; write nothing
```

The inverse of `hl sync`'s deploy: it reads the live Caddyfile from the host over
SSH and writes it locally. If your local file already matches, nothing is written;
otherwise the existing local file is copied into a `backups/` directory beside it
first. Use this to adopt a Caddyfile that was edited on the server, or to recover
the local copy on a fresh machine.

### `hl status` — preview without changing anything

```sh
hl status
```

Lists the hosts in your Caddyfile and the pending DNS plan (`+` create, `~`
update, `-` delete, `!` conflict with an unmanaged record) — a read-only
`sync --dry-run` that never deploys.

You edit the Caddyfile directly — add a block, add its annotation — then run
`hl sync`. There is no `add` command: the file is the source of truth.

### All commands

| Command | What it does |
| --- | --- |
| `hl sync` | Deploy the Caddyfile and reconcile DNS from annotations |
| `hl pull` | Download the live remote Caddyfile to the local file |
| `hl status` | Show hosts + the pending DNS plan (read-only) |
| `hl dns list` | List hl-managed records (`--zone` defaults to Caddyfile zones; `--all` for every record) |
| `hl config init` | Create the config file (interactive wizard on a TTY, template otherwise) |
| `hl config show` | Print effective config (token redacted) |

## Safety

- The local Caddyfile is copied into a `backups/` directory (timestamped names,
  the 2 most recent kept) before every write.
- Before pushing, the remote file is copied to `<path>.hldns.bak`. If the
  reload fails, the previous remote file is restored automatically.
- SSH host keys are checked against `~/.ssh/known_hosts`. An unknown host is
  accepted on first use with a warning; a **mismatch is rejected**.
- The Technitium token is never printed (`hl config show` shows `<set>`).
- DNS reconcile only ever updates or deletes records carrying the `managed_tag`
  comment that `hl` sets on records it creates. Records you made by hand (and
  infrastructure records like NS/MX/SOA) are never touched — an annotation that
  collides with one is reported as a conflict and skipped unless you pass
  `--adopt`. Preview with `hl status` and use `--no-prune` to suppress deletions.

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
`ui`); the Cobra command tree is in `cmd/`. See `AGENTS.md` for contributor
guidance.

## License

[MIT](LICENSE) © Ahmed Aburady. Built for homelabbers — fork it, ship it, make it
yours.
