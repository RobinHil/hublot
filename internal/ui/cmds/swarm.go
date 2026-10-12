package cmds

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/swarm"
)

// TaskPollInterval is how often tasks are listed while the Swarm view is on
// screen. Tasks emit no events, and a rolling update or a node going down is
// exactly what someone watching that view is waiting to see.
const TaskPollInterval = 2 * time.Second

// SwarmMsg carries every swarm list at once. A manager answers all of them; a
// node that is not one is refused all of them, so they succeed or fail
// together.
type SwarmMsg struct {
	Services []docker.Service
	Tasks    []docker.Task
	Nodes    []docker.Node
	Secrets  []docker.Secret
	Configs  []docker.Config
	Cluster  docker.Cluster
	Err      error
}

// TasksMsg carries a refreshed task list, the one thing polled.
type TasksMsg struct {
	Tasks []docker.Task
	Err   error
}

// TaskTickMsg drives the task poll.
type TaskTickMsg time.Time

// TaskTick schedules the next task poll.
func TaskTick() tea.Cmd {
	return tea.Tick(TaskPollInterval, func(t time.Time) tea.Msg { return TaskTickMsg(t) })
}

// RefreshSwarm lists everything a manager knows about the swarm.
func RefreshSwarm(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("listing the swarm", func() tea.Msg {
		var m SwarmMsg
		var err error
		if m.Services, err = c.ListServices(ctx); err != nil {
			return SwarmMsg{Err: err}
		}
		if m.Tasks, err = c.ListTasks(ctx); err != nil {
			return SwarmMsg{Err: err}
		}
		if m.Nodes, err = c.ListNodes(ctx); err != nil {
			return SwarmMsg{Err: err}
		}
		if m.Secrets, err = c.ListSecrets(ctx); err != nil {
			return SwarmMsg{Err: err}
		}
		if m.Configs, err = c.ListConfigs(ctx); err != nil {
			return SwarmMsg{Err: err}
		}
		if m.Cluster, err = c.InspectCluster(ctx); err != nil {
			return SwarmMsg{Err: err}
		}
		return m
	})
}

// ListTasks refreshes the task list alone.
func ListTasks(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("listing tasks", func() tea.Msg {
		ts, err := c.ListTasks(ctx)
		return TasksMsg{Tasks: ts, Err: err}
	})
}

// done reports a swarm mutation. A daemon warning is a change that happened
// with something worth saying, not a failure.
func done(label string, err error) tea.Msg {
	var warning *docker.Warning
	if errors.As(err, &warning) {
		return ActionDoneMsg{Label: label + ", but the daemon warns: " + warning.Text, Refresh: true}
	}
	if err != nil {
		return ActionDoneMsg{Label: label, Err: err, Refresh: true}
	}
	return ActionDoneMsg{Label: label, Refresh: true}
}

// UpdateService applies one change to a service.
func UpdateService(ctx context.Context, c *docker.Client, id, label string, change docker.ServiceChange) tea.Cmd {
	return safely(label, func() tea.Msg { return done(label, c.UpdateService(ctx, id, change)) })
}

// UpdateServices applies the same change to several services, reporting the
// first refusal after trying all of them.
func UpdateServices(ctx context.Context, c *docker.Client, ids []string, label string, change docker.ServiceChange) tea.Cmd {
	return safely(label, func() tea.Msg {
		var first error
		for _, id := range ids {
			if err := c.UpdateService(ctx, id, change); err != nil && first == nil {
				first = err
			}
		}
		return done(fmt.Sprintf("%s: %d service(s)", label, len(ids)), first)
	})
}

// RollbackService puts a service back on its previous spec.
func RollbackService(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("rollback", func() tea.Msg { return done("rolling back "+name, c.RollbackService(ctx, id)) })
}

// RemoveServices deletes each service.
func RemoveServices(ctx context.Context, c *docker.Client, ids []string) tea.Cmd {
	return each(ctx, "remove service", ids, func(id string) error { return c.RemoveService(ctx, id) })
}

// UpdateNode applies one change to a node.
func UpdateNode(ctx context.Context, c *docker.Client, id, label string, change docker.NodeChange) tea.Cmd {
	return safely(label, func() tea.Msg { return done(label, c.UpdateNode(ctx, id, change)) })
}

// RemoveNode takes a node out of the swarm's records.
func RemoveNode(ctx context.Context, c *docker.Client, id, name string, force bool) tea.Cmd {
	return safely("remove node", func() tea.Msg { return done("removed node "+name, c.RemoveNode(ctx, id, force)) })
}

// CreateSecret stores a new secret.
func CreateSecret(ctx context.Context, c *docker.Client, name string, data []byte) tea.Cmd {
	return safely("create secret", func() tea.Msg {
		_, err := c.CreateSecret(ctx, name, data, nil)
		return done("created secret "+name, err)
	})
}

// CreateConfig stores a new config from a file's contents.
func CreateConfig(ctx context.Context, c *docker.Client, name, path string) tea.Cmd {
	return safely("create config", func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return done("create config "+name, fmt.Errorf("reading %s: %w", path, err))
		}
		_, err = c.CreateConfig(ctx, name, data, nil)
		return done("created config "+name, err)
	})
}

