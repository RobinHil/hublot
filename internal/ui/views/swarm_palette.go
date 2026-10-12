package views

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/swarm"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
)

// servicePalette carries everything a stack, a service or a task can have
// done to it that has no key of its own.
func (v *Swarm) servicePalette(st swarm.Stack, svc docker.Service, task docker.Task) tea.Cmd {
	var title string
	var choices []components.Choice
	switch {
	case task.ID != "":
		title = "Actions on task " + v.taskNames(svc)[task.ID]
		choices = v.taskChoices(svc, task)
	case svc.ID != "":
		title = "Actions on " + svc.Name
		choices = v.serviceChoices(svc)
	default:
		title = "Actions on the stack " + st.Name
		choices = v.stackChoices(st)
	}
	if v.deps.ReadOnly {
		choices = keepSafe(choices)
	}
	return request(PickerRequest{Title: title, Choices: choices})
}

func (v *Swarm) stackChoices(st swarm.Stack) []components.Choice {
	if st.Loose() {
		return []components.Choice{
			{Label: "create a service", Detail: "one service, outside any stack", Destructive: true,
				Payload: func() tea.Cmd { return v.newService() }},
			{Label: "logs", Detail: "every service here, merged", Payload: func() tea.Cmd { return v.stackLogs(st) }},
		}
	}
	return []components.Choice{
		{Label: "deploy", Detail: "bring the stack in line with its file", Destructive: true,
			Payload: func() tea.Cmd { return v.deploy(st, false) }},
		{Label: "deploy --prune", Detail: "and remove the services the file no longer names", Destructive: true,
			Payload: func() tea.Cmd { return v.deploy(st, true) }},
		{Label: "read the stack file", Payload: func() tea.Cmd { return v.readStack(st) }},
		{Label: "resolved config", Detail: "the file as a deploy reads it", Payload: func() tea.Cmd { return v.stackConfig(st) }},
		{Label: "edit the stack file", Detail: "then offer to deploy it", Destructive: true,
			Payload: func() tea.Cmd { return v.editStack(st) }},
		{Label: "use another file", Detail: "tell hublot where this stack's file is",
			Payload: func() tea.Cmd { return v.relocateStack(st) }},
		{Label: "restart every task", Detail: "force an update of each service", Destructive: true,
			Payload: func() tea.Cmd { return v.forceUpdate(st, true) }},
		{Label: "logs", Detail: "every service of the stack, merged", Payload: func() tea.Cmd { return v.stackLogs(st) }},
		{Label: "remove the stack", Detail: "services, networks, secrets, configs", Destructive: true,
			Payload: func() tea.Cmd { return v.removeStack(st) }},
		{Label: "deploy another stack", Detail: "from a file on disk", Destructive: true,
			Payload: func() tea.Cmd { return v.newStack() }},
	}
}

// relocateStack forgets the remembered file and asks again.
func (v *Swarm) relocateStack(st swarm.Stack) tea.Cmd {
	return request(PromptRequest{
		Title: "The file of " + st.Name,
		Body:  []string{"The stack file this stack is deployed from, or the directory holding it."},
		Label: "path: ",
		Run: func(path string) tea.Cmd {
			file, err := stackFileAt(path)
			if err != nil {
				return v.info("that path cannot be used", err.Error())
			}
			return tea.Batch(
				request(RememberStackRequest{Stack: st.Name, Files: []string{file}}),
				announce(st.Name+" is deployed from "+file),
			)
		},
	})
}

func (v *Swarm) serviceChoices(svc docker.Service) []components.Choice {
	update := func(label string, change docker.ServiceChange) func() tea.Cmd {
		return func() tea.Cmd { return cmds.UpdateService(v.deps.Ctx, v.deps.Client, svc.ID, label, change) }
	}

	choices := []components.Choice{
		{Label: "inspect", Payload: func() tea.Cmd { return v.inspectService(svc) }},
		{Label: "logs", Payload: func() tea.Cmd { return v.serviceLogs(svc) }},
	}
	if svc.Scalable() {
		choices = append(choices, components.Choice{
			Label: "scale to", Detail: fmt.Sprintf("now %d", svc.Replicas), Destructive: true,
			Payload: func() tea.Cmd { return v.scaleTo(svc) }})
	}
	choices = append(choices,
		components.Choice{Label: "change the image", Detail: imageLabel(svc.Image), Destructive: true,
			Payload: func() tea.Cmd { return v.changeImage(svc) }},
		components.Choice{Label: "restart every task", Detail: "force an update", Destructive: true,
			Payload: func() tea.Cmd { return v.forceUpdate(swarm.Stack{}, false) }},
	)
	if svc.CanRollback {
		choices = append(choices, components.Choice{Label: "roll back", Detail: "to the spec before the last update",
			Destructive: true, Payload: func() tea.Cmd { return v.rollback(svc) }})
	}
	if svc.Update.State == "paused" || svc.Update.State == "rollback_paused" {
		choices = append(choices, components.Choice{Label: "resume the update",
			Detail: "the swarm paused it: " + svc.Update.Message, Destructive: true,
			Payload: update("resumed "+svc.Name, docker.ServiceChange{ForceUpdate: true})})
	}
	choices = append(choices, components.Choice{Label: "edit the spec", Detail: "the whole service, in your editor",
		Destructive: true, Payload: func() tea.Cmd { return v.editService(svc) }})

	choices = append(choices, v.envChoices(svc, update)...)
	choices = append(choices, v.placementChoices(svc, update)...)
	choices = append(choices, v.portChoices(svc, update)...)
	choices = append(choices, v.networkChoices(svc, update)...)
	choices = append(choices, v.mountChoices(svc, update)...)
	choices = append(choices, v.labelChoices(svc, update)...)

	return append(choices, components.Choice{Label: "remove", Destructive: true,
		Payload: func() tea.Cmd { return v.removeServices() }})
}

