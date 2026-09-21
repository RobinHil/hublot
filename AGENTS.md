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

Things are created here as well as managed: a container from a form that asks
what `docker run` takes and nothing more, a volume or a network from a prompt,
an image pulled by name, and a stack brought up from a compose file that
nothing has been created from yet. Anything more elaborate than that form
belongs in a compose file, which is the point of the compose view.

What an editing session did is decided by the file's contents before and after,
never by how the editor exited: quitting with `:q` has to change nothing and
say so, `:wq` with no edits is not a change either, and a stack created for the
occasion is taken back out when nothing was written, starter file and directory
included, touching nothing that was already there. An editor that exits
non-zero is not an error worth a dialog; one that could not be run at all is.

Files are edited in a real editor rather than in a text box hublot would have
to grow: `internal/editor` finds one, the app suspends the interface the same
way it does for a shell, and what happens afterwards is the caller's business,
which for a compose file is offering to apply it. The choice is asked once and
written to the configuration, and it comes from `editor` there, then `$VISUAL`
and `$EDITOR`, then a list of what is installed, terminal editors first. An
editor that opens a window is given the flag that makes it wait, or hublot
would return before anything had been typed.

Compose actions apply to the row under the cursor. Each project is a row of its
own above its services, drawn across the width, and that is the row that means
the whole stack: `l`, `u`, `S` and `R` on it act on everything, on a service
line they act on that service. `enter` folds a stack and works only from the
project line, because folding from a service moves that service out from under
the cursor and reads as the list jumping about. `L` is the whole stack's logs
from anywhere. This is the one rule in that view worth learning, so the help
overlay states it.

The coverage of those objects is meant to be complete, and the `x` palette is
where everything without a dedicated key lives. Containers:
create and run, start, stop, restart, pause, unpause, kill with a signal,
remove, rename, update cpu and memory limits, logs, exec, inspect, processes,
filesystem changes, commit, export, copy files in and out, connect and
disconnect networks. Images: list, inspect, history, pull by name or again, run
a container from one, tag, untag, save, load, remove. Volumes: create, list,
inspect, remove. Networks: create, list, inspect, connect, disconnect, remove.
Compose: up, down, stop, restart, pull, build, scale, config, drift, logs,
reading the compose file, and starting a stack from a file on disk. Reading it
is its own key: answering "what does this stack actually say" by opening an
editor is how a file gets changed by accident. System: info, version, events, df, prune
per category.

Out of scope, deliberately:

- **Remote Docker hosts.** Local daemon only. No context switching, no TCP, no
  SSH transport, and no abstractions "in case" we need them.

  Which local socket is still a question, and `ResolveSocket` answers it: what
  `DOCKER_HOST` names if it is a unix socket, then `/var/run/docker.sock`, then
  `$XDG_RUNTIME_DIR/docker.sock`, where a rootless installation listens. That is
  not context switching, it is finding the daemon this machine runs; without the
  last one a rootless user is told there is no daemon at a path their
  installation never creates. Do not add context files or a host flag on top.
- Kubernetes.
- Any web UI, HTTP server, or telemetry.
- Swarm, services, nodes, tasks, secrets, configs and plugins; image build and
  push, registry login and search; checkpoints. `attach` is not offered either:
  `exec` covers what it is used for without the risk of sending a signal to
  PID 1, and `wait` has no meaning in a view that already shows state.

Target platforms: Linux and macOS. Windows is not supported.

## 3. Tech stack

| Concern | Choice |
|---|---|
| Language | Go, at the version `go.mod` pins |
| TUI | `github.com/charmbracelet/bubbletea` |
| Styling | `github.com/charmbracelet/lipgloss` |
| Widgets | `github.com/charmbracelet/bubbles` (viewport, textinput, spinner, help, key) |
| Docker | `github.com/docker/docker/client` (official Engine SDK) |
| Config | `gopkg.in/yaml.v3` |

Do not add dependencies beyond these without a stated reason. In particular, do
**not** import `github.com/docker/compose/v2` as a library: it pulls an enormous
dependency tree and pins us to a Compose version. Compose actions shell out to the
`docker compose` binary instead (section 8).

