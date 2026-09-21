package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// graphTier is how much room the graphics get, which follows the width of the
// terminal: the figures matter more than the pictures, so the pictures shrink
// first and disappear before a number is ever cut.
type graphTier struct {
	meter, spark   int
	cpuCol, memCol int
}

func tierFor(width int) graphTier {
	switch {
	case width >= 118:
		return graphTier{meter: 6, spark: 8, cpuCol: 13, memCol: 18}
	case width >= 96:
		return graphTier{meter: 5, spark: 5, cpuCol: 12, memCol: 14}
	case width >= 78:
		return graphTier{meter: 4, spark: 0, cpuCol: 11, memCol: 8}
	default:
		return graphTier{meter: 0, spark: 0, cpuCol: 6, memCol: 7}
	}
}

// Containers is the default view: every container with its live stats.
type Containers struct {
	base
}

// NewContainers builds the containers view.
func NewContainers(d Deps) *Containers {
	t := components.NewTable(containerColumns(tierFor(0)))
	t.Empty = "no containers on this host"

	return &Containers{base: base{deps: d, table: t}}
}

// containerColumns builds the column set for a width tier.
func containerColumns(tier graphTier) []components.Column {
	return []components.Column{
		{Title: "NAME", SortKey: "name", MinWidth: 16},
		{Title: "IMAGE", SortKey: "image", MinWidth: 12, Priority: 3},
		{Title: "CPU", SortKey: "cpu", Width: tier.cpuCol, Right: tier.meter == 0},
		{Title: "MEM", SortKey: "mem", Width: tier.memCol, Right: tier.spark == 0},
		{Title: "NET I/O", Width: 14, Right: true, Priority: 4},
		{Title: "STATUS", SortKey: "state", Width: 14, Priority: 1},
		{Title: "PORTS", Width: 14, Priority: 5},
		{Title: "PROJECT", SortKey: "project", Width: 13, Priority: 2},
	}
}

// SetSize re-tiers the graphics before handing the space to the table.
func (v *Containers) SetSize(width, height int) {
	v.table.SetColumns(containerColumns(tierFor(width)))
	v.base.SetSize(width, height)
	v.Refresh()
}

// Title is the tab label.
func (v *Containers) Title() string { return "Containers" }

// Refresh rebuilds rows from the store, applying the active filter and sort.
func (v *Containers) Refresh() {
	cs := state.Filter(v.deps.Store.Containers, v.table.Query(), state.ContainerFields)

	if col, asc := v.table.SortKey(); col != "" {
		if less, ok := v.deps.Store.ContainerLess(col); ok {
			state.SortBy(cs, less, asc)
		}
	}

	rows := make([]components.Row, 0, len(cs))
	for _, c := range cs {
		rows = append(rows, v.row(c))
	}
	v.table.SetRows(rows)
}

// row renders one container, mixing list data with the latest stats sample.
// The graphics carry the reading: a meter for the instant, a sparkline for the
// trend, both tinted by level so a column scans without being read.
func (v *Containers) row(c docker.Container) components.Row {
	s := theme.Current()
	st, hasStats := v.deps.Store.Stats[c.ID]

	tier := tierFor(v.width)

	cpu := components.Cell{Text: formatCPU(st, hasStats)}
	// The bar is drawn for containers doing something. An empty meter on every
	// idle row is texture, not information, and it buries the one row that is
	// actually busy.
	if hasStats && st.CPUValid && tier.meter > 0 && st.CPUPercent >= 1 {
		cpu = components.Cell{
			Text:  components.PlainMeter(st.CPUPercent, tier.meter) + " " + formatCPU(st, hasStats),
			Style: styleOf(theme.LevelStyle(st.CPUPercent)),
		}
	} else if hasStats && st.CPUValid && tier.meter > 0 {
		cpu = components.Cell{
			Text:  strings.Repeat(" ", tier.meter) + " " + formatCPU(st, hasStats),
			Style: styleOf(theme.Current().Dim),
		}
	}

	mem := components.Cell{Text: formatMem(st, hasStats)}
	if hist := v.deps.Store.MemHistory[c.ID]; len(hist) > 0 && tier.spark > 0 {
		mem = components.Cell{
			Text:  components.Sparkline(hist, tier.spark) + " " + formatMem(st, hasStats),
			Style: styleOf(theme.LevelStyle(st.MemPercent)),
		}
	}

	return components.Row{
		ID:  c.ID,
		Dim: !c.Running(),
		Cells: []components.Cell{
			components.Cell{Text: stateDot(c.State) + " " + c.Name},
			components.Txt(c.Image),
			cpu,
			mem,
			components.Styled(formatIO(st.NetRx, st.NetTx), s.Dim),
			components.Styled(formatStatus(c), theme.StateStyle(c.State)),
			components.Styled(formatPorts(c.Ports), s.Accent),
			components.Styled(compose.ProjectOf(c.Labels), s.Dim),
		},
	}
}

