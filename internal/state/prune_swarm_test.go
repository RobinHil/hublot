package state

import (
	"strings"
	"testing"

	"github.com/RobinHil/hublot/internal/docker"
)

// The swarm half of the prune preview. Network prune on a manager reaches
// swarm networks, which local attachments say nothing about: a service whose
// tasks all run elsewhere still holds its network.

func stackLabels(stack string) map[string]string {
	return map[string]string{docker.LabelStackNamespace: stack}
}

func swarmNetworks() []docker.Network {
	return []docker.Network{
		{ID: "ing", Name: "ingress", Driver: "overlay", Scope: "swarm", Ingress: true},
		{ID: "used", Name: "shop_front", Driver: "overlay", Scope: "swarm", Labels: stackLabels("shop")},
		{ID: "byname", Name: "named_net", Driver: "overlay", Scope: "swarm"},
		{ID: "free", Name: "old_overlay", Driver: "overlay", Scope: "swarm"},
		{ID: "left", Name: "gone_default", Driver: "overlay", Scope: "swarm", Labels: stackLabels("gone")},
		{ID: "br", Name: "my_bridge", Driver: "bridge", Scope: "local"},
	}
}

func swarmServices() []docker.Service {
	return []docker.Service{
		// One service names its network by id, the other by name: a spec can
		// hold either, and both protect it.
		{ID: "s1", Name: "shop_web", Networks: []string{"used"}, Labels: stackLabels("shop"),
			Image: "nginx:1.27@sha256:aaa"},
		{ID: "s2", Name: "probe", Networks: []string{"named_net"}, Image: "busybox:latest"},
	}
}

func TestPruneNetworksOnAManager(t *testing.T) {
	snap := Snapshot{Now: testNow, Networks: swarmNetworks(), Services: swarmServices(), SwarmManager: true}
	p := BuildPreview(CatNetworks, snap)

	// Owned first: the leftover of a removed stack, then the loose ones.
	assertNames(t, p.Items, "gone_default", "my_bridge", "old_overlay")
	if p.StackOwned != 1 || p.Items[0].Owner() != "stack: gone" {
		t.Fatalf("stack ownership: %+v", p.Items[0])
	}
}

// A worker's network prune never reaches a swarm network: only managers
// prune those, and a worker cannot see which services use them anyway.
func TestPruneNetworksOnAWorkerSkipsSwarmNetworks(t *testing.T) {
	snap := Snapshot{Now: testNow, Networks: swarmNetworks()}
	p := BuildPreview(CatNetworks, snap)
	assertNames(t, p.Items, "my_bridge")
}

// The routing mesh is never pruned, even with no service publishing anything.
func TestPruneNetworksNeverTakesIngress(t *testing.T) {
	snap := Snapshot{
		Now:          testNow,
		SwarmManager: true,
		Networks:     []docker.Network{{ID: "ing", Name: "ingress", Scope: "swarm", Ingress: true}},
	}
	if p := BuildPreview(CatNetworks, snap); !p.Empty() {
		t.Fatalf("ingress would be pruned: %v", names(p.Items))
	}
}

func TestPruneContainersMarksFinishedTasks(t *testing.T) {
	labels := stackLabels("shop")
	labels[docker.LabelSwarmService] = "shop_web"
	snap := Snapshot{Now: testNow, Containers: []docker.Container{
		{ID: "1", Name: "loose", State: docker.StateExited},
		{ID: "2", Name: "shop_web.1.abc", State: docker.StateExited, Status: "Exited (0) 1 hour ago", Labels: labels},
	}}
	p := BuildPreview(CatContainers, snap)
	assertNames(t, p.Items, "shop_web.1.abc", "loose")
	it := p.Items[0]
	if it.Owner() != "stack: shop/shop_web" || !strings.HasPrefix(it.Detail, "swarm task") {
		t.Fatalf("task item = %+v", it)
	}
}

// The daemon prunes an image no local container uses, swarm or not. The
// preview keeps it in, and says which service will pull it back.
func TestPruneImagesAllFlagsImagesServicesRun(t *testing.T) {
	snap := Snapshot{
		Now:      testNow,
		Services: swarmServices(),
		Images: []docker.Image{
			{ID: "a", RepoTags: []string{"nginx:1.27"}, RepoDigests: []string{"nginx@sha256:aaa"}},
			{ID: "b", RepoDigests: []string{"nginx@sha256:aaa"}},
			{ID: "c", RepoTags: []string{"busybox:latest"}},
			{ID: "d", RepoTags: []string{"redis:7"}},
		},
	}
	p := BuildPreview(CatImagesAll, snap)
	if len(p.Items) != 4 {
		t.Fatalf("the preview must still list every image the daemon takes: %v", names(p.Items))
	}
	for _, it := range p.Items {
		used := it.ID != "d"
		if used != strings.Contains(it.Detail, "swarm service") {
			t.Errorf("%s: detail %q", it.ID, it.Detail)
		}
	}
}

func TestPruneVolumesStackOwned(t *testing.T) {
	snap := Snapshot{Now: testNow, VolumePruneAllSupported: true, Volumes: []docker.Volume{
		{Name: "shop_db", Labels: stackLabels("shop"), Size: 10},
		{Name: "loose", Size: 5},
	}}
	p := BuildPreview(CatVolumesAll, snap)
	assertNames(t, p.Items, "shop_db", "loose")
	if p.StackOwned != 1 || p.Owned() != 1 {
		t.Fatalf("owned = %d", p.Owned())
	}
	if !strings.Contains(p.Summary(), "1 belonging to swarm stacks") {
		t.Fatalf("summary = %q", p.Summary())
	}
}

func TestPruneVolumesNeverTakesClusterVolumes(t *testing.T) {
	snap := Snapshot{Now: testNow, VolumePruneAllSupported: true, Volumes: []docker.Volume{
		{Name: "csi_data", Cluster: true, Availability: "active"},
	}}
	if p := BuildPreview(CatVolumesAll, snap); !p.Empty() {
		t.Fatalf("a cluster volume would be pruned: %v", names(p.Items))
	}
}
