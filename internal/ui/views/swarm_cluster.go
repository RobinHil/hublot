package views

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

func newClusterTable() components.Table {
	t := components.NewTable([]components.Column{
		{Title: "SETTING", MinWidth: 20},
		{Title: "VALUE", Width: 60},
	})
	t.Empty = "reading the swarm settings..."
	return t
}

// setting is one row of the cluster section: what it is, its value, and how
// to change it when it can be changed after the swarm was created.
type setting struct {
	id, label, value string
	change           func() tea.Cmd
}

func (v *Swarm) settings() []setting {
	store := v.deps.Store
	c := store.Cluster
	if c == nil {
		return nil
	}
	total, reachable := store.Managers()
	needed, tolerated := state.Quorum(total)
	workers := len(store.Nodes) - total

	autolock := "off: a restarted manager rejoins on its own"
	if c.Autolock {
		autolock = "on: a restarted manager waits for its unlock key"
	}
	ca := "managed by the swarm"
	if len(c.ExternalCAs) > 0 {
		ca = "external: " + strings.Join(c.ExternalCAs, ", ")
	}
	if c.RootRotation {
		ca += ", root rotation in progress"
	}

	return []setting{
		{id: "id", label: "cluster", value: fmt.Sprintf("%s, created %s ago", docker.ShortID(c.ID), formatAge(c.CreatedAt))},
		{id: "managers", label: "managers", value: fmt.Sprintf("%d, %d reachable; quorum needs %d, survives losing %d",
			total, reachable, needed, tolerated)},
		{id: "workers", label: "workers", value: strconv.Itoa(workers)},
		{id: "tokens", label: "join tokens", value: "hidden: v shows the join commands"},
		{id: "autolock", label: "autolock", value: autolock, change: v.toggleAutolock},
		{id: "history", label: "task history", value: fmt.Sprintf("%d task(s) kept per slot", c.TaskHistory),
			change: v.askNumber("task history", "How many finished tasks the swarm keeps for each slot.", c.TaskHistory,
				func(n int64) docker.ClusterChange { return docker.ClusterChange{TaskHistory: &n} })},
		{id: "certexpiry", label: "node certificates", value: "renewed every " + c.CertExpiry.String(),
			change: v.askDuration("node certificate validity", "How long a node certificate lasts, such as 2160h.", c.CertExpiry,
				func(d time.Duration) docker.ClusterChange { return docker.ClusterChange{CertExpiry: &d} })},
		{id: "heartbeat", label: "heartbeat", value: "every " + c.Heartbeat.String(),
			change: v.askDuration("heartbeat period", "How often nodes report to the managers, such as 5s.", c.Heartbeat,
				func(d time.Duration) docker.ClusterChange { return docker.ClusterChange{Heartbeat: &d} })},
		{id: "snapshot", label: "raft snapshots", value: fmt.Sprintf("every %d entries", c.SnapshotInterval),
			change: v.askNumber("raft snapshot interval", "How many log entries between two snapshots.", int64(c.SnapshotInterval),
				func(n int64) docker.ClusterChange { u := uint64(n); return docker.ClusterChange{SnapshotInterval: &u} })},
		{id: "keepsnap", label: "old snapshots", value: fmt.Sprintf("%d kept", c.KeepOldSnapshots),
			change: v.askNumber("old raft snapshots", "How many snapshots to keep besides the current one.", int64(c.KeepOldSnapshots),
				func(n int64) docker.ClusterChange { u := uint64(n); return docker.ClusterChange{KeepOldSnapshots: &u} })},
		{id: "ca", label: "certificate authority", value: ca},
		{id: "pool", label: "address pool", value: fmt.Sprintf("%s, /%d per network, set when the swarm was created",
			addrPool(c.DefaultAddrPool), c.SubnetSize)},
		{id: "datapath", label: "data path port", value: fmt.Sprintf("%d, set when the swarm was created", c.DataPathPort)},
	}
}

// addrPool names the pool overlay subnets are cut from. The daemon reports
// nothing when the swarm was created with its default.
func addrPool(pool []string) string {
	if len(pool) == 0 {
		return "10.0.0.0/8, the default"
	}
	return strings.Join(pool, ", ")
}