// stateDot is the light at the head of every row: colour says running, paused
// or gone before the word does.
func stateDot(state string) string {
	return theme.StateStyle(state).Render("●")
}

// styleOf adapts a style for a cell, which keeps its own pointer.
func styleOf(st lipgloss.Style) *lipgloss.Style { return &st }

// Update handles the view's own bindings once the table has had its turn.
func (v *Containers) Update(msg tea.Msg) tea.Cmd {
	if cmd, handled := v.table.Update(msg, v.deps.Keys); handled {
		v.Refresh()
		return cmd
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	k := v.deps.Keys.Containers
	current, hasCurrent := v.current()

	switch {
	case key.Matches(km, k.Detail):
		if !hasCurrent {
			return nil
		}
		return request(DetailRequest{
			Title:  "container " + current.Name,
			Source: current.ID,
			Load: func() tea.Cmd {
				return cmds.InspectContainer(v.deps.Ctx, v.deps.Client, current.ID, current.Name)
			},
		})

	case key.Matches(km, k.Logs):
		if !hasCurrent {
			return nil
		}
		return request(LogsRequest{ContainerID: current.ID, Name: current.Name})

	case key.Matches(km, k.Exec):
		if !hasCurrent {
			return nil
		}
		if v.deps.ReadOnly {
			return denied()
		}
		if !current.Running() {
			return v.info("no shell to open", []string{
				fmt.Sprintf("%s is %s: a shell can only be opened in a running container.", current.Name, current.State),
			})
		}
		return request(ExecRequest{ContainerID: current.ID, Name: current.Name})

	case key.Matches(km, k.Start):
		return v.reversible("start", func(ids []string) tea.Cmd {
			return cmds.StartContainers(v.deps.Ctx, v.deps.Client, ids)
		})

	case key.Matches(km, k.New):
		if v.deps.ReadOnly {
			return denied()
		}
		return request(RunFormRequest(v.deps, ""))

	case key.Matches(km, k.Stop):
		return v.reversible("stop", func(ids []string) tea.Cmd {
			return cmds.StopContainers(v.deps.Ctx, v.deps.Client, ids, v.deps.StopTimeout)
		})

	case key.Matches(km, k.Restart):
		return v.reversible("restart", func(ids []string) tea.Cmd {
			return cmds.RestartContainers(v.deps.Ctx, v.deps.Client, ids, v.deps.StopTimeout)
		})

	case key.Matches(km, k.Pause):
		if !hasCurrent {
			return nil
		}
		if v.deps.ReadOnly {
			return denied()
		}
		return cmds.PauseContainer(v.deps.Ctx, v.deps.Client, current.ID, current.State == docker.StatePaused)

	case key.Matches(km, k.Kill):
		return v.killPicker()

	case key.Matches(km, k.Remove):
		return v.removeConfirm()

	case key.Matches(km, k.Palette):
		return v.palette()
	}

	return nil
}

// current is the container under the cursor.
func (v *Containers) current() (docker.Container, bool) {
	id := v.table.CurrentID()
	if id == "" {
		return docker.Container{}, false
	}
	return v.deps.Store.ContainerByID(id)
}

// targets resolves what an action applies to: the marked rows, or the cursor.
func (v *Containers) targets() []docker.Container {
	var out []docker.Container
	for _, id := range v.table.Targets() {
		if c, ok := v.deps.Store.ContainerByID(id); ok {
			out = append(out, c)
		}
	}
	return out
}

func ids(cs []docker.Container) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func names(cs []docker.Container) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// reversible runs an action that needs no confirmation at all: stopping and
// restarting are undoable (AGENTS.md section 10.1).
func (v *Containers) reversible(label string, run func([]string) tea.Cmd) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	targets := v.targets()
	if len(targets) == 0 {
		return nil
	}
	_ = label
	return run(ids(targets))
}

