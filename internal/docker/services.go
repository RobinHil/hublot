package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// ListServices returns every service with the daemon's own running and desired
// counts, which is cheaper and more accurate than counting tasks here: the
// daemon knows which ones are meant to be running.
func (c *Client) ListServices(ctx context.Context) ([]Service, error) {
	raw, err := c.api.ServiceList(ctx, swarm.ServiceListOptions{Status: true})
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}
	out := make([]Service, 0, len(raw))
	for _, r := range raw {
		out = append(out, toService(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func toService(r swarm.Service) Service {
	spec := r.Spec
	s := Service{
		ID:          r.ID,
		Name:        spec.Name,
		Version:     r.Version.Index,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		Labels:      spec.Labels,
		CanRollback: r.PreviousSpec != nil,
	}
	s.Mode, s.Replicas = serviceMode(spec.Mode)

	if cs := spec.TaskTemplate.ContainerSpec; cs != nil {
		s.Image = cs.Image
		s.Env = cs.Env
		s.TTY = cs.TTY
		s.ContainerLabels = cs.Labels
		for _, ref := range cs.Secrets {
			if ref != nil {
				s.Secrets = append(s.Secrets, Reference{ID: ref.SecretID, Name: ref.SecretName, Target: secretTarget(ref)})
			}
		}
		for _, ref := range cs.Configs {
			if ref != nil {
				s.Configs = append(s.Configs, Reference{ID: ref.ConfigID, Name: ref.ConfigName, Target: configTarget(ref)})
			}
		}
	}
	if p := spec.TaskTemplate.Placement; p != nil {
		s.Constraints = p.Constraints
		s.MaxPerNode = p.MaxReplicas
	}

	s.Ports = servicePorts(r)
	s.Networks = serviceNetworks(spec)

	if st := r.ServiceStatus; st != nil {
		s.Running, s.Desired, s.Completed = st.RunningTasks, st.DesiredTasks, st.CompletedTasks
	}
	if u := r.UpdateStatus; u != nil {
		s.Update = UpdateStatus{State: string(u.State), Message: u.Message}
		if u.StartedAt != nil {
			s.Update.StartedAt = *u.StartedAt
		}
		if u.CompletedAt != nil {
			s.Update.EndedAt = *u.CompletedAt
		}
	}
	return s
}

// serviceMode names the mode and the count that goes with it.
func serviceMode(m swarm.ServiceMode) (string, uint64) {
	switch {
	case m.Replicated != nil:
		if m.Replicated.Replicas != nil {
			return ModeReplicated, *m.Replicated.Replicas
		}
		return ModeReplicated, 1
	case m.ReplicatedJob != nil:
		if m.ReplicatedJob.TotalCompletions != nil {
			return ModeReplicatedJob, *m.ReplicatedJob.TotalCompletions
		}
		return ModeReplicatedJob, 1
	case m.GlobalJob != nil:
		return ModeGlobalJob, 0
	default:
		return ModeGlobal, 0
	}
}

// servicePorts prefers what the endpoint actually published, which carries
// the port the daemon picked when the spec left it to choose.
func servicePorts(r swarm.Service) []ServicePort {
	ports := r.Endpoint.Ports
	if len(ports) == 0 && r.Spec.EndpointSpec != nil {
		ports = r.Spec.EndpointSpec.Ports
	}
	out := make([]ServicePort, 0, len(ports))
	for _, p := range ports {
		out = append(out, ServicePort{
			Published: p.PublishedPort,
			Target:    p.TargetPort,
			Protocol:  string(p.Protocol),
			Mode:      string(p.PublishMode),
		})
	}
	return out
}

// serviceNetworks lists the networks a service is attached to, wherever the
// spec put them: the task template is current, the top-level field is the
// deprecated place older clients still write to.
func serviceNetworks(spec swarm.ServiceSpec) []string {
	attached := spec.TaskTemplate.Networks
	if len(attached) == 0 {
		attached = spec.Networks //nolint:staticcheck // older clients still write here
	}
	out := make([]string, 0, len(attached))
	for _, n := range attached {
		out = append(out, n.Target)
	}
	return out
}

func secretTarget(ref *swarm.SecretReference) string {
	if ref.File != nil {
		return ref.File.Name
	}
	return ""
}

func configTarget(ref *swarm.ConfigReference) string {
	if ref.File != nil {
		return ref.File.Name
	}
	return ""
}

// InspectService returns the raw inspect output for the detail panel.
func (c *Client) InspectService(ctx context.Context, id string) ([]byte, error) {
	_, raw, err := c.api.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspecting service %s: %w", ShortID(id), err)
	}
	return indentJSON(raw), nil
}

// RemoveService deletes a service. Its tasks are shut down by the managers.
func (c *Client) RemoveService(ctx context.Context, id string) error {
	if err := c.api.ServiceRemove(ctx, id); err != nil {
		return fmt.Errorf("removing service %s: %w", ShortID(id), err)
	}
	return nil
}

// RollbackService puts back the spec the service had before its last update,
// the way `docker service rollback` does: the current spec is sent with the
// rollback flag and the daemon swaps in the previous one.
func (c *Client) RollbackService(ctx context.Context, id string) error {
	svc, _, err := c.api.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspecting service %s: %w", ShortID(id), err)
	}
	opts := swarm.ServiceUpdateOptions{Rollback: "previous", RegistryAuthFrom: swarm.RegistryAuthFromPreviousSpec}
	resp, err := c.api.ServiceUpdate(ctx, id, svc.Version, svc.Spec, opts)
	if err != nil {
		return fmt.Errorf("rolling back %s: %w", svc.Spec.Name, err)
	}
	return warningsError(resp.Warnings)
}

// UpdateService applies a change to the spec the daemon holds now. The
// registry credentials already stored in the spec are kept, so nodes that
// have not pulled the image yet still can: only a change of image needs fresh
// ones, and that goes through the CLI (see internal/swarm).
func (c *Client) UpdateService(ctx context.Context, id string, change ServiceChange) error {
	svc, _, err := c.api.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspecting service %s: %w", ShortID(id), err)
	}
	spec := svc.Spec
	if err := applyServiceChange(&spec, change); err != nil {
		return fmt.Errorf("changing %s: %w", spec.Name, err)
	}
	return c.sendServiceSpec(ctx, id, svc.Version, spec)
}

func (c *Client) sendServiceSpec(ctx context.Context, id string, version swarm.Version, spec swarm.ServiceSpec) error {
	opts := swarm.ServiceUpdateOptions{RegistryAuthFrom: swarm.RegistryAuthFromSpec}
	resp, err := c.api.ServiceUpdate(ctx, id, version, spec, opts)
	if err != nil {
		return fmt.Errorf("updating service %s: %w", spec.Name, err)
	}
	return warningsError(resp.Warnings)
}

// warningsError surfaces what the daemon accepted but wanted said, such as an
// image it could not resolve to a digest. Swallowing it would hide exactly
// the reason a node later fails to start a task.
func warningsError(warnings []string) error {
	if len(warnings) == 0 {
		return nil
	}
	return &Warning{Text: strings.Join(warnings, "; ")}
}

// Warning is a change the daemon made while saying something about it. The
// change happened: callers report it as a message, not as a failure.
type Warning struct{ Text string }

func (w *Warning) Error() string { return w.Text }
