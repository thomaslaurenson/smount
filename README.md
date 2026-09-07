# smount

![Release Build](https://img.shields.io/github/actions/workflow/status/thomaslaurenson/smount/tag.yml?style=flat&label=release&logo=github) ![Main Build](https://img.shields.io/github/actions/workflow/status/thomaslaurenson/smount/main.yml?style=flat&label=main&logo=github)

![Release Version](https://img.shields.io/github/v/release/thomaslaurenson/smount?style=flat&logo=github) ![Release downloads](https://img.shields.io/github/downloads/thomaslaurenson/smount/total?label=downloads&logo=github)

![Go Version](https://img.shields.io/github/go-mod/go-version/thomaslaurenson/smount?logo=go) ![Code Coverage](https://img.shields.io/badge/Coverage-74.6%25-blue?logo=go)

Mount remote directories over SSH, using the hosts already in your ssh config.

## What

- `smount` is a wrapper around `sshfs` that takes the bookkeeping out of it
- Host aliases come from `~/.ssh/config` and every file it includes, so there is nothing new to configure
- Where a host actually points comes from `ssh -G`, so `Match` blocks and option precedence are handled by ssh itself
- Mount points are derived from the host and remote path, so several directories on one machine can be mounted at once
- **Favourites** name a target you use often, so `smount logs` is enough

## Installation

Download a pre-built binary from the [releases page](https://github.com/thomaslaurenson/smount/releases). For easier install, use the bash installer script:

```sh
curl -fsSL https://github.com/thomaslaurenson/smount/releases/latest/download/install.sh | bash
```

Or the PowerShell installer script if on Windows:

```ps
irm https://github.com/thomaslaurenson/smount/releases/latest/download/install.ps1 | iex
```

Install from source:

```sh
go install github.com/thomaslaurenson/smount@latest
```

`sshfs` is a separate package. `smount check` reports whether it is present:

```sh
sudo apt install sshfs
```

## Usage

Run `smount` with no arguments to pick a favourite or a host from a filterable
list, or name a target directly:

```sh
smount                       # choose a favourite or host interactively
smount web01                 # mount the remote home directory
smount web01:/var/log        # mount one remote path
smount logs                  # mount the favourite named "logs"

smount ls                    # list active mounts
smount umount [name]         # unmount by name, or choose interactively
smount hosts                 # list the SSH hosts smount can see
smount check                 # check the local environment
smount version               # print the version

smount fav list              # list saved favourites
smount fav add logs web01:/var/log
smount fav rm logs
smount fav import            # import from the war10ck sshfs shell function

smount completion bash > /etc/bash_completion.d/smount
```

Completion offers favourite names, host aliases and active mount names.

### Flags

| Flag | Description | Default |
|---|---|---|
| `--at` | Mount point to use instead of the derived one | |
| `--opt`, `-o` | Additional sshfs option (repeatable) | |
| `--ro` | Mount read only | `false` |
| `--yes`, `-y` | Skip the confirmation prompt | `false` |
| `--dry-run` | Print the sshfs command instead of running it | `false` |
| `--no-save` | Do not offer to save the mount as a favourite | `false` |

`smount umount` takes `--all` to unmount everything, and `--force` to lazily
unmount a dropped connection. `smount hosts` takes `--quiet` to print host names
without resolving them.

`smount fav add` takes `--at`, `--opt` and `--ro`, which mean the same as above
but are saved with the favourite rather than applied to one mount:

```sh
smount fav add logs web01:/var/log --ro -o compression=yes
smount fav add scratch web01:/tmp --at ~/scratch
```

## Mount points

An ad hoc mount is named after the host and the remote path:

| Target | Mount point |
|---|---|
| `smount web01` | `~/sshfs/web01` |
| `smount web01:/` | `~/sshfs/web01-root` |
| `smount web01:/var/log` | `~/sshfs/web01-var-log` |

A favourite is named after itself, so `smount logs` mounts at `~/sshfs/logs`
whatever host it points at. Override either with `--at`.

## Configuration

Both files live in `~/.smount` and are created on demand.

`config.json` holds the defaults:

```json
{
  "mount_base": "~/sshfs",
  "options": [
    "reconnect",
    "ServerAliveInterval=15",
    "ServerAliveCountMax=3",
    "follow_symlinks",
    "idmap=user"
  ],
  "ssh_config": "~/.ssh/config"
}
```

`reconnect` is paired with the two `ServerAlive` options deliberately. Without
them ssh never notices a dropped link, so `reconnect` has no failure to react to
and the mount hangs instead of recovering.

`compression=yes` is not a default. Above roughly 10 Mbit it costs more CPU time
than it saves in transfer time, so add it only for a slow or metered link.

`favourites.json` holds the saved targets:

```json
{
  "version": 1,
  "favourites": [
    {
      "name": "logs",
      "host": "web01",
      "path": "/var/log",
      "mountpoint": "~/scratch/logs",
      "options": ["compression=yes"],
      "readonly": true
    }
  ]
}
```

Only `name` and `host` are required. `mountpoint` pins where the favourite
mounts and is written only when it differs from the derived `~/sshfs/<name>`,
so a favourite left alone still follows `mount_base` when that changes.

Mount options are layered rather than replaced: `config.json` sets the baseline,
a favourite adds to it, and `-o` on the command line comes last. sshfs takes the
last value given for a repeated option, so later layers win.

## Hosts

Aliases are read from `ssh_config` and every file it reaches through `Include`,
globs and relative paths included. Wildcard and negated patterns such as
`Host *` are skipped, since they configure connections rather than name
somewhere to connect to.

`ssh_config` controls which file smount reads host *names* from, and nothing
else. Connecting is left to `ssh` and `sshfs`. Pointing it at one fragment, say
`~/.ssh/config.d/work`, therefore narrows the list smount offers without
affecting how anything mounts.