// removeConfirm grades the dialog by how much is being destroyed.
func (v *Containers) removeConfirm() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	targets := v.targets()
	if len(targets) == 0 {
		return nil
	}

	var running []string
	for _, c := range targets {
		if c.Running() {
			running = append(running, c.Name)
		}
	}
	force := len(running) > 0

	body := []string{fmt.Sprintf("These %d container(s) will be removed:", len(targets))}
	body = append(body, "  "+strings.Join(names(targets), ", "))
	if force {
		body = append(body,
			"",
			fmt.Sprintf("%d of them are running and will be killed first: %s",
				len(running), strings.Join(running, ", ")))
	}
	if p := composeProjects(targets); len(p) > 0 {
		body = append(body, "",
			"They belong to compose project(s) "+strings.Join(p, ", ")+
				", which will recreate them on the next up.")
	}

	sev := components.SevConfirm
	if len(targets) > 1 || force {
		sev = components.SevDanger
	}

	return request(ConfirmRequest{
		Severity: sev,
		Title:    fmt.Sprintf("Remove %d container(s)", len(targets)),
		Body:     body,
		Run: func() tea.Cmd {
			return cmds.RemoveContainers(v.deps.Ctx, v.deps.Client, ids(targets), force, false)
		},
	})
}

// composeProjects lists the distinct Compose projects a selection touches.
func composeProjects(cs []docker.Container) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cs {
		p := compose.ProjectOf(c.Labels)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// killPicker offers the signals worth having, since kill without a choice is
// rarely what the user wants.
func (v *Containers) killPicker() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	targets := v.targets()
	if len(targets) == 0 {
		return nil
	}

	signals := []struct{ name, detail string }{
		{"SIGTERM", "ask the process to stop"},
		{"SIGKILL", "cannot be caught or ignored"},
		{"SIGINT", "as if ctrl+c had been pressed"},
		{"SIGHUP", "reload configuration, for most daemons"},
		{"SIGQUIT", "stop and dump core"},
		{"SIGUSR1", "application defined"},
		{"SIGUSR2", "application defined"},
	}

	choices := make([]components.Choice, 0, len(signals))
	for _, s := range signals {
		sig := s.name
		choices = append(choices, components.Choice{
			Label:       sig,
			Detail:      s.detail,
			Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.KillContainers(v.deps.Ctx, v.deps.Client, ids(targets), sig)
			},
		})
	}

	return request(PickerRequest{
		Title:   fmt.Sprintf("Signal to send to %s", strings.Join(names(targets), ", ")),
		Choices: choices,
	})
}

