# hublot - agent instructions

`hublot` is a full-screen terminal UI for Docker, written in Go: `htop` for a
Docker host. Live resource usage at a glance, keyboard-driven navigation into
every object the daemon manages, every action available without leaving the TUI.

This file is the reference. It replaces the original spec document, which was
deleted once distilled here. Read it fully before writing code: it describes what
to build, and the specific Docker API traps that naive implementations fall into.

Module path: `github.com/RobinHil/hublot`. Binary: `hublot`.

---

## 1. Differentiators

Two things justify this tool existing next to `lazydocker`, `ctop` and `dry`:

1. **Disk hygiene.** A first-class view of where disk space went, granular pruning
   per object category, and an accurate preview of what a prune will destroy
   before it runs. The Docker API offers no dry-run, so we reconstruct one.
2. **Compose awareness.** Real hosts run Compose stacks, not loose containers.
   Projects are grouped, actionable, and carry drift detection (does the running
   config still match the YAML on disk). No other tool surfaces drift.

## 2. Scope

In scope: containers, images, volumes, networks, Compose projects, system disk
usage, live CPU/memory/network/block IO stats, every action the Engine API
exposes for those objects, log streaming, shell exec, inspect, granular and global
pruning with previews, multi-selection and filtering.

The coverage of those objects is meant to be complete, and the `x` palette is
where everything without a dedicated key lives. Containers:
start, stop, restart, pause, unpause, kill with a signal, remove, rename,
update cpu and memory limits, logs, exec, inspect, processes, filesystem
changes, commit, export, copy files in and out, connect and disconnect
networks. Images: list, inspect, history, pull, tag, untag, save, load, remove.
Volumes: create, list, inspect, remove. Networks: create, list, inspect,
connect, disconnect, remove. System: info, version, events, df, prune per
category.

Out of scope, deliberately:

- **Remote Docker hosts.** Local daemon only, via the default socket. No context
  switching, no TCP, no SSH transport, and no abstractions "in case" we need them.
- Kubernetes.
- Any web UI, HTTP server, or telemetry.
- Swarm, services, nodes, tasks, secrets, configs and plugins; image build and
  push, registry login and search; checkpoints; and creating containers, which
  is what a compose file or a run command is for. `attach` is not offered
  either: `exec` covers what it is used for without the risk of sending a
  signal to PID 1.

Target platforms: Linux and macOS. Windows is not supported.

## 3. Tech stack

| Concern | Choice |
|---|---|
| Language | Go 1.22+ |
| TUI | `github.com/charmbracelet/bubbletea` |
| Styling | `github.com/charmbracelet/lipgloss` |
| Widgets | `github.com/charmbracelet/bubbles` (viewport, textinput, spinner, help, key) |
| Docker | `github.com/docker/docker/client` (official Engine SDK) |
| Config | `gopkg.in/yaml.v3` |

Do not add dependencies beyond these without a stated reason. In particular, do
**not** import `github.com/docker/compose/v2` as a library: it pulls an enormous
dependency tree and pins us to a Compose version. Compose actions shell out to the
`docker compose` binary instead (section 8).

One addition beyond the table, with its reason: `github.com/charmbracelet/x/term`,
used by `internal/docker/exec.go` for raw mode, terminal size and restore. Bubble
Tea already pulls it in, so it costs nothing in the dependency tree, and the
alternative was a second terminal library for three calls.

## 4. Architecture

Four layers, strictly separated. This is the most important section.

```
internal/docker/   Engine SDK wrapper. Knows nothing about the UI.
internal/compose/  Compose model and CLI runner. Knows nothing about the UI.
internal/state/    Pure data types and reducers. No IO, no Docker, no UI.
internal/ui/       Bubble Tea. Talks to the layers above only through messages.
```

### Rule 1: the UI never sees a Docker SDK type

`internal/docker/types.go` defines our own structs (`Container`, `Image`,
`Volume`, `Network`, `DiskUsage`, ...). The SDK's `container.Summary` and friends
are converted at the boundary and never leak upward. This insulates us from SDK
churn and makes the UI testable with fabricated data.

### Rule 2: no shared mutable state, no mutexes

Bubble Tea's `Update` runs on a single goroutine. All state lives in the model.
Everything from the outside world arrives as a `tea.Msg`. Do not build a
mutex-protected store that background goroutines write into. There is no reason
for a `sync.Mutex` to exist anywhere in this codebase.

