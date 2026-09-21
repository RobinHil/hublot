package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Networks lists networks with their attached containers.
type Networks struct {
	base
}

// NewNetworks builds the networks view.
func NewNetworks(d Deps) *Networks {
	cols := []components.Column{
		{Title: "NAME", SortKey: "name", MinWidth: 18},
		{Title: "DRIVER", SortKey: "driver", Width: 10},
		{Title: "SCOPE", Width: 7, Priority: 4},
		{Title: "SUBNET", Width: 20, Priority: 3},
		{Title: "ATTACHED", Width: 24, Priority: 1},
		{Title: "PROJECT", SortKey: "project", Width: 14, Priority: 2},
	}

	t := components.NewTable(cols)
	t.Empty = "no networks on this host"

	return &Networks{base: base{deps: d, table: t}}
}

// Title is the tab label.
func (v *Networks) Title() string { return "Networks" }

// Refresh rebuilds the rows.
func (v *Networks) Refresh() {
	ns := state.Filter(v.deps.Store.Networks, v.table.Query(), state.NetworkFields)

	if col, asc := v.table.SortKey(); col != "" {
		state.SortBy(ns, networkLess(col), asc)
	}

	rows := make([]components.Row, 0, len(ns))
	for _, n := range ns {
		rows = append(rows, v.row(n))
	}
	v.table.SetRows(rows)
}

func networkLess(column string) func(a, b docker.Network) bool {
	switch column {
	case "driver":
		return func(a, b docker.Network) bool { return a.Driver < b.Driver }
	case "project":
		return func(a, b docker.Network) bool {
			pa, pb := a.Labels[compose.LabelProject], b.Labels[compose.LabelProject]
			if pa == pb {
				return a.Name < b.Name
			}
			return pa < pb
		}
	default:
		return func(a, b docker.Network) bool { return a.Name < b.Name }
	}
}

func (v *Networks) row(n docker.Network) components.Row {
	s := theme.Current()

	var attached []string
	for _, name := range n.Containers {
		attached = append(attached, name)
	}
	list := strings.Join(attached, ", ")
	if list == "" {
		list = "-"
	}

	name := n.Name
	if n.Predefined() {
		// The three built-ins cannot be removed, which the row should say
		// before the user tries.
		name += " (built-in)"
	}

	return components.Row{
		ID:  n.ID,
		Dim: len(n.Containers) == 0,
		Cells: []components.Cell{
			components.Txt(name),
			components.Txt(n.Driver),
			components.Txt(n.Scope),
			components.Txt(strings.Join(n.Subnets, " ")),
			components.Txt(list),
			components.Styled(n.Labels[compose.LabelProject], s.Accent),
		},
	}
}

// Update handles the network bindings.
func (v *Networks) Update(msg tea.Msg) tea.Cmd {
	if cmd, handled := v.table.Update(msg, v.deps.Keys); handled {
		v.Refresh()
		return cmd
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	k := v.deps.Keys.Objects
	switch {
	case key.Matches(km, k.Detail):
		if n, ok := v.current(); ok {
			return cmds.InspectNetwork(v.deps.Ctx, v.deps.Client, n.ID, n.Name)
		}
	case key.Matches(km, k.Remove), key.Matches(km, k.Force):
		return v.removeConfirm()

	case key.Matches(km, k.Palette):
		return v.palette()
	}
	return nil
}

// palette carries what has no key of its own on a network, chiefly detaching
// a container without going through the containers view.
func (v *Networks) palette() tea.Cmd {
	n, ok := v.current()
	if !ok {
		return nil
	}

	choices := []components.Choice{
		{
			Label: "create a network", Detail: "a user-defined bridge, so names resolve",
			Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "Create a network",
					Body: []string{
						"A bridge network, on which containers find each other by name.",
					},
					Label: "name: ",
					Run: func(newName string) tea.Cmd {
						return cmds.CreateNetwork(v.deps.Ctx, v.deps.Client, newName)
					},
				})
			},
		},
		{
			Label: "inspect", Detail: "raw inspect output, with the attached containers",
			Payload: func() tea.Cmd {
				return cmds.InspectNetwork(v.deps.Ctx, v.deps.Client, n.ID, n.Name)
			},
		},
	}

	for id, name := range n.Containers {
		containerID, containerName := id, name
		choices = append(choices, components.Choice{
			Label: "disconnect " + containerName, Detail: "detach it from " + n.Name,
			Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.DisconnectNetwork(v.deps.Ctx, v.deps.Client, n.ID, containerID)
			},
		})
	}

	for _, c := range v.deps.Store.Containers {
		if _, attached := n.Containers[c.ID]; attached || n.Predefined() {
			continue
		}
		container := c
		choices = append(choices, components.Choice{
			Label: "connect " + container.Name, Detail: "attach it to " + n.Name,
			Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.ConnectNetwork(v.deps.Ctx, v.deps.Client, n.ID, container.ID)
			},
		})
	}

	if !n.Predefined() {
		choices = append(choices, components.Choice{
			Label: "remove", Detail: "only when nothing is attached", Destructive: true,
			Payload: func() tea.Cmd { return v.removeConfirm() },
		})
	}

	if v.deps.ReadOnly {
		choices = keepSafe(choices)
	}
	return request(PickerRequest{Title: "Actions on " + n.Name, Choices: choices})
}

func (v *Networks) current() (docker.Network, bool) {
	id := v.table.CurrentID()
	for _, n := range v.deps.Store.Networks {
		if n.ID == id {
			return n, true
		}
	}
	return docker.Network{}, false
}

func (v *Networks) removeConfirm() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	targets := v.table.Targets()
	if len(targets) == 0 {
		return nil
	}

	byID := map[string]docker.Network{}
	for _, n := range v.deps.Store.Networks {
		byID[n.ID] = n
	}

	var removable []string
	var lines, refused, attached []string

	for _, id := range targets {
		n := byID[id]
		if n.Predefined() {
			refused = append(refused, n.Name)
			continue
		}
		removable = append(removable, id)
		lines = append(lines, "  "+n.Name+"  "+n.Driver)
		if len(n.Containers) > 0 {
			attached = append(attached, fmt.Sprintf("%s (%d container(s))", n.Name, len(n.Containers)))
		}
	}

	if len(removable) == 0 {
		return request(ConfirmRequest{
			Severity: components.SevInfo,
			Title:    "nothing to remove",
			Body: []string{
				"bridge, host and none are created by the daemon and cannot be removed: " +
					strings.Join(refused, ", "),
			},
		})
	}

	body := []string{fmt.Sprintf("These %d network(s) will be removed:", len(removable))}
	body = append(body, lines...)
	if len(refused) > 0 {
		body = append(body, "", "Skipped, built into the daemon: "+strings.Join(refused, ", "))
	}
	if len(attached) > 0 {
		body = append(body, "",
			"Containers are still attached, so the daemon will refuse: "+strings.Join(attached, ", "))
	}

	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    fmt.Sprintf("Remove %d network(s)", len(removable)),
		Body:     body,
		Run: func() tea.Cmd {
			return cmds.RemoveNetworks(v.deps.Ctx, v.deps.Client, removable)
		},
	})
}

// Hints are the footer bindings.
func (v *Networks) Hints() []key.Binding {
	k := v.deps.Keys
	return []key.Binding{
		k.Global.Help, k.Global.Filter, k.Global.Mark, k.Global.Sort,
		k.Objects.Detail, k.Objects.Remove, k.Global.Quit,
	}
}

// Filtering reports whether the filter input has focus.
func (v *Networks) Filtering() bool { return v.table.Filtering() }
