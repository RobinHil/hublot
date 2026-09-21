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

// Volumes lists volumes with what mounts them. Removing one destroys data, so
// the view leans on showing usage rather than on confirmation alone.
type Volumes struct {
	base
}

// NewVolumes builds the volumes view.
func NewVolumes(d Deps) *Volumes {
	cols := []components.Column{
		{Title: "NAME", SortKey: "name", MinWidth: 20},
		{Title: "DRIVER", Width: 8, Priority: 4},
		{Title: "SIZE", SortKey: "size", Width: 10, Right: true},
		{Title: "MOUNTED BY", Width: 22, Priority: 1},
		{Title: "PROJECT", SortKey: "project", Width: 14, Priority: 2},
		{Title: "CREATED", SortKey: "created", Width: 9, Right: true, Priority: 3},
	}

	t := components.NewTable(cols)
	t.Empty = "no volumes on this host"

	return &Volumes{base: base{deps: d, table: t}}
}

// Title is the tab label.
func (v *Volumes) Title() string { return "Volumes" }

// Refresh rebuilds the rows.
func (v *Volumes) Refresh() {
	vs := state.Filter(v.deps.Store.Volumes, v.table.Query(), state.VolumeFields)

	if col, asc := v.table.SortKey(); col != "" {
		state.SortBy(vs, volumeLess(col), asc)
	}

	users := v.mounters()
	rows := make([]components.Row, 0, len(vs))
	for _, vol := range vs {
		rows = append(rows, v.row(vol, users[vol.Name]))
	}
	v.table.SetRows(rows)
}

func volumeLess(column string) func(a, b docker.Volume) bool {
	switch column {
	case "size":
		return func(a, b docker.Volume) bool { return a.Size < b.Size }
	case "created":
		return func(a, b docker.Volume) bool { return a.CreatedAt.Before(b.CreatedAt) }
	case "project":
		return func(a, b docker.Volume) bool {
			pa, pb := a.Labels[compose.LabelProject], b.Labels[compose.LabelProject]
			if pa == pb {
				return a.Name < b.Name
			}
			return pa < pb
		}
	default:
		return func(a, b docker.Volume) bool { return a.Name < b.Name }
	}
}

// mounters maps a volume to the containers mounting it, which is what makes an
// unused volume recognisable.
func (v *Volumes) mounters() map[string][]string {
	out := map[string][]string{}
	for _, c := range v.deps.Store.Containers {
		for _, m := range c.Mounts {
			if m.Name != "" {
				out[m.Name] = append(out[m.Name], c.Name)
			}
		}
	}
	return out
}

func (v *Volumes) row(vol docker.Volume, users []string) components.Row {
	s := theme.Current()

	name := vol.Name
	if vol.Anonymous {
		name = truncateID(vol.Name) + " (anonymous)"
	}

	mounted := strings.Join(users, ", ")
	if mounted == "" {
		mounted = "-"
	}

	return components.Row{
		ID:  vol.Name,
		Dim: len(users) == 0,
		Cells: []components.Cell{
			components.Txt(name),
			components.Txt(vol.Driver),
			components.Txt(state.FormatBytes(vol.Size)),
			components.Txt(mounted),
			components.Styled(vol.Labels[compose.LabelProject], s.Accent),
			components.Txt(formatAge(vol.CreatedAt)),
		},
	}
}

// Update handles the volume bindings.
func (v *Volumes) Update(msg tea.Msg) tea.Cmd {
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
		if name := v.table.CurrentID(); name != "" {
			return cmds.InspectVolume(v.deps.Ctx, v.deps.Client, name)
		}
	case key.Matches(km, k.Remove):
		return v.removeConfirm(false)
	case key.Matches(km, k.Force):
		return v.removeConfirm(true)

	case key.Matches(km, k.Palette):
		return v.palette()
	}
	return nil
}

