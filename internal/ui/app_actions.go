package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/config"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/editor"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/views"
)

// openDetail shows something in the side panel, loading it with the command
// the view supplied. Asking for what is already there closes it, so the key
// that opens a detail is the key that dismisses it.
func (a *App) openDetail(req views.DetailRequest) tea.Cmd {
	if a.panel.Kind == components.PanelDetail && a.panel.Source == req.Source {
		a.closePanel()
		return nil
	}

	a.closePanel()
	a.panel.Open(components.PanelDetail, req.Title, "", req.Source)
	a.panel.Set([]string{"loading..."})
	a.layout()

	if req.Load == nil {
		return nil
	}
	return req.Load()
}

// openContainerLogs points the panel at one container's output.
func (a *App) openContainerLogs(id, name string) tea.Cmd {
	if a.panel.Kind == components.PanelLogs && a.panel.Source == id {
		a.closePanel()
		return nil
	}

	a.closePanel()
	a.panel.Open(components.PanelLogs, "logs: "+name, "", id)
	a.layout()

	ctx, cancel := context.WithCancel(a.ctx)
	a.panelCancel = cancel

	opts := docker.DefaultLogOptions()
	opts.Tail = fmt.Sprintf("%d", a.cfg.LogTail)
	lines, errs := a.client.StreamLogs(ctx, id, opts)

	go a.forwardLogs(ctx, lines, errs)
	return nil
}

// openProjectLogs merges one engine log stream per container, prefixed by
// service name. Compose is never shelled out to for logs
// (AGENTS.md section 8.6).
//
// A service narrows it to that service's replicas, which is what reading logs
// usually means; without one, the whole stack is merged.
func (a *App) openProjectLogs(p compose.Project, service string) tea.Cmd {
	source := "compose:" + p.Name
	title := "logs: " + p.Name
	if service != "" {
		source += "/" + service
		title = "logs: " + p.Name + " / " + service
	}

	if a.panel.Kind == components.PanelLogs && a.panel.Source == source {
		a.closePanel()
		return nil
	}

	a.closePanel()
	a.panel.Open(components.PanelLogs, title, "", source)
	a.layout()

	ctx, cancel := context.WithCancel(a.ctx)
	a.panelCancel = cancel

	var started int
	for _, svc := range p.Services {
		if service != "" && svc.Name != service {
			continue
		}
		for _, c := range svc.Containers {
			opts := docker.DefaultLogOptions()
			opts.Tail = fmt.Sprintf("%d", a.cfg.LogTail)
			// One replica needs no prefix: the panel title already says which
			// service this is, and the column is worth more than the repetition.
			if service == "" || len(svc.Containers) > 1 {
				opts.Service = svc.Name
				if len(svc.Containers) > 1 {
					opts.Service = c.Name
				}
			}

			lines, errs := a.client.StreamLogs(ctx, c.ID, opts)
			go a.forwardLogs(ctx, lines, errs)
			started++
		}
	}

	if started == 0 {
		a.panel.Append("nothing is running here to read logs from")
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

// emit turns a value into the command that delivers it, which is how the app
// opens one of its own overlays from inside an action.
func emit(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

// editFinishedMsg reports how an editing session ended.
type editFinishedMsg struct {
	path string
	err  error
	// before is what the file looked like when it was handed over, so what it
	// looks like now answers whether anything was written.
	before    string
	then      func() tea.Cmd
	unchanged func() tea.Cmd
}

// fingerprint says whether a file has been written since it was last looked
// at. It carries the modification time and size as well as a digest of the
// contents, because the question is whether the editor wrote, not whether the
// result differs.
//
// Both halves matter. :wq on a template nobody edited writes the same bytes
// back, and the person doing it has decided to keep that file; comparing
// contents alone would throw it away. :q writes nothing, leaves the timestamp
// alone, and must change nothing. The digest catches the other direction, an
// editor that rewrites a file it did not need to.
func fingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "absent"
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return "unreadable"
	}
	sum := sha256.Sum256(body)

	return fmt.Sprintf("%d:%d:%s", info.ModTime().UnixNano(), info.Size(),
		hex.EncodeToString(sum[:]))
}

// openEditor hands a file to an editor, asking which one the first time and
// remembering the answer. The terminal is released the same way it is for a
// shell, so a full screen editor gets the whole window.
func (a *App) openEditor(req views.EditRequest) tea.Cmd {
	// The same reasoning as runCompose: the refusal lives where the thing
	// happens. Handing someone an editor is how a file gets changed, and
	// read-only says no files change.
	if a.store.ReadOnly {
		return emit(views.ReadOnlyMsg{})
	}

	chosen, ok := a.resolveEditor()
	if !ok {
		// Nothing configured and nothing in the environment: ask, and come
		// back to this file once there is an answer.
		return a.askForEditor(req)
	}
	return a.runEditor(chosen, req)
}

// resolveEditor answers from the configuration first, then from the shell.
func (a *App) resolveEditor() (editor.Editor, bool) {
	if a.cfg.Editor != "" {
		if chosen, err := editor.Parse(a.cfg.Editor, nil); err == nil {
			return chosen, true
		}
		// Configured but no longer installed: fall through and ask again
		// rather than handing the screen to something that is not there.
	}
	return editor.FromEnvironment(nil, nil)
}

// askForEditor offers what this machine has, and remembers the answer.
func (a *App) askForEditor(req views.EditRequest) tea.Cmd {
	available := editor.Available(nil)

	choices := make([]components.Choice, 0, len(available)+1)
	for _, candidate := range available {
		chosen := candidate
		choices = append(choices, components.Choice{
			Label:  chosen.Label,
			Detail: chosen.Detail,
			Payload: func() tea.Cmd {
				a.rememberEditor(chosen)
				return a.runEditor(chosen, req)
			},
		})
	}

	choices = append(choices, components.Choice{
		Label:  "something else",
		Detail: "type a command, such as emacsclient -nw",
		Payload: func() tea.Cmd {
			return emit(views.PromptRequest{
				Title: "Which editor",
				Body: []string{
					"The command to run, as you would type it.",
					"An editor that opens a window needs whatever makes it wait, such as --wait.",
				},
				Label: "command: ",
				Run: func(command string) tea.Cmd {
					chosen, err := editor.Parse(command, nil)
					if err != nil {
						a.openModal(components.NewModal(components.SevError,
							"that editor cannot be used", []string{err.Error()}, nil))
						return nil
					}
					a.rememberEditor(chosen)
					return a.runEditor(chosen, req)
				},
			})
		},
	})

	if len(available) == 0 {
		a.openModal(components.NewModal(components.SevError, "no editor found",
			[]string{
				"None of the editors hublot knows about is on your PATH.",
				"Set $EDITOR, or put the command in " + configPathForMessage() + ".",
			}, nil))
		return nil
	}

	return emit(views.PickerRequest{
		Title:   "Open " + filepath.Base(req.Path) + " with",
		Choices: choices,
	})
}

// rememberEditor writes the choice to the configuration so the question is
// asked once. A failure to write is worth saying, but not worth stopping for:
// the editor still opens, it is only the memory that is lost.
func (a *App) rememberEditor(chosen editor.Editor) {
	a.cfg.Editor = chosen.String()
	if err := config.Save(a.cfg); err != nil {
		a.setMessage("editor chosen, but it could not be saved: "+err.Error(), true)
	}
}

// runEditor suspends the interface and gives the terminal to the editor,
// remembering what the file said beforehand.
func (a *App) runEditor(chosen editor.Editor, req views.EditRequest) tea.Cmd {
	before := fingerprint(req.Path)
	cmd := chosen.Cmd(req.Path)

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editFinishedMsg{
			path:      req.Path,
			err:       err,
			before:    before,
			then:      req.Then,
			unchanged: req.Unchanged,
		}
	})
}

