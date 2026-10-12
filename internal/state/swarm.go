package state

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/swarm"
)

// SwarmState is where this daemon stands in a swarm, from its info.
func (s *Store) SwarmState() docker.SwarmState { return s.Info.Swarm }

// SwarmManaged reports whether the swarm lists can be read here at all: only
// a manager of an active swarm answers for services, nodes and the rest.
func (s *Store) SwarmManaged() bool {
	st := s.Info.Swarm
	return st.Active() && st.Manager
}

// SetServices replaces the service list and regroups the stacks.
func (s *Store) SetServices(svcs []docker.Service) {
	s.Services = svcs
	s.rebuildStacks()
}

// SetTasks replaces the task list.
func (s *Store) SetTasks(ts []docker.Task) { s.Tasks = ts }

// SetNodes replaces the node list.
func (s *Store) SetNodes(ns []docker.Node) { s.Nodes = ns }

// SetSecrets replaces the secret list and regroups the stacks.
func (s *Store) SetSecrets(ss []docker.Secret) {
	s.Secrets = ss
	s.rebuildStacks()
}

// SetConfigs replaces the config list and regroups the stacks.
func (s *Store) SetConfigs(cs []docker.Config) {
	s.Configs = cs
	s.rebuildStacks()
}

// SetCluster records the swarm settings.
func (s *Store) SetCluster(c docker.Cluster) { s.Cluster = &c }

// ClearSwarm forgets every swarm object, for a node that left or was never
// part of one: showing the last services of a swarm this node has left would
// be showing something untrue.
func (s *Store) ClearSwarm() {
	s.Services, s.Tasks, s.Nodes, s.Secrets, s.Configs = nil, nil, nil, nil, nil
	s.Cluster = nil
	s.Stacks = nil
	s.SwarmErr = nil
}

func (s *Store) rebuildStacks() {
	s.Stacks = swarm.BuildStacks(swarm.Inventory{
		Services: s.Services,
		Networks: s.Networks,
		Secrets:  s.Secrets,
		Configs:  s.Configs,
	})
}

// StackByName finds a stack, the loose services answering to "".
func (s *Store) StackByName(name string) (swarm.Stack, bool) {
	for _, st := range s.Stacks {
		if st.Name == name {
			return st, true
		}
	}
	return swarm.Stack{}, false
}

// ServiceByID finds a service.
func (s *Store) ServiceByID(id string) (docker.Service, bool) {
	for _, svc := range s.Services {
		if svc.ID == id {
			return svc, true
		}
	}
	return docker.Service{}, false
}

// NodeByID finds a node.
func (s *Store) NodeByID(id string) (docker.Node, bool) {
	for _, n := range s.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return docker.Node{}, false
}

// TaskByID finds a task.
func (s *Store) TaskByID(id string) (docker.Task, bool) {
	for _, t := range s.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return docker.Task{}, false
}

// NodeName is a node's hostname, or its short id when it is not known.
func (s *Store) NodeName(id string) string {
	if n, ok := s.NodeByID(id); ok && n.Hostname != "" {
		return n.Hostname
	}
	if id == "" {
		return "unassigned"
	}
	return docker.ShortID(id)
}

// Slot is one replica of a service, or one node's share of a global service:
// the task currently standing for it, and how many earlier attempts failed.
type Slot struct {
	// Name is how the CLI names it: web.1 for a replica, web.<node> for a
	// global task.
	Name     string
	Task     docker.Task
	Node     string
	Failures int
}