// palette carries what has no key of its own on a volume.
func (v *Volumes) palette() tea.Cmd {
	name := v.table.CurrentID()
	if name == "" {
		return nil
	}

	var vol docker.Volume
	for _, candidate := range v.deps.Store.Volumes {
		if candidate.Name == name {
			vol = candidate
			break
		}
	}

	choices := []components.Choice{
		{
			Label: "create a volume", Detail: "a named volume with the default driver",
			Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "Create a volume",
					Body:  []string{"Named volumes survive the containers that mount them."},
					Label: "name: ",
					Run: func(newName string) tea.Cmd {
						return cmds.CreateVolume(v.deps.Ctx, v.deps.Client, newName)
					},
				})
			},
		},
		{
			Label: "inspect", Detail: "raw inspect output",
			Payload: func() tea.Cmd {
				return cmds.InspectVolume(v.deps.Ctx, v.deps.Client, name)
			},
		},
		{
			Label: "show the mountpoint", Detail: vol.Mountpoint,
			Payload: func() tea.Cmd {
				return request(ConfirmRequest{
					Severity: components.SevInfo,
					Title:    name,
					Body: []string{
						"On-disk path, readable as root:",
						"",
						"  " + vol.Mountpoint,
						"",
						"Driver: " + vol.Driver + "    Size: " + state.FormatBytes(vol.Size),
					},
				})
			},
		},
		{
			Label: "remove", Detail: "destroys the data it holds", Destructive: true,
			Payload: func() tea.Cmd { return v.removeConfirm(false) },
		},
	}

	if v.deps.ReadOnly {
		choices = keepSafe(choices)
	}
	return request(PickerRequest{Title: "Actions on " + name, Choices: choices})
}

// removeConfirm names the data at stake: a volume removal is the one action in
// the object views with no way back.
func (v *Volumes) removeConfirm(force bool) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	targets := v.table.Targets()
	if len(targets) == 0 {
		return nil
	}

	users := v.mounters()
	byName := map[string]docker.Volume{}
	for _, vol := range v.deps.Store.Volumes {
		byName[vol.Name] = vol
	}

	var total int64
	var lines, mounted, projects []string
	seenProject := map[string]bool{}

	for _, name := range targets {
		vol := byName[name]
		if vol.Size > 0 {
			total += vol.Size
		}
		line := "  " + name
		if vol.Size >= 0 {
			line += "  " + state.FormatBytes(vol.Size)
		} else {
			line += "  size unknown until the disk view is refreshed"
		}
		lines = append(lines, line)

		if u := users[name]; len(u) > 0 {
			mounted = append(mounted, fmt.Sprintf("%s (%s)", name, strings.Join(u, ", ")))
		}
		if p := vol.Labels[compose.LabelProject]; p != "" && !seenProject[p] {
			seenProject[p] = true
			projects = append(projects, p)
		}
	}

	body := []string{fmt.Sprintf(
		"These %d volume(s) and everything inside them will be destroyed, freeing %s:",
		len(targets), state.FormatBytes(total))}
	body = append(body, lines...)

	if len(projects) > 0 {
		body = append(body, "",
			"They hold the data of compose project(s) "+strings.Join(projects, ", ")+".")
	}
	if len(mounted) > 0 {
		body = append(body, "",
			"Still mounted by: "+strings.Join(mounted, ", "))
		if !force {
			body = append(body, "The daemon will refuse while a container holds them.")
		}
	}
	body = append(body, "", "There is no undo.")

	return request(ConfirmRequest{
		Severity: components.SevDanger,
		Title:    fmt.Sprintf("Destroy %d volume(s)", len(targets)),
		Body:     body,
		Run: func() tea.Cmd {
			return cmds.RemoveVolumes(v.deps.Ctx, v.deps.Client, targets, force)
		},
	})
}

// Hints are the footer bindings.
func (v *Volumes) Hints() []key.Binding {
	k := v.deps.Keys
	return []key.Binding{
		k.Global.Help, k.Global.Filter, k.Global.Mark, k.Global.Sort,
		k.Objects.Detail, k.Objects.Remove, k.Objects.Force, k.Global.Quit,
	}
}

// Filtering reports whether the filter input has focus.
func (v *Volumes) Filtering() bool { return v.table.Filtering() }