### Rule 3: nothing blocks Update

Every Docker call returns a `tea.Cmd` that runs it in a goroutine and returns a
result message. A synchronous `ContainerRemove` inside `Update` freezes the UI for
seconds.

### Rule 4: the stream pattern

Continuous sources (events, stats, logs, command output) use one goroutine writing
to a channel, plus a command that reads one item and reschedules itself:

```go
func waitFor[T any](ch <-chan T) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return StreamClosedMsg{}
		}
		return v
	}
}
```

On receiving the message in `Update`, call `waitFor` again. Implemented once in
`internal/ui/stream.go`.

## 5. Package layout

```
hublot/
  cmd/hublot/main.go          flag parsing, client init, tea.NewProgram
  internal/
    docker/
      client.go               connection, ping, API version negotiation
      types.go                our domain types
      containers.go           list, inspect, start, stop, restart, pause,
                              unpause, kill, rm, rename, update
      images.go               list, inspect, history, rm, tag, untag, pull, save
      volumes.go              list, inspect, rm, usage
      networks.go             list, inspect, rm, connect, disconnect
      system.go               df, version, info, prune per category
      events.go               event stream with backoff reconnection
      stats.go                per-container stats stream, CPU/mem computation
      logs.go                 log stream with stdcopy demultiplexing
      exec.go                 interactive exec session
    compose/
      labels.go               rebuild project tree from container labels
      cli.go                  binary detection, argument construction
      runner.go               command execution with streamed output
      drift.go                config-hash comparison
    state/
      store.go                the model's data, reducers, sorting, filtering
      prune.go                prune preview computation
    ui/
      app.go                  root model, tab routing, global keys
      stream.go               waitFor and friends
      keys/keys.go            all key.Binding definitions, centralized
      theme/theme.go          colors and styles, one place only
      components/
        table.go              generic sortable/filterable/selectable table
        modal.go              confirmation dialogs
        statusbar.go
        help.go
        sparkline.go
        tasks.go              long-running command output panel
      views/
        containers.go images.go volumes.go networks.go compose.go disk.go logs.go
    config/config.go          ~/.config/hublot/config.yaml
```

## 6. Docker API traps

Not optional refinements. Each one produces visibly wrong output if ignored.
Comments in the code should point back to this section.

### 6.1 CPU percentage

The API returns cumulative counters, not percentages:

```go
cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
sysDelta := float64(s.CPUStats.SystemUsage - s.PreCPUStats.SystemUsage)

cpus := float64(s.CPUStats.OnlineCPUs)
if cpus == 0 {
	cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
}

var percent float64
if sysDelta > 0 && cpuDelta > 0 {
	percent = (cpuDelta / sysDelta) * cpus * 100.0
}
```

`OnlineCPUs` is absent on older API versions, hence the fallback. The first sample
of a stream has no usable `PreCPUStats`: display `-`, not `0%`.

### 6.2 Memory usage

`MemoryStats.Usage` includes page cache, which inflates the number. To match
`docker stats`:

- cgroup v2 (`stats` map contains `inactive_file`): subtract `inactive_file`
- cgroup v1 (`stats` map contains `cache`): subtract `cache`

Detect by probing the map, not by inspecting the host.

### 6.3 Stats goroutine lifecycle

One goroutine and one `context.CancelFunc` per running container, kept in a map
owned by the model. Started when an event says a container started, cancelled when
it stops or is removed. Forgetting the cancellation leaks goroutines and HTTP
connections on every container restart.

Cap concurrent stat streams (default 50, configurable). Above the cap, only stream
containers currently visible in the viewport.

### 6.4 Log demultiplexing

Without a TTY the log stream is multiplexed: stdout and stderr interleaved with an
8-byte header per frame. Reading it raw produces garbage every few lines. Use
`github.com/docker/docker/pkg/stdcopy`.`StdCopy`, and read `TTY` from the
container inspect to decide.

### 6.5 Event stream reconnection

If the daemon restarts, the event stream dies quietly. Reconnect with exponential
backoff, and on reconnection **resync all lists**, because events were missed
during the gap. Show a degraded indicator in the status bar while disconnected.

### 6.6 `system df` is slow