// Slots is what `docker service ps` shows, minus the history: one line per
// slot still wanted. The swarm keeps the last few attempts of every slot,
// and listing them all buries the one that matters, so earlier failures are
// counted on the line instead.
func (s *Store) Slots(svc docker.Service) []Slot {
	groups := map[string][]docker.Task{}
	var keys []string
	for _, t := range s.Tasks {
		if t.ServiceID != svc.ID {
			continue
		}
		key := slotKey(t)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], t)
	}

	var out []Slot
	for _, key := range keys {
		current, failures := currentOf(groups[key])
		// A slot whose newest task is meant to be shut down belongs to a
		// replica that was scaled away or a node the service left.
		if current.DesiredState == docker.TaskShutdown || current.DesiredState == "remove" {
			continue
		}
		out = append(out, Slot{
			Name:     s.slotName(svc, current),
			Task:     current,
			Node:     s.NodeName(current.NodeID),
			Failures: failures,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Task, out[j].Task
		if a.Slot != b.Slot {
			return a.Slot < b.Slot
		}
		return out[i].Node < out[j].Node
	})
	return out
}

func slotKey(t docker.Task) string {
	if t.Slot > 0 {
		return "slot:" + strconv.Itoa(t.Slot)
	}
	return "node:" + t.NodeID
}

// currentOf picks the newest task of a slot and counts the failed ones before
// it.
func currentOf(tasks []docker.Task) (docker.Task, int) {
	current := tasks[0]
	for _, t := range tasks[1:] {
		if t.CreatedAt.After(current.CreatedAt) {
			current = t
		}
	}
	failures := 0
	for _, t := range tasks {
		if t.ID != current.ID && t.Failed() {
			failures++
		}
	}
	return current, failures
}

func (s *Store) slotName(svc docker.Service, t docker.Task) string {
	short := swarm.ShortName(svc.Stack(), svc.Name)
	if t.Slot > 0 {
		return fmt.Sprintf("%s.%d", short, t.Slot)
	}
	return short + "." + s.NodeName(t.NodeID)
}

// ServiceProblem is the reason a service runs fewer tasks than it should, as
// the scheduler put it, or "" when nothing is wrong. This is what the line
// says without anyone unfolding it: "no suitable node" is the answer to the
// question a 2/3 raises.
func (s *Store) ServiceProblem(svc docker.Service) string {
	if svc.Job() || svc.Running >= svc.Desired {
		return ""
	}
	for _, slot := range s.Slots(svc) {
		t := slot.Task
		if t.State == docker.TaskRunning {
			continue
		}
		if t.Err != "" {
			return t.Err
		}
	}
	for _, slot := range s.Slots(svc) {
		if t := slot.Task; t.State != docker.TaskRunning && t.Message != "" {
			return t.State + ": " + t.Message
		}
	}
	return ""
}

// NodeTasks are the slots a node is running right now, across services.
func (s *Store) NodeTasks(nodeID string) []Slot {
	var out []Slot
	for _, svc := range s.Services {
		for _, slot := range s.Slots(svc) {
			if slot.Task.NodeID == nodeID {
				out = append(out, slot)
			}
		}
	}
	return out
}

// RunningOn counts the tasks running on a node.
func (s *Store) RunningOn(nodeID string) int {
	n := 0
	for _, t := range s.Tasks {
		if t.NodeID == nodeID && t.State == docker.TaskRunning {
			n++
		}
	}
	return n
}

// SecretUsers are the services mounting a secret.
func (s *Store) SecretUsers(id string) []docker.Service {
	var out []docker.Service
	for _, svc := range s.Services {
		for _, ref := range svc.Secrets {
			if ref.ID == id {
				out = append(out, svc)
				break
			}
		}
	}
	return out
}

// ConfigUsers are the services mounting a config.
func (s *Store) ConfigUsers(id string) []docker.Service {
	var out []docker.Service
	for _, svc := range s.Services {
		for _, ref := range svc.Configs {
			if ref.ID == id {
				out = append(out, svc)
				break
			}
		}
	}
	return out
}

// NetworkServices are the services attached to a swarm network, by id or by
// name since a spec can hold either.
func (s *Store) NetworkServices(n docker.Network) []docker.Service {
	var out []docker.Service
	for _, svc := range s.Services {
		for _, target := range svc.Networks {
			if target == n.ID || target == n.Name {
				out = append(out, svc)
				break
			}
		}
	}
	return out
}

