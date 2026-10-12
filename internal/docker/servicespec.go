package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/swarm"
	"gopkg.in/yaml.v3"
)

// ServiceChange is one edit to a service, in our own terms so the UI never
// touches a spec. Zero fields are left alone.
type ServiceChange struct {
	Replicas *uint64
	// ForceUpdate restarts every task even though nothing changed, rolling
	// through them the way an update does.
	ForceUpdate bool

	AddConstraint    string
	RemoveConstraint string
	AddPort          *ServicePort
	RemovePort       *ServicePort
	AddNetwork       string
	RemoveNetwork    string
	// SetLabel is a key=value pair for the service labels.
	SetLabel    string
	RemoveLabel string
	// SetEnv is a KEY=value pair, RemoveEnv a bare key.
	SetEnv    string
	RemoveEnv string

	AddSecret    *Reference
	RemoveSecret string
	AddConfig    *Reference
	RemoveConfig string
	// SwapSecret and SwapConfig point a service at a new version of what it
	// already mounts, at the same path, which is what a rotation needs.
	SwapSecret *Swap
	SwapConfig *Swap
}

// Swap replaces one reference by another.
type Swap struct {
	FromID string
	To     Reference
}

// applyServiceChange writes a change into a spec. Pure, so every case is
// tested without a daemon.
func applyServiceChange(spec *swarm.ServiceSpec, c ServiceChange) error {
	if c.Replicas != nil {
		if spec.Mode.Replicated == nil {
			return errors.New("only a replicated service has a replica count")
		}
		n := *c.Replicas
		spec.Mode.Replicated.Replicas = &n
	}
	if c.ForceUpdate {
		spec.TaskTemplate.ForceUpdate++
	}
	applyPlacement(spec, c)
	if err := applyPorts(spec, c); err != nil {
		return err
	}
	applyNetworks(spec, c)
	if err := applyLabelsAndEnv(spec, c); err != nil {
		return err
	}
	return applyReferences(spec, c)
}

func applyPlacement(spec *swarm.ServiceSpec, c ServiceChange) {
	if c.AddConstraint == "" && c.RemoveConstraint == "" {
		return
	}
	if spec.TaskTemplate.Placement == nil {
		spec.TaskTemplate.Placement = &swarm.Placement{}
	}
	p := spec.TaskTemplate.Placement
	if c.RemoveConstraint != "" {
		p.Constraints = withoutString(p.Constraints, c.RemoveConstraint)
	}
	if c.AddConstraint != "" && !hasString(p.Constraints, c.AddConstraint) {
		p.Constraints = append(p.Constraints, c.AddConstraint)
	}
}

func applyPorts(spec *swarm.ServiceSpec, c ServiceChange) error {
	if c.AddPort == nil && c.RemovePort == nil {
		return nil
	}
	if spec.EndpointSpec == nil {
		spec.EndpointSpec = &swarm.EndpointSpec{}
	}
	e := spec.EndpointSpec
	if r := c.RemovePort; r != nil {
		kept := e.Ports[:0]
		for _, p := range e.Ports {
			if !samePort(p, *r) {
				kept = append(kept, p)
			}
		}
		e.Ports = kept
	}
	if a := c.AddPort; a != nil {
		if a.Target == 0 {
			return errors.New("a published port needs the port inside the container")
		}
		for _, p := range e.Ports {
			if p.PublishedPort != 0 && p.PublishedPort == a.Published && protocolOf(p.Protocol) == orDefault(a.Protocol, "tcp") {
				return fmt.Errorf("port %d is already published by this service", a.Published)
			}
		}
		e.Ports = append(e.Ports, swarm.PortConfig{
			PublishedPort: a.Published,
			TargetPort:    a.Target,
			Protocol:      swarm.PortConfigProtocol(orDefault(a.Protocol, "tcp")),
			PublishMode:   swarm.PortConfigPublishMode(orDefault(a.Mode, "ingress")),
		})
	}
	return nil
}

func samePort(p swarm.PortConfig, r ServicePort) bool {
	return p.PublishedPort == r.Published && p.TargetPort == r.Target &&
		protocolOf(p.Protocol) == orDefault(r.Protocol, "tcp")
}

