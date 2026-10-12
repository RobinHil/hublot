package views

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/keys"
)

func swarmDeps(st docker.SwarmState) Deps {
	store := state.New()
	store.Info.Swarm = st
	return Deps{Ctx: context.Background(), Store: store, Keys: keys.Default()}
}

func managerView(t *testing.T) *Swarm {
	t.Helper()
	d := swarmDeps(docker.SwarmState{State: docker.SwarmActive, Manager: true, NodeID: "n1"})
	store := d.Store
	store.SetNodes([]docker.Node{{ID: "n1", Hostname: "alpha", Role: "manager", Leader: true, State: "ready", Availability: "active"}})
	store.SetServices([]docker.Service{
		{ID: "s1", Name: "shop_web", Mode: docker.ModeReplicated, Replicas: 2, Running: 2, Desired: 2,
			Labels: map[string]string{docker.LabelStackNamespace: "shop"}, Image: "nginx:1.27@sha256:abc"},
		{ID: "s2", Name: "probe", Mode: docker.ModeGlobal, Running: 1, Desired: 1},
	})
	now := time.Now()
	store.SetTasks([]docker.Task{
		{ID: "t1", ServiceID: "s1", Slot: 1, NodeID: "n1", State: "running", DesiredState: "running", CreatedAt: now},
		{ID: "t2", ServiceID: "s1", Slot: 2, NodeID: "n1", State: "running", DesiredState: "running", CreatedAt: now},
	})
	v := NewSwarm(d)
	v.SetSize(120, 30)
	v.Refresh()
	return v
}

func rowIDs(v *Swarm) []string {
	var out []string
	for _, r := range v.table().Rows() {
		out = append(out, r.ID)
	}
	return out
}

func press(v *Swarm, keys string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}}
		if k == '\n' {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		cmd = v.Update(msg)
	}
	return cmd
}

func TestSwarmTreeShowsStacksThenLooseServices(t *testing.T) {
	v := managerView(t)
	got := strings.Join(rowIDs(v), " ")
	if got != "stack:shop svc:s1 stack: svc:s2" {
		t.Fatalf("rows = %q", got)
	}
	if !strings.Contains(v.Summary(), "1 stack") || !strings.Contains(v.Summary(), "3/3 tasks") {
		t.Fatalf("summary = %q", v.Summary())
	}
}

// Unfolding a service puts its tasks under it, so the cursor stays on the
// service: folding from a line that moves away is what reads as the list
// jumping about.
func TestSwarmEnterOnAServiceUnfoldsItsTasksInPlace(t *testing.T) {
	v := managerView(t)
	press(v, "j")
	if v.table().CurrentID() != "svc:s1" {
		t.Fatalf("cursor on %q", v.table().CurrentID())
	}
	press(v, "\n")
	if v.table().CurrentID() != "svc:s1" {
		t.Fatalf("unfolding moved the cursor to %q", v.table().CurrentID())
	}
	got := strings.Join(rowIDs(v), " ")
	if got != "stack:shop svc:s1 task:t1 task:t2 stack: svc:s2" {
		t.Fatalf("rows = %q", got)
	}
}

func TestSwarmEnterOnAStackFoldsIt(t *testing.T) {
	v := managerView(t)
	press(v, "\n")
	if got := strings.Join(rowIDs(v), " "); got != "stack:shop stack: svc:s2" {
		t.Fatalf("rows = %q", got)
	}
}

// The arrows walk the sections like tabs, and say when they run out of them
// so the arrow can carry on to the next tab.
func TestSwarmStepsThroughSections(t *testing.T) {
	v := managerView(t)
	if v.Step(-1) {
		t.Fatal("stepped left of the first section instead of leaving the view")
	}
	for want := secNodes; want <= secCluster; want++ {
		if !v.Step(1) || v.section != want {
			t.Fatalf("step to %d landed on %d", want, v.section)
		}
	}
	if v.Step(1) {
		t.Fatal("stepped right of the last section instead of leaving the view")
	}

	v.EnterFrom(-1)
	if v.section != secCluster {
		t.Fatalf("arriving from the right lands on %d, want the last section", v.section)
	}
	v.EnterFrom(1)
	if v.section != secServices {
		t.Fatalf("arriving from the left lands on %d, want the first section", v.section)
	}

	// Off a manager there are no sections, and the arrow goes straight on.
	off := NewSwarm(swarmDeps(docker.SwarmState{State: docker.SwarmInactive}))
	if off.Step(1) {
		t.Fatal("a view with no swarm stepped through sections")
	}
}

// Off a manager there are no lists to show, and the view says why rather
// than drawing an empty table.
func TestSwarmOffStates(t *testing.T) {
	cases := map[string]docker.SwarmState{
		"not part of a swarm": {State: docker.SwarmInactive},
		"locked":              {State: docker.SwarmLocked},
		"worker":              {State: docker.SwarmActive, RemoteManagers: []string{"10.0.0.1:2377"}},
	}
	for want, st := range cases {
		v := NewSwarm(swarmDeps(st))
		v.SetSize(100, 20)
		if !strings.Contains(v.View(), want) {
			t.Errorf("%s: view does not say so:\n%s", want, v.View())
		}
	}
}

func TestSwarmReadOnlyRefusesMutations(t *testing.T) {
	v := managerView(t)
	v.deps.ReadOnly = true
	press(v, "j")
	for _, k := range []string{"+", "R", "D", "E"} {
		cmd := press(v, k)
		if cmd == nil {
			t.Fatalf("%s did nothing at all", k)
		}
		if _, ok := cmd().(ReadOnlyMsg); !ok {
			t.Errorf("%s in a read-only session was not refused", k)
		}
	}
}

// A global service has no replica count: scaling it says so instead of
// sending a spec the daemon would refuse.
func TestSwarmScalingAGlobalServiceExplains(t *testing.T) {
	v := managerView(t)
	press(v, "jjj")
	if v.table().CurrentID() != "svc:s2" {
		t.Fatalf("cursor on %q", v.table().CurrentID())
	}
	cmd := press(v, "+")
	req, ok := cmd().(ConfirmRequest)
	if !ok || !strings.Contains(req.Title, "cannot be scaled") {
		t.Fatalf("got %#v", cmd())
	}
}

func TestStackName(t *testing.T) {
	cases := map[string]string{
		"/srv/My Shop": "my-shop",
		"/srv/blog":    "blog",
		"/":            "stack",
		"/x/_tmp.d":    "tmp-d",
	}
	for in, want := range cases {
		if got := stackName(in); got != want {
			t.Errorf("stackName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A blank row parts the section line from the table when there is room, and
// goes first on a short screen, where a row of data is worth more.
func TestSwarmSectionLineGap(t *testing.T) {
	v := managerView(t)
	v.SetSize(120, 30)
	if lines := strings.Split(v.View(), "\n"); strings.TrimSpace(lines[1]) != "" {
		t.Fatalf("no gap under the section line: %q", lines[1])
	}
	v.SetSize(120, 10)
	if lines := strings.Split(v.View(), "\n"); strings.TrimSpace(lines[1]) == "" {
		t.Fatal("a short screen spends a row on the gap")
	}
}
