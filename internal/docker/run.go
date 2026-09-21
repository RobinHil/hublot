package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
)

// RunSpec is what it takes to create a container, in the terms a person uses:
// an image, somewhere to publish it, somewhere to keep its data, and what to
// run. Anything rarer belongs in a compose file, which is what compose is for.
type RunSpec struct {
	Image string
	Name  string
	// Ports are "host:container" specs, as the docker CLI takes them.
	Ports []string
	// Mounts are "source:/path/inside[:ro]" bindings.
	Mounts []string
	// Env are KEY=value pairs.
	Env []string
	// Command overrides the image's own, already split into arguments.
	Command []string
	// RestartPolicy is one of no, on-failure, unless-stopped, always.
	RestartPolicy string
	// Start runs the container once it is created.
	Start bool
}

// RunContainer creates a container from a spec and starts it unless asked not
// to. It returns the new id.
func (c *Client) RunContainer(ctx context.Context, spec RunSpec) (string, error) {
	if strings.TrimSpace(spec.Image) == "" {
		return "", fmt.Errorf("an image is required")
	}

	// nat is docker's own parser for these, so "8080:80/udp" and the rest
	// behave exactly as they do on the command line.
	exposed, bindings, err := nat.ParsePortSpecs(spec.Ports)
	if err != nil {
		return "", fmt.Errorf("reading the ports: %w", err)
	}

	config := &container.Config{
		Image:        spec.Image,
		Env:          spec.Env,
		Cmd:          spec.Command,
		ExposedPorts: exposed,
	}

	hostConfig := &container.HostConfig{
		Binds:        spec.Mounts,
		PortBindings: bindings,
	}
	if spec.RestartPolicy != "" && spec.RestartPolicy != "no" {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: container.RestartPolicyMode(spec.RestartPolicy),
		}
	}

	created, err := c.api.ContainerCreate(ctx, config, hostConfig, nil, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("creating a container from %s: %w", spec.Image, err)
	}

	if !spec.Start {
		return created.ID, nil
	}
	if err := c.StartContainer(ctx, created.ID); err != nil {
		// The container exists and is worth reporting even though it did not
		// start: removing it here would hide why.
		return created.ID, err
	}
	return created.ID, nil
}

// PullAndRun fetches the image first when the host does not have it, which is
// what `docker run` does and what anyone typing a reference expects.
func (c *Client) HasImage(ctx context.Context, reference string) bool {
	_, err := c.api.ImageInspect(ctx, reference)
	return err == nil
}
