package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/ui/theme"
)

// TaskState is where a long-running command has got to.
type TaskState int

const (
	// TaskRunning means the command is still producing output.
	TaskRunning TaskState = iota
	// TaskSucceeded means it exited zero.
	TaskSucceeded
	// TaskFailed means it exited non-zero or could not start.
	TaskFailed
)

// Task is one long-running command, such as `compose up` or an image pull
// (AGENTS.md section 9.3).
type Task struct {
	ID       string
	Title    string
	Command  string
	State    TaskState
	Started  time.Time
	Ended    time.Time
	ExitCode int
	Err      error
	Output   []string
}

// Elapsed is how long the task ran, or has been running.
func (t Task) Elapsed() time.Duration {
	if t.Ended.IsZero() {
		return time.Since(t.Started)
	}
	return t.Ended.Sub(t.Started)
}

// maxTaskLines bounds the output kept per task, so a chatty build cannot grow
// the model without limit.
const maxTaskLines = 2000

// TaskPanel shows every task and the output of the selected one. Several tasks
// run concurrently.
type TaskPanel struct {
	tasks    []Task
	cursor   int
	viewport viewport.Model
	Visible  bool

	width  int
	height int
}

// NewTaskPanel builds an empty panel.
func NewTaskPanel() TaskPanel {
	return TaskPanel{viewport: viewport.New(80, 10)}
}

// SetSize records the space available.
func (p *TaskPanel) SetSize(width, height int) {
	p.width, p.height = width, height
	p.viewport.Width = width - 2
	p.viewport.Height = height - len(p.tasks) - 4
	if p.viewport.Height < 3 {
		p.viewport.Height = 3
	}
}

// Start registers a task and shows nothing: the panel only opens on demand or
// on failure.
func (p *TaskPanel) Start(id, title, command string) {
	p.tasks = append([]Task{{
		ID:      id,
		Title:   title,
		Command: command,
		State:   TaskRunning,
		Started: time.Now(),
	}}, p.tasks...)
	p.cursor = 0
	p.syncViewport()
}

// Append adds an output line to a task.
func (p *TaskPanel) Append(id, line string) {
	for i := range p.tasks {
		if p.tasks[i].ID != id {
			continue
		}
		p.tasks[i].Output = append(p.tasks[i].Output, line)
		if len(p.tasks[i].Output) > maxTaskLines {
			p.tasks[i].Output = p.tasks[i].Output[len(p.tasks[i].Output)-maxTaskLines:]
		}
		if i == p.cursor {
			p.syncViewport()
		}
		return
	}
}

// Finish records the outcome, reporting whether the panel should open itself:
// a failure is not something to leave unseen (AGENTS.md section 9.3).
func (p *TaskPanel) Finish(id string, exitCode int, err error) bool {
	for i := range p.tasks {
		if p.tasks[i].ID != id {
			continue
		}
		p.tasks[i].Ended = time.Now()
		p.tasks[i].ExitCode = exitCode
		p.tasks[i].Err = err
		if err != nil || exitCode != 0 {
			p.tasks[i].State = TaskFailed
			p.cursor = i
			p.Visible = true
			p.syncViewport()
			return true
		}
		p.tasks[i].State = TaskSucceeded
		return false
	}
	return false
}

// Running counts the tasks still going, for the status bar.
func (p *TaskPanel) Running() int {
	n := 0
	for _, t := range p.tasks {
		if t.State == TaskRunning {
			n++
		}
	}
	return n
}

// HasFailure reports whether any task ended badly.
func (p *TaskPanel) HasFailure() bool {
	for _, t := range p.tasks {
		if t.State == TaskFailed {
			return true
		}
	}
	return false
}

// Toggle opens or closes the panel.
func (p *TaskPanel) Toggle() { p.Visible = !p.Visible }

// Update handles scrolling and task selection while the panel is open.
func (p *TaskPanel) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch km.String() {
	case "up", "k":
		p.viewport.ScrollUp(1)
	case "down", "j":
		p.viewport.ScrollDown(1)
	case "left", "h":
		p.selectTask(-1)
	case "right", "l":
		p.selectTask(1)
	case "pgup":
		p.viewport.HalfPageUp()
	case "pgdown":
		p.viewport.HalfPageDown()
	}
	return nil
}

func (p *TaskPanel) selectTask(delta int) {
	p.cursor += delta
	if p.cursor >= len(p.tasks) {
		p.cursor = len(p.tasks) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.syncViewport()
}

func (p *TaskPanel) syncViewport() {
	if p.cursor < 0 || p.cursor >= len(p.tasks) {
		p.viewport.SetContent("")
		return
	}
	p.viewport.SetContent(strings.Join(p.tasks[p.cursor].Output, "\n"))
	p.viewport.GotoBottom()
}

// View renders the panel.
func (p *TaskPanel) View() string {
	s := theme.Current()

	if len(p.tasks) == 0 {
		return s.Dim.Render("no tasks yet")
	}

	var b strings.Builder
	for i, t := range p.tasks {
		marker := "  "
		if i == p.cursor {
			marker = s.Accent.Render("> ")
		}
		b.WriteString(marker + taskLine(t) + "\n")
	}

	b.WriteString(s.Dim.Render(strings.Repeat("-", max(1, p.width-2))) + "\n")
	b.WriteString(p.viewport.View())
	b.WriteString("\n" + s.Help.Render("left/right: task   up/down: scroll   t: close"))
	return b.String()
}

func taskLine(t Task) string {
	s := theme.Current()

	var state string
	switch t.State {
	case TaskRunning:
		state = s.Warning.Render("running")
	case TaskSucceeded:
		state = s.Running.Render("done")
	case TaskFailed:
		state = s.Danger.Render(fmt.Sprintf("failed (exit %d)", t.ExitCode))
	}

	line := fmt.Sprintf("%-28s %s  %s", t.Title, state, s.Dim.Render(fmtDuration(t.Elapsed())))
	if t.Err != nil && t.State == TaskFailed {
		line += "  " + s.Danger.Render(t.Err.Error())
	}
	return line
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
