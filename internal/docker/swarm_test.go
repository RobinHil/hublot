package docker

import (
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func replicated(n uint64) swarm.ServiceMode {
	return swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &n}}
}

func webSpec() swarm.ServiceSpec {
	return swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "shop_web", Labels: map[string]string{LabelStackNamespace: "shop"}},
		Mode:        replicated(3),
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: "nginx:1.27@sha256:abc",
				Env:   []string{"A=1", "B=2"},
				Secrets: []*swarm.SecretReference{{
					SecretID: "s1", SecretName: "db_pw",
					File: &swarm.SecretReferenceFileTarget{Name: "db_pw", UID: "0", GID: "0", Mode: 0o400},
				}},
			},
			Networks: []swarm.NetworkAttachmentConfig{{Target: "net1"}},
		},
		EndpointSpec: &swarm.EndpointSpec{Ports: []swarm.PortConfig{
			{PublishedPort: 8080, TargetPort: 80, Protocol: "tcp", PublishMode: "ingress"},
		}},
	}
}

func u64(n uint64) *uint64 { return &n }

func TestApplyServiceChangeScale(t *testing.T) {
	spec := webSpec()
	if err := applyServiceChange(&spec, ServiceChange{Replicas: u64(5)}); err != nil {
		t.Fatal(err)
	}
	if got := *spec.Mode.Replicated.Replicas; got != 5 {
		t.Fatalf("replicas = %d, want 5", got)
	}

	// A global service has no count; pretending to scale it would send a
	// spec the daemon rejects with a far less readable message.
	global := webSpec()
	global.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
	if err := applyServiceChange(&global, ServiceChange{Replicas: u64(2)}); err == nil {
		t.Fatal("scaling a global service was accepted")
	}
}

func TestApplyServiceChangeForceUpdateBumpsTheCounter(t *testing.T) {
	spec := webSpec()
	spec.TaskTemplate.ForceUpdate = 4
	if err := applyServiceChange(&spec, ServiceChange{ForceUpdate: true}); err != nil {
		t.Fatal(err)
	}
	if spec.TaskTemplate.ForceUpdate != 5 {
		t.Fatalf("ForceUpdate = %d, want 5", spec.TaskTemplate.ForceUpdate)
	}
}

func TestApplyServiceChangePorts(t *testing.T) {
	spec := webSpec()
	err := applyServiceChange(&spec, ServiceChange{AddPort: &ServicePort{Published: 8443, Target: 443}})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.EndpointSpec.Ports) != 2 || spec.EndpointSpec.Ports[1].PublishMode != "ingress" {
		t.Fatalf("ports = %+v", spec.EndpointSpec.Ports)
	}

	// The same published port twice is refused here rather than by the
	// daemon, which words it as a conflict with an endpoint id.
	err = applyServiceChange(&spec, ServiceChange{AddPort: &ServicePort{Published: 8080, Target: 81}})
	if err == nil {
		t.Fatal("publishing 8080 twice was accepted")
	}

	err = applyServiceChange(&spec, ServiceChange{RemovePort: &ServicePort{Published: 8080, Target: 80}})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.EndpointSpec.Ports) != 1 || spec.EndpointSpec.Ports[0].PublishedPort != 8443 {
		t.Fatalf("after removal: %+v", spec.EndpointSpec.Ports)
	}
}

func TestApplyServiceChangeConstraintsAreNotDuplicated(t *testing.T) {
	spec := webSpec()
	c := ServiceChange{AddConstraint: "node.role==worker"}
	for i := 0; i < 2; i++ {
		if err := applyServiceChange(&spec, c); err != nil {
			t.Fatal(err)
		}
	}
	if got := spec.TaskTemplate.Placement.Constraints; len(got) != 1 {
		t.Fatalf("constraints = %v", got)
	}
	if err := applyServiceChange(&spec, ServiceChange{RemoveConstraint: "node.role==worker"}); err != nil {
		t.Fatal(err)
	}
	if got := spec.TaskTemplate.Placement.Constraints; len(got) != 0 {
		t.Fatalf("constraints after removal = %v", got)
	}
}

