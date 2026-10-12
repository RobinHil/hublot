// Package docker wraps the Docker Engine SDK. It is the only package allowed to
// import the SDK: everything above it consumes the domain types defined here.
// See AGENTS.md section 4, rule 1.
package docker

import (
	"sort"
	"time"
)

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
	// Cluster is set for a CSI volume managed by a swarm, which lives beyond
	// any one node. ClusterID is its swarm id, Availability active, pause or
	// drain like a node's, and Group the volume group it was created in.
	Cluster      bool
	ClusterID    string
	Availability string
	Group        string
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
	// Ingress marks the swarm's routing-mesh network, which publishes service
	// ports on every node. Nothing prunes it; removing it is a deliberate act.
	Ingress    bool
	Subnets    []string
	Labels     map[string]string
	Containers map[string]string
}

// Attachment is a container attached to a network.
type Attachment struct {
	ID   string
	Name string
}

// Attached lists what is attached, in a stable order. Ranging over the map
// gives a different order on every call, so a column built from it reshuffles
// itself on every refresh, which is once a second: the text under the cursor
// changes while nothing about the host has.
func (n Network) Attached() []Attachment {
	out := make([]Attachment, 0, len(n.Containers))
	for id, name := range n.Containers {
		out = append(out, Attachment{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
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
	Swarm         SwarmState
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

// Swarm node states, as the daemon reports them in its info.
const (
	SwarmInactive = "inactive"
	SwarmPending  = "pending"
	SwarmActive   = "active"
	SwarmError    = "error"
	SwarmLocked   = "locked"
)

// SwarmState is where this daemon stands in a swarm. Every node knows this
// much, manager or not; anything more needs a manager.
type SwarmState struct {
	// State is one of the Swarm* constants.
	State string
	// Manager is set when this node can answer for the cluster: list
	// services, nodes, secrets. A worker is refused all of that.
	Manager  bool
	NodeID   string
	NodeAddr string
	// Error is what the daemon said went wrong, in the error state.
	Error string
	// RemoteManagers are the managers this node knows of, by address.
	RemoteManagers []string
	Nodes          int
	Managers       int
	ClusterID      string
}

// Active reports whether the node is part of a working swarm.
func (s SwarmState) Active() bool { return s.State == SwarmActive }

// Cluster is the swarm as a manager sees it: its settings and its tokens.
type Cluster struct {
	ID        string
	Version   uint64
	CreatedAt time.Time
	UpdatedAt time.Time

	WorkerToken  string
	ManagerToken string

	Autolock         bool
	TaskHistory      int64
	CertExpiry       time.Duration
	Heartbeat        time.Duration
	SnapshotInterval uint64
	KeepOldSnapshots uint64
	ElectionTick     int
	HeartbeatTick    int
	ExternalCAs      []string
	RootRotation     bool
	DefaultAddrPool  []string
	SubnetSize       uint32
	DataPathPort     uint32
}

// ClusterChange is what a settings edit asks for. Nil fields are left alone.
type ClusterChange struct {
	TaskHistory      *int64
	CertExpiry       *time.Duration
	Heartbeat        *time.Duration
	SnapshotInterval *uint64
	KeepOldSnapshots *uint64
	Autolock         *bool

	RotateWorkerToken  bool
	RotateManagerToken bool
	RotateUnlockKey    bool
	// RotateCA issues a new root certificate and rolls every node onto it.
	RotateCA bool
}

// InitSpec is what `docker swarm init` asks for.
type InitSpec struct {
	AdvertiseAddr   string
	ListenAddr      string
	DataPathAddr    string
	DefaultAddrPool []string
	Autolock        bool
	ForceNewCluster bool
}

// JoinSpec is what `docker swarm join` asks for.
type JoinSpec struct {
	RemoteAddr    string
	Token         string
	AdvertiseAddr string
	ListenAddr    string
	DataPathAddr  string
}

// Service modes. Jobs run to completion instead of being kept running.
const (
	ModeReplicated    = "replicated"
	ModeGlobal        = "global"
	ModeReplicatedJob = "replicated-job"
	ModeGlobalJob     = "global-job"
)

// ServicePort is a port a service publishes.
type ServicePort struct {
	Published uint32
	Target    uint32
	Protocol  string
	// Mode is "ingress", through the routing mesh on every node, or "host",
	// only on the nodes running a task.
	Mode string
}

// Reference is a secret or a config mounted into a service.
type Reference struct {
	ID     string
	Name   string
	Target string
}

// UpdateStatus is where a rolling update or a rollback stands.
type UpdateStatus struct {
	State     string
	StartedAt time.Time
	EndedAt   time.Time
	Message   string
}

// Service is a swarm service: a desired state the managers keep converging to.
type Service struct {
	ID        string
	Name      string
	Version   uint64
	CreatedAt time.Time
	UpdatedAt time.Time

	Image string
	Mode  string
	// Replicas is the desired count of a replicated service, and the total
	// completions of a replicated job.
	Replicas uint64
	// Running, Desired and Completed come from the daemon's own count.
	Running   uint64
	Desired   uint64
	Completed uint64

	Ports       []ServicePort
	Networks    []string
	Secrets     []Reference
	Configs     []Reference
	Constraints []string
	MaxPerNode  uint64
	Env         []string
	TTY         bool

	Labels          map[string]string
	ContainerLabels map[string]string

	Update UpdateStatus
	// CanRollback is set when the daemon kept a previous spec to go back to.
	CanRollback bool
}

// Stack is the stack a service was deployed by, empty for a loose one.
func (s Service) Stack() string { return s.Labels[LabelStackNamespace] }

// Job reports whether the service runs to completion.
func (s Service) Job() bool { return s.Mode == ModeReplicatedJob || s.Mode == ModeGlobalJob }

// Scalable reports whether the replica count means anything for this service.
func (s Service) Scalable() bool { return s.Mode == ModeReplicated }

// LabelList is the service labels in a stable order (AGENTS.md section 4,
// rule 0).
func (s Service) LabelList() []string { return sortedPairs(s.Labels) }

// LabelStackNamespace is what `docker stack deploy` writes on everything it
// creates: services, networks, secrets, configs.
const LabelStackNamespace = "com.docker.stack.namespace"

// Labels the swarm agent writes on the containers of its tasks.
const (
	LabelSwarmService = "com.docker.swarm.service.name"
	LabelSwarmTask    = "com.docker.swarm.task.name"
	LabelSwarmNode    = "com.docker.swarm.node.id"
)

// Task states worth naming outside the docker layer.
const (
	TaskRunning  = "running"
	TaskComplete = "complete"
	TaskFailed   = "failed"
	TaskRejected = "rejected"
	TaskShutdown = "shutdown"
)

// Task is one attempt at running a slot of a service on a node.
type Task struct {
	ID        string
	ServiceID string
	NodeID    string
	// Slot numbers the replicas of a replicated service. Global tasks have
	// none: the node is what tells them apart.
	Slot         int
	State        string
	DesiredState string
	Message      string
	Err          string
	At           time.Time
	CreatedAt    time.Time
	ContainerID  string
	ExitCode     int
	Image        string
}

// Failed reports whether the task ended in a way worth showing as an error.
func (t Task) Failed() bool {
	return t.State == TaskFailed || t.State == TaskRejected || t.Err != ""
}

// Node is one machine of the swarm.
type Node struct {
	ID       string
	Version  uint64
	Hostname string
	// Role is "manager" or "worker", Availability "active", "pause" or
	// "drain", State "ready", "down", "unknown" or "disconnected".
	Role         string
	Availability string
	State        string
	Message      string
	Addr         string
	Leader       bool
	Reachability string
	ManagerAddr  string

	EngineVersion string
	OS            string
	Arch          string
	NanoCPUs      int64
	Memory        int64

	Labels    map[string]string
	CreatedAt time.Time
}

// Manager reports whether the node takes part in the raft quorum.
func (n Node) Manager() bool { return n.Role == "manager" }

// LabelList is the node labels in a stable order.
func (n Node) LabelList() []string { return sortedPairs(n.Labels) }

// Secret is a swarm secret. The daemon never hands its value back.
type Secret struct {
	ID         string
	Name       string
	Version    uint64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Labels     map[string]string
	Driver     string
	Templating string
}

// Stack is the stack that deployed the secret, empty otherwise.
func (s Secret) Stack() string { return s.Labels[LabelStackNamespace] }

// Config is a swarm config, which unlike a secret can be read back.
type Config struct {
	ID         string
	Name       string
	Version    uint64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Labels     map[string]string
	Templating string
	Data       []byte
}

// Stack is the stack that deployed the config, empty otherwise.
func (c Config) Stack() string { return c.Labels[LabelStackNamespace] }

// sortedPairs renders a map as key=value pairs in key order.
func sortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
