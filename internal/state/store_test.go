package state

import (
	"errors"
	"testing"
	"time"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
)

func labelled(project, service string) map[string]string {
	return map[string]string{
		compose.LabelProject:     project,
		compose.LabelService:     service,
		compose.LabelConfigFiles: "/srv/" + project + "/compose.yml",
	}
}

func TestSetContainersRebuildsProjects(t *testing.T) {
	s := New()
	s.SetContainers([]docker.Container{
		{ID: "1", Name: "blog-web-1", State: docker.StateRunning, Labels: labelled("blog", "web")},
		{ID: "2", Name: "loose", State: docker.StateRunning},
	})

	if len(s.Projects) != 1 || s.Projects[0].Name != "blog" {
		t.Fatalf("projects: %+v", s.Projects)
	}
	total, running, stopped := s.Counts()
	if total != 2 || running != 2 || stopped != 0 {
		t.Errorf("counts: got %d/%d/%d", total, running, stopped)
	}
}

func TestSetVolumesKeepsGhostProjectDiscoverable(t *testing.T) {
	s := New()
	s.SetContainers(nil)
	s.SetVolumes([]docker.Volume{
		{Name: "blog_pgdata", Labels: map[string]string{compose.LabelProject: "blog"}},
	})

	if len(s.Projects) != 1 || !s.Projects[0].Ghost {
		t.Fatalf("a stopped stack must stay visible through its volumes: %+v", s.Projects)
	}
}

func TestSetDiskMergesVolumeSizes(t *testing.T) {
	s := New()
	s.SetVolumes([]docker.Volume{{Name: "pgdata", Size: -1, RefCount: -1}})
	s.SetDisk(docker.DiskUsage{
		At:      time.Now(),
		Volumes: []docker.Volume{{Name: "pgdata", Size: 4096, RefCount: 2}},
	})

	if s.Volumes[0].Size != 4096 || s.Volumes[0].RefCount != 2 {
		t.Errorf("df results must be merged back: %+v", s.Volumes[0])
	}
	if _, ok := s.DiskAge(); !ok {
		t.Error("disk age is known once df has run")
	}
}

func TestStatsLifecycle(t *testing.T) {
	s := New()
	s.SetContainers([]docker.Container{{ID: "1", Name: "web", State: docker.StateRunning}})

	for i := 0; i < historyLen+10; i++ {
		s.ApplyStats(docker.Stats{ContainerID: "1", CPUValid: true, CPUPercent: float64(i), MemUsage: 100})
	}
	if got := len(s.CPUHistory["1"]); got != historyLen {
		t.Errorf("history is capped: got %d, want %d", got, historyLen)
	}

	// An invalid first sample must not enter the CPU history.
	s.DropStats("1")
	s.ApplyStats(docker.Stats{ContainerID: "1", CPUValid: false, MemUsage: 50})
	if len(s.CPUHistory["1"]) != 0 {
		t.Error("a sample with no usable previous frame is not plotted")
	}
	if len(s.MemHistory["1"]) != 1 {
		t.Error("memory is always usable, even on the first sample")
	}

	// A container that disappears takes its samples with it.
	s.SetContainers(nil)
	if len(s.Stats) != 0 || len(s.MemHistory) != 0 {
		t.Errorf("stats of removed containers are dropped: %v %v", s.Stats, s.MemHistory)
	}
}

func TestRebuildProjectsPreservesDriftAcrossRefresh(t *testing.T) {
	s := New()
	s.SetContainers([]docker.Container{
		{ID: "1", Name: "blog-web-1", State: docker.StateRunning,
			Labels: withHash(labelled("blog", "web"), "aaa")},
	})
	s.ApplyDrift("blog", map[string]string{"web": "bbb"}, nil)

	if got := s.Projects[0].Services[0].Drift; got != compose.DriftChanged {
		t.Fatalf("drift: got %v, want DriftChanged", got)
	}

	// A plain list refresh must not reset the marker to unknown.
	s.SetContainers([]docker.Container{
		{ID: "1", Name: "blog-web-1", State: docker.StateRunning,
			Labels: withHash(labelled("blog", "web"), "aaa")},
	})
	if got := s.Projects[0].Services[0].Drift; got != compose.DriftChanged {
		t.Errorf("drift after refresh: got %v, want DriftChanged", got)
	}

	// Once the container is recreated with the new hash, the drift clears.
	s.SetContainers([]docker.Container{
		{ID: "2", Name: "blog-web-1", State: docker.StateRunning,
			Labels: withHash(labelled("blog", "web"), "bbb")},
	})
	if got := s.Projects[0].Services[0].Drift; got != compose.DriftNone {
		t.Errorf("drift after recreate: got %v, want DriftNone", got)
	}
}

