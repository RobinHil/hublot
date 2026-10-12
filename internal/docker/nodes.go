package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// ListNodes returns every node of the swarm. Managers only.
func (c *Client) ListNodes(ctx context.Context) ([]Node, error) {
	raw, err := c.api.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	out := make([]Node, 0, len(raw))
	for _, r := range raw {
		out = append(out, toNode(r))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hostname != out[j].Hostname {
			return out[i].Hostname < out[j].Hostname
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func toNode(r swarm.Node) Node {
	n := Node{
		ID:            r.ID,
		Version:       r.Version.Index,
		Hostname:      r.Description.Hostname,
		Role:          string(r.Spec.Role),
		Availability:  string(r.Spec.Availability),
		State:         string(r.Status.State),
		Message:       r.Status.Message,
		Addr:          r.Status.Addr,
		EngineVersion: r.Description.Engine.EngineVersion,
		OS:            r.Description.Platform.OS,
		Arch:          r.Description.Platform.Architecture,
		NanoCPUs:      r.Description.Resources.NanoCPUs,
		Memory:        r.Description.Resources.MemoryBytes,
		Labels:        r.Spec.Labels,
		CreatedAt:     r.CreatedAt,
	}
	if m := r.ManagerStatus; m != nil {
		n.Leader = m.Leader
		n.Reachability = string(m.Reachability)
		n.ManagerAddr = m.Addr
	}
	return n
}

// InspectNode returns the raw inspect output for the detail panel.
func (c *Client) InspectNode(ctx context.Context, id string) ([]byte, error) {
	_, raw, err := c.api.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspecting node %s: %w", ShortID(id), err)
	}
	return indentJSON(raw), nil
}

// NodeChange is one edit to a node. Empty fields are left alone.
type NodeChange struct {
	// Availability is "active", "pause" or "drain".
	Availability string
	// Role is "manager" or "worker": promote and demote.
	Role string
	// SetLabel is a key=value pair, RemoveLabel a bare key.
	SetLabel    string
	RemoveLabel string
}

// UpdateNode applies a change against the version just read.
func (c *Client) UpdateNode(ctx context.Context, id string, change NodeChange) error {
	node, _, err := c.api.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return fmt.Errorf("inspecting node %s: %w", ShortID(id), err)
	}
	spec := node.Spec
	if err := applyNodeChange(&spec, change); err != nil {
		return err
	}
	if err := c.api.NodeUpdate(ctx, id, node.Version, spec); err != nil {
		return fmt.Errorf("updating node %s: %w", node.Description.Hostname, err)
	}
	return nil
}

func applyNodeChange(spec *swarm.NodeSpec, change NodeChange) error {
	switch change.Availability {
	case "":
	case "active", "pause", "drain":
		spec.Availability = swarm.NodeAvailability(change.Availability)
	default:
		return fmt.Errorf("availability is active, pause or drain, not %q", change.Availability)
	}
	switch change.Role {
	case "":
	case "manager", "worker":
		spec.Role = swarm.NodeRole(change.Role)
	default:
		return fmt.Errorf("a node is a manager or a worker, not %q", change.Role)
	}
	if change.SetLabel != "" {
		k, v, ok := strings.Cut(change.SetLabel, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("a label is key=value, not %q", change.SetLabel)
		}
		if spec.Labels == nil {
			spec.Labels = map[string]string{}
		}
		spec.Labels[strings.TrimSpace(k)] = v
	}
	if change.RemoveLabel != "" {
		delete(spec.Labels, change.RemoveLabel)
	}
	return nil
}

// RemoveNode takes a node out of the swarm's records. Without force the
// daemon refuses a node that is not down.
func (c *Client) RemoveNode(ctx context.Context, id string, force bool) error {
	if err := c.api.NodeRemove(ctx, id, swarm.NodeRemoveOptions{Force: force}); err != nil {
		return fmt.Errorf("removing node %s: %w", ShortID(id), err)
	}
	return nil
}
