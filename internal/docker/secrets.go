package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/docker/docker/api/types/swarm"
)

// ListSecrets returns every secret, never its value: the daemon does not hand
// it back to anyone.
func (c *Client) ListSecrets(ctx context.Context) ([]Secret, error) {
	raw, err := c.api.SecretList(ctx, swarm.SecretListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	out := make([]Secret, 0, len(raw))
	for _, r := range raw {
		s := Secret{
			ID:        r.ID,
			Name:      r.Spec.Name,
			Version:   r.Version.Index,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			Labels:    r.Spec.Labels,
		}
		if r.Spec.Driver != nil {
			s.Driver = r.Spec.Driver.Name
		}
		if r.Spec.Templating != nil {
			s.Templating = r.Spec.Templating.Name
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ListConfigs returns every config with its contents.
func (c *Client) ListConfigs(ctx context.Context) ([]Config, error) {
	raw, err := c.api.ConfigList(ctx, swarm.ConfigListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing configs: %w", err)
	}
	out := make([]Config, 0, len(raw))
	for _, r := range raw {
		cfg := Config{
			ID:        r.ID,
			Name:      r.Spec.Name,
			Version:   r.Version.Index,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			Labels:    r.Spec.Labels,
			Data:      r.Spec.Data,
		}
		if r.Spec.Templating != nil {
			cfg.Templating = r.Spec.Templating.Name
		}
		out = append(out, cfg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateSecret stores a new secret. Secrets cannot be changed afterwards,
// which is why rotating one means creating another.
func (c *Client) CreateSecret(ctx context.Context, name string, data []byte, labels map[string]string) (string, error) {
	spec := swarm.SecretSpec{Annotations: swarm.Annotations{Name: name, Labels: labels}, Data: data}
	resp, err := c.api.SecretCreate(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("creating secret %s: %w", name, err)
	}
	return resp.ID, nil
}

// CreateConfig stores a new config. Like a secret, it is immutable.
func (c *Client) CreateConfig(ctx context.Context, name string, data []byte, labels map[string]string) (string, error) {
	spec := swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name, Labels: labels}, Data: data}
	resp, err := c.api.ConfigCreate(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("creating config %s: %w", name, err)
	}
	return resp.ID, nil
}

// RemoveSecret deletes a secret. The daemon refuses while a service uses it.
func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	if err := c.api.SecretRemove(ctx, id); err != nil {
		return fmt.Errorf("removing secret %s: %w", ShortID(id), err)
	}
	return nil
}

// RemoveConfig deletes a config. The daemon refuses while a service uses it.
func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	if err := c.api.ConfigRemove(ctx, id); err != nil {
		return fmt.Errorf("removing config %s: %w", ShortID(id), err)
	}
	return nil
}

// InspectSecret returns the raw inspect output, which carries no value.
func (c *Client) InspectSecret(ctx context.Context, id string) ([]byte, error) {
	_, raw, err := c.api.SecretInspectWithRaw(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspecting secret %s: %w", ShortID(id), err)
	}
	return indentJSON(raw), nil
}

// InspectConfig returns the raw inspect output, contents base64-encoded the
// way the API holds them.
func (c *Client) InspectConfig(ctx context.Context, id string) ([]byte, error) {
	_, raw, err := c.api.ConfigInspectWithRaw(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspecting config %s: %w", ShortID(id), err)
	}
	return indentJSON(raw), nil
}
