// Package views holds the per-tab models. A view owns its table and its local
// UI state; anything that touches Docker goes out as a command, and anything
// that needs a dialog goes out as a request the app fulfils.
package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/keys"
)

// Deps is everything a view is given. The store is read-only from here: views
// render it, the app mutates it.
type Deps struct {
	Ctx      context.Context
	Client   *docker.Client
	Store    *state.Store
	CLI      compose.CLI
	Keys     keys.Map
	ReadOnly bool
	// StopTimeout is how long a stop waits before SIGKILL.
	StopTimeout time.Duration
	// Shell is the command an exec session runs, empty for the default.
	Shell []string
}

// View is one tab.
type View interface {
	// Title is the tab label.
	Title() string
	// Refresh rebuilds the rows from the store, called whenever data changes.
	Refresh()
	// SetSize gives the view its drawing area.
	SetSize(width, height int)
	// Update handles a message while the view has focus.
	Update(msg tea.Msg) tea.Cmd
	// View renders the body.
	View() string
	// Hints are the bindings shown in the footer.
	Hints() []key.Binding
	// Marked is how many rows are marked, for the status bar.
	Marked() int
	// Position is the "3/24" indicator.
	Position() string
}

// ConfirmRequest asks the app to open a confirmation dialog. The severity
// grading follows AGENTS.md section 10.1.
type ConfirmRequest struct {
	Severity components.Severity
	Title    string
	Body     []string
	// Run is what happens on confirmation.
	Run func() tea.Cmd
}

// PickerRequest asks the app to open a searchable palette.
type PickerRequest struct {
	Title   string
	Choices []components.Choice
}

// PromptRequest asks the app to collect one value: a name, a reference, a
// path. Run is handed whatever was typed.
type PromptRequest struct {
	Title   string
	Body    []string
	Label   string
	Initial string
	Run     func(string) tea.Cmd
}

// LogsRequest asks the app to open the log viewer for one container.
type LogsRequest struct {
	ContainerID string
	Name        string
}

// ComposeLogsRequest asks for the aggregated logs of a whole project
// (AGENTS.md section 8.6).
type ComposeLogsRequest struct {
	Project compose.Project
}

// ExecRequest asks the app to suspend the UI and open a shell.
type ExecRequest struct {
	ContainerID string
	Name        string
}

// ComposeRunRequest asks the app to run a Compose command as a task.
type ComposeRunRequest struct {
	Project compose.Project
	Title   string
	Args    []string
	// Preflight runs `compose config --quiet` first, which every mutating
	// action needs (AGENTS.md section 8.3).
	Preflight bool
}

// PruneRequest asks the app to prune after showing a preview.
type PruneRequest struct {
	Categories []state.Category
	// Global marks the `system prune -a --volumes` equivalent, which requires
	// typing a word to confirm.
	Global bool
}

// ReadOnlyMsg is emitted when a mutating key is pressed in a read-only session.
type ReadOnlyMsg struct{}

// request wraps a value as a command emitting it.
func request(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

// denied reports the read-only refusal rather than doing nothing silently
// (AGENTS.md section 10.4).
func denied() tea.Cmd { return request(ReadOnlyMsg{}) }

// base is the shared plumbing of every list view.
type base struct {
	deps   Deps
	table  components.Table
	width  int
	height int
}

func (b *base) SetSize(width, height int) {
	b.width, b.height = width, height
	b.table.SetSize(width, height)
}

func (b *base) Marked() int      { return b.table.MarkedCount() }
func (b *base) Position() string { return b.table.Scrollbar() }
func (b *base) View() string     { return b.table.View() }

// formatPorts renders published ports the way the docker CLI does, keeping the
// published ones, which are the only ones worth the column width.
func formatPorts(ports []docker.Port) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range ports {
		if p.PublicPort == 0 {
			continue
		}
		s := fmt.Sprintf("%d->%d", p.PublicPort, p.PrivatePort)
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// formatAge renders a timestamp as a coarse age.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// formatCPU renders a CPU percentage, or a dash when the sample carries no
// usable previous frame (AGENTS.md section 6.1).
func formatCPU(st docker.Stats, ok bool) string {
	if !ok || !st.CPUValid {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", st.CPUPercent)
}

// formatMem renders memory usage against its limit.
func formatMem(st docker.Stats, ok bool) string {
	if !ok || st.MemUsage == 0 {
		return "-"
	}
	return state.FormatBytes(st.MemUsage)
}

// formatIO renders a receive/transmit pair.
func formatIO(rx, tx int64) string {
	if rx == 0 && tx == 0 {
		return "-"
	}
	return state.FormatBytes(rx) + "/" + state.FormatBytes(tx)
}

// truncateID shortens an id for display.
func truncateID(id string) string { return docker.ShortID(id) }
