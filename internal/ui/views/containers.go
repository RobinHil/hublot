package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// sparkWidth is how many samples of history the memory column shows.
const sparkWidth = 8

// Containers is the default view: every container with its live stats.
type Containers struct {
	base
	// detail holds inspect output when the pane is open.
	detail     viewport.Model
	detailOpen bool
	detailFor  string
}

// NewContainers builds the containers view.
func NewContainers(d Deps) *Containers {
	cols := []components.Column{
		{Title: "NAME", SortKey: "name", MinWidth: 14},
		{Title: "IMAGE", SortKey: "image", MinWidth: 12, Priority: 3},
		{Title: "CPU", SortKey: "cpu", Width: 11, Right: true},
		{Title: "MEM", SortKey: "mem", Width: 18, Right: true},
		{Title: "NET I/O", Width: 15, Right: true, Priority: 4},
		{Title: "STATUS", SortKey: "state", Width: 18, Priority: 1},
		{Title: "PORTS", Width: 16, Priority: 5},
		{Title: "PROJECT", SortKey: "project", Width: 14, Priority: 2},
	}

	t := components.NewTable(cols)
	t.Empty = "no containers on this host"

	v := &Containers{base: base{deps: d, table: t}}
	v.detail = viewport.New(80, 10)
	return v
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
func (v *Containers) row(c docker.Container) components.Row {
	s := theme.Current()
	st, hasStats := v.deps.Store.Stats[c.ID]

	cpu := formatCPU(st, hasStats)
	if hasStats && st.CPUValid {
		cpu = components.Bar(st.CPUPercent, 5) + " " + cpu
	}

	mem := formatMem(st, hasStats)
	if hist := v.deps.Store.MemHistory[c.ID]; len(hist) > 0 {
		mem = components.Sparkline(hist, sparkWidth) + " " + mem
	}

	return components.Row{
		ID:  c.ID,
		Dim: !c.Running(),
		Cells: []components.Cell{
			components.Txt(c.Name),
			components.Txt(c.Image),
			components.Txt(cpu),
			components.Txt(mem),
			components.Txt(formatIO(st.NetRx, st.NetTx)),
			components.Styled(c.Status, theme.StateStyle(c.State)),
			components.Txt(formatPorts(c.Ports)),
			components.Styled(compose.ProjectOf(c.Labels), s.Accent),
		},
	}
}

// SetSize splits the area between the table and the detail pane.
func (v *Containers) SetSize(width, height int) {
	v.width, v.height = width, height
	tableHeight := height
	if v.detailOpen {
		tableHeight = height / 2
		v.detail.Width = width
		v.detail.Height = height - tableHeight - 1
	}
	v.table.SetSize(width, tableHeight)
}

// Update handles the view's own bindings once the table has had its turn.
func (v *Containers) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(cmds.InspectMsg); ok && strings.HasPrefix(m.Title, "container ") {
		if m.Err == nil {
			v.detail.SetContent(m.Content)
			v.detail.GotoTop()
		}
		return nil
	}

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
		return v.toggleDetail()

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

	if v.detailOpen {
		var cmd tea.Cmd
		v.detail, cmd = v.detail.Update(msg)
		return cmd
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

	choices := []components.Choice{
		{
			Label: "start", Detail: "start the selected container(s)", Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.StartContainers(v.deps.Ctx, v.deps.Client, ids(targets))
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
						return cmds.Export(v.deps.Ctx, v.deps.Client, current.ID, current.Name, path)
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
						return cmds.CopyIntoContainer(v.deps.Ctx, v.deps.Client, current.ID, local, remote)
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
						return cmds.CopyOutOfContainer(v.deps.Ctx, v.deps.Client, current.ID, remote, localDir)
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

// toggleDetail opens or closes the inspect pane.
func (v *Containers) toggleDetail() tea.Cmd {
	current, ok := v.current()
	if !ok {
		return nil
	}

	if v.detailOpen && v.detailFor == current.ID {
		v.detailOpen = false
		v.SetSize(v.width, v.height)
		return nil
	}

	v.detailOpen = true
	v.detailFor = current.ID
	v.detail.SetContent("loading...")
	v.SetSize(v.width, v.height)
	return cmds.InspectContainer(v.deps.Ctx, v.deps.Client, current.ID, current.Name)
}

func (v *Containers) info(title string, body []string) tea.Cmd {
	return request(ConfirmRequest{Severity: components.SevInfo, Title: title, Body: body})
}

// View renders the table, plus the detail pane when it is open.
func (v *Containers) View() string {
	if !v.detailOpen {
		return v.table.View()
	}
	s := theme.Current()
	return v.table.View() + "\n" +
		s.Dim.Render(strings.Repeat("-", v.width)) + "\n" +
		v.detail.View()
}

// Hints are the footer bindings.
func (v *Containers) Hints() []key.Binding {
	k := v.deps.Keys
	return []key.Binding{
		k.Global.Help, k.Global.Filter, k.Global.Mark,
		k.Containers.Detail, k.Containers.Logs, k.Containers.Exec,
		k.Containers.Stop, k.Containers.Restart, k.Containers.Remove,
		k.Containers.Palette, k.Global.Quit,
	}
}

// VisibleIDs are the containers currently on screen. Above the stats stream
// cap, these are the ones worth streaming (AGENTS.md section 6.3).
func (v *Containers) VisibleIDs() []string { return v.table.VisibleIDs() }

// Filtering reports whether the filter input has focus, so the app leaves
// global keys alone while text is being typed.
func (v *Containers) Filtering() bool { return v.table.Filtering() }
