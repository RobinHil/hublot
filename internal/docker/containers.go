package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// ListContainers returns every container, running or not. The daemon's own
// ordering is unstable, so results come back sorted by name.
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	raw, err := c.api.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		out = append(out, toContainer(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// toContainer converts an SDK summary into our domain type. Nothing above this
// package ever sees container.Summary (AGENTS.md section 4, rule 1).
func toContainer(r container.Summary) Container {
	names := make([]string, 0, len(r.Names))
	for _, n := range r.Names {
		names = append(names, strings.TrimPrefix(n, "/"))
	}
	name := ShortID(r.ID)
	if len(names) > 0 {
		name = names[0]
	}

	ports := make([]Port, 0, len(r.Ports))
	for _, p := range r.Ports {
		ports = append(ports, Port{IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type})
	}

	mounts := make([]Mount, 0, len(r.Mounts))
	for _, m := range r.Mounts {
		mounts = append(mounts, Mount{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			RW:          m.RW,
		})
	}

	var networks []string
	if r.NetworkSettings != nil {
		for n := range r.NetworkSettings.Networks {
			networks = append(networks, n)
		}
		sort.Strings(networks)
	}

	return Container{
		ID:         r.ID,
		Name:       name,
		Names:      names,
		Image:      r.Image,
		ImageID:    r.ImageID,
		Command:    r.Command,
		Created:    time.Unix(r.Created, 0),
		State:      string(r.State),
		Status:     r.Status,
		Ports:      ports,
		Labels:     r.Labels,
		SizeRW:     r.SizeRw,
		SizeRootFS: r.SizeRootFs,
		Networks:   networks,
		Mounts:     mounts,
	}
}

// InspectContainer fetches the detail view, including the raw JSON so the UI can
// show it verbatim.
func (c *Client) InspectContainer(ctx context.Context, id string) (ContainerDetail, error) {
	insp, raw, err := c.api.ContainerInspectWithRaw(ctx, id, false)
	if err != nil {
		return ContainerDetail{}, fmt.Errorf("inspecting container %s: %w", ShortID(id), err)
	}

	d := ContainerDetail{Raw: raw}
	d.ID = insp.ID
	d.Name = strings.TrimPrefix(insp.Name, "/")
	d.Names = []string{d.Name}
	d.ImageID = insp.Image
	d.Path = insp.Path
	d.Args = insp.Args
	d.Driver = insp.Driver
	d.Platform = insp.Platform
	d.RestartCount = insp.RestartCount
	d.Created, _ = time.Parse(time.RFC3339Nano, insp.Created)

	if insp.State != nil {
		d.State = insp.State.Status
		d.ExitCode = insp.State.ExitCode
		d.Error = insp.State.Error
		d.StartedAt, _ = time.Parse(time.RFC3339Nano, insp.State.StartedAt)
		d.FinishedAt, _ = time.Parse(time.RFC3339Nano, insp.State.FinishedAt)
		if insp.State.Health != nil {
			d.Health = insp.State.Health.Status
		}
	}
	if insp.Config != nil {
		d.Image = insp.Config.Image
		d.TTY = insp.Config.Tty
		d.Env = insp.Config.Env
		d.WorkingDir = insp.Config.WorkingDir
		d.User = insp.Config.User
		d.Entrypoint = insp.Config.Entrypoint
		d.Labels = insp.Config.Labels
		d.Command = strings.Join(insp.Config.Cmd, " ")
	}
	if insp.HostConfig != nil {
		d.RestartPolicy = string(insp.HostConfig.RestartPolicy.Name)
		d.NanoCPUs = insp.HostConfig.NanoCPUs
		d.MemoryLimit = insp.HostConfig.Memory
	}
	for _, m := range insp.Mounts {
		d.Mounts = append(d.Mounts, Mount{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			RW:          m.RW,
		})
	}
	if insp.NetworkSettings != nil {
		for n := range insp.NetworkSettings.Networks {
			d.Networks = append(d.Networks, n)
		}
		sort.Strings(d.Networks)
	}
	return d, nil
}

// HasTTY reports whether the container was created with a TTY, which decides
// whether its log stream needs stdcopy demultiplexing (AGENTS.md section 6.4).
func (c *Client) HasTTY(ctx context.Context, id string) (bool, error) {
	insp, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return false, fmt.Errorf("inspecting container %s: %w", ShortID(id), err)
	}
	return insp.Config != nil && insp.Config.Tty, nil
}

// StartContainer starts a created or stopped container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting container %s: %w", ShortID(id), err)
	}
	return nil
}

// StopContainer sends SIGTERM then SIGKILL after the timeout.
func (c *Client) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	opts := container.StopOptions{}
	if secs > 0 {
		opts.Timeout = &secs
	}
	if err := c.api.ContainerStop(ctx, id, opts); err != nil {
		return fmt.Errorf("stopping container %s: %w", ShortID(id), err)
	}
	return nil
}

// RestartContainer stops then starts a container.
func (c *Client) RestartContainer(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	opts := container.StopOptions{}
	if secs > 0 {
		opts.Timeout = &secs
	}
	if err := c.api.ContainerRestart(ctx, id, opts); err != nil {
		return fmt.Errorf("restarting container %s: %w", ShortID(id), err)
	}
	return nil
}