func protocolOf(p swarm.PortConfigProtocol) string { return orDefault(string(p), "tcp") }

func applyNetworks(spec *swarm.ServiceSpec, c ServiceChange) {
	if c.AddNetwork == "" && c.RemoveNetwork == "" {
		return
	}
	// The top-level field is the deprecated home of the same list. Moving it
	// into the task template first means the change lands in the one place
	// the daemon reads.
	if len(spec.TaskTemplate.Networks) == 0 && len(spec.Networks) > 0 { //nolint:staticcheck // migrating off it
		spec.TaskTemplate.Networks = spec.Networks //nolint:staticcheck // migrating off it
		spec.Networks = nil                        //nolint:staticcheck // migrating off it
	}
	nets := spec.TaskTemplate.Networks
	if c.RemoveNetwork != "" {
		kept := nets[:0]
		for _, n := range nets {
			if n.Target != c.RemoveNetwork {
				kept = append(kept, n)
			}
		}
		nets = kept
	}
	if c.AddNetwork != "" {
		present := false
		for _, n := range nets {
			present = present || n.Target == c.AddNetwork
		}
		if !present {
			nets = append(nets, swarm.NetworkAttachmentConfig{Target: c.AddNetwork})
		}
	}
	spec.TaskTemplate.Networks = nets
}

func applyLabelsAndEnv(spec *swarm.ServiceSpec, c ServiceChange) error {
	if c.SetLabel != "" {
		k, v, ok := strings.Cut(c.SetLabel, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("a label is key=value, not %q", c.SetLabel)
		}
		if spec.Labels == nil {
			spec.Labels = map[string]string{}
		}
		spec.Labels[strings.TrimSpace(k)] = v
	}
	if c.RemoveLabel != "" {
		delete(spec.Labels, c.RemoveLabel)
	}
	if c.SetEnv == "" && c.RemoveEnv == "" {
		return nil
	}
	cs := spec.TaskTemplate.ContainerSpec
	if cs == nil {
		return errors.New("this service runs no container to set variables in")
	}
	if c.SetEnv != "" {
		k, _, ok := strings.Cut(c.SetEnv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("a variable is KEY=value, not %q", c.SetEnv)
		}
		cs.Env = append(withoutEnv(cs.Env, k), c.SetEnv)
	}
	if c.RemoveEnv != "" {
		cs.Env = withoutEnv(cs.Env, c.RemoveEnv)
	}
	return nil
}

func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if k, _, _ := strings.Cut(e, "="); k != key {
			out = append(out, e)
		}
	}
	return out
}

func applyReferences(spec *swarm.ServiceSpec, c ServiceChange) error {
	touches := c.AddSecret != nil || c.RemoveSecret != "" || c.SwapSecret != nil ||
		c.AddConfig != nil || c.RemoveConfig != "" || c.SwapConfig != nil
	if !touches {
		return nil
	}
	cs := spec.TaskTemplate.ContainerSpec
	if cs == nil {
		return errors.New("this service runs no container to mount anything in")
	}
	if err := applySecrets(cs, c); err != nil {
		return err
	}
	return applyConfigs(cs, c)
}

func applySecrets(cs *swarm.ContainerSpec, c ServiceChange) error {
	if c.RemoveSecret != "" {
		kept := cs.Secrets[:0]
		for _, ref := range cs.Secrets {
			if ref.SecretID != c.RemoveSecret {
				kept = append(kept, ref)
			}
		}
		cs.Secrets = kept
	}
	if s := c.SwapSecret; s != nil {
		found := false
		for _, ref := range cs.Secrets {
			if ref.SecretID == s.FromID {
				ref.SecretID, ref.SecretName = s.To.ID, s.To.Name
				found = true
			}
		}
		if !found {
			return errors.New("the service does not use that secret any more")
		}
	}
	if a := c.AddSecret; a != nil {
		cs.Secrets = append(cs.Secrets, &swarm.SecretReference{
			SecretID:   a.ID,
			SecretName: a.Name,
			File:       &swarm.SecretReferenceFileTarget{Name: orDefault(a.Target, a.Name), UID: "0", GID: "0", Mode: 0o444},
		})
	}
	return nil
}