// palette is the `x` escape valve: everything too rare for its own key.
func (v *Containers) palette() tea.Cmd {
	current, ok := v.current()
	if !ok {
		return nil
	}
	targets := v.targets()

	pauseLabel, pauseDetail := "pause", "freeze the container, leaving it in memory"
	if current.State == docker.StatePaused {
		pauseLabel = "unpause"
		pauseDetail = "let it run again: a paused container cannot be started, only unpaused"
	}

	choices := []components.Choice{
		{
			Label: "start", Detail: "start the selected container(s)", Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.StartContainers(v.deps.Ctx, v.deps.Client, ids(targets))
			},
		},
		{
			Label: pauseLabel, Detail: pauseDetail, Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.PauseContainer(v.deps.Ctx, v.deps.Client, current.ID,
					current.State == docker.StatePaused)
			},
		},
		{
			Label: "inspect", Detail: "raw inspect output",
			Payload: func() tea.Cmd {
				return cmds.InspectContainer(v.deps.Ctx, v.deps.Client, current.ID, current.Name)
			},
		},
		{
			Label: "remove with volumes", Detail: "also drops anonymous volumes", Destructive: true,
			Payload: func() tea.Cmd {
				return request(ConfirmRequest{
					Severity: components.SevDanger,
					Title:    "Remove containers and their anonymous volumes",
					Body: []string{
						strings.Join(names(targets), ", "),
						"",
						"Anonymous volumes attached to them are destroyed with them. Named volumes are left alone.",
					},
					Run: func() tea.Cmd {
						return cmds.RemoveContainers(v.deps.Ctx, v.deps.Client, ids(targets), true, true)
					},
				})
			},
		},
	}

	choices = append(choices,
		components.Choice{
			Label: "processes", Detail: "what is running inside, as docker top shows it",
			Payload: func() tea.Cmd {
				return cmds.Processes(v.deps.Ctx, v.deps.Client, current.ID, current.Name)
			},
		},
		components.Choice{
			Label: "filesystem changes", Detail: "what has been written since it started",
			Payload: func() tea.Cmd {
				return cmds.Changes(v.deps.Ctx, v.deps.Client, current.ID, current.Name)
			},
		},
		components.Choice{
			Label: "commit to an image", Detail: "freeze the filesystem as a new image",
			Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "Commit " + current.Name,
					Body: []string{
						"The container is paused while its filesystem is captured.",
						"Volumes are not part of the image.",
					},
					Label:   "reference: ",
					Initial: current.Name + ":committed",
					Run: func(ref string) tea.Cmd {
						return cmds.Commit(v.deps.Ctx, v.deps.Client, current.ID, current.Name, ref)
					},
				})
			},
		},
		components.Choice{
			Label: "export the filesystem", Detail: "a tarball of the whole container",
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title:   "Export " + current.Name,
					Body:    []string{"A flat tarball of the filesystem, without the image history."},
					Label:   "path: ",
					Initial: "./" + current.Name + ".tar",
					Run: func(path string) tea.Cmd {
						return cmds.Export(v.deps.Ctx, v.deps.Client, current.ID, current.Name,
							state.HomePath(path))
					},
				})
			},
		},
		components.Choice{
			Label: "rename", Detail: "give this container another name", Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title:   "Rename " + current.Name,
					Body:    []string{"Names must be unique on the host."},
					Label:   "name: ",
					Initial: current.Name,
					Run: func(name string) tea.Cmd {
						return cmds.RenameContainer(v.deps.Ctx, v.deps.Client, current.ID, name)
					},
				})
			},
		},
		components.Choice{
			Label: "set cpu limit", Detail: "cores, as docker update --cpus takes them",
			Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "CPU limit for " + current.Name,
					Body: []string{
						"A decimal number of cores, such as 1.5. Zero removes the limit.",
					},
					// The list carries no limit to prefill with: it lives in
					// the inspect output, which the detail pane shows.
					Label: "cpus: ",
					Run: func(value string) tea.Cmd {
						cpus, err := state.ParseCPUs(value)
						if err != nil {
							return v.info("that is not a cpu allowance", []string{err.Error()})
						}
						return cmds.UpdateLimits(v.deps.Ctx, v.deps.Client, current.ID, cpus, 0)
					},
				})
			},
		},
		components.Choice{
			Label: "set memory limit", Detail: "such as 512m, zero removes it",
			Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "Memory limit for " + current.Name,
					Body: []string{
						"A size with an optional unit, such as 512m or 2g. Zero removes the limit.",
					},
					Label:   "memory: ",
					Initial: "0",
					Run: func(value string) tea.Cmd {
						memory, err := state.ParseBytes(value)
						if err != nil {
							return v.info("that is not a size", []string{err.Error()})
						}
						return cmds.UpdateLimits(v.deps.Ctx, v.deps.Client, current.ID, 0, memory)
					},
				})
			},
		},
		components.Choice{
			Label: "copy a file in", Detail: "local path, then where it goes inside",
			Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "Copy into " + current.Name,
					Body: []string{
						"Local path first, then the destination inside the container,",
						"separated by a space. A trailing slash means a directory.",
						"",
						"example:  ./nginx.conf /etc/nginx/conf.d/",
					},
					Label: "paths: ",
					Run: func(value string) tea.Cmd {
						local, remote, ok := splitPair(value)
						if !ok {
							return v.info("two paths are needed", []string{
								"Give the local path and the destination inside the container, separated by a space.",
							})
						}
						return cmds.CopyIntoContainer(v.deps.Ctx, v.deps.Client, current.ID,
							state.HomePath(local), remote)
					},
				})
			},
		},
		components.Choice{
			Label: "copy a file out", Detail: "path inside, then a local directory",
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "Copy out of " + current.Name,
					Body: []string{
						"The path inside the container, then the local directory to write it to,",
						"separated by a space.",
						"",
						"example:  /var/log/nginx .",
					},
					Label: "paths: ",
					Run: func(value string) tea.Cmd {
						remote, localDir, ok := splitPair(value)
						if !ok {
							return v.info("two paths are needed", []string{
								"Give the path inside the container and a local directory, separated by a space.",
							})
						}
						return cmds.CopyOutOfContainer(v.deps.Ctx, v.deps.Client, current.ID,
							remote, state.HomePath(localDir))
					},
				})
			},
		},
	)

	for _, n := range current.Networks {
		network := n
		choices = append(choices, components.Choice{
			Label: "disconnect from " + network, Detail: "detach this container from the network",
			Destructive: true,
			Payload: func() tea.Cmd {
				id := v.networkID(network)
				if id == "" {
					return nil
				}
				return cmds.DisconnectNetwork(v.deps.Ctx, v.deps.Client, id, current.ID)
			},
		})
	}

	for _, n := range v.deps.Store.Networks {
		if containsString(current.Networks, n.Name) {
			continue
		}
		network := n
		choices = append(choices, components.Choice{
			Label: "connect to " + network.Name, Detail: network.Driver, Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.ConnectNetwork(v.deps.Ctx, v.deps.Client, network.ID, current.ID)
			},
		})
	}

	if v.deps.ReadOnly {
		choices = keepSafe(choices)
	}

	return request(PickerRequest{Title: "Actions on " + current.Name, Choices: choices})
}

