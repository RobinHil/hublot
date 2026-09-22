package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// ContainerSpec is an existing container described the way it would be
// created: what `docker run` was given, as far as the daemon still records it.
// It is what an edit form is filled from, and what a change is measured
// against.
type ContainerSpec struct {
	ID            string
	Name          string
	Image         string
	Command       []string
	Env           []string
	Ports         []string
	Mounts        []string
	RestartPolicy string
	NanoCPUs      int64
	Memory        int64
	Running       bool
	Labels        map[string]string
	Networks      []string
}

// Edit is a change to an existing container. A nil field was not touched, so
// it is never sent: a container carries far more than a form can show, and the
// only safe rule is that what nobody typed is left exactly as it was.
type Edit struct {
	Name          *string
	Image         *string
	Command       *[]string
	Env           *[]string
	Ports         *[]string
	Mounts        *[]string
	RestartPolicy *string
	NanoCPUs      *int64
	Memory        *int64
}

// Empty reports whether the edit asks for nothing.
func (e Edit) Empty() bool {
	return !e.NeedsRecreate() && !e.InPlace()
}

// InPlace reports whether anything can be applied to the container as it
// stands, without building a new one.
func (e Edit) InPlace() bool {
	return e.Name != nil || e.RestartPolicy != nil || e.NanoCPUs != nil || e.Memory != nil
}

// NeedsRecreate reports whether the change can only be made by replacing the
// container. The daemon reads a container's configuration once, when it is
// created, so an image, a command, an environment, a port map or a bind can
// only change by building another one.
//
// Clearing a limit belongs here too, and that is not obvious: the update
// endpoint reads zero as "leave this alone", so `--cpus 0` against a container
// limited to one core keeps the core. Removing a limit means recreating.
func (e Edit) NeedsRecreate() bool {
	if e.Image != nil || e.Command != nil || e.Env != nil || e.Ports != nil || e.Mounts != nil {
		return true
	}
	return (e.NanoCPUs != nil && *e.NanoCPUs == 0) || (e.Memory != nil && *e.Memory == 0)
}

// ContainerSpecOf reads a container back into the terms it was created with.
func (c *Client) ContainerSpecOf(ctx context.Context, id string) (ContainerSpec, error) {
	insp, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return ContainerSpec{}, fmt.Errorf("inspecting container %s: %w", ShortID(id), err)
	}

	spec := ContainerSpec{
		ID:      insp.ID,
		Name:    strings.TrimPrefix(insp.Name, "/"),
		Running: insp.State != nil && insp.State.Running,
	}

	if insp.Config != nil {
		spec.Image = insp.Config.Image
		spec.Command = insp.Config.Cmd
		spec.Labels = insp.Config.Labels
		spec.Env = c.givenEnv(ctx, insp.Config.Image, insp.Config.Env)
	}
	if insp.HostConfig != nil {
		spec.Ports = portSpecs(insp.HostConfig.PortBindings)
		spec.Mounts = append([]string(nil), insp.HostConfig.Binds...)
		spec.RestartPolicy = string(insp.HostConfig.RestartPolicy.Name)
		spec.NanoCPUs = insp.HostConfig.NanoCPUs
		spec.Memory = insp.HostConfig.Memory
	}
	spec.Networks = attachedNetworks(insp.NetworkSettings)

	return spec, nil
}

// givenEnv subtracts what the image declares from what the container carries,
// which leaves what was actually passed at creation. Inspect reports the two
// merged, so showing that list in a form would offer the image's own variables
// for editing and, worse, write them back as if a person had chosen them.
//
// An image that cannot be read gives the merged list back rather than nothing:
// too much is recoverable, too little is a silent loss.
func (c *Client) givenEnv(ctx context.Context, image string, env []string) []string {
	if len(env) == 0 || image == "" {
		return env
	}

	insp, err := c.api.ImageInspect(ctx, image)
	if err != nil || insp.Config == nil {
		return env
	}

	fromImage := make(map[string]bool, len(insp.Config.Env))
	for _, e := range insp.Config.Env {
		fromImage[e] = true
	}

	out := make([]string, 0, len(env))
	for _, e := range env {
		if !fromImage[e] {
			out = append(out, e)
		}
	}
	return out
}

// portSpecs renders the daemon's port map as the "host:container" strings the
// command line takes. Sorted, because a map has no order and a list that
// reorders itself between two refreshes reads as a list that will not sit
// still (AGENTS.md section 4, rule 0).
func portSpecs(bindings nat.PortMap) []string {
	var out []string
	for port, binds := range bindings {
		for _, b := range binds {
			spec := b.HostPort + ":" + port.Port()
			if b.HostIP != "" {
				spec = b.HostIP + ":" + spec
			}
			if port.Proto() != "tcp" {
				spec += "/" + port.Proto()
			}
			out = append(out, spec)
		}
	}
	sort.Strings(out)
	return out
}

