package state

import (
	"strings"
	"testing"
	"time"

	"github.com/RobinHil/hublot/internal/docker"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func task(id, svc string, slot int, node, state, desired string, age time.Duration) docker.Task {
	return docker.Task{
		ID: id, ServiceID: svc, Slot: slot, NodeID: node,
		State: state, DesiredState: desired, CreatedAt: t0.Add(-age),
	}
}

func swarmStore() *Store {
	s := New()
	s.Nodes = []docker.Node{
		{ID: "n1", Hostname: "alpha", Role: "manager", Leader: true, Reachability: "reachable"},
		{ID: "n2", Hostname: "beta", Role: "worker"},
	}
	web := docker.Service{
		ID: "s1", Name: "shop_web", Mode: docker.ModeReplicated, Running: 2, Desired: 3,
		Labels: map[string]string{docker.LabelStackNamespace: "shop"},
	}
	agent := docker.Service{ID: "s2", Name: "agent", Mode: docker.ModeGlobal, Running: 2, Desired: 2}
	s.SetServices([]docker.Service{web, agent})

	failed := task("t1a", "s1", 1, "n2", docker.TaskFailed, docker.TaskShutdown, 10*time.Minute)
	failed.Err = "task: non-zero exit (1)"
	rejected := task("t3", "s1", 3, "", "pending", docker.TaskRunning, time.Minute)
	rejected.Err = "no suitable node (scheduling constraints not satisfied on 2 nodes)"

	s.SetTasks([]docker.Task{
		failed,
		task("t1b", "s1", 1, "n1", docker.TaskRunning, docker.TaskRunning, 5*time.Minute),
		task("t2", "s1", 2, "n2", docker.TaskRunning, docker.TaskRunning, 5*time.Minute),
		rejected,
		// Slot 4 was scaled away: its last task is meant to be shut down.
		task("t4", "s1", 4, "n1", docker.TaskShutdown, docker.TaskShutdown, time.Hour),
		task("g1", "s2", 0, "n1", docker.TaskRunning, docker.TaskRunning, time.Hour),
		task("g2", "s2", 0, "n2", docker.TaskRunning, docker.TaskRunning, time.Hour),
	})
	return s
}

func TestSlotsKeepTheCurrentTaskAndCountFailures(t *testing.T) {
	s := swarmStore()
	web, _ := s.ServiceByID("s1")
	slots := s.Slots(web)

	var names []string
	for _, sl := range slots {
		names = append(names, sl.Name)
	}
	if got := strings.Join(names, ","); got != "web.1,web.2,web.3" {
		t.Fatalf("slots = %q", got)
	}
	if slots[0].Task.ID != "t1b" || slots[0].Failures != 1 || slots[0].Node != "alpha" {
		t.Fatalf("slot 1 = %+v", slots[0])
	}
	if slots[2].Node != "unassigned" {
		t.Fatalf("a task no node accepted reads as on %q", slots[2].Node)
	}
}

func TestSlotsOfAGlobalServiceAreNamedByNode(t *testing.T) {
	s := swarmStore()
	agent, _ := s.ServiceByID("s2")
	slots := s.Slots(agent)
	if len(slots) != 2 || slots[0].Name != "agent.alpha" || slots[1].Name != "agent.beta" {
		t.Fatalf("slots = %+v", slots)
	}
}

// Rule 0: what is drawn cannot come out of a map in map order.
func TestSlotsAreStable(t *testing.T) {
	s := swarmStore()
	web, _ := s.ServiceByID("s1")
	first := s.Slots(web)
	for i := 0; i < 50; i++ {
		again := s.Slots(web)
		for j := range first {
			if again[j].Name != first[j].Name {
				t.Fatalf("call %d reordered the slots", i)
			}
		}
	}
}

func TestServiceProblemQuotesTheScheduler(t *testing.T) {
	s := swarmStore()
	web, _ := s.ServiceByID("s1")
	if got := s.ServiceProblem(web); !strings.Contains(got, "no suitable node") {
		t.Fatalf("problem = %q", got)
	}
	agent, _ := s.ServiceByID("s2")
	if got := s.ServiceProblem(agent); got != "" {
		t.Fatalf("a healthy service reports %q", got)
	}
}

func TestNodeTasksAndCounts(t *testing.T) {
	s := swarmStore()
	if got := len(s.NodeTasks("n2")); got != 2 {
		t.Fatalf("beta runs %d slots, want 2 (web.2 and agent)", got)
	}
	if got := s.RunningOn("n1"); got != 2 {
		t.Fatalf("alpha runs %d tasks, want 2", got)
	}
	if total, reachable := s.Managers(); total != 1 || reachable != 1 {
		t.Fatalf("managers = %d/%d", reachable, total)
	}
}

func TestStacksFollowServicesAndNetworks(t *testing.T) {
	s := swarmStore()
	s.SetNetworks([]docker.Network{{ID: "net1", Name: "shop_default",
		Labels: map[string]string{docker.LabelStackNamespace: "shop"}}})
	shop, ok := s.StackByName("shop")
	if !ok || len(shop.Services) != 1 || len(shop.Networks) != 1 {
		t.Fatalf("shop = %+v", shop)
	}
	if loose, ok := s.StackByName(""); !ok || loose.Services[0].Name != "agent" {
		t.Fatalf("loose = %+v", loose)
	}

	s.ClearSwarm()
	if len(s.Stacks) != 0 || len(s.Services) != 0 {
		t.Fatal("ClearSwarm left swarm objects behind")
	}
}

func TestUsers(t *testing.T) {
	s := New()
	s.SetServices([]docker.Service{
		{ID: "a", Name: "a", Secrets: []docker.Reference{{ID: "sec"}}, Networks: []string{"front"}},
		{ID: "b", Name: "b", Configs: []docker.Reference{{ID: "cfg"}}, Networks: []string{"n-id"}},
	})
	if got := s.SecretUsers("sec"); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("secret users = %+v", got)
	}
	if got := s.ConfigUsers("cfg"); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("config users = %+v", got)
	}
	// A spec can name a network by id or by name.
	if got := s.NetworkServices(docker.Network{ID: "n-id", Name: "front"}); len(got) != 2 {
		t.Fatalf("network users = %+v", got)
	}
}