func applyConfigs(cs *swarm.ContainerSpec, c ServiceChange) error {
	if c.RemoveConfig != "" {
		kept := cs.Configs[:0]
		for _, ref := range cs.Configs {
			if ref.ConfigID != c.RemoveConfig {
				kept = append(kept, ref)
			}
		}
		cs.Configs = kept
	}
	if s := c.SwapConfig; s != nil {
		found := false
		for _, ref := range cs.Configs {
			if ref.ConfigID == s.FromID {
				ref.ConfigID, ref.ConfigName = s.To.ID, s.To.Name
				found = true
			}
		}
		if !found {
			return errors.New("the service does not use that config any more")
		}
	}
	if a := c.AddConfig; a != nil {
		cs.Configs = append(cs.Configs, &swarm.ConfigReference{
			ConfigID:   a.ID,
			ConfigName: a.Name,
			File:       &swarm.ConfigReferenceFileTarget{Name: orDefault(a.Target, "/"+a.Name), UID: "0", GID: "0", Mode: 0o444},
		})
	}
	return nil
}

func hasString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func withoutString(list []string, v string) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item != v {
			out = append(out, item)
		}
	}
	return out
}

// ServiceSpecYAML renders a service's spec for an editor, with the version it
// was read at: the edit is sent back against that version, so a change made
// meanwhile is refused rather than silently undone.
func (c *Client) ServiceSpecYAML(ctx context.Context, id string) ([]byte, uint64, error) {
	svc, _, err := c.api.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("inspecting service %s: %w", ShortID(id), err)
	}
	body, err := specToYAML(svc.Spec)
	if err != nil {
		return nil, 0, err
	}
	header := fmt.Sprintf("# Service %s, as the daemon holds it at version %d.\n"+
		"# This is the Engine API's own spec: durations are in nanoseconds.\n"+
		"# Write and quit to apply it; quit without writing to change nothing.\n\n",
		svc.Spec.Name, svc.Version.Index)
	return append([]byte(header), body...), svc.Version.Index, nil
}

// ApplyServiceYAML sends an edited spec back.
func (c *Client) ApplyServiceYAML(ctx context.Context, id string, version uint64, body []byte) error {
	spec, err := yamlToSpec(body)
	if err != nil {
		return err
	}
	return c.sendServiceSpec(ctx, id, swarm.Version{Index: version}, spec)
}

// specToYAML goes through JSON on purpose: the spec's field names are its JSON
// names, which are what the API documents and what an edit has to match.
func specToYAML(spec swarm.ServiceSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encoding the spec: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	// Numbers stay exact: a float64 rounds a large count or duration.
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("decoding the spec: %w", err)
	}
	out, err := yaml.Marshal(numbersIn(tree))
	if err != nil {
		return nil, fmt.Errorf("writing the spec as yaml: %w", err)
	}
	return out, nil
}

// numbersIn turns json.Number back into numbers, which YAML would otherwise
// write as quoted strings that no longer decode into the spec.
func numbersIn(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			t[k] = numbersIn(child)
		}
		return t
	case []any:
		for i, child := range t {
			t[i] = numbersIn(child)
		}
		return t
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if u, err := strconv.ParseUint(string(t), 10, 64); err == nil {
			return u
		}
		f, _ := t.Float64()
		return f
	}
	return v
}

// yamlToSpec reads an edited spec, refusing fields the API does not know: a
// misspelt key would otherwise vanish without a word and the edit with it.
func yamlToSpec(body []byte) (swarm.ServiceSpec, error) {
	var tree any
	if err := yaml.Unmarshal(body, &tree); err != nil {
		return swarm.ServiceSpec{}, fmt.Errorf("reading the edited spec: %w", err)
	}
	raw, err := json.Marshal(tree)
	if err != nil {
		return swarm.ServiceSpec{}, fmt.Errorf("reading the edited spec: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var spec swarm.ServiceSpec
	if err := dec.Decode(&spec); err != nil {
		return swarm.ServiceSpec{}, fmt.Errorf("the edited spec does not match the API: %w", err)
	}
	return spec, nil
}