Two additions beyond the table, with their reasons. `github.com/charmbracelet/x/term`,
used by `internal/docker/exec.go` for raw mode, terminal size and restore. Bubble
Tea already pulls it in, so it costs nothing in the dependency tree, and the
alternative was a second terminal library for three calls.

`github.com/docker/go-connections/nat`, used by `internal/docker/run.go` to
parse the port specifications a container is created with. It is the Docker
SDK's own parser, already in the tree because the SDK depends on it, and
writing a second one would mean reimplementing `8080:80/tcp`, ranges and
interface prefixes from scratch. `go mod tidy` promotes it to a direct
dependency; leave it there, and run `go mod tidy` before pushing, because CI
fails on a go.mod that is not tidy.

## 4. Architecture

Four layers, strictly separated. This is the most important section.

```
internal/docker/   Engine SDK wrapper. Knows nothing about the UI.
internal/compose/  Compose model and CLI runner. Knows nothing about the UI.
internal/state/    Pure data types and reducers. No IO, no Docker, no UI.
internal/ui/       Bubble Tea. Talks to the layers above only through messages.
```

### Rule 0: nothing drawn comes out of a map

A map has no order, and Go deliberately varies it between calls. A list built
by ranging over one reorders itself on every refresh, which is once a second:
the text under the cursor changes while nothing about the host has. The
networks view did this with its attached containers, and it read as a list that
would not sit still.

Anything a domain type exposes as a map gets a method that returns it in a
decided order, `Network.Attached` being the pattern, and the views call that
instead of ranging over the map themselves. Sums over a map are fine; sequences
are not.

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
      client.go               connection, socket resolution, API negotiation
      permission.go           what a refused socket means, and what to do
      types.go                our domain types
      containers.go           every container action, plus top/diff/commit/export
      images.go               list, inspect, history, rm, tag, untag, pull, save, load
      volumes.go              list, inspect, create, rm, usage
      networks.go             list, inspect, create, rm, connect, disconnect
      run.go                  creating and starting a container from a spec
      copy.go                 docker cp both ways, with the escape checks
      system.go               df, version, info, prune per category
      events.go               event stream with backoff reconnection
      stats.go                per-container stats stream, CPU/mem computation
      logs.go                 log stream with stdcopy demultiplexing
      exec.go                 interactive exec session, on its own screen
    compose/
      labels.go               rebuild project tree from container labels
      cli.go                  binary detection, argument construction
      runner.go               command execution with streamed output
      drift.go                config-hash comparison
    editor/editor.go          finding an editor, and how to wait for it
    state/
      store.go                the model's data, reducers, sorting, filtering
      prune.go                prune preview computation
      parse.go                parsing what people type: sizes, paths, mounts
      diagnose.go             explaining the failures that keep happening
    ui/
      app.go                  root model, message routing, global keys
      app_actions.go          what each request does
      app_view.go             the frame, the dashboard, the hints
      layout.go               how the screen is divided, and nowhere else
      stream.go               waitFor and friends
      cmds/                   every Docker call, as tea.Cmd
      keys/keys.go            all key.Binding definitions, centralized
      theme/theme.go          colors and styles, one place only
      components/
        table.go              generic sortable/filterable/selectable table
        panel.go              the side panel: logs, inspect, text
        modal.go              dialogs, graded by severity
        form.go prompt.go picker.go
        header.go statusbar.go help.go sparkline.go
        sanitize.go           the single choke point for untrusted text
        tasks.go              long-running command output panel
      views/
        containers.go compose.go images.go volumes.go networks.go disk.go
        run.go                the container creation form
        view.go               the View interface and the requests it sends
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

The session gets the alternate buffer to itself. Bubble Tea hands the terminal
back as it found it before an exec, so without this the shell opens underneath
the scrollback of whatever ran there before hublot started, and leaves its own
behind on the way out. `ownScreen` switches to the alternate buffer, clears it,
prints one dim line naming the container, and switches back afterwards: nothing
outside the session is touched, scrollback included. The order to check when
this breaks is `1049l` from Bubble Tea, `1049h` ours, `1049l` ours, `1049h`
from Bubble Tea.

