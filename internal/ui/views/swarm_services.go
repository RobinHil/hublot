package views

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/swarm"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

func newServicesTable() components.Table {
	t := components.NewTable([]components.Column{
		{Title: "SERVICE", SortKey: "name", MinWidth: 18},
		{Title: "MODE", Width: 6},
		{Title: "TASKS", Width: 10},
		{Title: "IMAGE", Width: 26, Priority: 2},
		{Title: "PORTS", Width: 16, Priority: 3},
		// Flexible like the name: it carries the scheduler's reason when a
		// service is short of tasks, the one sentence on this screen worth
		// reading whole.
		{Title: "UPDATE", MinWidth: 24, Priority: 1},
	})
	t.Empty = "no services: n creates one, or n on a stack line deploys a stack file"
	return t
}

// refreshServices builds the tree: a line per stack, its services under it,
// and under a service its tasks when it is unfolded.
func (v *Swarm) refreshServices() {
	store := v.deps.Store
	t := &v.tables[secServices]

	var rows []components.Row
	for _, st := range state.Filter(store.Stacks, t.Query(), state.StackFields) {
		rows = append(rows, components.Row{ID: "stack:" + st.Name, Full: v.stackLine(st)})
		if v.folded[st.Name] {
			continue
		}
		for _, svc := range st.Services {
			rows = append(rows, v.serviceRow(svc))
			if !v.open[svc.ID] {
				continue
			}
			for _, slot := range store.Slots(svc) {
				rows = append(rows, components.Row{ID: "task:" + slot.Task.ID, Full: taskLine(slot, "      ", true)})
			}
		}
	}
	t.SetRows(rows)
}

// stackLine carries what decides what can be done with the stack: whether it
// is short of tasks, and which file it is deployed from.
func (v *Swarm) stackLine(st swarm.Stack) string {
	s := theme.Current()
	marker := "v"
	if v.folded[st.Name] {
		marker = ">"
	}
	name := st.Name
	if st.Loose() {
		name = "outside any stack"
	}
	line := s.Faint.Render(marker+" ") + s.Title.Render(name) +
		s.Dim.Render(fmt.Sprintf("  %s, %d/%d tasks", plural(len(st.Services), "service"), st.Running(), st.Desired()))

	switch {
	case len(st.Services) == 0:
		line += s.Warning.Render(fmt.Sprintf("  removed, but %s left behind", leftovers(st)))
	case st.Degraded():
		line += s.Warning.Render("  short of tasks")
	}
	if files := v.deps.Store.StackFiles[st.Name]; len(files) > 0 {
		line += s.Faint.Render("  " + strings.Join(files, ", "))
	} else if !st.Loose() {
		line += s.Faint.Render("  file not known to hublot")
	}
	return line
}

func leftovers(st swarm.Stack) string {
	var parts []string
	if n := len(st.Networks); n > 0 {
		parts = append(parts, plural(n, "network"))
	}
	if n := len(st.Secrets); n > 0 {
		parts = append(parts, plural(n, "secret"))
	}
	if n := len(st.Configs); n > 0 {
		parts = append(parts, plural(n, "config"))
	}
	return strings.Join(parts, ", ")
}

func (v *Swarm) serviceRow(svc docker.Service) components.Row {
	s := theme.Current()
	marker := ">"
	if v.open[svc.ID] {
		marker = "v"
	}
	return components.Row{
		ID:  "svc:" + svc.ID,
		Dim: svc.Desired == 0 && !svc.Job(),
		Cells: []components.Cell{
			components.Txt("  " + marker + " " + swarm.ShortName(svc.Stack(), svc.Name)),
			components.Styled(modeLabel(svc.Mode), s.Dim),
			tasksCell(svc),
			components.Txt(imageLabel(svc.Image)),
			components.Txt(servicePorts(svc.Ports)),
			v.updateCell(svc),
		},
	}
}

func modeLabel(mode string) string {
	switch mode {
	case docker.ModeReplicated:
		return "repl"
	case docker.ModeReplicatedJob:
		return "job"
	case docker.ModeGlobalJob:
		return "g-job"
	default:
		return mode
	}
}

