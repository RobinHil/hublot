package views

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

func newNodesTable() components.Table {
	t := components.NewTable([]components.Column{
		{Title: "NODE", SortKey: "name", MinWidth: 18},
		{Title: "ROLE", Width: 11},
		{Title: "AVAIL", Width: 7},
		{Title: "STATUS", Width: 8},
		{Title: "TASKS", Width: 5, Right: true},
		{Title: "CPU", Width: 4, Right: true, Priority: 3},
		{Title: "MEM", Width: 7, Right: true, Priority: 3},
		{Title: "ENGINE", Width: 8, Priority: 4},
		{Title: "ADDRESS", Width: 15, Priority: 2},
		{Title: "LABELS", Width: 24, Priority: 5},
	})
	t.Empty = "no nodes"
	return t
}

func (v *Swarm) refreshNodes() {
	store := v.deps.Store
	t := &v.tables[secNodes]

	var rows []components.Row
	for _, n := range state.Filter(store.Nodes, t.Query(), state.NodeFields) {
		rows = append(rows, v.nodeRow(n))
		if !v.open[n.ID] {
			continue
		}
		slots := store.NodeTasks(n.ID)
		if len(slots) == 0 {
			rows = append(rows, components.Row{ID: "none:" + n.ID, Full: theme.Current().Faint.Render("      no task runs here")})
		}
		for _, slot := range slots {
			rows = append(rows, components.Row{ID: "ntask:" + slot.Task.ID, Full: taskLine(slot, "      ", false)})
		}
	}
	t.SetRows(rows)
}

func (v *Swarm) nodeRow(n docker.Node) components.Row {
	s := theme.Current()
	marker := ">"
	if v.open[n.ID] {
		marker = "v"
	}
	name := n.Hostname
	if n.ID == v.deps.Store.SwarmState().NodeID {
		name += " (this one)"
	}

	statusStyle := s.Running
	if n.State != "ready" {
		statusStyle = s.Danger
	}
	availStyle := s.Dim
	if n.Availability != "active" {
		availStyle = s.Warning
	}

	return components.Row{
		ID:  "node:" + n.ID,
		Dim: n.State != "ready",
		Cells: []components.Cell{
			components.Txt(marker + " " + name),
			roleCell(n),
			components.Styled(n.Availability, availStyle),
			components.Styled(n.State, statusStyle),
			components.Txt(fmt.Sprintf("%d", v.deps.Store.RunningOn(n.ID))),
			components.Txt(fmt.Sprintf("%g", float64(n.NanoCPUs)/1e9)),
			components.Txt(state.FormatBytes(n.Memory)),
			components.Txt(n.EngineVersion),
			components.Txt(n.Addr),
			components.Styled(strings.Join(n.LabelList(), " "), s.Dim),
		},
	}
}

// roleCell names the part a node plays in the quorum, which is what decides
// whether losing it matters.
func roleCell(n docker.Node) components.Cell {
	s := theme.Current()
	switch {
	case n.Leader:
		return components.Styled("leader", s.Accent)
	case n.Manager() && n.Reachability == "unreachable":
		return components.Styled("unreachable", s.Danger)
	case n.Manager():
		return components.Styled("manager", s.Text)
	default:
		return components.Styled("worker", s.Dim)
	}
}

// nodeTarget resolves the row under the cursor: the node, and the task when
// the cursor is on one of the tasks it runs.
func (v *Swarm) nodeTarget() (docker.Node, docker.Task, bool) {
	store := v.deps.Store
	kind, rest := v.currentKind()
	switch kind {
	case "node", "none":
		n, ok := store.NodeByID(rest)
		return n, docker.Task{}, ok
	case "ntask":
		t, ok := store.TaskByID(rest)
		if !ok {
			return docker.Node{}, docker.Task{}, false
		}
		n, _ := store.NodeByID(t.NodeID)
		return n, t, true
	}
	return docker.Node{}, docker.Task{}, false
}