// PauseContainer freezes every process in the container's cgroup.
func (c *Client) PauseContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerPause(ctx, id); err != nil {
		return fmt.Errorf("pausing container %s: %w", ShortID(id), err)
	}
	return nil
}

// UnpauseContainer resumes a paused container.
func (c *Client) UnpauseContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerUnpause(ctx, id); err != nil {
		return fmt.Errorf("unpausing container %s: %w", ShortID(id), err)
	}
	return nil
}

// KillContainer sends a signal. An empty signal means SIGKILL.
func (c *Client) KillContainer(ctx context.Context, id, signal string) error {
	if err := c.api.ContainerKill(ctx, id, signal); err != nil {
		return fmt.Errorf("sending %s to container %s: %w", orDefault(signal, "SIGKILL"), ShortID(id), err)
	}
	return nil
}

// RemoveContainer deletes a container, optionally forcing it and taking its
// anonymous volumes with it.
func (c *Client) RemoveContainer(ctx context.Context, id string, force, volumes bool) error {
	opts := container.RemoveOptions{Force: force, RemoveVolumes: volumes}
	if err := c.api.ContainerRemove(ctx, id, opts); err != nil {
		return fmt.Errorf("removing container %s: %w", ShortID(id), err)
	}
	return nil
}

// RenameContainer changes a container's name.
func (c *Client) RenameContainer(ctx context.Context, id, name string) error {
	if err := c.api.ContainerRename(ctx, id, name); err != nil {
		return fmt.Errorf("renaming container %s to %s: %w", ShortID(id), name, err)
	}
	return nil
}

// UpdateContainer changes resource limits in place. Zero values leave the
// corresponding limit untouched.
func (c *Client) UpdateContainer(ctx context.Context, id string, nanoCPUs, memory int64) error {
	cfg := container.UpdateConfig{}
	cfg.NanoCPUs = nanoCPUs
	cfg.Memory = memory
	if memory > 0 {
		cfg.MemorySwap = memory
	}
	if _, err := c.api.ContainerUpdate(ctx, id, cfg); err != nil {
		return fmt.Errorf("updating container %s: %w", ShortID(id), err)
	}
	return nil
}

// PruneContainers removes stopped containers. The preview shown to the user is
// computed separately in internal/state (AGENTS.md section 10.2); this call is
// what actually destroys them.
func (c *Client) PruneContainers(ctx context.Context, until time.Duration) (PruneReport, error) {
	args := filters.NewArgs()
	if until > 0 {
		args.Add("until", fmt.Sprintf("%ds", int(until.Seconds())))
	}
	rep, err := c.api.ContainersPrune(ctx, args)
	if err != nil {
		return PruneReport{}, fmt.Errorf("pruning containers: %w", err)
	}
	return PruneReport{Category: "containers", Deleted: rep.ContainersDeleted, Reclaimed: int64(rep.SpaceReclaimed)}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Processes lists what is running inside a container, as `docker top` does.
// The daemon runs ps on the host and returns its columns.
func (c *Client) Processes(ctx context.Context, id string) ([]string, [][]string, error) {
	top, err := c.api.ContainerTop(ctx, id, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("listing processes in %s: %w", ShortID(id), err)
	}
	return top.Titles, top.Processes, nil
}

// FilesystemChange is one difference between a container's filesystem and the
// image it came from.
type FilesystemChange struct {
	Path string
	// Kind is "added", "changed" or "deleted".
	Kind string
}

// Changes lists what has been written inside a container since it started,
// which is what its writable layer costs and what would be lost on a recreate.
func (c *Client) Changes(ctx context.Context, id string) ([]FilesystemChange, error) {
	raw, err := c.api.ContainerDiff(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reading filesystem changes of %s: %w", ShortID(id), err)
	}

	out := make([]FilesystemChange, 0, len(raw))
	for _, change := range raw {
		kind := "changed"
		switch change.Kind {
		case 0:
			kind = "changed"
		case 1:
			kind = "added"
		case 2:
			kind = "deleted"
		}
		out = append(out, FilesystemChange{Path: change.Path, Kind: kind})
	}
	return out, nil
}

// Commit turns a container's current filesystem into an image.
func (c *Client) Commit(ctx context.Context, id, reference, comment string) (string, error) {
	res, err := c.api.ContainerCommit(ctx, id, container.CommitOptions{
		Reference: reference,
		Comment:   comment,
		Pause:     true,
	})
	if err != nil {
		return "", fmt.Errorf("committing %s as %s: %w", ShortID(id), reference, err)
	}
	return res.ID, nil
}

// Export writes a container's whole filesystem to a tarball.
func (c *Client) Export(ctx context.Context, id, path string) error {
	rc, err := c.api.ContainerExport(ctx, id)
	if err != nil {
		return fmt.Errorf("exporting %s: %w", ShortID(id), err)
	}
	defer func() { _ = rc.Close() }()
	return writeStreamTo(rc, path)
}
