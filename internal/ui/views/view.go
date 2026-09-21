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
	"github.com/RobinHil/hublot/internal/ui/cmds"
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
	// Summary is the one line this view adds to the header: what its list
	// amounts to, which is the question a dashboard should answer without
	// anyone counting rows.
	Summary() string
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

// EditRequest asks the app to hand a file to an editor. The app owns the
// choosing and the suspending; a view only says what to open and what each
// outcome means.
//
// Which callback runs is decided by the file's contents, not by how the editor
// exited: quitting vim with :q leaves the file alone and must change nothing,
// while :wq is a write whatever the exit code says afterwards.
type EditRequest struct {
	Path string
	// Title is what the message says afterwards.
	Title string
	// Then runs when the file was written and its contents differ.
	Then func() tea.Cmd
	// Unchanged runs when the editor left the file exactly as it found it. A
	// file hublot created for the occasion is taken back out here.
	Unchanged func() tea.Cmd
}

// FormRequest asks the app to collect several values at once, which is what
// creating something takes: one prompt cannot ask for a name, ports and
// volumes in the same breath.
type FormRequest struct {
	Title    string
	Subtitle string
	Fields   []components.Field
	Run      func(map[string]string) tea.Cmd
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

// DetailRequest asks the app to show something in the side panel, loading it
// with the command given. Reopening the same source closes it again, so the
// key that opens a detail also closes it.
type DetailRequest struct {
	Title  string
	Source string
	Load   func() tea.Cmd
}

// LogsRequest asks the app to open the log viewer for one container.
type LogsRequest struct {
	ContainerID string
	Name        string
}

// ComposeLogsRequest asks for compose logs: one service when Service is set,
// the whole project otherwise (AGENTS.md section 8.6).
type ComposeLogsRequest struct {
	Project compose.Project
	Service string
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

// announce puts a line in the footer, for an outcome worth reporting that is
// not an error and does not deserve a dialog.
func announce(text string) tea.Cmd {
	return func() tea.Msg { return cmds.ActionDoneMsg{Label: text} }
}

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

// SetOrigin tells the table which screen row it starts on, so a click can be
// turned back into a row.
func (b *base) SetOrigin(top int) { b.table.SetOrigin(top) }

func (b *base) Marked() int      { return b.table.MarkedCount() }
func (b *base) Position() string { return b.table.Scrollbar() }
func (b *base) View() string     { return b.table.View() }

// plural renders a count with its unit, singular when there is one of them.
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// summaryOf joins the parts of a summary line, dropping the empty ones so a
// view never shows a stray separator.
func summaryOf(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

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

// statusWords are the daemon's own phrasings, which are written for a person
// reading one line rather than for a column: "Up About an hour" costs three
// times what "up 1h" does and says the same thing.
var statusWords = strings.NewReplacer(
	"About an hour", "1h",
	"About a minute", "1m",
	"Less than a second", "0s",
	" seconds", "s",
	" second", "s",
	" minutes", "m",
	" minute", "m",
	" hours", "h",
	" hour", "h",
	" days", "d",
	" day", "d",
	" weeks", "w",
	" week", "w",
	" months", "mo",
	" month", "mo",
	" years", "y",
	" year", "y",
	" ago", "",
	"(healthy)", "ok",
	"(unhealthy)", "sick",
	"(health: starting)", "starting",
)

// formatStatus compacts what the daemon reported so the column carries the
// state, the age and the exit code instead of a sentence.
func formatStatus(c docker.Container) string {
	status := statusWords.Replace(c.Status)
	status = strings.TrimPrefix(status, "Up ")
	if rest, found := strings.CutPrefix(status, "Exited "); found {
		return "exited " + strings.TrimSpace(rest)
	}
	if rest, found := strings.CutPrefix(status, "Created"); found {
		return "created" + strings.TrimSpace(rest)
	}
	if strings.Contains(status, "(Paused)") {
		return "paused " + strings.TrimSpace(strings.ReplaceAll(status, "(Paused)", ""))
	}
	if strings.HasPrefix(status, "Restarting") {
		return strings.ToLower(status)
	}
	if status == "" {
		return strings.ToLower(c.State)
	}
	return "up " + strings.TrimSpace(status)
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
// usable previous frame (AGENTS.md section 6.1). Padded so a column of them
// lines up on the decimal point rather than ragging.
func formatCPU(st docker.Stats, ok bool) string {
	if !ok || !st.CPUValid {
		return "    -"
	}
	return fmt.Sprintf("%5.1f%%", st.CPUPercent)
}

// formatMem renders memory usage, right-aligned in the width a size takes.
func formatMem(st docker.Stats, ok bool) string {
	if !ok || st.MemUsage == 0 {
		return "     -"
	}
	return fmt.Sprintf("%6s", state.FormatBytes(st.MemUsage))
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