// keepSafe drops the mutating entries in a read-only session.
func keepSafe(choices []components.Choice) []components.Choice {
	out := make([]components.Choice, 0, len(choices))
	for _, c := range choices {
		if !c.Destructive {
			out = append(out, c)
		}
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// splitPair reads the two paths a copy needs out of one line.
func splitPair(value string) (string, string, bool) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

func (v *Containers) networkID(name string) string {
	for _, n := range v.deps.Store.Networks {
		if n.Name == name {
			return n.ID
		}
	}
	return ""
}

func (v *Containers) info(title string, body []string) tea.Cmd {
	return request(ConfirmRequest{Severity: components.SevInfo, Title: title, Body: body})
}

// Hints are the footer bindings.
func (v *Containers) Hints() []key.Binding {
	k := v.deps.Keys
	// Ordered by what gets used, because the bar drops what does not fit: the
	// actions come before the niceties.
	return []key.Binding{
		k.Containers.Logs, k.Containers.Detail, k.Containers.Start,
		k.Containers.Stop, k.Containers.Restart, v.pauseHint(),
		k.Containers.Remove, k.Containers.Exec, k.Containers.New,
		k.Containers.Palette, k.Global.Filter, k.Global.Help, k.Global.Quit,
	}
}

// pauseHint says which half of the toggle the row under the cursor needs. A
// paused container cannot be started, only unpaused, and a bar that says
// "pause" in front of one is worse than saying nothing.
func (v *Containers) pauseHint() key.Binding {
	binding := v.deps.Keys.Containers.Pause
	label := "pause"
	if current, ok := v.current(); ok && current.State == docker.StatePaused {
		label = "unpause"
	}
	return key.NewBinding(
		key.WithKeys(binding.Keys()...),
		key.WithHelp(binding.Help().Key, label),
	)
}

// VisibleIDs are the containers currently on screen. Above the stats stream
// cap, these are the ones worth streaming (AGENTS.md section 6.3).
func (v *Containers) VisibleIDs() []string { return v.table.VisibleIDs() }

// Filtering reports whether the filter input has focus, so the app leaves
// global keys alone while text is being typed.
func (v *Containers) Filtering() bool { return v.table.Filtering() }

// Summary says what the dots in the header cannot: how the containers group
// into stacks, and whether any of them no longer match their file.
func (v *Containers) Summary() string {
	projects := ""
	if n := len(v.deps.Store.Projects); n > 0 {
		projects = plural(n, "compose project")
	}
	drift := ""
	if n := v.deps.Store.DriftedProjects(); n > 0 {
		drift = fmt.Sprintf("%d drifted", n)
	}

	loose := 0
	for _, c := range v.deps.Store.Containers {
		if compose.ProjectOf(c.Labels) == "" {
			loose++
		}
	}
	unmanaged := ""
	if loose > 0 && projects != "" {
		unmanaged = fmt.Sprintf("%d outside a project", loose)
	}

	return summaryOf(
		plural(len(v.deps.Store.Containers), "container"),
		projects,
		unmanaged,
		drift,
	)
}
