package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
)

// The stream pattern: one goroutine writes to a channel, and a command reads a
// single item and reschedules itself from Update. Nothing else ever touches the
// model from another goroutine (AGENTS.md section 4, rule 4).

// StreamClosedMsg says a source ended. What it means depends on the source, so
// the channel it came from is named.
type StreamClosedMsg struct {
	Source string
}

// waitFor reads one value from a channel and wraps it for Update. On close it
// returns StreamClosedMsg instead, which is how the app knows to stop
// rescheduling.
func waitFor[T any](source string, ch <-chan T, wrap func(T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return StreamClosedMsg{Source: source}
		}
		return wrap(v)
	}
}

// EventMsg carries one update off the daemon event stream.
type EventMsg struct {
	Update docker.EventUpdate
}

// StatsMsg carries one computed stats sample.
type StatsMsg struct {
	Sample docker.Stats
}

// LogMsg carries one line of container output.
type LogMsg struct {
	Line docker.LogLine
}

// TaskLineMsg carries one line of a running task's output.
type TaskLineMsg struct {
	TaskID string
	Line   string
}

// TaskDoneMsg ends a task.
type TaskDoneMsg struct {
	TaskID   string
	ExitCode int
	Err      error
}

// taskEvent is what the task goroutines publish on the shared channel.
type taskEvent struct {
	id       string
	line     string
	done     bool
	exitCode int
	err      error
}

// waitForEvents reschedules the event stream read.
func waitForEvents(ch <-chan docker.EventUpdate) tea.Cmd {
	return waitFor("events", ch, func(u docker.EventUpdate) tea.Msg { return EventMsg{Update: u} })
}

// waitForStats reschedules the merged stats read.
func waitForStats(ch <-chan docker.Stats) tea.Cmd {
	return waitFor("stats", ch, func(s docker.Stats) tea.Msg { return StatsMsg{Sample: s} })
}

// waitForLogs reschedules the log read.
func waitForLogs(ch <-chan docker.LogLine) tea.Cmd {
	return waitFor("logs", ch, func(l docker.LogLine) tea.Msg { return LogMsg{Line: l} })
}

// waitForTasks reschedules the task output read.
func waitForTasks(ch <-chan taskEvent) tea.Cmd {
	return waitFor("tasks", ch, func(e taskEvent) tea.Msg {
		if e.done {
			return TaskDoneMsg{TaskID: e.id, ExitCode: e.exitCode, Err: e.err}
		}
		return TaskLineMsg{TaskID: e.id, Line: e.line}
	})
}
