// Package swarm reconstructs swarm stacks from the labels `docker stack deploy`
// writes, and builds the CLI invocations for what only the docker CLI can do.
// Like Compose, a stack does not exist in the Engine API: it is services,
// networks, secrets and configs sharing a namespace label.
package swarm

import (
	"sort"

	"github.com/RobinHil/hublot/internal/docker"
)

// Stack is everything one `docker stack deploy` created. The stack with an
// empty name gathers the services nobody deployed as part of a stack.
type Stack struct {
	Name     string
	Services []docker.Service
	Networks []docker.Network
	Secrets  []docker.Secret
	Configs  []docker.Config
}

// Loose reports whether this is the group of services that belong to no stack.
func (s Stack) Loose() bool { return s.Name == "" }

// Running and Desired add up the daemon's own counts across the services.
// Jobs are left out: a job that finished runs nothing and wants nothing more,
// and counted in it reads as a stack short of tasks. Its own line says how far
// it got.
func (s Stack) Running() uint64 {
	var n uint64
	for _, svc := range s.Services {
		if !svc.Job() {
			n += svc.Running
		}
	}
	return n
}

// Desired is how many tasks the stack's services should be running.
func (s Stack) Desired() uint64 {
	var n uint64
	for _, svc := range s.Services {
		if !svc.Job() {
			n += svc.Desired
		}
	}
	return n
}

// Degraded reports whether a service of the stack runs fewer tasks than it
// should. Jobs are left out: a job that finished is not short of anything.
func (s Stack) Degraded() bool {
	for _, svc := range s.Services {
		if !svc.Job() && svc.Running < svc.Desired {
			return true
		}
	}
	return false
}

// Inventory is what stacks are built from.
type Inventory struct {
	Services []docker.Service
	Networks []docker.Network
	Secrets  []docker.Secret
	Configs  []docker.Config
}

// BuildStacks groups everything by namespace. A stack whose services are all
// gone is still listed when it left networks, secrets or configs behind,
// which is what `docker stack rm` interrupted halfway looks like. Stacks come
// out sorted by name, with the loose services last.
func BuildStacks(inv Inventory) []Stack {
	byName := map[string]*Stack{}
	get := func(name string) *Stack {
		if s, ok := byName[name]; ok {
			return s
		}
		s := &Stack{Name: name}
		byName[name] = s
		return s
	}

	for _, svc := range inv.Services {
		st := get(svc.Stack())
		st.Services = append(st.Services, svc)
	}
	for _, n := range inv.Networks {
		if name := n.Labels[docker.LabelStackNamespace]; name != "" {
			st := get(name)
			st.Networks = append(st.Networks, n)
		}
	}
	for _, sec := range inv.Secrets {
		if name := sec.Stack(); name != "" {
			st := get(name)
			st.Secrets = append(st.Secrets, sec)
		}
	}
	for _, cfg := range inv.Configs {
		if name := cfg.Stack(); name != "" {
			st := get(name)
			st.Configs = append(st.Configs, cfg)
		}
	}

	out := make([]Stack, 0, len(byName))
	for _, st := range byName {
		sort.Slice(st.Services, func(i, j int) bool { return st.Services[i].Name < st.Services[j].Name })
		sort.Slice(st.Networks, func(i, j int) bool { return st.Networks[i].Name < st.Networks[j].Name })
		sort.Slice(st.Secrets, func(i, j int) bool { return st.Secrets[i].Name < st.Secrets[j].Name })
		sort.Slice(st.Configs, func(i, j int) bool { return st.Configs[i].Name < st.Configs[j].Name })
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Loose() != out[j].Loose() {
			return out[j].Loose()
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ShortName is a stack service's name without the stack prefix the deploy
// added, which the stack line already says.
func ShortName(stack, service string) string {
	if stack == "" {
		return service
	}
	prefix := stack + "_"
	if len(service) > len(prefix) && service[:len(prefix)] == prefix {
		return service[len(prefix):]
	}
	return service
}