// attachedNetworks lists the networks a container is on, in a decided order.
func attachedNetworks(settings *container.NetworkSettings) []string {
	if settings == nil {
		return nil
	}
	out := make([]string, 0, len(settings.Networks))
	for name := range settings.Networks {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ApplyEdit changes what a container can be changed about while it exists: its
// name, its restart policy and its limits. Everything else is fixed at
// creation, which is what RecreateContainer is for.
func (c *Client) ApplyEdit(ctx context.Context, id string, e Edit) error {
	if e.RestartPolicy != nil || e.NanoCPUs != nil || e.Memory != nil {
		cfg := container.UpdateConfig{}

		// Zero means "leave this alone" to the update endpoint, so a zero
		// never reaches it: clearing a limit is a recreate, decided above.
		if e.NanoCPUs != nil && *e.NanoCPUs > 0 {
			cfg.NanoCPUs = *e.NanoCPUs
		}
		if e.Memory != nil && *e.Memory > 0 {
			cfg.Memory = *e.Memory
			cfg.MemorySwap = *e.Memory
		}
		if e.RestartPolicy != nil {
			cfg.RestartPolicy = container.RestartPolicy{
				Name: container.RestartPolicyMode(*e.RestartPolicy),
			}
		}

		if _, err := c.api.ContainerUpdate(ctx, id, cfg); err != nil {
			return fmt.Errorf("updating container %s: %w", ShortID(id), err)
		}
	}

	if e.Name != nil {
		return c.RenameContainer(ctx, id, *e.Name)
	}
	return nil
}

// RecreateContainer replaces a container with one built from the same
// configuration plus the edit, and returns the new id.
//
// Everything the form does not cover is carried over from the old container
// verbatim: user, working directory, entrypoint, health check, capabilities,
// labels, the lot. A recreate that rebuilt a container from the handful of
// visible fields would quietly drop the rest, and a compose container that
// lost its labels would drop out of its stack.
//
// The order is what makes it safe. The old container is stopped and parked
// under another name, the new one is created and started, and only then is the
// old one removed: anything that fails before that last step is put back the
// way it was.
func (c *Client) RecreateContainer(ctx context.Context, id string, e Edit, timeout time.Duration) (string, error) {
	insp, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return "", fmt.Errorf("inspecting container %s: %w", ShortID(id), err)
	}
	if insp.Config == nil || insp.HostConfig == nil {
		return "", fmt.Errorf("the daemon reports no configuration for %s, so it cannot be rebuilt", ShortID(id))
	}

	name := strings.TrimPrefix(insp.Name, "/")
	wanted := name
	if e.Name != nil {
		wanted = *e.Name
	}
	wasRunning := insp.State != nil && insp.State.Running

	config := *insp.Config
	hostConfig := *insp.HostConfig

	// Always the variables that were given rather than the merged list: the
	// daemon adds the image's own back at creation, and writing them into the
	// configuration as well would double them at every edit.
	config.Env = c.givenEnv(ctx, insp.Config.Image, insp.Config.Env)

	if e.Image != nil {
		config.Image = *e.Image
	}
	if e.Command != nil {
		config.Cmd = *e.Command
	}
	if e.Env != nil {
		config.Env = *e.Env
	}
	if e.Ports != nil {
		exposed, bindings, err := nat.ParsePortSpecs(*e.Ports)
		if err != nil {
			return "", fmt.Errorf("reading the ports: %w", err)
		}
		config.ExposedPorts = exposed
		hostConfig.PortBindings = bindings
	}
	if e.Mounts != nil {
		hostConfig.Binds = *e.Mounts
	}
	if e.RestartPolicy != nil {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: container.RestartPolicyMode(*e.RestartPolicy),
		}
	}
	if e.NanoCPUs != nil {
		hostConfig.NanoCPUs = *e.NanoCPUs
	}
	if e.Memory != nil {
		hostConfig.Memory = *e.Memory
		hostConfig.MemorySwap = *e.Memory
		if *e.Memory == 0 {
			// Zero on both is what "no limit" looks like to the daemon; a swap
			// limit left behind on its own is refused.
			hostConfig.MemorySwap = 0
		}
	}

	primary, extra := endpointsOf(insp, hostConfig.NetworkMode)

	if wasRunning {
		if err := c.StopContainer(ctx, id, timeout); err != nil {
			return "", err
		}
	}

	// The name has to be free before the new container can take it, and the
	// old container has to survive until the new one runs.
	parked := parkedName(name, insp.ID)
	if err := c.api.ContainerRename(ctx, id, parked); err != nil {
		if wasRunning {
			_ = c.StartContainer(context.WithoutCancel(ctx), id)
		}
		return "", fmt.Errorf("making room for the new %s: %w", wanted, err)
	}

	// The rollback runs on a context of its own, detached from the caller's.
	// The commonest reason for the next call to fail is that the context was
	// cancelled, which is exactly when putting the old container back matters
	// most: a safety net that gives up for the same reason as the fall is not
	// a safety net.
	back := context.WithoutCancel(ctx)
	restore := func() {
		_ = c.api.ContainerRename(back, id, name)
		if wasRunning {
			_ = c.StartContainer(back, id)
		}
	}

	created, err := c.api.ContainerCreate(ctx, &config, &hostConfig, primary, nil, wanted)
	if err != nil {
		restore()
		return "", fmt.Errorf("creating the new %s: %w", wanted, err)
	}

	for name, endpoint := range extra {
		if err := c.api.NetworkConnect(ctx, name, created.ID, endpoint); err != nil {
			_ = c.api.ContainerRemove(back, created.ID, container.RemoveOptions{Force: true})
			restore()
			return "", fmt.Errorf("attaching the new %s to %s: %w", wanted, name, err)
		}
	}

	if wasRunning {
		if err := c.StartContainer(ctx, created.ID); err != nil {
			// Nothing is lost yet: the new one goes, the old one comes back.
			_ = c.api.ContainerRemove(back, created.ID, container.RemoveOptions{Force: true})
			restore()
			return "", fmt.Errorf("starting the new %s: %w", wanted, err)
		}
	}

	// Without RemoveVolumes: the anonymous volumes of the old container are
	// data nobody asked to destroy. They are left behind, unreferenced, where
	// the disk view will find them.
	if err := c.api.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		return created.ID, fmt.Errorf(
			"%s was rebuilt, but the old container is still here as %s: %w", wanted, parked, err)
	}

	return created.ID, nil
}