// An older client wrote networks at the top level. Adding one must not leave
// the service split between the two places, or the daemon reads only half.
func TestApplyServiceChangeNetworksMigrateTheDeprecatedField(t *testing.T) {
	spec := webSpec()
	spec.TaskTemplate.Networks = nil
	spec.Networks = []swarm.NetworkAttachmentConfig{{Target: "old"}} //nolint:staticcheck // the case under test

	if err := applyServiceChange(&spec, ServiceChange{AddNetwork: "new"}); err != nil {
		t.Fatal(err)
	}
	if len(spec.Networks) != 0 { //nolint:staticcheck // the case under test
		t.Fatalf("deprecated field still set: %v", spec.Networks) //nolint:staticcheck // the case under test
	}
	if got := serviceNetworks(spec); strings.Join(got, ",") != "old,new" {
		t.Fatalf("networks = %v", got)
	}
}

func TestApplyServiceChangeEnv(t *testing.T) {
	spec := webSpec()
	if err := applyServiceChange(&spec, ServiceChange{SetEnv: "A=9"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(spec.TaskTemplate.ContainerSpec.Env, " "); got != "B=2 A=9" {
		t.Fatalf("env = %q", got)
	}
	if err := applyServiceChange(&spec, ServiceChange{RemoveEnv: "B"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(spec.TaskTemplate.ContainerSpec.Env, " "); got != "A=9" {
		t.Fatalf("env = %q", got)
	}
	if err := applyServiceChange(&spec, ServiceChange{SetEnv: "novalue"}); err == nil {
		t.Fatal("a variable without = was accepted")
	}
}

// Rotation keeps the path, owner and mode the service mounted the old secret
// with: the application reads the same file and finds the new value.
func TestApplyServiceChangeSwapSecretKeepsTheTarget(t *testing.T) {
	spec := webSpec()
	swap := &Swap{FromID: "s1", To: Reference{ID: "s2", Name: "db_pw.v2"}}
	if err := applyServiceChange(&spec, ServiceChange{SwapSecret: swap}); err != nil {
		t.Fatal(err)
	}
	ref := spec.TaskTemplate.ContainerSpec.Secrets[0]
	if ref.SecretID != "s2" || ref.SecretName != "db_pw.v2" {
		t.Fatalf("reference = %+v", ref)
	}
	if ref.File.Name != "db_pw" || ref.File.Mode != 0o400 {
		t.Fatalf("target changed: %+v", ref.File)
	}

	if err := applyServiceChange(&spec, ServiceChange{SwapSecret: swap}); err == nil {
		t.Fatal("swapping a secret the service no longer uses was accepted")
	}
}

func TestApplyServiceChangeAddAndRemoveConfig(t *testing.T) {
	spec := webSpec()
	add := &Reference{ID: "c1", Name: "nginx.conf", Target: "/etc/nginx/nginx.conf"}
	if err := applyServiceChange(&spec, ServiceChange{AddConfig: add}); err != nil {
		t.Fatal(err)
	}
	cs := spec.TaskTemplate.ContainerSpec
	if len(cs.Configs) != 1 || cs.Configs[0].File.Name != "/etc/nginx/nginx.conf" {
		t.Fatalf("configs = %+v", cs.Configs)
	}
	if err := applyServiceChange(&spec, ServiceChange{RemoveConfig: "c1"}); err != nil {
		t.Fatal(err)
	}
	if len(cs.Configs) != 0 {
		t.Fatalf("configs after removal = %+v", cs.Configs)
	}
}

// The edit goes out as YAML and comes back the same, numbers included: a
// duration in nanoseconds or a replica count read back as a quoted string
// would no longer decode into the spec.
func TestServiceSpecYAMLRoundTrip(t *testing.T) {
	spec := webSpec()
	delay := 5 * time.Second
	spec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{Condition: "on-failure", Delay: &delay}
	spec.UpdateConfig = &swarm.UpdateConfig{Parallelism: 2, Delay: 10 * time.Second, Order: "start-first"}

	body, err := specToYAML(spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"5000000000"`) {
		t.Fatalf("a duration was written as a string:\n%s", body)
	}

	back, err := yamlToSpec(body)
	if err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if *back.Mode.Replicated.Replicas != 3 || *back.TaskTemplate.RestartPolicy.Delay != delay ||
		back.UpdateConfig.Order != "start-first" || back.TaskTemplate.ContainerSpec.Secrets[0].File.Mode != 0o400 {
		t.Fatalf("round trip lost something: %+v", back)
	}
}

// A misspelt key would otherwise be dropped on the way back, and the edit
// with it, without a word.
func TestServiceSpecYAMLRefusesUnknownFields(t *testing.T) {
	_, err := yamlToSpec([]byte("Name: web\nTaskTemplate:\n  ContainerSpec:\n    Imgae: nginx\n"))
	if err == nil || !strings.Contains(err.Error(), "Imgae") {
		t.Fatalf("err = %v, want one naming the unknown field", err)
	}
}

func TestApplyNodeChange(t *testing.T) {
	spec := swarm.NodeSpec{Role: "worker", Availability: "active"}
	if err := applyNodeChange(&spec, NodeChange{Availability: "drain", Role: "manager", SetLabel: "zone=eu"}); err != nil {
		t.Fatal(err)
	}
	if spec.Availability != "drain" || spec.Role != "manager" || spec.Labels["zone"] != "eu" {
		t.Fatalf("spec = %+v", spec)
	}
	if err := applyNodeChange(&spec, NodeChange{RemoveLabel: "zone"}); err != nil || len(spec.Labels) != 0 {
		t.Fatalf("labels = %v, err = %v", spec.Labels, err)
	}
	for _, bad := range []NodeChange{{Availability: "off"}, {Role: "leader"}, {SetLabel: "zone"}} {
		if err := applyNodeChange(&spec, bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}

func TestApplyClusterChange(t *testing.T) {
	var spec swarm.Spec
	history, expiry, auto := int64(10), 48*time.Hour, true
	applyClusterChange(&spec, ClusterChange{TaskHistory: &history, CertExpiry: &expiry, Autolock: &auto, RotateCA: true})
	if *spec.Orchestration.TaskHistoryRetentionLimit != 10 || spec.CAConfig.NodeCertExpiry != expiry ||
		!spec.EncryptionConfig.AutoLockManagers || spec.CAConfig.ForceRotate != 1 {
		t.Fatalf("spec = %+v", spec)
	}

	// Untouched fields stay as they were: a settings edit of one row must not
	// reset the rest.
	applyClusterChange(&spec, ClusterChange{})
	if *spec.Orchestration.TaskHistoryRetentionLimit != 10 || spec.CAConfig.ForceRotate != 1 {
		t.Fatalf("an empty change altered the spec: %+v", spec)
	}
}

func TestSplitDetails(t *testing.T) {
	line := "com.docker.swarm.node.id=n1,com.docker.swarm.service.id=s1,com.docker.swarm.task.id=t1 hello world"
	task, rest, ok := splitDetails(line)
	if !ok || task != "t1" || rest != "hello world" {
		t.Fatalf("got %q %q %v", task, rest, ok)
	}

	// A line that merely contains an equals sign is somebody's output.
	if _, rest, ok := splitDetails("key=value then text"); ok || rest != "key=value then text" {
		t.Fatalf("a plain line was treated as details: %q", rest)
	}
}

func TestToSwarmState(t *testing.T) {
	got := toSwarmState(swarm.Info{})
	if got.State != SwarmInactive || got.Active() {
		t.Fatalf("an empty info reads as %+v", got)
	}

	got = toSwarmState(swarm.Info{
		LocalNodeState:   swarm.LocalNodeStateActive,
		ControlAvailable: true,
		RemoteManagers:   []swarm.Peer{{NodeID: "a", Addr: "10.0.0.1:2377"}},
		Cluster:          &swarm.ClusterInfo{ID: "c1"},
	})
	if !got.Active() || !got.Manager || got.ClusterID != "c1" || got.RemoteManagers[0] != "10.0.0.1:2377" {
		t.Fatalf("got %+v", got)
	}
}

func TestServiceMode(t *testing.T) {
	total := uint64(5)
	cases := []struct {
		mode  swarm.ServiceMode
		name  string
		count uint64
	}{
		{replicated(3), ModeReplicated, 3},
		{swarm.ServiceMode{Global: &swarm.GlobalService{}}, ModeGlobal, 0},
		{swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{TotalCompletions: &total}}, ModeReplicatedJob, 5},
		{swarm.ServiceMode{GlobalJob: &swarm.GlobalJob{}}, ModeGlobalJob, 0},
	}
	for _, c := range cases {
		name, count := serviceMode(c.mode)
		if name != c.name || count != c.count {
			t.Errorf("got %s/%d, want %s/%d", name, count, c.name, c.count)
		}
	}
}