// configPathForMessage is the configuration file, or a sensible description of
// it when the path cannot be worked out.
func configPathForMessage() string {
	if path, err := config.Path(); err == nil {
		return path
	}
	return "the hublot configuration file"
}

// explain turns a failure into something a person can act on. The daemon's own
// message stays where it was, in the task panel or the error dialog: this adds
// the sentence it is missing, which is what the mistake usually was.
//
// Nothing is invented. When the output says nothing recognisable, the caller
// falls back to showing it as it came.
func (a *App) explain(output string) (components.Modal, bool) {
	d, ok := state.Diagnose(output)
	if !ok {
		return components.Modal{}, false
	}

	lines := append([]string{}, d.Lines...)

	// A port clash is far more useful when it says what is holding the port,
	// and hublot is already looking at every container on the host.
	if d.Port != "" {
		var known []state.Container
		for _, c := range a.store.Containers {
			entry := state.Container{Name: c.Name}
			for _, p := range c.Ports {
				entry.Ports = append(entry.Ports, state.Port{Public: p.PublicPort})
			}
			known = append(known, entry)
		}
		if holder := state.PortHolder(known, d.Port); holder != "" {
			lines = append(lines, "", "Right now it is held by the container "+holder+".")
		}
	}

	return components.NewModal(components.SevError, d.Title, lines, nil), true
}

// composeTask is what a compose task was doing, kept only until it ends.
type composeTask struct {
	project compose.Project
	args    []string
}