func withHash(labels map[string]string, hash string) map[string]string {
	labels[compose.LabelConfigHash] = hash
	return labels
}

func TestApplyDriftOnUnknownProjectIsHarmless(t *testing.T) {
	s := New()
	s.ApplyDrift("missing", nil, errors.New("boom"))
}

func TestFilterRequiresEveryTerm(t *testing.T) {
	cs := []docker.Container{
		{ID: "1", Name: "blog-web-1", Image: "nginx:alpine", State: docker.StateRunning, Labels: labelled("blog", "web")},
		{ID: "2", Name: "blog-db-1", Image: "postgres:16", State: docker.StateExited, Labels: labelled("blog", "db")},
		{ID: "3", Name: "shop-web-1", Image: "nginx:alpine", State: docker.StateRunning, Labels: labelled("shop", "web")},
	}

	if got := Filter(cs, "", ContainerFields); len(got) != 3 {
		t.Errorf("an empty query filters nothing: got %d", len(got))
	}
	if got := Filter(cs, "NGINX", ContainerFields); len(got) != 2 {
		t.Errorf("matching is case-insensitive: got %d", len(got))
	}
	if got := Filter(cs, "blog running", ContainerFields); len(got) != 1 || got[0].Name != "blog-web-1" {
		t.Errorf("every term must match: got %v", got)
	}
	if got := Filter(cs, "nothing-here", ContainerFields); len(got) != 0 {
		t.Errorf("no match means no rows: got %d", len(got))
	}
}

func TestSortByIsStableAndReversible(t *testing.T) {
	s := New()
	cs := []docker.Container{
		{ID: "1", Name: "b", State: docker.StateRunning},
		{ID: "2", Name: "a", State: docker.StateRunning},
		{ID: "3", Name: "c", State: docker.StateExited},
	}
	s.ApplyStats(docker.Stats{ContainerID: "1", CPUValid: true, CPUPercent: 50})
	s.ApplyStats(docker.Stats{ContainerID: "2", CPUValid: true, CPUPercent: 10})

	byName, ok := s.ContainerLess("name")
	if !ok {
		t.Fatal("name is a sortable column")
	}
	SortBy(cs, byName, true)
	if cs[0].Name != "a" || cs[2].Name != "c" {
		t.Errorf("ascending by name: %v", cs)
	}
	SortBy(cs, byName, false)
	if cs[0].Name != "c" || cs[2].Name != "a" {
		t.Errorf("descending by name: %v", cs)
	}

	byCPU, _ := s.ContainerLess("cpu")
	SortBy(cs, byCPU, false)
	if cs[0].ID != "1" {
		t.Errorf("highest cpu first: %v", cs)
	}

	if _, ok := s.ContainerLess("nonexistent"); ok {
		t.Error("an unknown column is reported as unknown")
	}
}

func TestSnapshotForPruneCarriesApiSemantics(t *testing.T) {
	s := New()
	s.VolumePruneAll = true
	s.SetContainers([]docker.Container{{ID: "1", Name: "x", State: docker.StateExited}})
	s.SetDisk(docker.DiskUsage{BuildCache: []docker.BuildCacheRecord{{ID: "b1", Size: 10}}})

	snap := s.SnapshotForPrune(24 * time.Hour)
	if !snap.VolumePruneAllSupported {
		t.Error("the negotiated api version must reach the preview")
	}
	if snap.Until != 24*time.Hour {
		t.Error("the until filter must reach the preview")
	}
	if len(snap.BuildCache) != 1 {
		t.Error("build cache comes from the cached df result")
	}
}

func TestRunningContainers(t *testing.T) {
	s := New()
	s.SetContainers([]docker.Container{
		{ID: "1", Name: "a", State: docker.StateRunning},
		{ID: "2", Name: "b", State: docker.StatePaused},
		{ID: "3", Name: "c", State: docker.StateExited},
	})
	// A paused container has no deltas to stream, so it is not "running".
	if got := s.RunningContainers(); len(got) != 1 || got[0].ID != "1" {
		t.Errorf("running containers: %v", got)
	}
}

func TestLookups(t *testing.T) {
	s := New()
	s.SetContainers([]docker.Container{
		{ID: "abc", Name: "blog-web-1", State: docker.StateRunning, Labels: labelled("blog", "web")},
	})

	if _, ok := s.ContainerByID("abc"); !ok {
		t.Error("existing container must be found")
	}
	if _, ok := s.ContainerByID("nope"); ok {
		t.Error("missing container must not be found")
	}
	if _, ok := s.ProjectByName("blog"); !ok {
		t.Error("existing project must be found")
	}
	if _, ok := s.ProjectByName("nope"); ok {
		t.Error("missing project must not be found")
	}
}