func (v *Swarm) refreshCluster() {
	s := theme.Current()
	var rows []components.Row
	for _, set := range v.settings() {
		style := s.Dim
		if set.change != nil {
			style = s.Text
		}
		rows = append(rows, components.Row{
			ID:    "set:" + set.id,
			Cells: []components.Cell{components.Txt(set.label), components.Styled(set.value, style)},
		})
	}
	v.tables[secCluster].SetRows(rows)
}

func (v *Swarm) clusterKey(km tea.KeyMsg) tea.Cmd {
	k := v.deps.Keys.Swarm
	_, id := v.currentKind()
	switch {
	case key.Matches(km, k.Expand):
		for _, set := range v.settings() {
			if set.id != id {
				continue
			}
			if set.change == nil {
				return v.info(set.label+" cannot be changed here",
					"It is fixed when the swarm is created, or only read: "+set.value)
			}
			if v.deps.ReadOnly {
				return denied()
			}
			return set.change()
		}
	case key.Matches(km, k.View):
		return cmds.ShowJoinCommands(v.deps.Ctx, v.deps.Client, v.managerAddr())
	case key.Matches(km, k.Palette):
		return v.clusterPalette()
	}
	return nil
}

// managerAddr is the address other machines join through: this manager's
// own, as the swarm advertises it.
func (v *Swarm) managerAddr() string {
	st := v.deps.Store.SwarmState()
	if n, ok := v.deps.Store.NodeByID(st.NodeID); ok && n.ManagerAddr != "" {
		return n.ManagerAddr
	}
	return net.JoinHostPort(st.NodeAddr, "2377")
}

func (v *Swarm) clusterChange(label string, change docker.ClusterChange) tea.Cmd {
	return cmds.UpdateCluster(v.deps.Ctx, v.deps.Client, label, change)
}

func (v *Swarm) askNumber(what, hint string, current int64, change func(int64) docker.ClusterChange) func() tea.Cmd {
	return func() tea.Cmd {
		return request(PromptRequest{
			Title: "Change the " + what, Body: []string{hint}, Label: what + ": ",
			Initial: strconv.FormatInt(current, 10),
			Run: func(value string) tea.Cmd {
				n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
				if err != nil || n < 0 {
					return v.info("that is not a count", fmt.Sprintf("%q is not a whole number.", value))
				}
				return v.clusterChange(fmt.Sprintf("%s set to %d", what, n), change(n))
			},
		})
	}
}

func (v *Swarm) askDuration(what, hint string, current time.Duration, change func(time.Duration) docker.ClusterChange) func() tea.Cmd {
	return func() tea.Cmd {
		return request(PromptRequest{
			Title: "Change the " + what, Body: []string{hint}, Label: what + ": ",
			Initial: current.String(),
			Run: func(value string) tea.Cmd {
				d, err := time.ParseDuration(strings.TrimSpace(value))
				if err != nil || d <= 0 {
					return v.info("that is not a duration", fmt.Sprintf("%q is not a duration such as 90s or 2160h.", value))
				}
				return v.clusterChange(what+" set to "+d.String(), change(d))
			},
		})
	}
}

// toggleAutolock turns autolock on or off. On means the next restart of any
// manager needs the key, which is shown straight after.
func (v *Swarm) toggleAutolock() tea.Cmd {
	c := v.deps.Store.Cluster
	on := !c.Autolock
	if !on {
		return request(ConfirmRequest{
			Severity: components.SevConfirm,
			Title:    "Turn autolock off",
			Body:     []string{"Managers will restart without asking for a key, and the raft keys they", "hold go back to being stored unencrypted on disk."},
			Run:      func() tea.Cmd { return v.clusterChange("autolock off", docker.ClusterChange{Autolock: &on}) },
		})
	}
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    "Turn autolock on",
		Body: []string{
			"From now on, a manager that restarts stays locked until it is given the unlock",
			"key. The key is shown next: keep it, because a manager restarted without it",
			"cannot rejoin, and losing it on every manager means rebuilding the swarm.",
		},
		Run: func() tea.Cmd {
			return tea.Sequence(
				v.clusterChange("autolock on", docker.ClusterChange{Autolock: &on}),
				cmds.ShowUnlockKey(v.deps.Ctx, v.deps.Client),
			)
		},
	})
}