On a loaded host it takes seconds: the daemon walks every layer. Never put it in a
refresh loop. Compute on demand, cache, display its age. The Disk view shows a
spinner and a last-updated timestamp.

### 6.7 Interactive exec

Exec requires suspending Bubble Tea, putting the terminal in raw mode, wiring the
hijacked connection to stdin/stdout, and restoring cleanly on exit, including on
panic. Handle resize with `ContainerExecResize`. Prefer `tea.ExecProcess` where it
fits. Test "exit while output is still flowing".

### 6.8 SDK package layout

The modern SDK splits `types` into subpackages: `container.Summary`,
`image.Summary`, `volume.Volume`, `network.Summary`, `system.Info`,
`events.Message`, `container.StatsResponse`. Old `types.Container` aliases are
gone. Check the actual signatures before writing a call.

## 7. Refresh cadence

| Source | Mode |
|---|---|
| Events | Push. Source of truth for state changes. |
| Stats | Continuous stream, roughly 1 sample/sec per running container |
| Object lists | Resync on relevant event, plus a 10s safety poll |
| Disk usage | On demand only |
| Compose drift | On demand, or on entering the Compose view |

## 8. The Compose layer

Compose does not exist in the Engine API. There is no `/compose` endpoint. Compose
v2 is a CLI plugin that creates ordinary containers, networks and volumes carrying
labels. Everything below follows from that.

### 8.1 Reading state: labels

```
com.docker.compose.project                project name
com.docker.compose.service                service name
com.docker.compose.container-number       replica index
com.docker.compose.project.working_dir    original directory
com.docker.compose.project.config_files   comma-separated paths to the yml files
com.docker.compose.config-hash            hash of the service's resolved config
com.docker.compose.depends_on             dependencies
com.docker.compose.oneoff                 "True" for containers from `compose run`
```

Group by project, then by service, to build the tree. Networks and volumes carry
the project label too, so a project with no remaining containers is still
discoverable through them.

### 8.2 Acting: shell out

Reconstruct the invocation from the labels:

```go
args := []string{"compose", "--project-name", p.Name}
for _, f := range p.ConfigFiles {
	args = append(args, "-f", f)
}
args = append(args, "up", "-d")

cmd := exec.CommandContext(ctx, "docker", args...)
cmd.Dir = p.WorkingDir
```

Always pass an argument slice, never a shell string. Project names and paths come
from daemon labels and are external input.

Detect the binary at startup: try `docker compose version` (v2 plugin), fall back
to `docker-compose` (v1). If neither exists, disable Compose actions with a
visible explanation rather than failing at press time.

Always run with `--progress plain --ansi never`. Compose's default output uses
cursor repositioning and is unreadable when captured.

### 8.3 The environment variable trap

Compose resolves variables at `up` time and records nothing about where they came
from. A stack originally started from a shell holding `export DB_PASSWORD=...`
comes back up differently, or fails, when restarted from `hublot`.

The `.env` file in `working_dir` is picked up automatically because we set
`cmd.Dir`. Everything else is invisible to us.

Mitigation: run `docker compose config --quiet` as a preflight before any mutating
action. If it fails or reports unresolved variables, show the error and require
explicit confirmation before proceeding. Do not paper over it.

### 8.4 Drift detection

`config-hash` is what Compose itself uses to decide whether a container needs
recreating. Compare the hash on the running container against the hash the current
YAML would produce (`docker compose config --hash="*"`; verify the flag on the
installed version and degrade gracefully if unsupported).

Per-service marker in the UI:

- `OK` running config matches the file
- `->` drift: the file changed since the container was created
- `X` config cannot be read or resolved
- `o` not running

### 8.5 Ghost and orphan projects

Three cases the UI must handle distinctly:

1. **Stopped project with surviving volumes/networks.** Discoverable through the
   project label on those objects. Common, and a frequent source of wasted disk.
2. **Project whose `config_files` no longer exist on disk.** Detect with
   `os.Stat`. Mark as orphaned and restrict actions to what the Engine API can do
   directly (stop, rm); a `compose down` without its files fails.
3. **`oneoff=True` containers**, leftovers from `compose run`. List them
   separately in the Disk view. Nobody ever cleans these up.

### 8.6 Aggregated logs