func (v *Swarm) nodesKey(km tea.KeyMsg) tea.Cmd {
	k := v.deps.Keys.Swarm
	n, t, ok := v.nodeTarget()
	if !ok {
		return nil
	}
	svc, _ := v.deps.Store.ServiceByID(t.ServiceID)

	switch {
	case key.Matches(km, k.Expand):
		if t.ID != "" {
			return v.inspectTask(t)
		}
		v.open[n.ID] = !v.open[n.ID]
		v.refreshNodes()
		return nil
	case key.Matches(km, k.View):
		return v.inspectNode(n)
	case key.Matches(km, k.Logs):
		if t.ID != "" {
			return v.taskLogs(svc, t)
		}
		return v.info("logs are read per service or per task",
			"Unfold the node with enter and put the cursor on one of its tasks.")
	case key.Matches(km, k.Exec):
		return v.execTask(svc, t)
	case key.Matches(km, k.Drain):
		return v.toggleAvailability(n, "drain")
	case key.Matches(km, k.Pause):
		return v.toggleAvailability(n, "pause")
	case key.Matches(km, k.Remove):
		return v.removeNode(n)
	case key.Matches(km, k.Palette):
		return v.nodePalette(n)
	}
	return nil
}

func (v *Swarm) inspectNode(n docker.Node) tea.Cmd {
	return cmds.InspectSwarmObject(v.deps.Ctx, "node "+n.Hostname, func(ctx context.Context) ([]byte, error) {
		return v.deps.Client.InspectNode(ctx, n.ID)
	})
}

// toggleAvailability drains or pauses a node, or puts it back to active.
// Draining moves its tasks elsewhere, which is what the dialog says before it
// happens; going back to active moves nothing back by itself.
func (v *Swarm) toggleAvailability(n docker.Node, to string) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if n.Availability == to {
		return cmds.UpdateNode(v.deps.Ctx, v.deps.Client, n.ID, n.Hostname+" is active again",
			docker.NodeChange{Availability: "active"})
	}
	change := docker.NodeChange{Availability: to}
	if to == "pause" {
		return cmds.UpdateNode(v.deps.Ctx, v.deps.Client, n.ID,
			n.Hostname+" paused: nothing new is scheduled there", change)
	}

	running := v.deps.Store.RunningOn(n.ID)
	body := []string{
		fmt.Sprintf("The %d task(s) running on %s are stopped there and rescheduled on other nodes,", running, n.Hostname),
		"and nothing new is placed on it until it is made active again.",
		"Going back to active moves nothing back: the swarm only places new tasks there.",
	}
	if n.ID == v.deps.Store.SwarmState().NodeID {
		body = append(body, "", "This is the machine hublot runs on.")
	}
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    "Drain " + n.Hostname,
		Body:     body,
		Run: func() tea.Cmd {
			return cmds.UpdateNode(v.deps.Ctx, v.deps.Client, n.ID, n.Hostname+" drained", change)
		},
	})
}

// removeNode takes a node out of the swarm's records. A node still up is
// refused by the daemon unless forced, and forcing it is the case where the
// machine keeps running tasks nobody manages any more: that one is typed.
func (v *Swarm) removeNode(n docker.Node) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if n.ID == v.deps.Store.SwarmState().NodeID {
		return v.info("this is the node hublot runs on",
			"A node cannot remove itself: leave the swarm from it instead, with leave in the palette.")
	}
	if n.State == "down" {
		return request(ConfirmRequest{
			Severity: components.SevConfirm,
			Title:    "Remove " + n.Hostname,
			Body: []string{
				n.Hostname + " is down. Its record goes, and the swarm stops waiting for it.",
				"If it comes back it has to join again.",
			},
			Run: func() tea.Cmd { return cmds.RemoveNode(v.deps.Ctx, v.deps.Client, n.ID, n.Hostname, false) },
		})
	}
	body := []string{
		n.Hostname + " is " + n.State + ". The daemon refuses to remove a node that is up unless",
		"forced, and forcing it leaves that machine running its tasks with no manager.",
		"The usual way is to leave the swarm from the node itself, then remove it here.",
	}
	if n.Manager() {
		total, _ := v.deps.Store.Managers()
		body = append(body, "")
		body = append(body, state.QuorumChange(total, -1)...)
	}
	req := ConfirmRequest{
		Severity: components.SevTyped,
		Title:    "Force the removal of " + n.Hostname,
		Body:     body,
		Word:     n.Hostname,
		Run:      func() tea.Cmd { return cmds.RemoveNode(v.deps.Ctx, v.deps.Client, n.ID, n.Hostname, true) },
	}
	return request(req)
}