func (v *Swarm) clusterPalette() tea.Cmd {
	choices := []components.Choice{
		{Label: "join commands", Detail: "for a new worker or manager, tokens included",
			Payload: func() tea.Cmd { return cmds.ShowJoinCommands(v.deps.Ctx, v.deps.Client, v.managerAddr()) }},
		{Label: "inspect", Detail: "the swarm's raw settings, tokens left out",
			Payload: func() tea.Cmd {
				return cmds.InspectSwarmObject(v.deps.Ctx, "swarm", func(ctx context.Context) ([]byte, error) {
					return v.deps.Client.InspectClusterRaw(ctx)
				})
			}},
		{Label: "rotate the worker token", Detail: "nodes already in stay in", Destructive: true,
			Payload: func() tea.Cmd {
				return v.clusterChange("worker token rotated", docker.ClusterChange{RotateWorkerToken: true})
			}},
		{Label: "rotate the manager token", Detail: "nodes already in stay in", Destructive: true,
			Payload: func() tea.Cmd {
				return v.clusterChange("manager token rotated", docker.ClusterChange{RotateManagerToken: true})
			}},
		{Label: "show the unlock key", Detail: "needed by a manager restarted with autolock on",
			Destructive: true, Payload: func() tea.Cmd { return cmds.ShowUnlockKey(v.deps.Ctx, v.deps.Client) }},
		{Label: "rotate the unlock key", Destructive: true, Payload: func() tea.Cmd {
			return tea.Sequence(
				v.clusterChange("unlock key rotated", docker.ClusterChange{RotateUnlockKey: true}),
				cmds.ShowUnlockKey(v.deps.Ctx, v.deps.Client),
			)
		}},
		{Label: "rotate the root CA", Detail: "every node gets a new certificate", Destructive: true,
			Payload: func() tea.Cmd { return v.rotateCA() }},
		{Label: "toggle autolock", Destructive: true, Payload: v.toggleAutolock},
		{Label: "leave the swarm", Detail: "this node only", Destructive: true, Payload: v.leave},
	}
	if v.deps.ReadOnly {
		choices = keepSafe(choices)
	}
	return request(PickerRequest{Title: "Actions on the swarm", Choices: choices})
}

func (v *Swarm) rotateCA() tea.Cmd {
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    "Rotate the root certificate",
		Body: []string{
			"The managers issue a new root and every node is moved onto it, one",
			"certificate at a time. A node that is down while it happens has to",
			"rejoin. Nothing running is interrupted.",
		},
		Run: func() tea.Cmd {
			return v.clusterChange("root CA rotation started", docker.ClusterChange{RotateCA: true})
		},
	})
}

// leave takes this node out of the swarm, which on a manager is the action
// that can end the swarm itself: the dialog says which case this is.
func (v *Swarm) leave() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	st := v.deps.Store.SwarmState()
	if !st.Manager || !st.Active() {
		return request(ConfirmRequest{
			Severity: components.SevConfirm,
			Title:    "Leave the swarm",
			Body: []string{
				"This node stops being part of the swarm. Its tasks are stopped here and",
				"rescheduled elsewhere by the managers; its record stays on them, as down,",
				"until it is removed from the Nodes section of a manager.",
			},
			Run: func() tea.Cmd { return cmds.LeaveSwarm(v.deps.Ctx, v.deps.Client, true) },
		})
	}

	total, _ := v.deps.Store.Managers()
	body := []string{"This node is a manager, so leaving needs force and takes its vote with it."}
	if total <= 1 {
		body = append(body, "",
			"It is the only manager: the swarm ends here. Every service, secret and config",
			"is lost, and the other nodes are left with nothing to answer to.")
	} else {
		body = append(body, "")
		body = append(body, state.QuorumChange(total, -1)...)
		body = append(body, "", "Demoting it first, from the Nodes section, is the gentler way.")
	}
	return request(ConfirmRequest{
		Severity: components.SevTyped,
		Title:    "Leave the swarm from a manager",
		Body:     body,
		Word:     "leave",
		Run:      func() tea.Cmd { return cmds.LeaveSwarm(v.deps.Ctx, v.deps.Client, true) },
	})
}