// tasksCell is the running count against what is wanted, coloured by how far
// short it falls. A job counts completions instead.
func tasksCell(svc docker.Service) components.Cell {
	s := theme.Current()
	if svc.Job() {
		want := svc.Replicas
		if svc.Mode == docker.ModeGlobalJob {
			want = svc.Desired + svc.Completed
		}
		return components.Styled(fmt.Sprintf("%d/%d done", svc.Completed, want), s.Dim)
	}
	text := fmt.Sprintf("%d/%d", svc.Running, svc.Desired)
	switch {
	case svc.Running >= svc.Desired:
		return components.Styled(text, s.Running)
	case svc.Running == 0:
		return components.Styled(text, s.Danger)
	default:
		return components.Styled(text, s.Warning)
	}
}

// imageLabel drops the digest a deploy pins the tag to: it is the same image,
// and sixty-four characters of it push everything else off the line.
func imageLabel(image string) string {
	if i := strings.Index(image, "@sha256:"); i > 0 {
		return image[:i]
	}
	return image
}

func servicePorts(ports []docker.ServicePort) string {
	var out []string
	for _, p := range ports {
		s := fmt.Sprintf("%d:%d", p.Published, p.Target)
		if p.Published == 0 {
			s = fmt.Sprintf("%d", p.Target)
		}
		if p.Protocol != "" && p.Protocol != "tcp" {
			s += "/" + p.Protocol
		}
		if p.Mode == "host" {
			s += " host"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// updateCell says what is happening to the service: the scheduler's reason
// when it is short of tasks, otherwise where its last update stands.
func (v *Swarm) updateCell(svc docker.Service) components.Cell {
	s := theme.Current()
	if problem := v.deps.Store.ServiceProblem(svc); problem != "" {
		return components.Styled(problem, s.Danger)
	}
	u := svc.Update
	if u.State == "" {
		return components.Styled("-", s.Faint)
	}
	when := u.EndedAt
	if when.IsZero() {
		when = u.StartedAt
	}
	text := strings.ReplaceAll(u.State, "_", " ") + " " + formatAge(when)
	switch u.State {
	case "completed", "rollback_completed":
		return components.Styled(text, s.Dim)
	case "paused", "rollback_paused":
		return components.Styled(text+": "+u.Message, s.Danger)
	default:
		return components.Styled(text, s.Warning)
	}
}

// taskLine is one slot, drawn across the width: where it runs, what state it
// is in, and the error that explains it when there is one.
func taskLine(slot state.Slot, indent string, withNode bool) string {
	s := theme.Current()
	t := slot.Task
	state := t.State + " " + formatAge(t.At)
	style := s.Running
	switch {
	case t.Failed():
		style = s.Danger
	case t.State != docker.TaskRunning && t.State != docker.TaskComplete:
		style = s.Warning
	}
	line := s.Faint.Render(indent) + s.Text.Render(slot.Name) + "  "
	// Under a node, the node is the line above; saying it again on every
	// task only pushes the error further right.
	if withNode {
		line += s.Dim.Render(slot.Node) + "  "
	}
	line += style.Render(state)
	if slot.Failures > 0 {
		line += s.Warning.Render(fmt.Sprintf("  %d failed before", slot.Failures))
	}
	if t.Err != "" {
		line += s.Danger.Render("  " + t.Err)
	} else if t.Message != "" && t.State != docker.TaskRunning {
		line += s.Faint.Render("  " + t.Message)
	}
	return line
}

// serviceTarget resolves the row under the cursor: the stack, and the service
// and task when the cursor is on one.
func (v *Swarm) serviceTarget() (swarm.Stack, docker.Service, docker.Task, bool) {
	store := v.deps.Store
	kind, rest := v.currentKind()
	switch kind {
	case "stack":
		st, ok := store.StackByName(rest)
		return st, docker.Service{}, docker.Task{}, ok
	case "svc":
		svc, ok := store.ServiceByID(rest)
		st, _ := store.StackByName(svc.Stack())
		return st, svc, docker.Task{}, ok
	case "task":
		t, ok := store.TaskByID(rest)
		if !ok {
			return swarm.Stack{}, docker.Service{}, docker.Task{}, false
		}
		svc, _ := store.ServiceByID(t.ServiceID)
		st, _ := store.StackByName(svc.Stack())
		return st, svc, t, true
	}
	return swarm.Stack{}, docker.Service{}, docker.Task{}, false
}

func (v *Swarm) servicesKey(km tea.KeyMsg) tea.Cmd {
	k := v.deps.Keys.Swarm
	st, svc, task, ok := v.serviceTarget()
	if !ok {
		if key.Matches(km, k.New) {
			return v.newService()
		}
		return nil
	}
	onStack := svc.ID == ""
	onTask := task.ID != ""

	switch {
	case key.Matches(km, k.Expand):
		return v.expandService(st, svc, task)
	case key.Matches(km, k.Logs):
		switch {
		case onTask:
			return v.taskLogs(svc, task)
		case onStack:
			return v.stackLogs(st)
		}
		return v.serviceLogs(svc)
	case key.Matches(km, k.LogsAll):
		return v.stackLogs(st)
	case key.Matches(km, k.Exec):
		return v.execTask(svc, task)
	case key.Matches(km, k.ScaleUp):
		return v.scale(svc, 1)
	case key.Matches(km, k.ScaleDown):
		return v.scale(svc, -1)
	case key.Matches(km, k.Deploy):
		return v.deploy(st, false)
	case key.Matches(km, k.Redeploy):
		return v.forceUpdate(st, onStack)
	case key.Matches(km, k.Rollback):
		return v.rollback(svc)
	case key.Matches(km, k.Remove):
		if onStack {
			return v.removeStack(st)
		}
		return v.removeServices()
	case key.Matches(km, k.Edit):
		if onStack {
			return v.editStack(st)
		}
		return v.editService(svc)
	case key.Matches(km, k.View):
		if onStack {
			return v.readStack(st)
		}
		return v.inspectService(svc)
	case key.Matches(km, k.Config):
		return v.stackConfig(st)
	case key.Matches(km, k.New):
		if onStack {
			return v.newStack()
		}
		return v.newService()
	case key.Matches(km, k.Palette):
		return v.servicePalette(st, svc, task)
	}
	return nil
}

// expandService folds what the line heads: a stack, or the tasks of a
// service. A task heads nothing, so there it opens the detail.
func (v *Swarm) expandService(st swarm.Stack, svc docker.Service, task docker.Task) tea.Cmd {
	switch {
	case task.ID != "":
		return v.inspectTask(task)
	case svc.ID != "":
		v.open[svc.ID] = !v.open[svc.ID]
	default:
		v.folded[st.Name] = !v.folded[st.Name]
	}
	v.refreshServices()
	return nil
}

func (v *Swarm) inspectService(svc docker.Service) tea.Cmd {
	return cmds.InspectSwarmObject(v.deps.Ctx, "service "+svc.Name, func(ctx context.Context) ([]byte, error) {
		return v.deps.Client.InspectService(ctx, svc.ID)
	})
}

func (v *Swarm) inspectTask(t docker.Task) tea.Cmd {
	return cmds.InspectSwarmObject(v.deps.Ctx, "task "+docker.ShortID(t.ID), func(ctx context.Context) ([]byte, error) {
		return v.deps.Client.InspectTask(ctx, t.ID)
	})
}

// taskNames labels log lines by slot, which is what tells two replicas'
// output apart.
func (v *Swarm) taskNames(svcs ...docker.Service) map[string]string {
	names := map[string]string{}
	for _, svc := range svcs {
		for _, t := range v.deps.Store.Tasks {
			if t.ServiceID != svc.ID {
				continue
			}
			short := swarm.ShortName(svc.Stack(), svc.Name)
			if t.Slot > 0 {
				names[t.ID] = fmt.Sprintf("%s.%d", short, t.Slot)
			} else {
				names[t.ID] = short + "." + v.deps.Store.NodeName(t.NodeID)
			}
		}
	}
	return names
}

func (v *Swarm) serviceLogs(svc docker.Service) tea.Cmd {
	return request(SwarmLogsRequest{
		Title:    "logs: " + svc.Name,
		Source:   "service:" + svc.ID,
		Services: []docker.Service{svc},
		Names:    v.taskNames(svc),
	})
}

func (v *Swarm) stackLogs(st swarm.Stack) tea.Cmd {
	if len(st.Services) == 0 {
		return v.info("nothing to read", "This stack has no services left to read logs from.")
	}
	title := "logs: " + st.Name
	if st.Loose() {
		title = "logs: services outside any stack"
	}
	return request(SwarmLogsRequest{
		Title:    title,
		Source:   "stack:" + st.Name,
		Services: st.Services,
		Names:    v.taskNames(st.Services...),
	})
}

func (v *Swarm) taskLogs(svc docker.Service, t docker.Task) tea.Cmd {
	return request(SwarmLogsRequest{
		Title:   "logs: " + v.taskNames(svc)[t.ID],
		Source:  "task:" + t.ID,
		TaskID:  t.ID,
		TaskTTY: svc.TTY,
	})
}

// execTask opens a shell in a task, which is only possible when it runs on
// this machine: hublot talks to the local daemon and nothing else.
func (v *Swarm) execTask(svc docker.Service, t docker.Task) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if t.ID == "" {
		return v.info("a shell runs in one task",
			"Unfold the service with enter and put the cursor on one of its tasks.")
	}
	self := v.deps.Store.SwarmState().NodeID
	if t.NodeID != self {
		return v.info("that task runs on "+v.deps.Store.NodeName(t.NodeID),
			"hublot talks to the daemon of this machine only, and a shell has to be opened",
			"by the daemon running the container. l still reads its logs from here:",
			"the managers fetch them from whichever node it runs on.")
	}
	if t.ContainerID == "" || t.State != docker.TaskRunning {
		return v.info("that task is not running", "Only a running task has a container to open a shell in.")
	}
	return request(ExecRequest{ContainerID: t.ContainerID, Name: v.taskNames(svc)[t.ID]})
}

func (v *Swarm) scale(svc docker.Service, delta int) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if svc.ID == "" {
		return v.info("no service selected", "Scaling applies to one service: put the cursor on its line.")
	}
	if !svc.Scalable() {
		return v.info(svc.Name+" cannot be scaled",
			fmt.Sprintf("A %s service runs one task per eligible node, or runs to completion:", svc.Mode),
			"there is no replica count to change. Constraints decide where it runs.")
	}
	want := int(svc.Replicas) + delta
	if want < 0 {
		return nil
	}
	n := uint64(want)
	label := fmt.Sprintf("scaled %s to %d", svc.Name, n)
	return cmds.UpdateService(v.deps.Ctx, v.deps.Client, svc.ID, label, docker.ServiceChange{Replicas: &n})
}

// forceUpdate restarts every task of a service, or of every service of the
// stack from its line, rolling through them the way an update does.
func (v *Swarm) forceUpdate(st swarm.Stack, wholeStack bool) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	var ids, names []string
	if wholeStack {
		for _, svc := range st.Services {
			ids, names = append(ids, svc.ID), append(names, "  "+svc.Name)
		}
	} else {
		for _, id := range v.targetsOf("svc") {
			if svc, ok := v.deps.Store.ServiceByID(id); ok {
				ids, names = append(ids, id), append(names, "  "+svc.Name)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	body := append([]string{"Every task of these services is replaced, following each one's update settings:"}, names...)
	body = append(body, "", "The image is pulled again only where its tag moved; nothing else changes.")
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    "Restart the tasks of " + plural(len(ids), "service"),
		Body:     body,
		Run: func() tea.Cmd {
			return cmds.UpdateServices(v.deps.Ctx, v.deps.Client, ids, "restarted", docker.ServiceChange{ForceUpdate: true})
		},
	})
}

func (v *Swarm) rollback(svc docker.Service) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if svc.ID == "" {
		return v.info("no service selected", "A rollback applies to one service: put the cursor on its line.")
	}
	if !svc.CanRollback {
		return v.info(svc.Name+" has nothing to roll back to",
			"The daemon keeps the spec a service had before its last update; this one has never been updated.")
	}
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    "Roll " + svc.Name + " back",
		Body: []string{
			"The spec it had before its last update is put back, and its tasks are",
			"replaced following its rollback settings. Rolling back twice returns to",
			"where it is now.",
		},
		Run: func() tea.Cmd { return cmds.RollbackService(v.deps.Ctx, v.deps.Client, svc.ID, svc.Name) },
	})
}

