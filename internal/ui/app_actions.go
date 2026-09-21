package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/views"
)

// openContainerLogs replaces whatever the viewer was showing with one
// container's output.
func (a *App) openContainerLogs(id, name string) tea.Cmd {
	a.closeLogs()

	a.logs.Open("logs: " + name)
	a.logsOpen = true
	a.layout()

	ctx, cancel := context.WithCancel(a.ctx)
	a.logsCancel = cancel

	opts := docker.DefaultLogOptions()
	opts.Tail = fmt.Sprintf("%d", a.cfg.LogTail)
	lines, errs := a.client.StreamLogs(ctx, id, opts)

	go a.forwardLogs(ctx, lines, errs)
	return nil
}

// openProjectLogs merges one engine log stream per container of the project,
// prefixed by service name. Compose is never shelled out to for logs
// (AGENTS.md section 8.6).
func (a *App) openProjectLogs(p compose.Project) tea.Cmd {
	a.closeLogs()

	a.logs.Open("logs: " + p.Name)
	a.logsOpen = true
	a.layout()

	ctx, cancel := context.WithCancel(a.ctx)
	a.logsCancel = cancel

	var started int
	for _, svc := range p.Services {
		for _, c := range svc.Containers {
			opts := docker.DefaultLogOptions()
			opts.Tail = fmt.Sprintf("%d", a.cfg.LogTail)
			opts.Service = svc.Name

			lines, errs := a.client.StreamLogs(ctx, c.ID, opts)
			go a.forwardLogs(ctx, lines, errs)
			started++
		}
	}

	if started == 0 {
		a.logs.Append(docker.LogLine{Text: "no containers in this project to read logs from"})
	}
	return nil
}

// forwardLogs pipes one stream into the shared channel the app reads from.
func (a *App) forwardLogs(ctx context.Context, lines <-chan docker.LogLine, errs <-chan error) {
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-lines:
			if !ok {
				return
			}
			select {
			case a.logCh <- l:
			case <-ctx.Done():
				return
			}
		case err, ok := <-errs:
			if !ok || err == nil {
				continue
			}
			select {
			case a.logCh <- docker.LogLine{Text: "log stream error: " + err.Error(), Stream: "stderr"}:
			case <-ctx.Done():
			}
			return
		}
	}
}

// closeLogs stops every stream feeding the viewer.
func (a *App) closeLogs() {
	if a.logsCancel != nil {
		a.logsCancel()
		a.logsCancel = nil
	}
	a.logsOpen = false
	a.layout()
}

// execFinishedMsg reports how an interactive shell ended.
type execFinishedMsg struct {
	name string
	err  error
}

// openExec suspends the renderer and hands the terminal to a shell inside the
// container. Bubble Tea restores the terminal afterwards, including when the
// session panics (AGENTS.md section 6.7).
func (a *App) openExec(id, name string) tea.Cmd {
	session := a.client.NewExec(id, a.cfg.Shell, "")

	return tea.Exec(session, func(err error) tea.Msg {
		if err == nil && session.ExitCode != 0 {
			// 126 and 127 are what a missing shell looks like from here.
			err = fmt.Errorf("the shell exited with code %d: the image may have no shell",
				session.ExitCode)
		}
		return execFinishedMsg{name: name, err: err}
	})
}

// runCompose starts a Compose command as a task, with the preflight that
// catches unresolved variables before anything is changed
// (AGENTS.md section 8.3).
func (a *App) runCompose(req views.ComposeRunRequest) tea.Cmd {
	a.taskSeq++
	taskID := fmt.Sprintf("task-%d", a.taskSeq)

	title := req.Project.Name + ": " + req.Title
	a.tasks.Start(taskID, title, a.cli.DisplayCommand(req.Project, req.Args...))
	a.setMessage("running "+title, false)

	go func() {
		if req.Preflight {
			if err := a.cli.Preflight(a.ctx, req.Project); err != nil {
				a.publishTask(taskEvent{id: taskID, line: err.Error()})
				a.publishTask(taskEvent{
					id:       taskID,
					done:     true,
					exitCode: 1,
					err:      fmt.Errorf("preflight failed, nothing was changed: %w", err),
				})
				return
			}
		}

		lines, result := a.cli.Run(a.ctx, req.Project, req.Args...)
		for line := range lines {
			a.publishTask(taskEvent{id: taskID, line: line.Text})
		}
		res := <-result
		a.publishTask(taskEvent{id: taskID, done: true, exitCode: res.ExitCode, err: res.Err})
	}()

	return nil
}

