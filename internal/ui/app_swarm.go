package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/config"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/views"
)

// refreshAll resyncs every list, and the daemon info with them: the info is
// what says whether this node manages a swarm, and so whether the swarm lists
// are worth asking for (see applyInfo).
func (a *App) refreshAll() tea.Cmd {
	return tea.Batch(cmds.RefreshAll(a.ctx, a.client), cmds.LoadInfo(a.ctx, a.client))
}

// applyInfo records the daemon info, and follows this node's place in the
// swarm: a manager gets its lists, anything else has them cleared, since the
// services of a swarm this node has left are not this node's any more.
func (a *App) applyInfo(m cmds.InfoMsg) tea.Cmd {
	if m.Err != nil {
		return nil
	}
	a.store.Info = m.Info
	if a.store.SwarmManaged() {
		return cmds.RefreshSwarm(a.ctx, a.client)
	}
	a.store.ClearSwarm()
	a.refreshViews()
	return nil
}

// applySwarm records the swarm lists. A refusal is kept and shown rather than
// read as an empty swarm.
func (a *App) applySwarm(m cmds.SwarmMsg) {
	if m.Err != nil {
		a.store.SwarmErr = m.Err
		a.refreshViews()
		return
	}
	a.store.SwarmErr = nil
	a.store.SetServices(m.Services)
	a.store.SetTasks(m.Tasks)
	a.store.SetNodes(m.Nodes)
	a.store.SetSecrets(m.Secrets)
	a.store.SetConfigs(m.Configs)
	a.store.SetCluster(m.Cluster)
	a.refreshViews()
}

// swarmEvent resyncs what a swarm event touched. A node event can change this
// daemon's own role, so the info is read again too.
func (a *App) swarmEvent(e docker.Event) []tea.Cmd {
	var batch []tea.Cmd
	if e.AffectsNodes() {
		batch = append(batch, cmds.LoadInfo(a.ctx, a.client))
	}
	if a.store.SwarmManaged() && (e.AffectsSwarm() || e.AffectsContainers() && e.Attributes[docker.LabelSwarmService] != "") {
		batch = append(batch, cmds.RefreshSwarm(a.ctx, a.client))
	}
	return batch
}

// swarmVisible reports whether the Swarm view is on screen.
func (a *App) swarmVisible() bool {
	_, ok := a.views[a.active].(*views.Swarm)
	return ok
}

// startTaskPoll starts polling tasks while the Swarm view is on screen. Tasks
// emit no events, and the rollout someone opened that view to watch would
// otherwise only move every ten seconds.
func (a *App) startTaskPoll() tea.Cmd {
	if a.taskPolling || !a.store.SwarmManaged() {
		return nil
	}
	a.taskPolling = true
	return tea.Batch(cmds.ListTasks(a.ctx, a.client), cmds.TaskTick())
}

// taskTick polls once more, or stops when nobody is looking.
func (a *App) taskTick() tea.Cmd {
	if !a.swarmVisible() || !a.store.SwarmManaged() {
		a.taskPolling = false
		return nil
	}
	// Services carry the running counts, which move with the tasks.
	return tea.Batch(cmds.ListTasks(a.ctx, a.client), cmds.ListServices(a.ctx, a.client), cmds.TaskTick())
}

// rememberStack records which files a stack was deployed from, and writes it
// to the configuration so the next session knows too.
func (a *App) rememberStack(stack string, files []string) {
	if stack == "" || len(files) == 0 {
		return
	}
	a.store.StackFiles[stack] = files
	if a.cfg.Stacks == nil {
		a.cfg.Stacks = map[string][]string{}
	}
	a.cfg.Stacks[stack] = files
	if err := config.Save(a.cfg); err != nil {
		a.setMessage("the stack file could not be remembered: "+err.Error(), true)
	}
	a.refreshViews()
}

// swarmPreflightMsg is what checking a stack's files found.
type swarmPreflightMsg struct {
	req     views.SwarmRunRequest
	missing []string
	err     error
}

// runSwarm runs a docker CLI command as a task. A deploy is checked first for
// variables nothing sets, which the CLI would deploy as blanks without a word
// (see swarm.CLI.Preflight).
func (a *App) runSwarm(req views.SwarmRunRequest) tea.Cmd {
	// Checked here as well as in the view, for the same reason as runCompose:
	// every swarm CLI mutation goes through this function (AGENTS.md section
	// 10.4).
	if a.store.ReadOnly {
		return emit(views.ReadOnlyMsg{})
	}
	if req.Preflight && !req.Accepted {
		cli := a.store.SwarmCLI
		return func() tea.Msg {
			missing, err := cli.Preflight(a.ctx, req.Dir, req.Files)
			return swarmPreflightMsg{req: req, missing: missing, err: err}
		}
	}
	a.rememberStack(req.Stack, req.Files)

	a.taskSeq++
	taskID := fmt.Sprintf("task-%d", a.taskSeq)
	if len(req.Files) > 0 {
		a.stackOf[taskID] = req
	}
	a.tasks.Start(taskID, req.Title, a.store.SwarmCLI.Display(req.Args...))
	a.setMessage("running "+req.Title, false)

	go func() {
		lines, result := a.store.SwarmCLI.Run(a.ctx, req.Dir, req.Args...)
		for line := range lines {
			a.publishTask(taskEvent{id: taskID, line: line.Text})
		}
		res := <-result
		a.publishTask(taskEvent{id: taskID, done: true, exitCode: res.ExitCode, err: res.Err})
	}()
	return nil
}

