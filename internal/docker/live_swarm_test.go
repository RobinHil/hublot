//go:build live

// Checks the swarm half of the docker layer against a real daemon. Making a
// swarm changes what the daemon is, so on top of the live tag this needs
// HUBLOT_LIVE_SWARM=1, and refuses to run on a daemon already in a swarm: it
// creates a single-node swarm, works in it, and leaves it at the end.
//
//	HUBLOT_LIVE_SWARM=1 go test -tags live -run Swarm -v ./internal/docker/
//
// The messages the daemon writes for the failures diagnose.go recognises are
// logged with -v, which is where the patterns are captured from.
package docker

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func liveSwarm(t *testing.T) (*Client, context.Context) {
	t.Helper()
	if os.Getenv("HUBLOT_LIVE_SWARM") != "1" {
		t.Skip("set HUBLOT_LIVE_SWARM=1: this test creates a swarm on the daemon and leaves it")
	}
	c, ctx := live(t)
	info, err := c.Info(ctx)
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.Swarm.State != SwarmInactive {
		t.Skipf("the daemon is already in a swarm (%s); this test only makes its own", info.Swarm.State)
	}
	if _, err := c.InitSwarm(ctx, InitSpec{AdvertiseAddr: "127.0.0.1"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() {
		if err := c.LeaveSwarm(context.Background(), true); err != nil {
			t.Errorf("leaving the swarm: %v", err)
		}
	})
	return c, ctx
}

// eventually polls until cond holds, for the swarm converging.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func createLiveService(t *testing.T, c *Client, ctx context.Context, name string, replicas uint64, constraints ...string) string {
	t.Helper()
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: name, Labels: map[string]string{LabelStackNamespace: "hublot-live"}},
		Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:   testImage,
				Command: []string{"sh", "-c", "while true; do echo hublot-live-line; sleep 1; done"},
			},
			Placement: &swarm.Placement{Constraints: constraints},
		},
	}
	resp, err := c.api.ServiceCreate(ctx, spec, swarm.ServiceCreateOptions{})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	t.Cleanup(func() { _ = c.RemoveService(context.Background(), resp.ID) })
	return resp.ID
}