func (v *Swarm) removeServices() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	ids := v.targetsOf("svc")
	if len(ids) == 0 {
		return nil
	}
	var lines []string
	stacks := map[string]bool{}
	for _, id := range ids {
		svc, _ := v.deps.Store.ServiceByID(id)
		lines = append(lines, fmt.Sprintf("  %s  %d task(s)", svc.Name, svc.Running))
		if svc.Stack() != "" {
			stacks[svc.Stack()] = true
		}
	}
	body := append([]string{"These services are removed and every one of their tasks stopped:"}, lines...)
	if len(stacks) > 0 {
		body = append(body, "", "They belong to a stack: deploying that stack again brings them back.")
	}
	sev := components.SevConfirm
	if len(ids) > 1 {
		sev = components.SevDanger
	}
	return request(ConfirmRequest{
		Severity: sev,
		Title:    "Remove " + plural(len(ids), "service"),
		Body:     body,
		Run:      func() tea.Cmd { return cmds.RemoveServices(v.deps.Ctx, v.deps.Client, ids) },
	})
}

// editService hands the service's spec to the editor. A service deployed by a
// stack is the stack file's to describe: an edit made here lasts until that
// stack is next deployed, which the dialog says before anything opens.
func (v *Swarm) editService(svc docker.Service) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	edit := func() tea.Cmd { return cmds.WriteServiceSpec(v.deps.Ctx, v.deps.Client, svc.ID, svc.Name) }
	if svc.Stack() == "" {
		return edit()
	}
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    svc.Name + " belongs to the stack " + svc.Stack(),
		Body: []string{
			"An edit made to the service itself lasts until " + svc.Stack() + " is next deployed,",
			"which puts back whatever its file says. To change it for good, E on the",
			"stack line edits that file instead.",
			"",
			"Edit the service anyway?",
		},
		Run: edit,
	})
}

// servicesHints name what the row under the cursor offers.
func (v *Swarm) servicesHints() []key.Binding {
	k := v.deps.Keys.Swarm
	st, svc, task, ok := v.serviceTarget()
	switch {
	case !ok:
		return []key.Binding{k.New}
	case task.ID != "":
		return []key.Binding{rename(k.Expand, "detail"), rename(k.Logs, "logs"), k.Exec, k.Palette}
	case svc.ID != "":
		hints := []key.Binding{rename(k.Expand, "tasks"), rename(k.Logs, "logs")}
		if svc.Scalable() {
			hints = append(hints, k.ScaleUp, k.ScaleDown)
		}
		hints = append(hints, rename(k.Redeploy, "restart"))
		if svc.CanRollback {
			hints = append(hints, k.Rollback)
		}
		return append(hints, rename(k.Edit, "edit spec"), k.Remove, k.Palette)
	case st.Loose():
		return []key.Binding{rename(k.Expand, "fold"), rename(k.Logs, "logs"), rename(k.New, "new service")}
	default:
		return []key.Binding{rename(k.Expand, "fold"), rename(k.Logs, "logs"), k.Deploy,
			rename(k.Edit, "edit file"), rename(k.Remove, "remove stack"), k.Palette}
	}
}
