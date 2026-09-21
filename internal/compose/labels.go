// Package compose reconstructs the Compose object model from the labels the
// Compose CLI writes on ordinary Docker objects, and shells out to that CLI for
// mutations. There is no /compose endpoint in the Engine API
// (AGENTS.md section 8).
package compose

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/RobinHil/hublot/internal/docker"
)

// Labels written by Compose v2 on every object it creates.
const (
	LabelProject     = "com.docker.compose.project"
	LabelService     = "com.docker.compose.service"
	LabelNumber      = "com.docker.compose.container-number"
	LabelWorkingDir  = "com.docker.compose.project.working_dir"
	LabelConfigFiles = "com.docker.compose.project.config_files"
	LabelConfigHash  = "com.docker.compose.config-hash"
	LabelDependsOn   = "com.docker.compose.depends_on"
	LabelOneOff      = "com.docker.compose.oneoff"
	LabelVersion     = "com.docker.compose.version"
)

// Drift is the per-service marker described in AGENTS.md section 8.4.
type Drift int

const (
	// DriftUnknown means the config could not be read or resolved.
	DriftUnknown Drift = iota
	// DriftNone means the running config matches the file.
	DriftNone
	// DriftChanged means the file changed since the container was created.
	DriftChanged
	// DriftStopped means nothing is running for this service.
	DriftStopped
)

// Marker is the one or two character badge shown in the Compose view.
func (d Drift) Marker() string {
	switch d {
	case DriftNone:
		return "ok"
	case DriftChanged:
		return "->"
	case DriftStopped:
		return "o"
	default:
		return "x"
	}
}

// Service is one service of a project, with its replicas.
type Service struct {
	Name       string
	Containers []docker.Container
	// ConfigHash is the hash recorded on the running containers. Replicas of
	// the same service always share it; if they do not, the project was
	// updated partially and the first one wins.
	ConfigHash string
	// FileHash is what the YAML on disk resolves to, filled in by drift
	// detection. Empty until then.
	FileHash  string
	Drift     Drift
	DependsOn []string
}

// Running counts the replicas actually running.
func (s Service) Running() int {
	n := 0
	for _, c := range s.Containers {
		if c.Running() {
			n++
		}
	}
	return n
}

// Replicas is the number of containers Compose created for this service.
func (s Service) Replicas() int { return len(s.Containers) }

// State summarises the service for display, e.g. "2/3 running".
func (s Service) State() string {
	if len(s.Containers) == 0 {
		return "not created"
	}
	return strconv.Itoa(s.Running()) + "/" + strconv.Itoa(len(s.Containers)) + " running"
}

// Project is one Compose stack, rebuilt from labels.
type Project struct {
	Name        string
	WorkingDir  string
	ConfigFiles []string
	Services    []Service
	Volumes     []docker.Volume
	Networks    []docker.Network
	// OneOff holds `compose run` leftovers, which belong to no service
	// (AGENTS.md section 8.5).
	OneOff []docker.Container
	// Orphaned means the config files are gone from disk, so any CLI action
	// that needs them will fail.
	Orphaned bool
	// Ghost means nothing runs and nothing was ever created but volumes or
	// networks survive: a stopped stack still holding disk.
	Ghost bool
}

// Containers flattens every container of the project, one-off ones included.
func (p Project) Containers() []docker.Container {
	var out []docker.Container
	for _, s := range p.Services {
		out = append(out, s.Containers...)
	}
	return append(out, p.OneOff...)
}

// Running counts running containers across every service.
func (p Project) Running() int {
	n := 0
	for _, c := range p.Containers() {
		if c.Running() {
			n++
		}
	}
	return n
}

// Total counts every container of the project.
func (p Project) Total() int { return len(p.Containers()) }

// Actionable reports whether CLI actions needing the YAML can run at all.
func (p Project) Actionable() bool { return !p.Orphaned && len(p.ConfigFiles) > 0 }

// Inventory is the raw material the project tree is built from.
type Inventory struct {
	Containers []docker.Container
	Volumes    []docker.Volume
	Networks   []docker.Network
	// Stat reports whether a config file still exists. Injected so the model
	// stays testable without touching the filesystem; nil means os.Stat.
	Stat func(string) error
}