type updater func(label string, change docker.ServiceChange) func() tea.Cmd

// ask prompts for one value and turns it into a change.
func (v *Swarm) ask(title, label, hint string, change func(string) (docker.ServiceChange, error), apply updater) func() tea.Cmd {
	return func() tea.Cmd {
		return request(PromptRequest{
			Title: title,
			Body:  []string{hint},
			Label: label,
			Run: func(value string) tea.Cmd {
				c, err := change(value)
				if err != nil {
					return v.info("that cannot be applied", err.Error())
				}
				return apply(title+": "+value, c)()
			},
		})
	}
}

func (v *Swarm) scaleTo(svc docker.Service) tea.Cmd {
	return request(PromptRequest{
		Title:   "Scale " + svc.Name,
		Body:    []string{"How many tasks to run. Zero keeps the service and stops all of them."},
		Label:   "replicas: ",
		Initial: strconv.FormatUint(svc.Replicas, 10),
		Run: func(value string) tea.Cmd {
			n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
			if err != nil {
				return v.info("that is not a count", fmt.Sprintf("%q is not a whole number.", value))
			}
			return cmds.UpdateService(v.deps.Ctx, v.deps.Client, svc.ID,
				fmt.Sprintf("scaled %s to %d", svc.Name, n), docker.ServiceChange{Replicas: &n})
		},
	})
}

// changeImage goes through the CLI, which resolves the tag to a digest so
// every node runs the same build, and sends the registry credentials.
func (v *Swarm) changeImage(svc docker.Service) tea.Cmd {
	if cmd := v.needsCLI(); cmd != nil {
		return cmd
	}
	return request(PromptRequest{
		Title: "A new image for " + svc.Name,
		Body: []string{
			"Its tasks are replaced following its update settings.",
			"A service of a stack goes back to the file's image on the next deploy.",
		},
		Label:   "image: ",
		Initial: imageLabel(svc.Image),
		Run: func(image string) tea.Cmd {
			return request(SwarmRunRequest{
				Title: "image of " + svc.Name,
				Args:  swarm.UpdateImageArgs(svc.Name, image),
			})
		},
	})
}

func (v *Swarm) envChoices(svc docker.Service, apply updater) []components.Choice {
	choices := []components.Choice{{
		Label: "set a variable", Detail: "KEY=value", Destructive: true,
		Payload: v.ask("Set a variable on "+svc.Name, "KEY=value: ",
			"Replaces the variable when it is already set. The tasks are replaced.",
			func(s string) (docker.ServiceChange, error) { return docker.ServiceChange{SetEnv: s}, nil }, apply),
	}}
	for _, e := range svc.Env {
		name, _, _ := strings.Cut(e, "=")
		choices = append(choices, components.Choice{Label: "unset " + name, Detail: "remove the variable",
			Destructive: true, Payload: apply("unset "+name, docker.ServiceChange{RemoveEnv: name})})
	}
	return choices
}

func (v *Swarm) placementChoices(svc docker.Service, apply updater) []components.Choice {
	choices := []components.Choice{{
		Label: "add a constraint", Detail: "where its tasks may run", Destructive: true,
		Payload: v.ask("Constrain "+svc.Name, "constraint: ",
			"node.role==worker, node.hostname!=db1, node.labels.zone==eu, node.platform.arch==x86_64",
			func(s string) (docker.ServiceChange, error) {
				c, err := swarm.ParseConstraint(s)
				return docker.ServiceChange{AddConstraint: c}, err
			}, apply),
	}}
	for _, c := range svc.Constraints {
		choices = append(choices, components.Choice{Label: "drop " + c, Detail: "remove the constraint",
			Destructive: true, Payload: apply("dropped "+c, docker.ServiceChange{RemoveConstraint: c})})
	}
	return choices
}