// offerFix turns a dialog that reports a failure into one that does something
// about it. A compose file that will not parse is the clearest case: the file
// is known, the editor is a keypress away, and telling someone their YAML is
// wrong and then making them go and find it is most of the annoyance.
//
// The offer only appears when there is a file to open. Nothing here guesses.
func (a *App) offerFix(modal *components.Modal, task composeTask, output string) {
	// Nothing to offer when nothing may be changed: read-only means the file
	// is not opened for editing either.
	if a.store.ReadOnly {
		return
	}

	path := a.fileToFix(task, output)
	if path == "" {
		return
	}

	modal.Action = "edit " + filepath.Base(path)
	project := task.project
	args := task.args
	a.modalRun = func() tea.Cmd {
		return a.openEditor(views.EditRequest{
			Path:  path,
			Title: project.Name,
			Then: func() tea.Cmd {
				// The command that failed is the one worth running again, but
				// it is asked for rather than assumed: the same path carries
				// `down -v`, and an editor closing is not consent to destroy
				// anything.
				return emit(views.ConfirmRequest{
					Severity: components.SevConfirm,
					Title:    "Run it again on " + project.Name + "?",
					Body: []string{
						"The file was written. This is the command that failed, run again",
						"against what is now on disk.",
						"",
						a.cli.DisplayCommand(project, args...),
					},
					Run: func() tea.Cmd {
						return emit(views.ComposeRunRequest{
							Project:   project,
							Title:     strings.Join(args, " "),
							Args:      args,
							Preflight: true,
						})
					},
				})
			},
		})
	}
}

// fileToFix picks which file the offer opens: the one compose named, when it
// named one, otherwise the file the project was built from. Either has to
// exist, since an offer to open nothing is worse than no offer.
func (a *App) fileToFix(task composeTask, output string) string {
	if d, ok := state.Diagnose(output); ok && d.File != "" {
		if _, err := os.Stat(d.File); err == nil {
			return d.File
		}
	}
	if len(task.args) == 0 || len(task.project.ConfigFiles) == 0 {
		return ""
	}
	path := task.project.ConfigFiles[0]
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// editFinished decides what an editing session meant. The file answers that,
// not the editor: someone who quits without writing has asked for nothing to
// happen, and saying so is the whole point of the distinction.
func (a *App) editFinished(m editFinishedMsg) tea.Cmd {
	// An editor that never ran at all is worth a message; one that merely
	// exited non-zero is not, since the file still says what happened.
	var exitErr *exec.ExitError
	if m.err != nil && !errors.As(m.err, &exitErr) {
		a.openModal(components.NewModal(components.SevError,
			"the editor could not be run", []string{m.err.Error()}, nil))
		return nil
	}

	if fingerprint(m.path) == m.before {
		a.setMessage(filepath.Base(m.path)+" was not changed", false)
		if m.unchanged != nil {
			return m.unchanged()
		}
		return nil
	}

	a.setMessage("edited "+filepath.Base(m.path), false)
	if m.then != nil {
		return m.then()
	}
	return nil
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
	session.Name = name

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
	// Checked here rather than only in the view that asked. Every mutation
	// goes through this function, so this is where read-only actually holds;
	// a guard in each caller holds only as long as every caller remembers
	// (AGENTS.md section 10.4).
	if a.store.ReadOnly {
		return emit(views.ReadOnlyMsg{})
	}

	a.taskSeq++
	taskID := fmt.Sprintf("task-%d", a.taskSeq)

	title := req.Project.Name + ": " + req.Title
	a.taskOf[taskID] = composeTask{project: req.Project, args: req.Args}
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

	// Inspect output is what the detail panel was opened for, so it fills
	// whatever is already there rather than replacing it.
	if a.panel.Kind != components.PanelDetail || a.panel.Title != m.Title {
		a.closePanel()
		a.panel.Open(components.PanelDetail, m.Title, "", m.Title)
		a.layout()
	}
	a.panel.Set(strings.Split(m.Content, "\n"))
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

	a.closePanel()
	a.panel.Open(components.PanelText, m.Title, fmt.Sprintf("%d lines", len(m.Lines)), m.Title)
	a.panel.Set(m.Lines)
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

	lines := make([]string, 0, len(m.Layers))
	for _, l := range m.Layers {
		lines = append(lines, fmt.Sprintf("%-14s %-10s %s",
			docker.ShortID(l.ID), formatBytes(l.Size), strings.TrimSpace(l.CreatedBy)))
	}

	a.closePanel()
	a.panel.Open(components.PanelText, "history: "+m.Image,
		fmt.Sprintf("%d layers", len(m.Layers)), "history:"+m.Image)
	a.panel.Set(lines)
	a.layout()
	return nil
}