Do **not** shell out for logs. Open one Engine log stream per container in the
project, prefix each line with the service name, colorize by a stable hash of the
service name, merge into one channel. This keeps buffering and search consistent
with the normal log view.

## 9. The UI

### 9.1 Layout

Full-screen tabs, not side-by-side panes. Stats tables need the width.

```
| Containers  Images  Volumes  Networks  Compose  Disk ---- local - 24 ctr |
| filter: running                                                          |
| > NAME         IMAGE         CPU     MEM        NET I/O   STATUS  PORTS  |
|   nginx-proxy  nginx:alpine  ## 12%  ... 45MB   1.2/0.8M  Up 3d   :80    |
|--------------------------------------------------------------------------|
| detail pane (toggled)                                                    |
| ?:help  /:filter  space:mark  enter:detail  x:actions  q:quit            |
```

One generic table component serves all views, parameterized by column definitions
and per-cell renderers. Write it once, well. It supports sorting by any column,
incremental filtering, multi-selection, viewport scrolling with a fixed header,
and responsive column widths that drop low-priority columns on narrow terminals.

Handle `tea.WindowSizeMsg` properly from the start. Minimum usable width: 80
columns; below that show a message rather than rendering garbage.

The frame fills the terminal exactly, like a window: the status bar on the first
line, the key hints on the last, the pane stretched to fill everything between,
and the message line always reserved so the pane does not resize under the
cursor when a message appears or expires. `App.frame` places those rows and pads
or cuts the pane to the remaining height; `App.layout` hands each view the same
arithmetic, and a view that prints a header of its own above its table (the disk
view does) subtracts it in `SetSize`.

The table budgets lines rather than counting rows: a group heading takes a line
like a row does, so `rowsFitting` decides the window and `View` renders exactly
that. Anything that measures or cuts rendered text goes through
`truncateToWidth`, which counts display width: styled strings carry escape
sequences that plain rune slicing both miscounts and splits.

### 9.2 Keybindings

Convention, applied consistently: **lowercase is safe, uppercase is destructive or
forced.** All bindings live in `internal/ui/keys`, never inline in views, so the
help overlay is generated from the same source.

Global: `1`-`6` / `tab` / `shift+tab` / left and right arrows switch view (no
table scrolls sideways, so the horizontal arrows are free), `/` filter, `s` cycle sort,
`space` mark, `a` mark all (respects filter), `esc` clear filter / selection /
close modal, `?` help, `r` force refresh, `q` and `ctrl+c` quit.

Containers: `enter` detail pane, `l` logs, `e` exec shell, `S` stop, `R` restart,
`P` pause/unpause, `K` kill with signal picker, `D` remove, `x` contextual action
palette. `s` is taken by sort, so start lives in the palette.

The `x` palette is the escape valve: anything too rare for a dedicated key
(rename, update limits, connect to network, save image, copy files) lives there,
searchable by typing.

Compose: `u` / `U` up -d / up -d --force-recreate, `p` / `b` pull / build, `S` /
`R` stop / restart stack, `l` aggregated logs, `+` / `-` scale selected service,
`c` show resolved config, `D` down, `X` down -v --remove-orphans.

### 9.3 Task panel

Long-running commands (`compose up`, `pull`, `build`) stream into a task panel
showing state (running / succeeded / failed), elapsed time, exit code, and the
captured output in a scrollable viewport. Multiple tasks run concurrently. Toggled
with a key; auto-opens on failure.

## 10. Destructive actions

### 10.1 Graded confirmation

| Risk | Confirmation |
|---|---|
| Reversible (stop, restart, pause) | none |
| Single object removal | `y`/`n` modal naming the object |
| Batch removal, category prune | modal listing everything affected plus reclaimable space |
| `system prune -a --volumes` equivalent | type a word to confirm |

Modals state exactly what will be destroyed, never "are you sure?".

### 10.2 Prune preview: build it ourselves

The Engine API has no dry-run. Prune endpoints delete first and report
`SpaceReclaimed` afterwards. Since the preview is the flagship feature, we
reconstruct it: list objects applying exactly the same filters the daemon would,
and show that list.

Rules to replicate faithfully:

- **Containers**: status `exited` or `created`, honoring an `until` filter
- **Images**: `dangling=true` by default; with the `-a` equivalent, every image not
  referenced by any container