// nodePalette carries promotion, demotion and labels.
func (v *Swarm) nodePalette(n docker.Node) tea.Cmd {
	update := func(label string, change docker.NodeChange) func() tea.Cmd {
		return func() tea.Cmd { return cmds.UpdateNode(v.deps.Ctx, v.deps.Client, n.ID, label, change) }
	}
	choices := []components.Choice{
		{Label: "inspect", Payload: func() tea.Cmd { return v.inspectNode(n) }},
		{Label: "tasks", Detail: "fold or unfold what runs there", Payload: func() tea.Cmd {
			v.open[n.ID] = !v.open[n.ID]
			v.refreshNodes()
			return nil
		}},
	}
	for _, a := range []string{"active", "pause", "drain"} {
		if a != n.Availability {
			to := a
			choices = append(choices, components.Choice{Label: "make it " + to, Destructive: true, Payload: func() tea.Cmd {
				if to == "active" {
					return update(n.Hostname+" is active again", docker.NodeChange{Availability: to})()
				}
				return v.toggleAvailability(n, to)
			}})
		}
	}
	if n.Manager() {
		choices = append(choices, components.Choice{Label: "demote to worker", Detail: "it leaves the quorum",
			Destructive: true, Payload: func() tea.Cmd { return v.changeRole(n, "worker") }})
	} else {
		choices = append(choices, components.Choice{Label: "promote to manager", Detail: "it joins the quorum",
			Destructive: true, Payload: func() tea.Cmd { return v.changeRole(n, "manager") }})
	}
	choices = append(choices, components.Choice{
		Label: "set a label", Detail: "for placement constraints", Destructive: true,
		Payload: func() tea.Cmd {
			return request(PromptRequest{
				Title: "Label " + n.Hostname,
				Body:  []string{"Services find it with a constraint such as node.labels.zone==eu."},
				Label: "key=value: ",
				Run: func(pair string) tea.Cmd {
					return update("labelled "+n.Hostname+" "+pair, docker.NodeChange{SetLabel: pair})()
				},
			})
		},
	})
	for _, pair := range n.LabelList() {
		name, _, _ := strings.Cut(pair, "=")
		choices = append(choices, components.Choice{Label: "drop label " + name, Destructive: true,
			Payload: update("dropped "+name+" from "+n.Hostname, docker.NodeChange{RemoveLabel: name})})
	}
	choices = append(choices, components.Choice{Label: "remove", Detail: "from the swarm's records",
		Destructive: true, Payload: func() tea.Cmd { return v.removeNode(n) }})
	if v.deps.ReadOnly {
		choices = keepSafe(choices)
	}
	return request(PickerRequest{Title: "Actions on " + n.Hostname, Choices: choices})
}

// changeRole promotes or demotes, with the quorum arithmetic in front of it:
// this is the action that breaks a cluster when it is done blind.
func (v *Swarm) changeRole(n docker.Node, role string) tea.Cmd {
	total, _ := v.deps.Store.Managers()
	delta, verb := 1, "Promote"
	if role == "worker" {
		delta, verb = -1, "Demote"
	}
	body := state.QuorumChange(total, delta)
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    verb + " " + n.Hostname,
		Body:     body,
		Run: func() tea.Cmd {
			return cmds.UpdateNode(v.deps.Ctx, v.deps.Client, n.ID,
				strings.ToLower(verb)+"d "+n.Hostname, docker.NodeChange{Role: role})
		},
	})
}

func (v *Swarm) nodesHints() []key.Binding {
	k := v.deps.Keys.Swarm
	n, t, ok := v.nodeTarget()
	if !ok {
		return nil
	}
	if t.ID != "" {
		return []key.Binding{rename(k.Expand, "detail"), rename(k.Logs, "logs"), k.Exec}
	}
	drain, pause := "drain", "pause"
	if n.Availability == "drain" {
		drain = "activate"
	}
	if n.Availability == "pause" {
		pause = "activate"
	}
	return []key.Binding{rename(k.Expand, "tasks"), rename(k.View, "inspect"),
		rename(k.Drain, drain), rename(k.Pause, pause), k.Remove, k.Palette}
}
