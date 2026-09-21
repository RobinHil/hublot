# hublot

htop for your Docker host: live per-container resource usage, keyboard-driven
navigation into every object the daemon manages, and every action available
without leaving the terminal.

Two things set it apart from the tools next to it:

- **Disk hygiene.** The Docker API has no dry run, so prune endpoints delete
  first and report afterwards. hublot reconstructs the preview, applying the
  same filters the daemon would, and lists the objects by name before anything
  is removed. Objects belonging to a Compose project are shown first and
  flagged, because a blanket prune is how stacks lose their data.
- **Compose awareness.** Stacks are rebuilt from the labels Compose leaves on
  ordinary objects, grouped by project and service, with drift detection: does
  what is running still match the YAML on disk?

Linux and macOS. It talks to the local daemon socket only, by design: no
contexts, no TCP, no SSH.

## Install

Packages carry the binary, the man page and a desktop entry that opens hublot
in a terminal.

| Where | How |
|---|---|
| Arch | `cd packaging/aur && makepkg -si` |
| Debian, Ubuntu | `sudo apt install ./hublot_*.deb` from the [latest release](https://github.com/RobinHil/hublot/releases/latest) |
| Fedora, RHEL | `sudo dnf install ./hublot-*.rpm` from the latest release |
| Anywhere else | the `.AppImage` from the latest release, which needs nothing installed |
| From source | `make build && ./dist/hublot`, with Go 1.22 or newer |

Then `hublot`, or `hublot --read-only` to look at a server without being able
to change anything. `man hublot` documents the keys and the optional
configuration file at `~/.config/hublot/config.yaml`.

## Keys

Lowercase is safe, uppercase is destructive or forced, everywhere. `?` opens
the help overlay, which is generated from the same definitions the program
dispatches on, so it cannot fall out of date.

## Building and contributing

```sh
make build     # the binary, into dist/
make test      # the test suite
make lint      # golangci-lint, if it is installed
make dist      # binary, deb, rpm, AppImage and source tarball
```

[AGENTS.md](AGENTS.md) is the reference for how the project is built: the
architecture rules, the Docker API traps that produce visibly wrong output when
ignored, the packaging layout and what CI checks. Read it before changing
anything.

## Licence

MIT, see [LICENSE](LICENSE). The binary links modules under MIT, Apache-2.0 and
BSD-3 licences; every package installs their full text and NOTICE files at
`/usr/share/licenses/hublot/THIRD-PARTY-LICENSES.txt`, and
`scripts/third-party-licenses.sh` regenerates it.

Docker is a trademark of Docker, Inc. hublot is an independent project, not
affiliated with, endorsed by or sponsored by Docker, Inc. It speaks to the
Docker Engine API and shells out to the Compose CLI, both of which are the
user's own installation.