- **Volumes**: not referenced by any container. The daemon's default volume prune
  skips named volumes unless `all` is set, and this behavior changed across
  versions. Check the negotiated API version and reflect it.
- **Networks**: user-defined, not referenced by any container, excluding built-in
  `bridge`, `host` and `none`
- **Build cache**: reported separately by `df`; prune via the dedicated endpoint

**This is the only place in the codebase where a bug destroys user data.** It gets
thorough unit tests against fabricated object sets, covering every filter
combination.

### 10.3 Compose awareness in pruning

A blanket `system prune -a --volumes` destroys the volumes of every stopped
Compose stack, which is usually data loss. In every prune preview, split affected
objects into two groups: objects belonging to a Compose project, with the project
name shown per object, and genuinely orphaned objects. Sort the Compose-owned ones
first and style them as a warning. This is the difference between a tool people
dare to run and one they never open.

### 10.4 Read-only mode

A `--read-only` flag disables every mutating action, greys out the corresponding
keys, and shows a badge in the status bar. This makes `hublot` safe to open on a
server just to look.

## 11. Error handling

- The daemon unreachable at startup is a clear message, not a stack trace: say
  which socket was tried and that the user may lack permission on it.
- Losing the daemon mid-session degrades: keep displaying the last known state,
  mark it stale, retry in the background.
- A failed action surfaces the daemon's own error message in a modal. Docker's
  errors are usually informative; do not swallow or rewrite them.
- Never `panic` in a `tea.Cmd`: recover and turn it into an error message.

## 12. Testing

- `internal/state` and `internal/compose/labels.go` are pure: table-driven tests,
  no Docker required. These carry most of the value.
- `internal/state/prune.go`: exhaustive tests. Non-negotiable.
- Stats computation: tests against recorded sample pairs, including first-sample
  and zero-delta cases.
- `internal/docker` is exercised against a real daemon by
  `internal/docker/live_check_test.go`, behind the `live` build tag: it never
  runs in CI and never runs by default. It is how a change to the docker layer
  is checked by hand, with `go test -tags live ./internal/docker/`, and it is
  the only place a test needs a daemon. It also carries the goroutine-leak check
  the definition of done asks for.
- Everything else runs with no daemon at all.

## 13. Build order

Do not build views before the data layer is solid.

1. **Skeleton**: module, `main.go`, client connection with a clear error path,
   `docker/types.go`, container listing. A static table on screen.
2. **Liveness**: event stream with reconnection, stats streams with correct CPU and
   memory computation, goroutine lifecycle tied to events.
3. **Compose model**: `compose/labels.go` and the project tree. Before finalizing
   the table component, because grouping by project affects its design.
4. **Table component**: sorting, filtering, selection, responsive columns.
5. **Container actions** plus the graded confirmation modal.
6. **Logs** with `stdcopy`, then **exec**.
7. **Images, volumes, networks views**, reusing the table.
8. **Disk view**, `df`, prune previews, granular prunes, global prune.
9. **Compose actions**: CLI runner, task panel, drift detection.
10. Config file, theming, polish.

## 14. Conventions

- Standard Go layout, `internal/` for everything not meant to be imported.
- Errors wrapped with `%w` and context; no bare `err` returns across layers.
- No global variables except the theme.
- Comments explain why, not what. The traps in section 6 deserve comments pointing
  back to this file.
- `gofmt`, `go vet`, `golangci-lint` clean.
- Keep functions short enough to see whole. Views get large fast; split early.
- No emoji anywhere: code, comments, UI strings, commit messages.
- Commit messages: one line, one sentence, no body, no signature. Never commit or
  push unprompted.

## 15. Definition of done for v1

- Opens on a host with 50+ containers and stays responsive.
- CPU and memory figures match `docker stats` within rounding.
- Every object type listable, inspectable, removable.
- Compose projects grouped, actionable, with drift detection.
- Disk view accurate, prune previews match what actually gets deleted.
- No goroutine leak after 30 minutes of containers starting and stopping.
- Quitting always restores the terminal, including after exec and after a panic.

## 16. Packaging and release

The project ships native packages, not just a `go build`. Three targets, all
driven from the `Makefile` so a release is one command:

- **Debian/Ubuntu `.deb`** and **RPM `.rpm`**: generated from a single
  `packaging/nfpm.yaml` via `nfpm pkg`. Keep the two in sync by construction;
  never hand-write a spec file or a `DEBIAN/control`.
