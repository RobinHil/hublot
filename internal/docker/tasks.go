package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/docker/docker/api/types/swarm"
)

// ListTasks returns every task the managers remember, history included: the
// failed attempts of a slot are what explain why it is not running. Tasks
// emit no events, so this is polled (AGENTS.md section 7).
func (c *Client) ListTasks(ctx context.Context) ([]Task, error) {
	raw, err := c.api.TaskList(ctx, swarm.TaskListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	out := make([]Task, 0, len(raw))
	for _, r := range raw {
		out = append(out, toTask(r))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServiceID != out[j].ServiceID {
			return out[i].ServiceID < out[j].ServiceID
		}
		if out[i].Slot != out[j].Slot {
			return out[i].Slot < out[j].Slot
		}
		if out[i].NodeID != out[j].NodeID {
			return out[i].NodeID < out[j].NodeID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func toTask(r swarm.Task) Task {
	t := Task{
		ID:           r.ID,
		ServiceID:    r.ServiceID,
		NodeID:       r.NodeID,
		Slot:         r.Slot,
		State:        string(r.Status.State),
		DesiredState: string(r.DesiredState),
		Message:      r.Status.Message,
		Err:          r.Status.Err,
		At:           r.Status.Timestamp,
		CreatedAt:    r.CreatedAt,
	}
	if cs := r.Status.ContainerStatus; cs != nil {
		t.ContainerID = cs.ContainerID
		t.ExitCode = cs.ExitCode
	}
	if spec := r.Spec.ContainerSpec; spec != nil {
		t.Image = spec.Image
	}
	return t
}

// InspectTask returns the raw inspect output for the detail panel.
func (c *Client) InspectTask(ctx context.Context, id string) ([]byte, error) {
	_, raw, err := c.api.TaskInspectWithRaw(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspecting task %s: %w", ShortID(id), err)
	}
	return indentJSON(raw), nil
}