A shell inside the side panel is possible, and is not done on purpose rather
than because it cannot be. It needs a terminal emulator in the panel: full
escape parsing, cursor addressing, scroll regions, and its own alternate buffer
for whatever the user runs in there. `github.com/charmbracelet/x/vt` does that
and comes from the same family as what is already here, at the cost of eight
new modules, `charmbracelet/ultraviolet` among them, on an untagged
pseudo-version.

The rest is not the library: every keystroke has to reach the shell, including
the ones Bubble Tea and hublot use themselves, with a way back out; the remote
pty has to follow a pane the user can drag; and the panel repaints on every
byte the shell writes. Against that, a shell in a third of a 150 column
terminal is fifty columns, which is not where anyone wants to run `vim` or
`top`. Full screen on its own buffer is both cheaper and better. Revisit only
if someone asks for it knowing that.

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
| Containers  Compose  Images  Volumes  Networks  Disk ---- local - 24 ctr |
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
line, the key hints on the last, everything else stretched between them. One
place decides all of it, `internal/ui/layout.go`, and both the renderer and the
sizing read it rather than each doing the arithmetic. It is pure and tested: a
screen too small is refused with a message, the message line goes at 13 rows,
the tab bar at 11, and under 72 columns the labels shorten.

The head of the screen is a dashboard, not a title bar. Two rows when there is
room: what the session is and what the active view adds up to, then the two
figures that matter with their history and the host broken down into coloured
dots. One row when there is not. The history graphs are plotted against a fixed
ceiling, not against their own maximum: a machine doing nothing has to read as
doing nothing, and a graph rescaled to its own noise reads as load. Per-row
sparklines do the same through a flatness guard.

Every view answers `Summary()` with the one line it adds to that header: what
its list amounts to, so nobody has to count rows. Those lines complement the
header rather than repeating it, and no fact appears twice on one screen.

The hint bar names what the row under the cursor actually needs: `P` reads
"unpause" in front of a paused container and "pause" in front of a running one.
A paused container cannot be started, only unpaused, and a bar offering the
wrong half of a toggle is worse than one offering nothing.

Selecting rows is the one thing in a list that does nothing visible on its own,
so the interface explains it the first time it happens, the selected rows carry
a bar in the margin, and the word everywhere is "select", never "mark".

Reading happens in a side panel, not instead of the list: logs, inspect output,
process lists and resolved configuration open beside what they are about.
Beside becomes below under 96 columns, and takes the screen when neither half
would be worth reading. `ctrl+w` moves the focus, `W` cycles the share, and the
divider between the two is dragged with the mouse: the share is a percentage,
not one of four fractions, so the key cycles the useful stops and the mouse
lands wherever it likes. A drag stops short of full, because a divider dragged
off the edge cannot be dragged back. The panel owns its own search and follow,
so the list keys never fight with it.

Graphics shrink before figures do. The containers view picks a tier from its
width in `tierFor`: meter and sparkline, then a narrower meter, then numbers
alone. A cut number is a bug; a missing picture is a choice.

The wheel moves the window, not the cursor. Moving the cursor instead is why a
long list used to sit still and then jump: nothing happened until the cursor
reached an edge. The cursor follows only when the window would leave it behind,
and on a list short enough to fit, where there is no window to move, the wheel
moves the selection instead: a wheel that does nothing reads as a broken one.

The cursor itself keeps `scrollMargin` rows of context ahead of it, so the list
starts moving before the cursor reaches the edge. Without it the movement all
happens in the last few rows, which is what reads as a list that lurches.
The same goes for text: the side panel and the task panel keep their offset
while output arrives, and follow the newest line only when they are already at
the end, which scrolling away turns off and scrolling back on. Both buffers are
windows on a stream, so once full, a line arriving drops one from the front and
every row shifts up by one. The offset is corrected by the rows dropped, or the
text being read creeps upward exactly when the output is busy enough to matter.

Laying a buffer out costs a pass over every line in it, and a container can
print faster than the screen refreshes. Appending marks the panel dirty and
nothing else; `sync` lays it out once, before something is drawn or a key is
handled. Done per line it is quadratic: ten thousand lines through a five
thousand line buffer took tens of seconds, which is what a "sluggish" panel
actually was.