- **Arch `PKGBUILD`** in `packaging/aur/`, building from the released source
  tarball, with `sha256sums` updated per release.
- **AppImage** in `packaging/appimage/`: an `AppDir` (desktop file, icon,
  `AppRun`) assembled by the Makefile and sealed with `appimagetool`. `hublot` is
  a terminal program, so the desktop entry sets `Terminal=true`; the AppImage
  exists for distro-less installs, not for a launcher menu.

Every format also ships the desktop launcher, so `hublot` appears in the
applications menu the way `htop` does: `packaging/hublot.desktop` with
`Terminal=true`, because a launcher that starts it without a tty would just
exit. The icon is `packaging/icons/hublot.svg`, a porthole (which is what the
name means) with a stack of containers behind the glass, and deliberately
nothing of Docker's own branding. The PNG sizes next to it are rendered from it
by `make icons` and committed, so building a package needs no renderer; CI
re-renders them and fails if they have fallen behind the SVG.

Rules:

- The binary links Apache-2.0 code, whose NOTICE files must travel with what is
  redistributed. `scripts/third-party-licenses.sh` collects the licence of every
  module actually linked in, and every package installs the result next to the
  project's own LICENCE. Do not drop it from a package format.
- Binary installs to `/usr/bin/hublot`, licence to
  `/usr/share/licenses/hublot/LICENSE`, man page to `/usr/share/man/man1`,
  launcher to `/usr/share/applications`, icons to
  `/usr/share/icons/hicolor/<size>/apps` plus `scalable` for the SVG.
- No install scriptlets: `desktop-file-utils` and `hicolor-icon-theme` own the
  caches those paths land in, and every distribution's package manager refreshes
  them through its own triggers or hooks.
- Version comes from git (`git describe --tags`) and is injected with
  `-ldflags "-X main.version=..."`. Never hardcode a version in Go source.
- Build with `CGO_ENABLED=0` so packages have no libc version constraint.
- Runtime dependency on the docker daemon is a recommendation, not a hard
  dependency: `hublot` must install on a machine where Docker is not present yet.
- `make dist` builds binary, deb, rpm, AppImage and the source tarball into
  `dist/`. `dist/` is git-ignored.
- Packaging tools (`nfpm`, `appimagetool`, `rpmbuild`) may be absent on a given
  machine: each Makefile target checks for its tool and fails with an install
  hint rather than a cryptic error.

## 17. Continuous integration

`.github/workflows/ci.yml` runs on pushes to main, on tags matching `v*`, and on
pull requests. Its shape is deliberate and worth keeping:

- **check** is the gate: gofmt, `go mod tidy` cleanliness, `go vet`,
  `go test -race`, and golangci-lint, on Linux and macOS both. Nothing else
  starts until it passes. The race detector needs cgo, so `CGO_ENABLED=0` is set
  per build job rather than workflow-wide.
- **build-linux**, **build-macos** and **build-arch** produce the artifacts.
  build-arch rewrites the PKGBUILD's `source=` to a tarball of the checkout, so
  a pull request builds the code under review rather than the last release;
  `build()`, `check()` and `package()` run byte for byte as an AUR user runs
  them.
- **install-linux** installs what was built on stock images of the distributions
  the packages target: the `.deb` on Debian, the `.rpm` and the AppImage on
  Fedora, the Arch package on Arch. Each one starts the binary. This is the only
  place a package manager other than the runner's ever resolves what a package
  declares.
- **release** attaches the artifacts to the tag, and waits on the install jobs:
  nothing reaches the releases page that has not been installed somewhere and
  started. The Arch package is built and installed but never published, because
  a binary Arch package is linked against the day's rolling libraries.

Package versions cannot be taken from `git describe` verbatim: dpkg refuses a
version that does not start with a digit, and rpm splits on hyphens. The
Makefile normalises them; the binary still reports the exact git description.

## 18. Local development

Go is not installed system-wide on this machine. A toolchain lives in the session
scratchpad; if it is gone, download one (`https://go.dev/dl/`) and extract it
rather than installing system-wide:

```sh
export GOROOT=<scratchpad>/go PATH=$GOROOT/bin:$PATH
go build ./... && go vet ./... && go test ./...
```

Docker and Compose v2 are available locally for manual checks.
