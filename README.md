# smount

![Release Build](https://img.shields.io/github/actions/workflow/status/thomaslaurenson/smount/tag.yml?style=flat&label=release&logo=github) ![Main Build](https://img.shields.io/github/actions/workflow/status/thomaslaurenson/smount/main.yml?style=flat&label=main&logo=github)

![Release Version](https://img.shields.io/github/v/release/thomaslaurenson/smount?style=flat&logo=github) ![Release downloads](https://img.shields.io/github/downloads/thomaslaurenson/smount/total?label=downloads&logo=github)

![Go Version](https://img.shields.io/github/go-mod/go-version/thomaslaurenson/smount?logo=go) ![Code Coverage](https://img.shields.io/badge/Coverage-90.1%25-blue?logo=go)

Mount remote directories over SSH, using `sshfs` and the hosts already in your ssh config.

## What

- `smount` is a wrapper around `sshfs` that takes the bookkeeping out of it
- Host aliases come from `~/.ssh/config` and every file it includes
- Mount points are derived from the host and remote path, allowing multiple mounts from the same machine
- **Favourites** name a target you use often, so `smount <favourite>` works

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

`sshfs` is a separate system package. `smount check` reports whether it is present:

```sh
sudo apt install sshfs
```

## Usage

Run `smount` with no arguments to pick a favourite or a host from a filterable list, or name a target directly:

```sh
smount                 # choose a host or favourite interactively
smount web01           # mount the remote home directory
smount web01:/var/www  # mount one remote path
smount web01_www       # mount the favourite named "web01_www"

smount umount            # unmount interactively
smount umount web01_www  # unmount by name
smount umount --all      # unmount all

smount ls           # list active mounts
smount hosts        # list the SSH hosts smount can see
smount check        # check the local environment
smount version      # print the version

smount completion bash | sudo tee /etc/bash_completion.d/smount
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
| `--color` | When to colour output: `auto`, `always` or `never` | `auto` |

`--color` is available on every subcommand, and `auto` styles output only when the stream is a terminal and `NO_COLOR` is unset.

`smount umount` takes `--all` to unmount everything, and `--force` to lazily unmount a dropped connection. `smount hosts` takes `--quiet` to print host names without resolving them.

A favourite is saved from a mount rather than declared: once an ad hoc mount succeeds, smount offers to keep it, and the `--at`, `--opt` and `--ro` that mount used are saved with it. Favourites can also be written straight into `favourites.json`.

## Mount points

An ad hoc mount is named after the host and the remote path:

| Target | Mount point |
|---|---|
| `smount web01` | `~/sshfs/web01` |
| `smount web01:/` | `~/sshfs/web01-root` |
| `smount web01:/var/log` | `~/sshfs/web01-var-log` |

A favourite is named after itself, so `smount logs` mounts at `~/sshfs/logs` whatever host it points at. Override either with `--at`.

## Configuration

Both files live in `~/.smount` and are created on demand. The `config.json` file holds the defaults:

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

`reconnect` is paired with the two `ServerAlive` options deliberately. Without them ssh never notices a dropped link, so `reconnect` has no failure to react to and the mount hangs instead of recovering.

`compression=yes` is not a default. Above roughly 10 Mbit it costs more CPU time than it saves in transfer time, so add it only for a slow or metered link. The `favourites.json` file holds the saved targets:

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

Only `name` and `host` are required. `mountpoint` pins where the favourite mounts and is written only when it differs from the derived `~/sshfs/<name>`, so a favourite left alone still follows `mount_base` when that changes.

Mount options are layered rather than replaced: `config.json` sets the baseline, a favourite adds to it, and `-o` on the command line comes last. sshfs takes the last value given for a repeated option, so later layers win.

## Hosts

Aliases are read from `ssh_config` and every file it reaches through `Include`, globs and relative paths included. Wildcard and negated patterns such as `Host *` are skipped, since they configure connections rather than name somewhere to connect to.

`ssh_config` controls which file smount reads host *names* from, and nothing else. Connecting is left to `ssh` and `sshfs`. Pointing it at one fragment, say `~/.ssh/config.d/work`, therefore narrows the list smount offers without affecting how anything mounts.