Wrapped lines are indented by their continuation. Without it a line carrying on
looks exactly like the next one, and scrolling lands on half a sentence with
nothing saying it is half a sentence. Searching has to count rows rather than
lines for the same reason: `rowOf` maps a line to the row it starts on, and a
match found by line number lands further off the more the buffer wraps.

`Row.Full` is a row drawn across the whole width instead of into columns: a
heading the cursor can land on. The Compose view uses it for the project line.
Selected, it is stripped before it is painted, for the same reason a selected
row drops its per-cell colours: a colour left inside resets the background and
the band stops at the first one.

The table budgets lines rather than counting rows: a group heading takes a line
like a row does, so `rowsFitting` decides the window and `View` renders exactly
that. Anything that measures or cuts rendered text goes through
`truncateToWidth`, which counts display width: styled strings carry escape
sequences that plain rune slicing both miscounts and splits.

### 9.2 Keybindings

Convention, applied consistently: **lowercase is safe, uppercase is destructive or
forced.** All bindings live in `internal/ui/keys`, never inline in views, so the
help overlay is generated from the same source.

The tabs run Containers, Compose, Images, Volumes, Networks, Disk: stacks are
how hosts are run, so they come before the loose objects underneath them. The
digit keys follow that order and nothing else depends on it.

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
`R` stop / restart stack, `l` logs of the row, `L` logs of the stack, `+` / `-`
scale selected service, `v` read the compose file, `E` edit it, `c` show
resolved config, `d` check drift, `n` a new or existing stack, `enter` fold,
`D` down, `X` down -v --remove-orphans.

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

The refusal lives where the change would happen, not only in the view that asked
for it: `runCompose` and `openEditor` check it themselves. A guard in every
caller holds only as long as every caller remembers, and the dialog that offers
to fix a compose file after a failure was a caller added later that did not.
Anything new that mutates gets the check at its own choke point.

## 10.5 Untrusted text

Everything the daemon reports is untrusted input, and most of it is written by
whatever runs inside a container: log lines above all, but also names, labels,
image references and error messages. Drawn as they arrive, they can move the
cursor, clear the screen, redraw the interface to say something untrue, set the
window title, or reach the clipboard through OSC.

`components.Sanitize` is the single choke point, and everything drawn goes
through it: the table sanitises in `pad`, the panel on append, the status bar on
the message line. Colour survives, because an SGR sequence cannot address the
terminal; every other escape, and the C0 and C1 controls, do not. Tabs become
spaces so no column can be pushed out of alignment. The tests name the attack
each case prevents.

## 11. Access, and who may use hublot

hublot enforces nothing of its own. The socket's mode is the access control,
exactly as it is for the docker CLI: a user in the group that owns the socket
uses hublot without sudo, anyone else is refused by `open()` before a single
request is sent, and root always gets through. There is no setuid bit, no
capability and no privileged helper, and there must never be one: anything that
can open that socket can already mount the host filesystem into a container, so
a second check here would be theatre.

What the code does add is a diagnosis, in `internal/docker/permission.go`. A
refusal is one of three situations and they need different answers: not a member
of the group, a member whose session predates the membership (the case that
wastes the most time, and the one `newgrp` fixes), or a member who is still
refused, where the group is not the problem. The wording lives in `groupAdvice`,
away from the lookups, so each branch is tested without a socket.

## 12. Error handling

- The daemon unreachable at startup is a clear message, not a stack trace: say
  which socket was tried and that the user may lack permission on it.
- Losing the daemon mid-session degrades: keep displaying the last known state,
  mark it stale, retry in the background.
- A failed action surfaces the daemon's own error message in a modal. Docker's
  errors are usually informative; do not swallow or rewrite them.
- Never `panic` in a `tea.Cmd`: recover and turn it into an error message.

### 12.1 Diagnosing the common failures

Docker's errors are accurate and written for someone who already knows what went
wrong. `driver failed programming external connectivity on endpoint x (64 hex
characters): Bind for 0.0.0.0:8080 failed: port is already allocated` means "that
port is taken", and nothing in it says so.