// RemoveSecrets deletes each secret.
func RemoveSecrets(ctx context.Context, c *docker.Client, ids []string) tea.Cmd {
	return each(ctx, "remove secret", ids, func(id string) error { return c.RemoveSecret(ctx, id) })
}

// RemoveConfigs deletes each config.
func RemoveConfigs(ctx context.Context, c *docker.Client, ids []string) tea.Cmd {
	return each(ctx, "remove config", ids, func(id string) error { return c.RemoveConfig(ctx, id) })
}

// Replacement is a secret or config being replaced by a new version: what it
// was, the name and value of what replaces it, and who mounts it.
type Replacement struct {
	Secret  bool
	OldID   string
	OldName string
	NewName string
	Data    []byte
	Users   []docker.Service
}

// Replace rotates a secret or a config. Neither can be changed in place, so a
// new value is a new object: create it, point every service at it on the same
// path, and remove the old one. A service that refuses puts the ones already
// moved back, and the new object is removed again: nothing is left half
// rotated, the same promise as a container rebuild.
func Replace(ctx context.Context, c *docker.Client, r Replacement) tea.Cmd {
	return safely("rotate", func() tea.Msg {
		create, remove := c.CreateConfig, c.RemoveConfig
		swap := func(to docker.Reference, from string) docker.ServiceChange {
			return docker.ServiceChange{SwapConfig: &docker.Swap{FromID: from, To: to}}
		}
		if r.Secret {
			create, remove = c.CreateSecret, c.RemoveSecret
			swap = func(to docker.Reference, from string) docker.ServiceChange {
				return docker.ServiceChange{SwapSecret: &docker.Swap{FromID: from, To: to}}
			}
		}

		newID, err := create(ctx, r.NewName, r.Data, nil)
		if err != nil {
			return done("rotate "+r.OldName, err)
		}
		next := docker.Reference{ID: newID, Name: r.NewName}
		back := docker.Reference{ID: r.OldID, Name: r.OldName}

		var moved []docker.Service
		for _, svc := range r.Users {
			if err := c.UpdateService(ctx, svc.ID, swap(next, r.OldID)); err != nil && !isWarning(err) {
				for _, m := range moved {
					_ = c.UpdateService(ctx, m.ID, swap(back, newID))
				}
				_ = remove(ctx, newID)
				return done("rotate "+r.OldName, fmt.Errorf("%s refused the new version, everything was put back: %w", svc.Name, err))
			}
			moved = append(moved, svc)
		}

		if err := remove(ctx, r.OldID); err != nil {
			return done(fmt.Sprintf("%s replaced by %s, but the old one stays", r.OldName, r.NewName), err)
		}
		return done(fmt.Sprintf("%s replaced by %s on %d service(s)", r.OldName, r.NewName, len(moved)), nil)
	})
}

func isWarning(err error) bool {
	var w *docker.Warning
	return errors.As(err, &w)
}

// InitSwarm makes this node the first manager of a new swarm.
func InitSwarm(ctx context.Context, c *docker.Client, spec docker.InitSpec) tea.Cmd {
	return safely("swarm init", func() tea.Msg {
		_, err := c.InitSwarm(ctx, spec)
		return done("this node now manages a new swarm", err)
	})
}

// JoinSwarm adds this node to an existing swarm.
func JoinSwarm(ctx context.Context, c *docker.Client, spec docker.JoinSpec) tea.Cmd {
	return safely("swarm join", func() tea.Msg {
		return done("joined the swarm at "+spec.RemoteAddr, c.JoinSwarm(ctx, spec))
	})
}

// LeaveSwarm takes this node out of the swarm.
func LeaveSwarm(ctx context.Context, c *docker.Client, force bool) tea.Cmd {
	return safely("swarm leave", func() tea.Msg { return done("left the swarm", c.LeaveSwarm(ctx, force)) })
}

// UnlockSwarm unlocks a manager after a restart.
func UnlockSwarm(ctx context.Context, c *docker.Client, unlockKey string) tea.Cmd {
	return safely("swarm unlock", func() tea.Msg { return done("swarm unlocked", c.UnlockSwarm(ctx, unlockKey)) })
}

// UpdateCluster applies a settings change.
func UpdateCluster(ctx context.Context, c *docker.Client, label string, change docker.ClusterChange) tea.Cmd {
	return safely(label, func() tea.Msg { return done(label, c.UpdateCluster(ctx, change)) })
}

// ShowUnlockKey puts the unlock key in the panel, which is the one place it
// is shown: the swarm cannot be restarted without it.
func ShowUnlockKey(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("unlock key", func() tea.Msg {
		key, err := c.UnlockKey(ctx)
		if err != nil {
			return TextMsg{Title: "unlock key", Err: err}
		}
		if key == "" {
			return TextMsg{Title: "unlock key", Lines: []string{"autolock is off: managers restart without a key"}}
		}
		return TextMsg{Title: "unlock key", Lines: []string{
			"Keep this somewhere safe. A manager that restarts stays locked until it is given:",
			"",
			"  " + key,
			"",
			"docker swarm unlock, or u in hublot's Swarm view, asks for it.",
		}}
	})
}