func TestQuorum(t *testing.T) {
	cases := []struct{ managers, needed, tolerated int }{
		{0, 0, 0}, {1, 1, 0}, {2, 2, 0}, {3, 2, 1}, {4, 3, 1}, {5, 3, 2}, {7, 4, 3},
	}
	for _, c := range cases {
		n, tol := Quorum(c.managers)
		if n != c.needed || tol != c.tolerated {
			t.Errorf("Quorum(%d) = %d/%d, want %d/%d", c.managers, n, tol, c.needed, c.tolerated)
		}
	}
}

func TestQuorumChangeWarnsAboutEvenCounts(t *testing.T) {
	got := strings.Join(QuorumChange(3, -1), " ")
	if !strings.Contains(got, "survives losing one") || !strings.Contains(got, "losing any one") ||
		!strings.Contains(got, "even number") {
		t.Fatalf("demoting from 3: %q", got)
	}
	if got := strings.Join(QuorumChange(1, -1), " "); !strings.Contains(got, "No manager") {
		t.Fatalf("demoting the last manager: %q", got)
	}
}

func TestNextVersionName(t *testing.T) {
	cases := []struct {
		name  string
		taken []string
		want  string
	}{
		{"db_pw", nil, "db_pw.v2"},
		{"db_pw.v2", nil, "db_pw.v3"},
		{"db_pw", []string{"db_pw.v2", "db_pw.v3"}, "db_pw.v4"},
		{"app.v0", nil, "app.v0.v2"},
		{"nginx.conf", nil, "nginx.conf.v2"},
	}
	for _, c := range cases {
		if got := NextVersionName(c.name, c.taken); got != c.want {
			t.Errorf("NextVersionName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
