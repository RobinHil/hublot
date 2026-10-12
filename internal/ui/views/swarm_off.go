package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// offView explains where this node stands when there is no swarm to list:
// not in one, locked, failing, or a worker, which is refused the lists.
func (v *Swarm) offView() string {
	s := theme.Current()
	st := v.deps.Store.SwarmState()
	k := v.deps.Keys.Swarm

	keyLine := func(b key.Binding, text string) string {
		return "  " + s.Key.Render(b.Help().Key) + "  " + s.Text.Render(text)
	}

	var lines []string
	switch st.State {
	case docker.SwarmInactive:
		lines = []string{
			s.Title.Render("This daemon is not part of a swarm."),
			"",
			s.Dim.Render("A swarm runs services across several machines: the managers keep each one"),
			s.Dim.Render("at the number of tasks it asks for, moving them when a node goes away."),
			s.Dim.Render("A single machine can be a swarm of one, which is how most people start."),
			"",
			keyLine(k.Init, "create a swarm, with this node as its first manager"),
			keyLine(k.Join, "join an existing swarm, with a token from one of its managers"),
		}
	case docker.SwarmLocked:
		lines = []string{
			s.Danger.Render("This manager is locked."),
			"",
			s.Dim.Render("Autolock is on, and the daemon restarted: until it is given the unlock key it"),
			s.Dim.Render("cannot read the swarm's state, and nothing can be listed or changed from here."),
			s.Dim.Render("Another manager shows the key with docker swarm unlock-key."),
			"",
			keyLine(k.Unlock, "unlock it"),
		}
	case docker.SwarmPending:
		lines = []string{
			s.Warning.Render("Joining the swarm..."),
			"",
			s.Dim.Render("The daemon is still reaching the managers. r refreshes."),
		}
	case docker.SwarmError:
		lines = []string{
			s.Danger.Render("The swarm is in an error state on this node."),
			"",
			s.Text.Render(st.Error),
			"",
			keyLine(k.Palette, "leave it, to start again"),
		}
	default:
		lines = v.workerLines()
	}
	if err := v.deps.Store.SwarmErr; err != nil && st.Active() {
		lines = append(lines, "", s.Danger.Render(err.Error()))
	}
	return strings.Join(lines, "\n")
}

// workerLines is what a worker can say about the swarm: who manages it, and
// what runs here. The managers hold the rest.
func (v *Swarm) workerLines() []string {
	s := theme.Current()
	st := v.deps.Store.SwarmState()
	lines := []string{
		s.Title.Render("This node is a worker of a swarm."),
		"",
		s.Dim.Render("Services, nodes, secrets and configs are only listed by a manager, which"),
		s.Dim.Render("refuses a worker. Open hublot on one of these to manage the swarm:"),
	}
	for _, addr := range st.RemoteManagers {
		lines = append(lines, "  "+s.Text.Render(addr))
	}
	lines = append(lines, "", s.Title.Render("Running here"))
	tasks := v.deps.Store.TaskContainers()
	if len(tasks) == 0 {
		lines = append(lines, s.Faint.Render("  no task of the swarm runs on this node"))
	}
	for _, c := range tasks {
		lines = append(lines, "  "+s.Text.Render(c.Labels[docker.LabelSwarmTask])+"  "+s.Dim.Render(formatStatus(c)))
	}
	lines = append(lines, "", s.Faint.Render("  The Containers view acts on them; x offers to leave the swarm."))
	return lines
}

func (v *Swarm) offHints() []key.Binding {
	k := v.deps.Keys.Swarm
	switch v.deps.Store.SwarmState().State {
	case docker.SwarmInactive:
		return []key.Binding{k.Init, k.Join}
	case docker.SwarmLocked:
		return []key.Binding{k.Unlock}
	case docker.SwarmPending:
		return nil
	}
	return []key.Binding{rename(k.Palette, "leave")}
}

// updateOff handles the keys of a node with no swarm to list.
func (v *Swarm) updateOff(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	k := v.deps.Keys.Swarm
	st := v.deps.Store.SwarmState()
	switch {
	case st.State == docker.SwarmInactive && key.Matches(km, k.Init):
		return v.initForm()
	case st.State == docker.SwarmInactive && key.Matches(km, k.Join):
		return v.joinForm()
	case st.State == docker.SwarmLocked && key.Matches(km, k.Unlock):
		return v.unlockPrompt()
	case st.State != docker.SwarmInactive && key.Matches(km, k.Palette):
		return v.leave()
	}
	return nil
}

func (v *Swarm) initForm() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return request(FormRequest{
		Title:    "Create a swarm",
		Subtitle: "this node becomes its first manager",
		Fields: []components.Field{
			{Name: "advertise", Label: "advertise address",
				Hint: "where other nodes reach this one; empty to let it pick"},
			{Name: "listen", Label: "listen address", Value: "0.0.0.0:2377", Hint: "where the managers' API listens"},
			{Name: "datapath", Label: "data path address", Hint: "optional: a separate interface for service traffic"},
			{Name: "pool", Label: "address pool", Hint: "optional: where overlay subnets come from"},
			{Name: "autolock", Label: "autolock", Value: "no", Options: []string{"no", "yes"},
				Hint: "yes: a restarted manager waits for its key"},
		},
		Run: func(values map[string]string) tea.Cmd {
			spec := docker.InitSpec{
				AdvertiseAddr:   values["advertise"],
				ListenAddr:      values["listen"],
				DataPathAddr:    values["datapath"],
				DefaultAddrPool: state.ParseList(values["pool"]),
				Autolock:        values["autolock"] == "yes",
			}
			init := cmds.InitSwarm(v.deps.Ctx, v.deps.Client, spec)
			if spec.Autolock {
				return tea.Sequence(init, cmds.ShowUnlockKey(v.deps.Ctx, v.deps.Client))
			}
			return init
		},
	})
}

func (v *Swarm) joinForm() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return request(FormRequest{
		Title:    "Join a swarm",
		Subtitle: "docker swarm join-token worker, on a manager, prints both values",
		Fields: []components.Field{
			{Name: "remote", Label: "manager address", Required: true, Hint: "host:2377 of any manager"},
			{Name: "token", Label: "token", Required: true, Hint: "the token decides: worker or manager"},
			{Name: "advertise", Label: "advertise address", Hint: "optional: where the others reach this node"},
			{Name: "listen", Label: "listen address", Value: "0.0.0.0:2377"},
		},
		Run: func(values map[string]string) tea.Cmd {
			return cmds.JoinSwarm(v.deps.Ctx, v.deps.Client, docker.JoinSpec{
				RemoteAddr:    values["remote"],
				Token:         strings.TrimSpace(values["token"]),
				AdvertiseAddr: values["advertise"],
				ListenAddr:    values["listen"],
			})
		},
	})
}

func (v *Swarm) unlockPrompt() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return request(PromptRequest{
		Title: "Unlock this manager",
		Body: []string{
			"The key starts with SWMKEY-. Another manager shows it with",
			"docker swarm unlock-key.",
		},
		Label:  "key: ",
		Secret: true,
		Run: func(k string) tea.Cmd {
			return cmds.UnlockSwarm(v.deps.Ctx, v.deps.Client, k)
		},
	})
}