// ShowJoinCommands puts the two join commands in the panel, ready to copy.
func ShowJoinCommands(ctx context.Context, c *docker.Client, addr string) tea.Cmd {
	return safely("join tokens", func() tea.Msg {
		cl, err := c.InspectCluster(ctx)
		if err != nil {
			return TextMsg{Title: "join commands", Err: err}
		}
		return TextMsg{Title: "join commands", Lines: []string{
			"A worker joins with:",
			"",
			"docker swarm join --token " + cl.WorkerToken + " " + addr,
			"",
			"A manager joins with:",
			"",
			"docker swarm join --token " + cl.ManagerToken + " " + addr,
			"",
			"Each command is one line: W widens the panel until it fits, which",
			"is what copying it needs. Anyone holding a token can join a machine",
			"to this swarm; rotating it from the palette invalidates the old one",
			"without touching the nodes already in.",
		}}
	})
}

// InspectSwarmObject fetches any swarm object's raw inspect output.
func InspectSwarmObject(ctx context.Context, title string, fetch func(context.Context) ([]byte, error)) tea.Cmd {
	return safely("inspect", func() tea.Msg {
		raw, err := fetch(ctx)
		return InspectMsg{Title: title, Content: string(raw), Err: err}
	})
}

// ShowConfigData puts a config's contents in the panel.
func ShowConfigData(cfg docker.Config) tea.Cmd {
	return func() tea.Msg {
		return TextMsg{Title: "config " + cfg.Name, Lines: strings.Split(strings.TrimRight(string(cfg.Data), "\n"), "\n")}
	}
}

// ShowStackConfig resolves a stack's files the way a deploy would.
func ShowStackConfig(ctx context.Context, cli swarm.CLI, stack string, files []string) tea.Cmd {
	return safely("stack config", func() tea.Msg {
		out, _, err := cli.Output(ctx, filepath.Dir(files[0]), swarm.ConfigArgs(files)...)
		return InspectMsg{Title: "stack config: " + stack, Content: out, Err: err}
	})
}

// ServiceSpecMsg carries a service's spec written to a file for editing.
type ServiceSpecMsg struct {
	ID      string
	Name    string
	Path    string
	Version uint64
	Err     error
}

// WriteServiceSpec writes a service's spec to a private file an editor can
// open. A spec can hold environment values nobody else should read, so the
// directory is the user's alone and the file goes once the edit is over.
func WriteServiceSpec(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("reading the service", func() tea.Msg {
		body, version, err := c.ServiceSpecYAML(ctx, id)
		if err != nil {
			return ServiceSpecMsg{Err: err}
		}
		dir, err := os.MkdirTemp("", "hublot-service-")
		if err != nil {
			return ServiceSpecMsg{Err: fmt.Errorf("creating a file to edit: %w", err)}
		}
		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return ServiceSpecMsg{Err: fmt.Errorf("writing %s: %w", path, err)}
		}
		return ServiceSpecMsg{ID: id, Name: name, Path: path, Version: version}
	})
}

// ApplyServiceSpec sends an edited spec back, then removes the file.
func ApplyServiceSpec(ctx context.Context, c *docker.Client, m ServiceSpecMsg) tea.Cmd {
	return safely("update service", func() tea.Msg {
		body, err := os.ReadFile(m.Path)
		if err == nil {
			err = c.ApplyServiceYAML(ctx, m.ID, m.Version, body)
		}
		if err != nil && !isWarning(err) {
			// The file is kept on a failure: an edit that took a while to
			// write is not thrown away because one field was wrong.
			return ActionDoneMsg{Label: "update " + m.Name,
				Err: fmt.Errorf("%w\n\nyour edit is still in %s", err, m.Path), Refresh: true}
		}
		_ = os.RemoveAll(filepath.Dir(m.Path))
		return done("updated "+m.Name, err)
	})
}

// DiscardServiceSpec removes the file of an edit that changed nothing.
func DiscardServiceSpec(path string) tea.Cmd {
	return func() tea.Msg {
		_ = os.RemoveAll(filepath.Dir(path))
		return nil
	}
}

// ServicesMsg carries a refreshed service list, polled with the tasks: the
// running counts move with them.
type ServicesMsg struct {
	Services []docker.Service
	Err      error
}

// ListServices refreshes the service list alone.
func ListServices(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("listing services", func() tea.Msg {
		svcs, err := c.ListServices(ctx)
		return ServicesMsg{Services: svcs, Err: err}
	})
}

// SetVolumeAvailability takes a cluster volume offline or back.
func SetVolumeAvailability(ctx context.Context, c *docker.Client, name, availability string) tea.Cmd {
	return safely("volume availability", func() tea.Msg {
		return done(name+" is now "+availability, c.SetVolumeAvailability(ctx, name, availability))
	})
}
