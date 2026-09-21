// Package docker wraps the Docker Engine SDK. It is the only package allowed to
// import the SDK: everything above it consumes the domain types defined here.
// See AGENTS.md section 4, rule 1.
package docker

import "time"

// ContainerState values as reported by the daemon.
const (
	StateCreated    = "created"
	StateRunning    = "running"
	StatePaused     = "paused"
	StateRestarting = "restarting"
	StateRemoving   = "removing"
	StateExited     = "exited"
	StateDead       = "dead"
)

// Port is a published or exposed port of a container.
type Port struct {
	IP          string
	PrivatePort uint16
	PublicPort  uint16
	Type        string
}

// Mount is a filesystem mount attached to a container.
type Mount struct {
	Type        string
	Name        string
	Source      string
	Destination string
	RW          bool
}

// Container is the listing-level view of a container.
type Container struct {
	ID         string
	Name       string
	Names      []string
	Image      string
	ImageID    string
	Command    string
	Created    time.Time
	State      string
	Status     string
	Ports      []Port
	Labels     map[string]string
	SizeRW     int64
	SizeRootFS int64
	Networks   []string
	Mounts     []Mount
}

// Running reports whether the container consumes CPU right now. Paused
// containers keep their cgroup but report no deltas, so they are excluded.
func (c Container) Running() bool { return c.State == StateRunning }

// Removable reports whether the container can be removed without force.
func (c Container) Removable() bool {
	return c.State == StateExited || c.State == StateCreated || c.State == StateDead
}

// ContainerDetail is the inspect-level view, fetched on demand.
type ContainerDetail struct {
	Container
	Path          string
	Args          []string
	TTY           bool
	Platform      string
	Driver        string
	RestartCount  int
	ExitCode      int
	Error         string
	StartedAt     time.Time
	FinishedAt    time.Time
	Health        string
	Env           []string
	WorkingDir    string
	User          string
	Entrypoint    []string
	RestartPolicy string
	NanoCPUs      int64
	MemoryLimit   int64
	Raw           []byte
}

// Image is the listing-level view of an image.
type Image struct {
	ID          string
	RepoTags    []string
	RepoDigests []string
	Created     time.Time
	Size        int64
	SharedSize  int64
	Containers  int64
	Labels      map[string]string
}

// Dangling reports whether the image has no usable tag, which is the filter the
// daemon applies by default when pruning images (AGENTS.md section 10.2).
func (i Image) Dangling() bool {
	for _, t := range i.RepoTags {
		if t != "" && t != "<none>:<none>" {
			return false
		}
	}
	return true
}

// Ref is the display name of an image: first tag, else a short digest.
func (i Image) Ref() string {
	for _, t := range i.RepoTags {
		if t != "" && t != "<none>:<none>" {
			return t
		}
	}
	return ShortID(i.ID)
}

// HistoryLayer is one entry of an image build history.
type HistoryLayer struct {
	ID        string
	Created   time.Time
	CreatedBy string
	Size      int64
	Comment   string
	Tags      []string
}

// Volume is the listing-level view of a volume.
type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	CreatedAt  time.Time
	Labels     map[string]string
	Scope      string
	// Size is -1 when unknown: only `system df` computes it.
	Size int64
	// RefCount is -1 when unknown.
	RefCount  int64
	Anonymous bool
}

// Network is the listing-level view of a network.
type Network struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Created    time.Time
	Internal   bool
	Attachable bool
	IPv6       bool
	Subnets    []string
	Labels     map[string]string
	Containers map[string]string
}

// Predefined reports whether the network is one of the three the daemon creates
// itself and refuses to remove. Prune previews must exclude them.
func (n Network) Predefined() bool {
	switch n.Name {
	case "bridge", "host", "none":
		return true
	}
	return false
}

// BuildCacheRecord is one entry of the builder cache.
type BuildCacheRecord struct {
	ID          string
	Parent      string
	Type        string
	Description string
	InUse       bool
	Shared      bool
	Size        int64
	CreatedAt   time.Time
	LastUsedAt  time.Time
	UsageCount  int64
}

// DiskUsage is the result of `system df`. Expensive: on demand only
// (AGENTS.md section 6.6).
type DiskUsage struct {
	LayersSize int64
	Images     []Image
	Containers []Container
	Volumes    []Volume
	BuildCache []BuildCacheRecord
	// ActiveImages and friends are the daemon's own counts, kept for display.
	BuilderSize int64
	At          time.Time
}

// Info is the subset of `docker info` and `docker version` we display.
type Info struct {
	Name          string
	ServerVersion string
	APIVersion    string
	OS            string
	Arch          string
	KernelVersion string
	NCPU          int
	MemTotal      int64
	StorageDriver string
	CgroupVersion string
	Containers    int
	Running       int
	Paused        int
	Stopped       int
	Images        int
	DockerRootDir string
}

// Stats is one computed sample for one container. Percentages are already
// derived from the raw cumulative counters (AGENTS.md sections 6.1 and 6.2).
type Stats struct {
	ContainerID string
	At          time.Time
	// CPUValid is false for the first sample of a stream, where PreCPUStats
	// holds nothing usable. The UI must render "-", never "0%".
	CPUValid   bool
	CPUPercent float64
	MemUsage   int64
	MemLimit   int64
	MemPercent float64
	NetRx      int64
	NetTx      int64
	BlockRead  int64
	BlockWrite int64
	PIDs       int64
}

// Event is a daemon event, narrowed to what the UI reacts to.
type Event struct {
	Type       string
	Action     string
	ActorID    string
	ActorName  string
	Attributes map[string]string
	At         time.Time
}

// LogLine is one line of container output.
type LogLine struct {
	ContainerID string
	// Service is set only for aggregated Compose logs.
	Service string
	Stream  string // "stdout" or "stderr"
	At      time.Time
	Text    string
}

// PruneReport is what a prune actually did, as reported by the daemon.
type PruneReport struct {
	Category  string
	Deleted   []string
	Reclaimed int64
}

// ShortID truncates an ID the way the docker CLI does, tolerating the
// "sha256:" prefix the SDK returns on images.
func ShortID(id string) string {
	if len(id) > 7 && id[:7] == "sha256:" {
		id = id[7:]
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