// TaskContainers are the containers on this machine that belong to a swarm
// task, which is all a worker can see of the swarm.
func (s *Store) TaskContainers() []docker.Container {
	var out []docker.Container
	for _, c := range s.Containers {
		if c.Labels[docker.LabelSwarmService] != "" {
			out = append(out, c)
		}
	}
	return out
}

// Managers counts the manager nodes, and how many of them are reachable.
func (s *Store) Managers() (total, reachable int) {
	for _, n := range s.Nodes {
		if !n.Manager() {
			continue
		}
		total++
		if n.Reachability == "reachable" || n.Leader {
			reachable++
		}
	}
	return total, reachable
}

// Quorum is what raft needs out of a number of managers: a majority to keep
// working, so it tolerates losing the rest of them.
func Quorum(managers int) (needed, tolerated int) {
	if managers <= 0 {
		return 0, 0
	}
	return managers/2 + 1, (managers - 1) / 2
}

// QuorumChange describes what adding or removing one manager does to the
// quorum, in the sentence a promote or demote dialog needs. An even number
// of managers tolerates no more failures than the odd number below it, which
// is the part people do not expect.
func QuorumChange(managers, delta int) []string {
	_, before := Quorum(managers)
	after := managers + delta
	_, tolerated := Quorum(after)

	lines := []string{
		fmt.Sprintf("Now: %d manager(s), %s.", managers, tolerance(before)),
		fmt.Sprintf("After: %d manager(s), %s.", after, tolerance(tolerated)),
	}
	switch {
	case after == 0:
		lines = append(lines, "No manager would be left: the swarm could not be managed at all.")
	case after%2 == 0:
		lines = append(lines, "An even number of managers tolerates no more failures than one fewer would.")
	}
	return lines
}

func tolerance(n int) string {
	switch n {
	case 0:
		return "losing any one of them stops the swarm being managed"
	case 1:
		return "it survives losing one"
	default:
		return fmt.Sprintf("it survives losing %d", n)
	}
}

// NextVersionName names the replacement of a secret or a config: name.v2,
// then name.v3 and so on, skipping names already taken. Secrets and configs
// cannot be changed in place, so a new value is always a new object.
func NextVersionName(name string, taken []string) string {
	base, n := name, 1
	if i := strings.LastIndex(name, ".v"); i > 0 {
		if v, err := strconv.Atoi(name[i+2:]); err == nil && v > 0 {
			base, n = name[:i], v
		}
	}
	used := map[string]bool{}
	for _, t := range taken {
		used[t] = true
	}
	for {
		n++
		candidate := fmt.Sprintf("%s.v%d", base, n)
		if !used[candidate] {
			return candidate
		}
	}
}

// ServiceFields is what a service filter query is matched against.
func ServiceFields(svc docker.Service) []string {
	return []string{svc.Name, svc.Image, svc.Mode, svc.Stack(), svc.Update.State, docker.ShortID(svc.ID)}
}

// NodeFields is what a node filter query is matched against.
func NodeFields(n docker.Node) []string {
	return append([]string{n.Hostname, n.Role, n.Availability, n.State, n.Addr, n.EngineVersion}, n.LabelList()...)
}

// SecretFields is what a secret filter query is matched against.
func SecretFields(sec docker.Secret) []string {
	return []string{sec.Name, sec.Stack(), sec.Driver, docker.ShortID(sec.ID)}
}

// ConfigFields is what a config filter query is matched against.
func ConfigFields(c docker.Config) []string {
	return []string{c.Name, c.Stack(), docker.ShortID(c.ID)}
}

// StackFields is what a stack filter query is matched against: its name and
// those of its services, so a service can be found inside a folded stack.
func StackFields(st swarm.Stack) []string {
	fields := []string{st.Name}
	for _, svc := range st.Services {
		fields = append(fields, svc.Name, svc.Image)
	}
	return fields
}
