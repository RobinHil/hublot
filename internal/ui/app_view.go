package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// View renders the whole screen: status bar, tabs, the active view beside or
// above the panel, the message line and the key hints, with any overlay on top.
func (a *App) View() string {
	if a.quitting {
		return ""
	}

	l := a.geometry()
	if l.TooSmall {
		return a.tooSmall(l)
	}

	body := a.body(l)

	if a.helpOpen {
		overlay, maxOffset := components.HelpOverlay(
			a.keys, a.store.ReadOnly, a.width, a.height, a.helpOffset)
		a.helpMax = maxOffset
		return components.Overlay(body, overlay, a.width, a.height)
	}
	if a.form != nil {
		return components.Overlay(body, a.form.View(), a.width, a.height)
	}
	if a.prompt != nil {
		return components.Overlay(body, a.prompt.View(), a.width, a.height)
	}
	if a.picker != nil {
		return components.Overlay(body, a.picker.View(), a.width, a.height)
	}
	if a.modal != nil {
		return components.Overlay(body, a.modal.View(), a.width, a.height)
	}
	return body
}

// tooSmall says what is wrong rather than drawing a mangled table, and says it
// in whatever room is left.
func (a *App) tooSmall(l Layout) string {
	s := theme.Current()
	return s.Warning.Render(fmt.Sprintf("%dx%d is too small", l.Width, l.Height)) + "\n" +
		s.Dim.Render(fmt.Sprintf("hublot needs %dx%d", minWidth, minHeight))
}

// body stacks the fixed rows around the content and fills the terminal exactly.
func (a *App) body(l Layout) string {
	rows := components.HeaderLines(a.dashboard(l), l.Width, l.HeaderRows)

	if l.HeaderGap {
		rows = append(rows, "")
	}
	if l.ShowTabs {
		rows = append(rows, components.Tabs(a.titles(), a.active, l.Width, l.Compact))
	}
	if l.HeaderGap {
		rows = append(rows, "")
	}

	rows = append(rows, fitLines(a.content(l), l.Height-len(rows)-1-boolToInt(l.ShowMessage))...)

	if l.ShowMessage {
		rows = append(rows, components.FooterMessage(a.message, a.messageErr))
	}
	rows = append(rows, components.HintBar(a.hints(), l.Width))

	return strings.Join(rows, "\n")
}

// content is the middle of the screen: the tasks panel, or the active view
// with the side panel beside it, under it, or instead of it.
func (a *App) content(l Layout) string {
	if a.tasks.Visible {
		return a.tasks.View()
	}

	view := a.views[a.active].View()

	switch {
	case !l.PanelOpen:
		return view

	case l.ListHidden:
		return a.panel.View()

	case l.PanelStacked:
		return strings.Join(append(
			fitLines(view, l.ListHeight),
			fitLines(a.panel.View(), l.PanelHeight)...,
		), "\n")

	default:
		left := lipgloss.NewStyle().Width(l.ListWidth).Height(l.PanelHeight).
			MaxWidth(l.ListWidth).Render(strings.Join(fitLines(view, l.PanelHeight), "\n"))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, a.panel.View())
	}
}

// hints are the keys worth showing, which depend on where the focus is.
func (a *App) hints() []key.Binding {
	if a.tasks.Visible {
		return []key.Binding{a.keys.Global.Tasks, a.keys.Global.Help, a.keys.Global.Quit}
	}
	if a.panel.Focused() {
		return []key.Binding{
			a.keys.Panel.Focus, a.keys.Panel.Search, a.keys.Panel.Follow,
			a.keys.Panel.Width, a.keys.Panel.Close, a.keys.Global.Quit,
		}
	}

	hints := a.views[a.active].Hints()
	if a.panel.IsOpen() {
		hints = append([]key.Binding{a.keys.Panel.Focus, a.keys.Panel.Width}, hints...)
	}
	return hints
}

func (a *App) titles() []string {
	out := make([]string, 0, len(a.views))
	for _, v := range a.views {
		out = append(out, v.Title())
	}
	return out
}

// dashboard assembles what the header shows. The figures come from the store
// rather than being accumulated here, so a container that goes away takes its
// share with it.
func (a *App) dashboard(l Layout) components.Dashboard {
	cpu, mem := a.store.Totals()
	running, paused, stopped := a.store.StateCounts()

	d := components.Dashboard{
		Version:    a.store.Info.ServerVersion,
		View:       a.views[a.active].Title(),
		CPUPercent: cpu,
		CPUHistory: a.store.HostCPU,
		MemUsage:   mem,
		MemTotal:   a.store.Info.MemTotal,
		MemHistory: a.store.HostMem,
		Running:    running,
		Paused:     paused,
		Stopped:    stopped,
		Projects:   len(a.store.Projects),
		Drifted:    a.store.DriftedProjects(),
		Stale:      a.store.Stale,
		ReadOnly:   a.store.ReadOnly,
		Marked:     a.views[a.active].Marked(),
		Position:   a.views[a.active].Position(),
		Tasks:      a.tasks.Running(),
		TaskFailed: a.tasks.HasFailure(),
		Summary:    a.views[a.active].Summary(),
	}
	if a.panel.IsOpen() && !l.Compact {
		d.Panel = a.panelShare.Label()
	}
	return d
}

// fitLines pads a pane with blank lines, or cuts it, so it is exactly n lines
// tall. Cutting is a backstop: a pane that overflows would otherwise push the
// hint bar off the bottom of the screen.
func fitLines(pane string, n int) []string {
	if n < 0 {
		n = 0
	}
	lines := strings.Split(pane, "\n")
	if len(lines) > n {
		return lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// formatBytes is the state layer's formatter, re-exported so the ui package
// does not grow a second one.
func formatBytes(n int64) string { return state.FormatBytes(n) }