// preflightDone runs the deploy, or asks first when variables would be blank.
func (a *App) preflightDone(m swarmPreflightMsg) tea.Cmd {
	if m.err != nil {
		if modal, ok := a.explain(m.err.Error()); ok {
			modal.Body = append(modal.Body, "", m.err.Error())
			a.openModal(modal)
			return nil
		}
		a.openModal(components.NewModal(components.SevError, "the stack file does not resolve",
			[]string{m.err.Error(), "", "Nothing was deployed."}, nil))
		return nil
	}
	req := m.req
	req.Accepted = true
	if len(m.missing) == 0 {
		return a.runSwarm(req)
	}
	body := []string{
		"These variables are used by the stack file and set nowhere hublot can see:",
		"  " + strings.Join(m.missing, ", "),
		"",
		"docker stack deploy reads no .env file, only the environment it runs in, which",
		"is hublot's. Deployed now, each of them comes out as an empty string. If the",
		"stack was first deployed from a shell that exported them, quit and start",
		"hublot from such a shell instead.",
	}
	return emit(views.ConfirmRequest{
		Severity: components.SevDanger,
		Title:    "Deploy with blank variables?",
		Body:     body,
		Run:      func() tea.Cmd { return a.runSwarm(req) },
	})
}

// offerStackFix turns the dialog of a failed deploy into the way out of it:
// the stack file is a keypress from the editor, and once it is written the
// same deploy is offered again. Offered, not run: an editor closing is not
// consent to replace running tasks. Same reasoning as offerFix for compose.
func (a *App) offerStackFix(modal *components.Modal, req views.SwarmRunRequest) {
	if a.store.ReadOnly || len(req.Files) == 0 {
		return
	}
	path := req.Files[0]
	if _, err := os.Stat(path); err != nil {
		return
	}
	modal.Action = "edit " + filepath.Base(path)
	again := req
	again.Accepted = false
	a.modalRun = func() tea.Cmd {
		return a.openEditor(views.EditRequest{
			Path:  path,
			Title: req.Stack,
			Then: func() tea.Cmd {
				return emit(views.ConfirmRequest{
					Severity: components.SevConfirm,
					Title:    "Deploy " + req.Stack + " again?",
					Body: []string{
						"The file was written. This is the deploy that failed, run again",
						"against what is now on disk.",
						"",
						a.store.SwarmCLI.Display(req.Args...),
					},
					Run: func() tea.Cmd { return a.runSwarm(again) },
				})
			},
		})
	}
}

// openSwarmLogs points the panel at a task, or merges every task of the
// services given. The managers fetch the output from whichever node ran it,
// so this reads remote tasks too, unlike a shell.
func (a *App) openSwarmLogs(req views.SwarmLogsRequest) tea.Cmd {
	if a.panel.Kind == components.PanelLogs && a.panel.Source == req.Source {
		a.closePanel()
		return nil
	}

	a.closePanel()
	a.panel.Open(components.PanelLogs, req.Title, "", req.Source)
	a.layout()

	ctx, cancel := context.WithCancel(a.ctx)
	a.panelCancel = cancel

	opts := docker.DefaultLogOptions()
	opts.Tail = fmt.Sprintf("%d", a.cfg.LogTail)
	opts.Names = req.Names

	if req.TaskID != "" {
		lines, errs := a.client.StreamTaskLogs(ctx, req.TaskID, req.TaskTTY, opts)
		go a.forwardLogs(ctx, lines, errs)
		return nil
	}

	for _, svc := range req.Services {
		lines, errs := a.client.StreamServiceLogs(ctx, svc.ID, svc.TTY, opts)
		go a.forwardLogs(ctx, lines, errs)
	}
	if len(req.Services) == 0 {
		a.panel.Append("nothing to read logs from")
	}
	return nil
}

// editServiceSpec hands the written spec to the editor. What the file says
// afterwards decides: written, it is sent back; untouched, it is thrown away.
func (a *App) editServiceSpec(m cmds.ServiceSpecMsg) tea.Cmd {
	if m.Err != nil {
		a.openModal(components.NewModal(components.SevError, "that service cannot be read",
			[]string{m.Err.Error()}, nil))
		return nil
	}
	return a.openEditor(views.EditRequest{
		Path:      m.Path,
		Title:     m.Name,
		Then:      func() tea.Cmd { return cmds.ApplyServiceSpec(a.ctx, a.client, m) },
		Unchanged: func() tea.Cmd { return cmds.DiscardServiceSpec(m.Path) },
	})
}

// swarmBadge is what the header says about the swarm, when there is one.
func (a *App) swarmBadge() (string, bool) {
	st := a.store.SwarmState()
	switch st.State {
	case docker.SwarmLocked:
		return "swarm locked", true
	case docker.SwarmError:
		return "swarm error", true
	case docker.SwarmPending:
		return "swarm joining", false
	case docker.SwarmActive:
	default:
		return "", false
	}
	if !st.Manager {
		return "swarm worker", false
	}
	role := "swarm manager"
	if n, ok := a.store.NodeByID(st.NodeID); ok && n.Leader {
		role = "swarm leader"
	}
	nodes := len(a.store.Nodes)
	if nodes == 0 {
		nodes = st.Nodes
	}
	return fmt.Sprintf("%s, %d node(s)", role, nodes), false
}
