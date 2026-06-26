# hl — homelab Caddy + Technitium DNS CLI

`hl` is a small Go CLI for managing a homelab's reverse proxies and DNS in one
shot. Running `hl add app.home.lab 192.168.1.50:8080` will:

1. Add (or update) a `reverse_proxy` site block in your **local** Caddyfile.
2. Push that file to your Caddy host over SSH and reload Caddy.
3. Add a matching **A** or **CNAME** record to a Technitium DNS zone.

The local Caddyfile stays the source of truth, so you can still hand-edit it and
re-deploy with `hl caddy sync`.

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
#    Prompts for your Technitium admin user + password, saves the token.

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
  cname_target: caddy.home.lab.   # default value for CNAME records (hl add)
  a_value: 192.168.1.10           # default value for A records
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

## Usage

### The flagship command: `hl add`

```sh
hl add app.home.lab 192.168.1.50:8080
```

This adds a Caddy block like:

```caddyfile
app.home.lab {
	reverse_proxy http://192.168.1.50:8080
}
```

pushes it to the remote Caddy host, reloads Caddy, and adds a DNS record
(default CNAME pointing to `caddy.cname_target`).

If you omit `<host>` or `<target>`, `hl` prompts you for them interactively.

Flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--scheme` | from config (`http`) | upstream scheme when the target has none |
| `--zone` | `technitium.default_zone` | DNS zone for the record |
| `--ttl` | 0 (server default) | DNS record TTL in seconds |
| `--dns-type` | `CNAME` | DNS record type: `A` or `CNAME` |
| `--dns-value` | from config | record value (IP for A, target for CNAME) |
| `--no-dns` | false | skip adding the DNS record |
| `--no-deploy` | false | edit the local Caddyfile only; don't push/reload |
| `--force` | false | overwrite an existing reverse_proxy block / DNS record |
| `--comments` | none | comment attached to the DNS record |
| `-c, --config` | default path | path to a config file |

Examples:

```sh
# A record pointing directly at an IP, overwrite if it exists
hl add app.home.lab 192.168.1.50:8080 --dns-type A --dns-value 192.168.1.10 --force

# Only edit the local Caddyfile; deploy later
hl add app.home.lab 192.168.1.50:8080 --no-deploy

# Only manage Caddy, leave DNS alone
hl add app.home.lab 192.168.1.50:8080 --no-dns
```

### All commands

| Command | What it does |
| --- | --- |
| `hl add [host] [target]` | Caddy block + deploy + DNS record (the whole flow) |
| `hl caddy add [host] [target]` | Caddyfile edit + deploy only (no DNS) |
| `hl caddy sync` | Push the current local Caddyfile and reload |
| `hl caddy list` | List site blocks in the local Caddyfile |
| `hl dns add [domain]` | Add one A/CNAME record (defaults to type A) |
| `hl dns list` | List records in a zone (`--zone`) |
| `hl dns login` | Create a Technitium API token and save it |
| `hl config init` | Write a default config file |
| `hl config show` | Print effective config (token redacted) |

`hl dns add` flags: `--type` (A/CNAME), `--value`, `--zone`, `--ttl`,
`--overwrite`, `--comments`.

## Safety

- The local Caddyfile is backed up to a timestamped `.bak` before every write.
- Before pushing, the remote file is copied to `<path>.hldns.bak`. If the
  reload fails, the previous remote file is restored automatically.
- SSH host keys are checked against `~/.ssh/known_hosts`. An unknown host is
  accepted on first use with a warning; a **mismatch is rejected**.
- The Technitium token is never printed (`hl config show` shows `<set>`).

## Development

```sh
go build ./...     # compile everything
go build -o hl .   # build the runnable binary
go vet ./...       # lint
go test ./...      # unit tests (Caddyfile parser + Technitium client)
go fix ./...       # apply safe Go modernizations
```

Module: `github.com/AhmedAburady/hl`, Go 1.26. Internal packages live
under `internal/` (`config`, `caddy`, `technitium`, `sshx`, `prompt`); the
Cobra command tree is in `cmd/`. See `AGENTS.md` for contributor guidance.
