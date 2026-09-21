package state

import (
	"sort"
	"strings"
	"time"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
)

// historyLen is how many samples the sparklines keep per container.
const historyLen = 40

// Store is every piece of data the UI renders. It lives inside the Bubble Tea
// model and is only ever mutated from Update, on a single goroutine, which is
// why nothing here is protected by a mutex (AGENTS.md section 4, rule 2).
type Store struct {
	Containers []docker.Container
	Images     []docker.Image
	Volumes    []docker.Volume
	Networks   []docker.Network
	Projects   []compose.Project

	// Disk is nil until the user asks for it: `system df` is too slow to poll
	// (AGENTS.md section 6.6).
	Disk *docker.DiskUsage

	// Stats holds the latest sample per running container.
	Stats map[string]docker.Stats
	// CPUHistory and MemHistory feed the sparklines.
	CPUHistory map[string][]float64
	MemHistory map[string][]float64

	Info       docker.Info
	APIVersion string
	Socket     string
	// VolumePruneAll reflects whether the daemon understands the `all` filter,
	// which changes what a volume prune destroys (AGENTS.md section 10.2).
	VolumePruneAll bool

	// Stale is set while the event stream is disconnected. The UI keeps showing
	// the last known state and marks it (AGENTS.md section 12).
	Stale      bool
	StaleErr   error
	LastSync   time.Time
	ReadOnly   bool
	ComposeCLI compose.CLI
}

// New returns an empty store with its maps ready.
func New() *Store {
	return &Store{
		Stats:      map[string]docker.Stats{},
		CPUHistory: map[string][]float64{},
		MemHistory: map[string][]float64{},
	}
}

// SetContainers replaces the container list and rebuilds the Compose tree,
// since projects are derived entirely from container labels.
func (s *Store) SetContainers(cs []docker.Container) {
	s.Containers = cs
	s.LastSync = time.Now()
	s.rebuildProjects()
	s.dropOrphanStats()
}

// SetImages replaces the image list.
func (s *Store) SetImages(is []docker.Image) { s.Images = is }

// SetVolumes replaces the volume list and refreshes the Compose tree, because a
// stopped project stays discoverable through its volumes.
func (s *Store) SetVolumes(vs []docker.Volume) {
	s.Volumes = vs
	s.rebuildProjects()
}

// SetNetworks replaces the network list and refreshes the Compose tree.
func (s *Store) SetNetworks(ns []docker.Network) {
	s.Networks = ns
	s.rebuildProjects()
}

// SetDisk stores a `system df` result. Volume sizes only exist here, so they
// are merged back into the volume list to make prune previews accurate.
func (s *Store) SetDisk(du docker.DiskUsage) {
	s.Disk = &du
	sizes := make(map[string]docker.Volume, len(du.Volumes))
	for _, v := range du.Volumes {
		sizes[v.Name] = v
	}
	for i := range s.Volumes {
		if v, ok := sizes[s.Volumes[i].Name]; ok {
			s.Volumes[i].Size = v.Size
			s.Volumes[i].RefCount = v.RefCount
		}
	}
	s.rebuildProjects()
}

// DiskAge is how long ago the cached `system df` ran, for the Disk view header.
func (s *Store) DiskAge() (time.Duration, bool) {
	if s.Disk == nil {
		return 0, false
	}
	return time.Since(s.Disk.At), true
}

// rebuildProjects regroups every object by Compose project, preserving the
// drift markers already computed for services that still exist.
func (s *Store) rebuildProjects() {
	previous := map[string]map[string]compose.Service{}
	for _, p := range s.Projects {
		previous[p.Name] = map[string]compose.Service{}
		for _, svc := range p.Services {
			previous[p.Name][svc.Name] = svc
		}
	}

	s.Projects = compose.BuildProjects(compose.Inventory{
		Containers: s.Containers,
		Volumes:    s.Volumes,
		Networks:   s.Networks,
	})

	// Drift is computed on demand by shelling out; a plain list refresh must
	// not silently reset every marker to unknown.
	for i := range s.Projects {
		p := &s.Projects[i]
		for j := range p.Services {
			svc := &p.Services[j]
			old, ok := previous[p.Name][svc.Name]
			if !ok || old.FileHash == "" {
				continue
			}
			svc.FileHash = old.FileHash
			switch {
			case svc.Running() == 0:
				svc.Drift = compose.DriftStopped
			case svc.ConfigHash == "":
				svc.Drift = compose.DriftUnknown
			case svc.ConfigHash == svc.FileHash:
				svc.Drift = compose.DriftNone
			default:
				svc.Drift = compose.DriftChanged
			}
		}
	}
}

// ApplyDrift records freshly computed hashes for one project.
func (s *Store) ApplyDrift(project string, hashes map[string]string, err error) {
	for i := range s.Projects {
		if s.Projects[i].Name == project {
			compose.ApplyDrift(&s.Projects[i], hashes, err)
			return
		}
	}
}

// ApplyStats records a sample and appends to the sparkline histories.
func (s *Store) ApplyStats(st docker.Stats) {
	s.Stats[st.ContainerID] = st
	if st.CPUValid {
		s.CPUHistory[st.ContainerID] = appendHistory(s.CPUHistory[st.ContainerID], st.CPUPercent)
	}
	s.MemHistory[st.ContainerID] = appendHistory(s.MemHistory[st.ContainerID], float64(st.MemUsage))
}

// DropStats forgets a container's samples, called when it stops or disappears.
func (s *Store) DropStats(id string) {
	delete(s.Stats, id)
	delete(s.CPUHistory, id)
	delete(s.MemHistory, id)
}

