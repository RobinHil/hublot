package ui

import (
	"fmt"
	"strings"

	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/components"
)

// View renders the whole screen: tab bar, status bar, body, footer, and any
// overlay on top.
func (a *App) View() string {
	if a.quitting {
		return ""
	}
	if a.width > 0 && a.width < minWidth {
		// Below the minimum, say so rather than rendering a mangled table
		// (AGENTS.md section 9.1).
		return fmt.Sprintf("hublot needs at least %d columns, this terminal has %d.\n",
			minWidth, a.width)
	}

	body := a.body()

	if a.helpOpen {
		overlay, maxOffset := components.HelpOverlay(
			a.keys, a.store.ReadOnly, a.width, a.height, a.helpOffset)
		a.helpMax = maxOffset
		return components.Overlay(body, overlay, a.width, a.height)
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

// body renders the frame around whichever pane currently has the screen.
//
// The frame occupies the terminal exactly: the status bar on the first line,
// the key hints on the last, and the pane stretched to fill everything between
// them. Sections are placed rather than concatenated, so the hints sit at the
// bottom of the window whether the list has three rows or three hundred.
func (a *App) body() string {
	header := components.StatusBar(a.status(), a.width)
	message := components.FooterMessage(a.status())

	switch {
	case a.logsOpen:
		return a.frame(header, a.logs.View(), message,
			components.HintBar(a.logs.Hints(), a.width))

	case a.tasks.Visible:
		return a.frame(header, a.tasks.View(), message, "")
	}

	pane := components.Tabs(a.titles(), a.active, a.width) + "\n" +
		a.views[a.active].View()

	return a.frame(header, pane, message,
		components.HintBar(a.views[a.active].Hints(), a.width))
}

// frame stacks the fixed rows around a pane and pads the pane so the whole
// thing is exactly as tall as the terminal. The message line is always
// reserved, blank when there is nothing to say, so the pane does not resize
// under the cursor every time a message appears or expires.
func (a *App) frame(header, pane, message, hints string) string {
	if a.height <= 0 {
		return pane
	}

	// Header, message line, hint line.
	paneHeight := a.height - 3
	if paneHeight < 1 {
		paneHeight = 1
	}

	rows := make([]string, 0, a.height)
	rows = append(rows, header)
	rows = append(rows, fitLines(pane, paneHeight)...)
	rows = append(rows, message, hints)

	return strings.Join(rows, "\n")
}

// fitLines pads a pane with blank lines, or cuts it, so it is exactly n lines
// tall. Cutting is a backstop: a pane that overflows its allotted height would
// otherwise push the hint bar off the bottom of the screen.
func fitLines(pane string, n int) []string {
	lines := strings.Split(pane, "\n")
	if len(lines) > n {
		return lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

func (a *App) titles() []string {
	out := make([]string, 0, len(a.views))
	for _, v := range a.views {
		out = append(out, v.Title())
	}
	return out
}

// status assembles what the bars show about the session.
func (a *App) status() components.Status {
	total, running, _ := a.store.Counts()

	return components.Status{
		Socket:     a.store.Socket,
		Version:    a.store.Info.ServerVersion,
		Containers: total,
		Running:    running,
		Stale:      a.store.Stale,
		ReadOnly:   a.store.ReadOnly,
		Marked:     a.views[a.active].Marked(),
		Position:   a.views[a.active].Position(),
		Tasks:      a.tasks.Running(),
		TaskFailed: a.tasks.HasFailure(),
		Message:    a.message,
		MessageErr: a.messageErr,
		MessageAt:  a.messageAt,
	}
}

// formatBytes is the state layer's formatter, re-exported so the ui package
// does not grow a second one.
func formatBytes(n int64) string { return state.FormatBytes(n) }