func (v *Swarm) portChoices(svc docker.Service, apply updater) []components.Choice {
	choices := []components.Choice{{
		Label: "publish a port", Detail: "through the routing mesh, on every node", Destructive: true,
		Payload: v.ask("Publish a port of "+svc.Name, "published:target: ",
			"8080:80, 53:53/udp; the published port is opened on every node of the swarm.",
			func(s string) (docker.ServiceChange, error) {
				p, err := swarm.ParsePort(s)
				return docker.ServiceChange{AddPort: &p}, err
			}, apply),
	}}
	for _, p := range svc.Ports {
		port := p
		label := servicePorts([]docker.ServicePort{p})
		choices = append(choices, components.Choice{Label: "unpublish " + label, Destructive: true,
			Payload: apply("unpublished "+label, docker.ServiceChange{RemovePort: &port})})
	}
	return choices
}

func (v *Swarm) networkChoices(svc docker.Service, apply updater) []components.Choice {
	var choices []components.Choice
	attached := map[string]bool{}
	for _, target := range svc.Networks {
		attached[target] = true
	}
	for _, n := range v.deps.Store.Networks {
		if n.Scope != "swarm" || n.Name == "ingress" {
			continue
		}
		if attached[n.ID] || attached[n.Name] {
			target := n.ID
			if attached[n.Name] {
				target = n.Name
			}
			choices = append(choices, components.Choice{Label: "disconnect " + n.Name, Destructive: true,
				Payload: apply("disconnected "+n.Name, docker.ServiceChange{RemoveNetwork: target})})
			continue
		}
		choices = append(choices, components.Choice{Label: "connect " + n.Name, Detail: "an overlay network",
			Destructive: true, Payload: apply("connected "+n.Name, docker.ServiceChange{AddNetwork: n.ID})})
	}
	return choices
}

func (v *Swarm) mountChoices(svc docker.Service, apply updater) []components.Choice {
	var choices []components.Choice
	mounted := map[string]bool{}
	for _, ref := range svc.Secrets {
		mounted[ref.ID] = true
		choices = append(choices, components.Choice{Label: "detach secret " + ref.Name, Destructive: true,
			Payload: apply("detached "+ref.Name, docker.ServiceChange{RemoveSecret: ref.ID})})
	}
	for _, ref := range svc.Configs {
		mounted[ref.ID] = true
		choices = append(choices, components.Choice{Label: "detach config " + ref.Name, Destructive: true,
			Payload: apply("detached "+ref.Name, docker.ServiceChange{RemoveConfig: ref.ID})})
	}
	for _, s := range v.deps.Store.Secrets {
		if mounted[s.ID] {
			continue
		}
		ref := docker.Reference{ID: s.ID, Name: s.Name}
		choices = append(choices, components.Choice{Label: "attach secret " + s.Name,
			Detail: "at /run/secrets/" + s.Name, Destructive: true,
			Payload: apply("attached "+s.Name, docker.ServiceChange{AddSecret: &ref})})
	}
	for _, c := range v.deps.Store.Configs {
		if mounted[c.ID] {
			continue
		}
		ref := docker.Reference{ID: c.ID, Name: c.Name}
		choices = append(choices, components.Choice{Label: "attach config " + c.Name,
			Detail: "at /" + c.Name, Destructive: true,
			Payload: apply("attached "+c.Name, docker.ServiceChange{AddConfig: &ref})})
	}
	return choices
}

func (v *Swarm) labelChoices(svc docker.Service, apply updater) []components.Choice {
	choices := []components.Choice{{
		Label: "set a label", Detail: "on the service, which restarts nothing", Destructive: true,
		Payload: v.ask("Label "+svc.Name, "key=value: ", "A label on the service itself; its tasks are left alone.",
			func(s string) (docker.ServiceChange, error) { return docker.ServiceChange{SetLabel: s}, nil }, apply),
	}}
	for _, pair := range svc.LabelList() {
		name, _, _ := strings.Cut(pair, "=")
		if name == docker.LabelStackNamespace || strings.HasPrefix(name, "com.docker.stack.") {
			// The stack's own labels say what it owns; removing one would
			// orphan the service from the stack it was deployed by.
			continue
		}
		choices = append(choices, components.Choice{Label: "drop label " + name, Destructive: true,
			Payload: apply("dropped "+name, docker.ServiceChange{RemoveLabel: name})})
	}
	return choices
}

func (v *Swarm) taskChoices(svc docker.Service, t docker.Task) []components.Choice {
	return []components.Choice{
		{Label: "inspect", Payload: func() tea.Cmd { return v.inspectTask(t) }},
		{Label: "logs", Payload: func() tea.Cmd { return v.taskLogs(svc, t) }},
		{Label: "shell", Detail: "only for a task running on this machine", Destructive: true,
			Payload: func() tea.Cmd { return v.execTask(svc, t) }},
		{Label: "restart the service's tasks", Destructive: true,
			Payload: func() tea.Cmd {
				return cmds.UpdateService(v.deps.Ctx, v.deps.Client, svc.ID, "restarted "+svc.Name,
					docker.ServiceChange{ForceUpdate: true})
			}},
	}
}