func serviceNamed(t *testing.T, c *Client, ctx context.Context, id string) Service {
	t.Helper()
	svcs, err := c.ListServices(ctx)
	if err != nil {
		t.Fatalf("listing services: %v", err)
	}
	for _, s := range svcs {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("service %s is not listed", id)
	return Service{}
}

func TestLiveSwarm(t *testing.T) {
	c, ctx := liveSwarm(t)
	before := runtime.NumGoroutine()

	info, err := c.Info(ctx)
	if err != nil || !info.Swarm.Active() || !info.Swarm.Manager {
		t.Fatalf("after init: %+v, %v", info.Swarm, err)
	}

	id := createLiveService(t, c, ctx, "hublot-live-web", 1)
	eventually(t, "one running task", func() bool { return serviceNamed(t, c, ctx, id).Running == 1 })

	svc := serviceNamed(t, c, ctx, id)
	if svc.Stack() != "hublot-live" || svc.Mode != ModeReplicated || svc.Replicas != 1 {
		t.Fatalf("service = %+v", svc)
	}

	t.Run("scale", func(t *testing.T) {
		two := uint64(2)
		if err := c.UpdateService(ctx, id, ServiceChange{Replicas: &two}); err != nil {
			t.Fatalf("scale: %v", err)
		}
		eventually(t, "two running tasks", func() bool { return serviceNamed(t, c, ctx, id).Running == 2 })
	})

	t.Run("tasks", func(t *testing.T) {
		tasks, err := c.ListTasks(ctx)
		if err != nil {
			t.Fatalf("tasks: %v", err)
		}
		running := 0
		for _, task := range tasks {
			if task.ServiceID == id && task.State == TaskRunning {
				running++
				if task.ContainerID == "" || task.NodeID != info.Swarm.NodeID {
					t.Errorf("task %+v has no container on this node", task)
				}
			}
		}
		if running != 2 {
			t.Fatalf("%d running tasks, want 2", running)
		}
	})

	t.Run("logs carry the task they came from", func(t *testing.T) {
		tasks, _ := c.ListTasks(ctx)
		names := map[string]string{}
		for _, task := range tasks {
			if task.ServiceID == id {
				names[task.ID] = "web.slot"
			}
		}
		lctx, cancel := context.WithCancel(ctx)
		defer cancel()
		opts := LogOptions{Tail: "5", Follow: true, Names: names}
		lines, _ := c.StreamServiceLogs(lctx, id, false, opts)
		select {
		case l := <-lines:
			if l.Text != "hublot-live-line" || l.Service != "web.slot" {
				t.Fatalf("line = %+v: the swarm details were not peeled off", l)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("no log line")
		}
	})

	t.Run("spec round trip", func(t *testing.T) {
		body, version, err := c.ServiceSpecYAML(ctx, id)
		if err != nil {
			t.Fatalf("spec: %v", err)
		}
		edited := strings.Replace(string(body), "hublot-live-line", "hublot-live-edited", 1)
		if err := c.ApplyServiceYAML(ctx, id, version, []byte(edited)); err != nil {
			t.Fatalf("apply: %v", err)
		}
		// The same version twice is a change made meanwhile, and refused.
		err = c.ApplyServiceYAML(ctx, id, version, []byte(edited))
		if err == nil {
			t.Fatal("a stale version was accepted")
		}
		t.Logf("stale version: %v", err)
	})

	t.Run("rollback", func(t *testing.T) {
		if !serviceNamed(t, c, ctx, id).CanRollback {
			t.Fatal("an updated service offers no rollback")
		}
		if err := c.RollbackService(ctx, id); err != nil {
			t.Fatalf("rollback: %v", err)
		}
	})

	t.Run("secrets and configs", func(t *testing.T) {
		secID, err := c.CreateSecret(ctx, "hublot-live-secret", []byte("s3cret"), nil)
		if err != nil {
			t.Fatalf("secret: %v", err)
		}
		t.Cleanup(func() { _ = c.RemoveSecret(context.Background(), secID) })
		cfgID, err := c.CreateConfig(ctx, "hublot-live-config", []byte("a=1\n"), nil)
		if err != nil {
			t.Fatalf("config: %v", err)
		}
		t.Cleanup(func() { _ = c.RemoveConfig(context.Background(), cfgID) })

		ref := Reference{ID: secID, Name: "hublot-live-secret"}
		if err := c.UpdateService(ctx, id, ServiceChange{AddSecret: &ref}); err != nil {
			t.Fatalf("attach: %v", err)
		}
		err = c.RemoveSecret(ctx, secID)
		if err == nil {
			t.Fatal("a mounted secret was removed")
		}
		t.Logf("secret in use: %v", err)

		configs, _ := c.ListConfigs(ctx)
		for _, cfg := range configs {
			if cfg.ID == cfgID && string(cfg.Data) != "a=1\n" {
				t.Errorf("config data = %q", cfg.Data)
			}
		}
	})

	t.Run("a task no node can take", func(t *testing.T) {
		stuck := createLiveService(t, c, ctx, "hublot-live-stuck", 1, "node.labels.hublot==nowhere")
		var reason string
		eventually(t, "the scheduler to give up", func() bool {
			tasks, _ := c.ListTasks(ctx)
			for _, task := range tasks {
				if task.ServiceID == stuck && task.Err != "" {
					reason = task.Err
					return true
				}
			}
			return false
		})
		t.Logf("no suitable node: %s", reason)
	})

	t.Run("nodes", func(t *testing.T) {
		nodes, err := c.ListNodes(ctx)
		if err != nil || len(nodes) != 1 || !nodes[0].Leader {
			t.Fatalf("nodes = %+v, %v", nodes, err)
		}
		n := nodes[0]
		if err := c.UpdateNode(ctx, n.ID, NodeChange{SetLabel: "hublot=live"}); err != nil {
			t.Fatalf("label: %v", err)
		}
		err = c.RemoveNode(ctx, n.ID, false)
		if err == nil {
			t.Fatal("removing the only manager was accepted")
		}
		t.Logf("remove a node that is up: %v", err)
		err = c.UpdateNode(ctx, n.ID, NodeChange{Role: "worker"})
		if err == nil {
			t.Fatal("demoting the last manager was accepted")
		}
		t.Logf("demote the last manager: %v", err)
	})

	t.Run("cluster", func(t *testing.T) {
		history := int64(3)
		if err := c.UpdateCluster(ctx, ClusterChange{TaskHistory: &history, RotateWorkerToken: true}); err != nil {
			t.Fatalf("update: %v", err)
		}
		cl, err := c.InspectCluster(ctx)
		if err != nil || cl.TaskHistory != 3 || cl.WorkerToken == "" {
			t.Fatalf("cluster = %+v, %v", cl, err)
		}
		if key, err := c.UnlockKey(ctx); err != nil || key != "" {
			t.Fatalf("unlock key without autolock = %q, %v", key, err)
		}
	})

	_ = c.RemoveService(ctx, id)
	time.Sleep(2 * time.Second)
	if after := runtime.NumGoroutine(); after > before+5 {
		t.Errorf("goroutines: %d before, %d after", before, after)
	}
}