// runPull streams an image pull into the task panel.
func (a *App) runPull(ref string) tea.Cmd {
	a.taskSeq++
	taskID := fmt.Sprintf("task-%d", a.taskSeq)

	a.tasks.Start(taskID, "pull "+ref, "docker pull "+ref)
	a.setMessage("pulling "+ref, false)

	go func() {
		lines, errs := a.client.PullImage(a.ctx, ref)

		var wg sync.WaitGroup
		var pullErr error

		wg.Add(1)
		go func() {
			defer wg.Done()
			for err := range errs {
				if err != nil {
					pullErr = err
				}
			}
		}()

		for line := range lines {
			a.publishTask(taskEvent{id: taskID, line: line})
		}
		wg.Wait()

		code := 0
		if pullErr != nil {
			code = 1
		}
		a.publishTask(taskEvent{id: taskID, done: true, exitCode: code, err: pullErr})
	}()

	return nil
}

// publishTask hands one task event to the model, dropping it if the app is
// shutting down.
func (a *App) publishTask(e taskEvent) {
	select {
	case a.taskCh <- e:
	case <-a.ctx.Done():
	}
}

// applyPruneResult reports what was actually destroyed, next to what the
// preview promised.
func (a *App) applyPruneResult(m cmds.PruneDoneMsg) tea.Cmd {
	if m.Err != nil {
		a.openModal(components.NewModal(components.SevError, "prune failed",
			[]string{m.Err.Error()}, nil))
		return cmds.RefreshAll(a.ctx, a.client)
	}

	var total int64
	var lines []string
	for _, rep := range m.Reports {
		total += rep.Reclaimed
		lines = append(lines, fmt.Sprintf("%s: %d removed, %s reclaimed",
			rep.Category, len(rep.Deleted), formatBytes(rep.Reclaimed)))
	}

	a.setMessage(fmt.Sprintf("pruned, %s reclaimed", formatBytes(total)), false)
	a.openModal(components.NewModal(components.SevInfo, "prune done", lines, nil))

	// The disk figures are stale the moment a prune runs, so df is recomputed
	// rather than left showing what used to be there.
	a.diskView().SetLoading(true)
	return tea.Batch(cmds.RefreshAll(a.ctx, a.client), cmds.DiskUsage(a.ctx, a.client))
}

// showInspect puts raw inspect output in a scrollable modal, except for the
// container detail pane, which the containers view renders itself.
func (a *App) showInspect(m cmds.InspectMsg) tea.Cmd {
	if m.Err != nil {
		a.openModal(components.NewModal(components.SevError, "inspect failed",
			[]string{m.Err.Error()}, nil))
		return nil
	}

	if strings.HasPrefix(m.Title, "container ") {
		// The containers view keeps this one in its detail pane.
		return a.views[a.active].Update(m)
	}

	a.logs.Open(m.Title)
	for _, line := range strings.Split(m.Content, "\n") {
		a.logs.Append(docker.LogLine{Text: line})
	}
	a.logsOpen = true
	a.layout()
	return nil
}

// showText puts a block of text in the pager: process lists, filesystem
// changes, anything that is read rather than acted on.
func (a *App) showText(m cmds.TextMsg) tea.Cmd {
	if m.Err != nil {
		a.openModal(components.NewModal(components.SevError, m.Title,
			[]string{m.Err.Error()}, nil))
		return nil
	}

	a.logs.Open(m.Title)
	for _, line := range m.Lines {
		a.logs.Append(docker.LogLine{Text: line})
	}
	a.logsOpen = true
	a.layout()
	return nil
}

// showHistory renders an image's layers in the same pager as inspect output.
func (a *App) showHistory(m cmds.HistoryMsg) tea.Cmd {
	if m.Err != nil {
		a.openModal(components.NewModal(components.SevError, "history failed",
			[]string{m.Err.Error()}, nil))
		return nil
	}

	a.logs.Open("history: " + m.Image)
	for _, l := range m.Layers {
		a.logs.Append(docker.LogLine{
			Text: fmt.Sprintf("%-14s %-10s %s",
				docker.ShortID(l.ID), formatBytes(l.Size), strings.TrimSpace(l.CreatedBy)),
		})
	}
	a.logsOpen = true
	a.layout()
	return nil
}