// dropOrphanStats clears samples of containers that no longer exist, so the
// maps cannot grow without bound over a long session.
func (s *Store) dropOrphanStats() {
	alive := make(map[string]bool, len(s.Containers))
	for _, c := range s.Containers {
		alive[c.ID] = true
	}
	for id := range s.Stats {
		if !alive[id] {
			s.DropStats(id)
		}
	}
	for id := range s.CPUHistory {
		if !alive[id] {
			s.DropStats(id)
		}
	}
}

func appendHistory(h []float64, v float64) []float64 {
	h = append(h, v)
	if len(h) > historyLen {
		h = h[len(h)-historyLen:]
	}
	return h
}

// ContainerByID finds a container, reporting whether it is still there.
func (s *Store) ContainerByID(id string) (docker.Container, bool) {
	for _, c := range s.Containers {
		if c.ID == id {
			return c, true
		}
	}
	return docker.Container{}, false
}

// ProjectByName finds a Compose project.
func (s *Store) ProjectByName(name string) (compose.Project, bool) {
	for _, p := range s.Projects {
		if p.Name == name {
			return p, true
		}
	}
	return compose.Project{}, false
}

// RunningContainers lists containers that should have a stats stream open
// (AGENTS.md section 6.3).
func (s *Store) RunningContainers() []docker.Container {
	var out []docker.Container
	for _, c := range s.Containers {
		if c.Running() {
			out = append(out, c)
		}
	}
	return out
}

// Counts summarises the container list for the status bar.
func (s *Store) Counts() (total, running, stopped int) {
	for _, c := range s.Containers {
		total++
		if c.Running() {
			running++
		} else {
			stopped++
		}
	}
	return total, running, stopped
}

// SnapshotForPrune assembles what the prune previews are computed against.
func (s *Store) SnapshotForPrune(until time.Duration) Snapshot {
	var cache []docker.BuildCacheRecord
	if s.Disk != nil {
		cache = s.Disk.BuildCache
	}
	return Snapshot{
		Containers:              s.Containers,
		Images:                  s.Images,
		Volumes:                 s.Volumes,
		Networks:                s.Networks,
		BuildCache:              cache,
		Until:                   until,
		VolumePruneAllSupported: s.VolumePruneAll,
	}
}

// Filter narrows a list with an incremental query. Matching is
// case-insensitive and every term must match somewhere, so typing "web run"
// finds running containers named web.
func Filter[T any](items []T, query string, fields func(T) []string) []T {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return items
	}

	out := make([]T, 0, len(items))
	for _, it := range items {
		hay := strings.ToLower(strings.Join(fields(it), " "))
		matched := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				matched = false
				break
			}
		}
		if matched {
			out = append(out, it)
		}
	}
	return out
}

// ContainerFields is what a container filter query is matched against.
func ContainerFields(c docker.Container) []string {
	return []string{
		c.Name, c.Image, c.State, c.Status,
		compose.ProjectOf(c.Labels), compose.ServiceOf(c.Labels),
		docker.ShortID(c.ID),
	}
}

// ImageFields is what an image filter query is matched against.
func ImageFields(i docker.Image) []string {
	return append(append([]string{}, i.RepoTags...), docker.ShortID(i.ID))
}

// VolumeFields is what a volume filter query is matched against.
func VolumeFields(v docker.Volume) []string {
	return []string{v.Name, v.Driver, v.Mountpoint, v.Labels[compose.LabelProject]}
}

// NetworkFields is what a network filter query is matched against.
func NetworkFields(n docker.Network) []string {
	return append([]string{n.Name, n.Driver, n.Scope, n.Labels[compose.LabelProject]}, n.Subnets...)
}

// ProjectFields is what a Compose project filter query is matched against.
func ProjectFields(p compose.Project) []string {
	fields := []string{p.Name, p.WorkingDir}
	for _, s := range p.Services {
		fields = append(fields, s.Name)
	}
	return fields
}

// SortBy orders items with the given less function, ascending or descending,
// keeping the sort stable so equal rows do not jitter between refreshes.
func SortBy[T any](items []T, less func(a, b T) bool, asc bool) {
	sort.SliceStable(items, func(i, j int) bool {
		if asc {
			return less(items[i], items[j])
		}
		return less(items[j], items[i])
	})
}

// ContainerLess returns the comparison for a sort column, and whether the
// column is known.
func (s *Store) ContainerLess(column string) (func(a, b docker.Container) bool, bool) {
	switch column {
	case "name":
		return func(a, b docker.Container) bool { return a.Name < b.Name }, true
	case "image":
		return func(a, b docker.Container) bool { return a.Image < b.Image }, true
	case "state":
		return func(a, b docker.Container) bool { return a.State < b.State }, true
	case "created":
		return func(a, b docker.Container) bool { return a.Created.Before(b.Created) }, true
	case "cpu":
		return func(a, b docker.Container) bool {
			return s.Stats[a.ID].CPUPercent < s.Stats[b.ID].CPUPercent
		}, true
	case "mem":
		return func(a, b docker.Container) bool {
			return s.Stats[a.ID].MemUsage < s.Stats[b.ID].MemUsage
		}, true
	case "project":
		return func(a, b docker.Container) bool {
			pa, pb := compose.ProjectOf(a.Labels), compose.ProjectOf(b.Labels)
			if pa == pb {
				return a.Name < b.Name
			}
			return pa < pb
		}, true
	}
	return nil, false
}