// parkedName is where the old container waits while the new one is built. The
// short id is in there because a name has to be unique and a previous edit may
// have left one behind.
func parkedName(name, id string) string {
	return fmt.Sprintf("%s-hublot-old-%s", name, ShortID(id))
}

// endpointsOf rebuilds the network attachments for the new container: the one
// it is created on, and the others it is connected to afterwards.
//
// Only one endpoint is passed to create because older daemons refuse more, and
// the rest go through connect, which every version takes. What is carried over
// is what a person chose, aliases and addresses; the operational half of an
// endpoint, its ids and the address the daemon handed out, belongs to the
// container that is about to be removed.
func endpointsOf(insp container.InspectResponse, mode container.NetworkMode) (
	*network.NetworkingConfig, map[string]*network.EndpointSettings,
) {
	if insp.NetworkSettings == nil || len(insp.NetworkSettings.Networks) == 0 {
		return nil, nil
	}

	self := strings.TrimPrefix(insp.Name, "/")
	wanted := make(map[string]*network.EndpointSettings, len(insp.NetworkSettings.Networks))
	for name, ep := range insp.NetworkSettings.Networks {
		if ep == nil {
			continue
		}
		wanted[name] = &network.EndpointSettings{
			IPAMConfig: ep.IPAMConfig,
			Links:      ep.Links,
			Aliases:    meaningfulAliases(ep, self, insp.ID),
			DriverOpts: ep.DriverOpts,
		}
	}

	// The network the host config names is the one to create on, so the new
	// container comes up the way the old one did rather than being attached in
	// a different order.
	first := mode.NetworkName()
	if _, ok := wanted[first]; !ok {
		first = sortedFirst(wanted)
	}

	primary := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{first: wanted[first]},
	}
	delete(wanted, first)
	return primary, wanted
}

// meaningfulAliases keeps the names someone chose and drops the ones the
// daemon writes for every container. A compose service is reachable by its
// service name, which is worth carrying over; the container's own name and its
// short id are not, and an alias holding the old id would be a name pointing
// at nothing.
func meaningfulAliases(ep *network.EndpointSettings, name, id string) []string {
	seen := map[string]bool{name: true, ShortID(id): true, id: true}

	var out []string
	for _, candidate := range append(append([]string{}, ep.Aliases...), ep.DNSNames...) {
		if candidate == "" || seen[candidate] || strings.HasPrefix(id, candidate) {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

// sortedFirst picks a network by name, so the choice is the same on every run.
func sortedFirst(endpoints map[string]*network.EndpointSettings) string {
	names := make([]string, 0, len(endpoints))
	for name := range endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
