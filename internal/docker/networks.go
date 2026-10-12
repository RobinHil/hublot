package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// ListNetworks returns every network with its attached containers resolved.
// NetworkList omits the container map, so each network is inspected.
func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	raw, err := c.api.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}

	out := make([]Network, 0, len(raw))
	for _, r := range raw {
		n := toNetwork(r)
		// The list endpoint returns no Containers map; prune previews and the
		// detail pane both need it, and the inspect calls are cheap.
		if insp, err := c.api.NetworkInspect(ctx, r.ID, network.InspectOptions{}); err == nil {
			n.Containers = make(map[string]string, len(insp.Containers))
			for id, ep := range insp.Containers {
				n.Containers[id] = ep.Name
			}
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func toNetwork(r network.Summary) Network {
	var subnets []string
	for _, cfg := range r.IPAM.Config {
		if cfg.Subnet != "" {
			subnets = append(subnets, cfg.Subnet)
		}
	}
	return Network{
		ID:         r.ID,
		Name:       r.Name,
		Driver:     r.Driver,
		Scope:      r.Scope,
		Created:    r.Created,
		Internal:   r.Internal,
		Attachable: r.Attachable,
		IPv6:       r.EnableIPv6,
		Ingress:    r.Ingress,
		Subnets:    subnets,
		Labels:     r.Labels,
	}
}

// InspectNetwork returns the raw inspect JSON for display.
func (c *Client) InspectNetwork(ctx context.Context, id string) ([]byte, error) {
	insp, err := c.api.NetworkInspect(ctx, id, network.InspectOptions{Verbose: true})
	if err != nil {
		return nil, fmt.Errorf("inspecting network %s: %w", ShortID(id), err)
	}
	return marshalIndent(insp)
}

// RemoveNetwork deletes a user-defined network.
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	if err := c.api.NetworkRemove(ctx, id); err != nil {
		return fmt.Errorf("removing network %s: %w", ShortID(id), err)
	}
	return nil
}

// ConnectNetwork attaches a container to a network.
func (c *Client) ConnectNetwork(ctx context.Context, networkID, containerID string) error {
	if err := c.api.NetworkConnect(ctx, networkID, containerID, nil); err != nil {
		return fmt.Errorf("connecting container %s to network %s: %w", ShortID(containerID), ShortID(networkID), err)
	}
	return nil
}

// DisconnectNetwork detaches a container from a network.
func (c *Client) DisconnectNetwork(ctx context.Context, networkID, containerID string, force bool) error {
	if err := c.api.NetworkDisconnect(ctx, networkID, containerID, force); err != nil {
		return fmt.Errorf("disconnecting container %s from network %s: %w", ShortID(containerID), ShortID(networkID), err)
	}
	return nil
}

// PruneNetworks removes user-defined networks no container is attached to.
func (c *Client) PruneNetworks(ctx context.Context) (PruneReport, error) {
	rep, err := c.api.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		return PruneReport{}, fmt.Errorf("pruning networks: %w", err)
	}
	// Networks hold no data, so the daemon reports no reclaimed space.
	return PruneReport{Category: "networks", Deleted: rep.NetworksDeleted}, nil
}

// NetworkSpec is what creating a network asks for.
type NetworkSpec struct {
	Name string
	// Driver is "bridge" for one machine, "overlay" for a swarm.
	Driver     string
	Attachable bool
	Internal   bool
	// Encrypted turns on IPsec between nodes, for overlay networks only.
	Encrypted bool
	Subnet    string
}

// CreateNetwork makes a user-defined network. A bridge is what a network
// created by hand is usually for, letting a few containers find each other by
// name; an overlay spans the nodes of a swarm.
func (c *Client) CreateNetwork(ctx context.Context, spec NetworkSpec) error {
	opts := network.CreateOptions{
		Driver:     orDefault(spec.Driver, "bridge"),
		Attachable: spec.Attachable,
		Internal:   spec.Internal,
	}
	if spec.Driver == "overlay" {
		opts.Scope = "swarm"
		if spec.Encrypted {
			opts.Options = map[string]string{"encrypted": ""}
		}
	}
	if spec.Subnet != "" {
		opts.IPAM = &network.IPAM{Config: []network.IPAMConfig{{Subnet: spec.Subnet}}}
	}
	if _, err := c.api.NetworkCreate(ctx, spec.Name, opts); err != nil {
		return fmt.Errorf("creating network %s: %w", spec.Name, err)
	}
	return nil
}