// BuildProjects groups an inventory into projects, then services. Volumes and
// networks carry the project label too, which is how a stack with no remaining
// container stays discoverable (AGENTS.md section 8.1).
func BuildProjects(inv Inventory) []Project {
	stat := inv.Stat
	if stat == nil {
		stat = func(p string) error { _, err := os.Stat(p); return err }
	}

	byName := map[string]*Project{}
	get := func(name string) *Project {
		p, ok := byName[name]
		if !ok {
			p = &Project{Name: name}
			byName[name] = p
		}
		return p
	}

	services := map[string]map[string]*Service{}

	for _, c := range inv.Containers {
		name := c.Labels[LabelProject]
		if name == "" {
			continue
		}
		p := get(name)
		absorbProjectMeta(p, c.Labels)

		if isOneOff(c.Labels) {
			p.OneOff = append(p.OneOff, c)
			continue
		}

		svcName := c.Labels[LabelService]
		if svcName == "" {
			// A container labelled with a project but no service cannot be
			// addressed by the CLI; treat it as a one-off leftover.
			p.OneOff = append(p.OneOff, c)
			continue
		}

		if services[name] == nil {
			services[name] = map[string]*Service{}
		}
		s, ok := services[name][svcName]
		if !ok {
			s = &Service{Name: svcName}
			if deps := c.Labels[LabelDependsOn]; deps != "" {
				s.DependsOn = parseDependsOn(deps)
			}
			services[name][svcName] = s
		}
		s.Containers = append(s.Containers, c)
		if s.ConfigHash == "" {
			s.ConfigHash = c.Labels[LabelConfigHash]
		}
	}

	for _, v := range inv.Volumes {
		if name := v.Labels[LabelProject]; name != "" {
			p := get(name)
			p.Volumes = append(p.Volumes, v)
		}
	}
	for _, n := range inv.Networks {
		if name := n.Labels[LabelProject]; name != "" {
			p := get(name)
			p.Networks = append(p.Networks, n)
		}
	}

	out := make([]Project, 0, len(byName))
	for name, p := range byName {
		for _, s := range services[name] {
			sortContainers(s.Containers)
			if s.Running() == 0 {
				s.Drift = DriftStopped
			}
			p.Services = append(p.Services, *s)
		}
		sort.Slice(p.Services, func(i, j int) bool { return p.Services[i].Name < p.Services[j].Name })
		sortContainers(p.OneOff)

		p.Orphaned = configFilesMissing(p.ConfigFiles, stat)
		p.Ghost = len(p.Services) == 0 && len(p.OneOff) == 0 &&
			(len(p.Volumes) > 0 || len(p.Networks) > 0)

		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// absorbProjectMeta fills project-level fields from whichever container carries
// them. Compose writes them on every container, but a partially recreated stack
// can have some without.
func absorbProjectMeta(p *Project, labels map[string]string) {
	if p.WorkingDir == "" {
		p.WorkingDir = labels[LabelWorkingDir]
	}
	if len(p.ConfigFiles) == 0 {
		p.ConfigFiles = ParseConfigFiles(labels[LabelConfigFiles])
	}
}

// ParseConfigFiles splits the comma-separated label into paths.
func ParseConfigFiles(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseDependsOn splits the dependency label, which looks like
// "db:service_started:true,cache:service_healthy:false".
func parseDependsOn(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		name, _, _ := strings.Cut(part, ":")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// isOneOff reports whether the container came from `compose run`. Compose
// writes "True", but the value has been lowercase in some versions.
func isOneOff(labels map[string]string) bool {
	return strings.EqualFold(labels[LabelOneOff], "true")
}

// configFilesMissing reports whether any of the project's YAML files is gone.
// A project whose files vanished cannot be brought down through the CLI
// (AGENTS.md section 8.5, case 2).
func configFilesMissing(files []string, stat func(string) error) bool {
	if len(files) == 0 {
		return true
	}
	for _, f := range files {
		if stat(f) != nil {
			return true
		}
	}
	return false
}

// sortContainers orders replicas by their Compose index, falling back to name
// so the ordering stays stable when the label is absent.
func sortContainers(cs []docker.Container) {
	sort.Slice(cs, func(i, j int) bool {
		ni, erri := strconv.Atoi(cs[i].Labels[LabelNumber])
		nj, errj := strconv.Atoi(cs[j].Labels[LabelNumber])
		if erri == nil && errj == nil && ni != nj {
			return ni < nj
		}
		return cs[i].Name < cs[j].Name
	})
}

// ProjectOf returns the Compose project a container belongs to, or "".
func ProjectOf(labels map[string]string) string { return labels[LabelProject] }

// ServiceOf returns the Compose service a container belongs to, or "".
func ServiceOf(labels map[string]string) string { return labels[LabelService] }