`internal/state/diagnose.go` recognises the handful of failures that account for
most of them: a host port already bound, a container name already used, an image
that cannot be pulled, a compose variable with no value, a port below 1024, a
full disk. It adds a title and a paragraph saying what to do, and the daemon's
own message stays on screen underneath, never replaced.

Rules:

- It returns false when it recognises nothing. An invented explanation is worse
  than none, so the raw message is shown alone.
- The port case is enriched in the UI with `state.PortHolder`, which names the
  container currently publishing that port. hublot is already looking at every
  container on the host, and this is the answer the user was about to go and
  find by hand.
- Parse the port from an address (`0.0.0.0:8080`, `[::]:8080`, `:::8080`), never
  as "the first number in the message". These errors carry container ids, and
  `starting container 14dde66ee0cc` yields a very convincing `14`.
- New patterns get a test built from a message captured from a real daemon, not
  from one written from memory. Compose and the engine word the same failure
  differently, and both wordings have to match.

A dialog that can do something about the failure does it rather than describing
it. `Modal.Action` is that offer, taken with `e`, and a compose file that will
not parse is the case it was built for: compose names the file, or the project
was built from one, and either way it is a keypress from the editor. What
happens after the write is asked for, never assumed, because the command being
retried can be `down -v`.

## 13. Testing

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

## 14. Build order

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

## 15. Conventions

- Standard Go layout, `internal/` for everything not meant to be imported.
- Errors wrapped with `%w` and context; no bare `err` returns across layers.
- No global variables except the theme.
- A path typed into the interface goes through `state.HomePath`. The shell
  expands `~` and `$VARIABLES` before a program sees its arguments, and a text
  field inside a program gets none of that: left alone, `~/stacks/blog` creates
  a directory actually named `~` wherever hublot happens to be running. Every
  prompt that takes a path does this, not only the compose ones.
- Comments explain why, not what. The traps in section 6 deserve comments pointing
  back to this file.
- `gofmt`, `go vet`, `golangci-lint` clean.
- Keep functions short enough to see whole. Views get large fast; split early.
- No emoji anywhere: code, comments, UI strings, commit messages.
- Commit messages: one line, one sentence, no body, no signature. Never commit or
  push unprompted.

## 16. Definition of done for v1

- Opens on a host with 50+ containers and stays responsive.
- CPU and memory figures match `docker stats` within rounding.
- Every object type listable, inspectable, removable.
- Compose projects grouped, actionable, with drift detection.
- Disk view accurate, prune previews match what actually gets deleted.
- No goroutine leak after 30 minutes of containers starting and stopping.
- Quitting always restores the terminal, including after exec and after a panic.

## 17. Packaging and release

The project ships native packages, not just a `go build`. Three targets, all
driven from the `Makefile` so a release is one command:

- **Debian/Ubuntu `.deb`** and **RPM `.rpm`**: generated from a single
  `packaging/nfpm.yaml` via `nfpm pkg`. Keep the two in sync by construction;
  never hand-write a spec file or a `DEBIAN/control`.
- **Arch `PKGBUILD`** in `packaging/aur/`, building from the released source
  tarball, with `sha256sums` updated per release. `make arch` builds it against
  the working tree instead, uncommitted and untracked files included, since the
  point of building locally is to install what is on disk rather than the last
  release. Only `pkgver` and the `source=` line differ from the published
  recipe, exactly as in CI.
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

## 18. Continuous integration

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

## 19. Local development

```sh
go build ./... && go vet ./... && go test ./...
```

Docker and Compose v2 are available locally for manual checks. `make install-user`
puts the binary in `~/.local/bin`, which comes before `/usr/bin` in PATH and so
takes precedence over an installed package; a shell that already resolved
`hublot` keeps the old path until `hash -r`.

Verifying the interface means driving it, not reading it: a pty plus `pyte`
renders what a terminal would show, which is how the layout, the mouse, the
small-terminal tiers and the failure dialogs are checked. Scripts for that
belong in the scratchpad, not in the repository.
