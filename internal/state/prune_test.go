package state

import (
	"testing"
	"time"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
)

// A bug here destroys user data, so every filter combination is covered
// (AGENTS.md section 10.2).

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func composeLabels(project, service string) map[string]string {
	return map[string]string{compose.LabelProject: project, compose.LabelService: service}
}

func names(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.Name)
	}
	return out
}

func assertNames(t *testing.T, got []Item, want ...string) {
	t.Helper()
	g := names(got)
	if len(g) != len(want) {
		t.Fatalf("items: got %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("items: got %v, want %v", g, want)
		}
	}
}

func TestPruneContainers(t *testing.T) {
	snap := Snapshot{
		Now: testNow,
		Containers: []docker.Container{
			{ID: "1", Name: "running", State: docker.StateRunning, SizeRW: 100},
			{ID: "2", Name: "exited", State: docker.StateExited, SizeRW: 200, Status: "Exited (0) 3 days ago"},
			{ID: "3", Name: "created", State: docker.StateCreated, SizeRW: 0},
			{ID: "4", Name: "paused", State: docker.StatePaused, SizeRW: 400},
			{ID: "5", Name: "restarting", State: docker.StateRestarting},
			{ID: "6", Name: "dead", State: docker.StateDead},
		},
	}

	p := BuildPreview(CatContainers, snap)
	// Only exited and created go. `dead` is not pruned by the daemon.
	assertNames(t, p.Items, "created", "exited")
	if p.Reclaimable != 200 {
		t.Errorf("reclaimable: got %d, want 200", p.Reclaimable)
	}
}

func TestPruneContainersUntilFilter(t *testing.T) {
	snap := Snapshot{
		Now:   testNow,
		Until: 24 * time.Hour,
		Containers: []docker.Container{
			{ID: "1", Name: "old", State: docker.StateExited, Created: testNow.Add(-48 * time.Hour)},
			{ID: "2", Name: "recent", State: docker.StateExited, Created: testNow.Add(-2 * time.Hour)},
			{ID: "3", Name: "exactly-at-cutoff", State: docker.StateExited, Created: testNow.Add(-24 * time.Hour)},
		},
	}

	// The cutoff is exclusive: an object created exactly at it is kept.
	assertNames(t, BuildPreview(CatContainers, snap).Items, "old")
}

func TestPruneContainersComposeOwnedComeFirst(t *testing.T) {
	snap := Snapshot{
		Now: testNow,
		Containers: []docker.Container{
			{ID: "1", Name: "zzz-orphan", State: docker.StateExited},
			{ID: "2", Name: "aaa-orphan", State: docker.StateExited},
			{ID: "3", Name: "shop-api-1", State: docker.StateExited, Labels: composeLabels("shop", "api")},
			{ID: "4", Name: "blog-web-1", State: docker.StateExited, Labels: composeLabels("blog", "web")},
		},
	}

	p := BuildPreview(CatContainers, snap)
	assertNames(t, p.Items, "blog-web-1", "shop-api-1", "aaa-orphan", "zzz-orphan")
	if p.ComposeOwned != 2 {
		t.Errorf("compose owned: got %d, want 2", p.ComposeOwned)
	}
}

func TestPruneImagesDanglingOnly(t *testing.T) {
	snap := Snapshot{
		Now: testNow,
		Images: []docker.Image{
			{ID: "sha256:tagged", RepoTags: []string{"nginx:alpine"}, Size: 1000},
			{ID: "sha256:dangling", RepoTags: []string{"<none>:<none>"}, Size: 2000},
			{ID: "sha256:notags", Size: 3000},
		},
	}

	p := BuildPreview(CatImages, snap)
	assertNames(t, p.Items, "dangling", "notags")
	if p.Reclaimable != 5000 {
		t.Errorf("reclaimable: got %d, want 5000", p.Reclaimable)
	}
}

func TestPruneImagesAllTakesUnreferencedTaggedImages(t *testing.T) {
	snap := Snapshot{
		Now: testNow,
		Containers: []docker.Container{
			{ID: "c1", Name: "web", State: docker.StateRunning, ImageID: "sha256:used", Image: "nginx:alpine"},
			// A stopped container still pins its image.
			{ID: "c2", Name: "old", State: docker.StateExited, ImageID: "sha256:pinned", Image: "redis:7"},
		},
		Images: []docker.Image{
			{ID: "sha256:used", RepoTags: []string{"nginx:alpine"}, Size: 1000},
			{ID: "sha256:pinned", RepoTags: []string{"redis:7"}, Size: 2000},
			{ID: "sha256:unused", RepoTags: []string{"postgres:16"}, Size: 3000},
			{ID: "sha256:dangling", Size: 4000},
		},
	}

	assertNames(t, BuildPreview(CatImages, snap).Items, "dangling")
	assertNames(t, BuildPreview(CatImagesAll, snap).Items, "dangling", "postgres:16")
}

func TestPruneImagesSkipsImageReferencedOnlyByTag(t *testing.T) {
	// A container created from a tag that the daemon reports under a different
	// ID must still protect the image.
	snap := Snapshot{
		Now: testNow,
		Containers: []docker.Container{
			{ID: "c1", Name: "web", State: docker.StateRunning, Image: "nginx:alpine", ImageID: ""},
		},
		Images: []docker.Image{
			{ID: "sha256:abc", RepoTags: []string{"nginx:alpine"}, Size: 1000},
		},
	}
	if p := BuildPreview(CatImagesAll, snap); !p.Empty() {
		t.Errorf("an image pinned by tag is never pruned: %v", names(p.Items))
	}
}

func TestPruneImagesUntilFilter(t *testing.T) {
	snap := Snapshot{
		Now:   testNow,
		Until: 7 * 24 * time.Hour,
		Images: []docker.Image{
			{ID: "sha256:old", Created: testNow.Add(-30 * 24 * time.Hour), Size: 1},
			{ID: "sha256:new", Created: testNow.Add(-1 * time.Hour), Size: 2},
		},
	}
	assertNames(t, BuildPreview(CatImages, snap).Items, "old")
}

func TestPruneVolumesModernDefaultKeepsNamedVolumes(t *testing.T) {
	anon := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	snap := Snapshot{
		Now:                     testNow,
		VolumePruneAllSupported: true,
		Containers: []docker.Container{
			{ID: "c1", Name: "web", State: docker.StateRunning,
				Mounts: []docker.Mount{{Name: "in-use", Type: "volume"}}},
		},
		Volumes: []docker.Volume{
			{Name: "in-use", Size: 1000},
			{Name: "pgdata", Size: 2000, Labels: composeLabels("blog", "db")},
			{Name: anon, Anonymous: true, Size: 3000},
		},
	}

	// From API 1.42 the default prune only takes anonymous volumes.
	p := BuildPreview(CatVolumes, snap)
	assertNames(t, p.Items, anon)
	if p.ComposeOwned != 0 {
		t.Errorf("compose owned: got %d, want 0", p.ComposeOwned)
	}

	// With the all filter, the Compose-owned named volume goes too, and is
	// listed first as a warning.
	all := BuildPreview(CatVolumesAll, snap)
	assertNames(t, all.Items, "pgdata", anon)
	if all.ComposeOwned != 1 {
		t.Errorf("compose owned: got %d, want 1", all.ComposeOwned)
	}
	if all.Items[0].Project != "blog" {
		t.Errorf("project of first item: got %q, want blog", all.Items[0].Project)
	}
	if all.Reclaimable != 5000 {
		t.Errorf("reclaimable: got %d, want 5000", all.Reclaimable)
	}
}

func TestPruneVolumesOldDaemonTakesNamedVolumesByDefault(t *testing.T) {
	anon := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	snap := Snapshot{
		Now:                     testNow,
		VolumePruneAllSupported: false,
		Volumes: []docker.Volume{
			{Name: "pgdata", Size: 2000},
			{Name: anon, Anonymous: true, Size: 3000},
		},
	}
	// Before 1.42 there is no `all` filter and the plain prune removes
	// everything unused, named volumes included.
	assertNames(t, BuildPreview(CatVolumes, snap).Items, anon, "pgdata")
}

func TestPruneVolumesRefCountProtects(t *testing.T) {
	snap := Snapshot{
		Now:                     testNow,
		VolumePruneAllSupported: true,
		Volumes: []docker.Volume{
			// df filled RefCount even though no container in the snapshot
			// mounts it: the daemon knows better than our container list.
			{Name: "referenced", RefCount: 1, Size: 1000},
			{Name: "free", RefCount: 0, Anonymous: true, Size: 2000},
		},
	}
	assertNames(t, BuildPreview(CatVolumesAll, snap).Items, "free")
}

func TestPruneVolumesUnknownSizeIsCountedSeparately(t *testing.T) {
	// Sizes are only known after `system df`; a plain list reports -1.
	snap := Snapshot{
		Now:                     testNow,
		VolumePruneAllSupported: true,
		Volumes: []docker.Volume{
			{Name: "unsized", Size: -1, RefCount: -1},
			{Name: "sized", Size: 500, RefCount: -1},
		},
	}
	p := BuildPreview(CatVolumesAll, snap)
	if p.Reclaimable != 500 {
		t.Errorf("reclaimable: got %d, want 500", p.Reclaimable)
	}
	if p.Unknown != 1 {
		t.Errorf("unknown: got %d, want 1", p.Unknown)
	}
}

func TestPruneNetworks(t *testing.T) {
	snap := Snapshot{
		Now: testNow,
		Containers: []docker.Container{
			{ID: "c1", Name: "web", State: docker.StateRunning, Networks: []string{"attached-by-name"}},
		},
		Networks: []docker.Network{
			{ID: "n1", Name: "bridge", Driver: "bridge"},
			{ID: "n2", Name: "host", Driver: "host"},
			{ID: "n3", Name: "none", Driver: "null"},
			{ID: "n4", Name: "empty", Driver: "bridge"},
			{ID: "n5", Name: "attached-by-name", Driver: "bridge"},
			{ID: "n6", Name: "attached-by-inspect", Driver: "bridge",
				Containers: map[string]string{"c9": "other"}},
			{ID: "n7", Name: "blog_default", Driver: "bridge", Labels: composeLabels("blog", "")},
		},
	}

	p := BuildPreview(CatNetworks, snap)
	assertNames(t, p.Items, "blog_default", "empty")
	if p.Reclaimable != 0 {
		t.Errorf("networks reclaim no space, got %d", p.Reclaimable)
	}
}

func TestPruneBuildCache(t *testing.T) {
	snap := Snapshot{
		Now: testNow,
		BuildCache: []docker.BuildCacheRecord{
			{ID: "b1", Description: "used-layer", InUse: true, Size: 1000, Type: "regular"},
			{ID: "b2", Description: "free-layer", Size: 2000, Type: "regular"},
			{ID: "b3", Size: 500, Type: "source.local"},
		},
	}

	assertNames(t, BuildPreview(CatBuildCache, snap).Items, "b3", "free-layer")
	assertNames(t, BuildPreview(CatBuildCacheAll, snap).Items, "b3", "free-layer", "used-layer")
	if got := BuildPreview(CatBuildCacheAll, snap).Reclaimable; got != 3500 {
		t.Errorf("reclaimable: got %d, want 3500", got)
	}
}

func TestGlobalPreviewMirrorsSystemPrune(t *testing.T) {
	anon := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	snap := Snapshot{
		Now:                     testNow,
		VolumePruneAllSupported: true,
		Containers: []docker.Container{
			{ID: "c1", Name: "stopped", State: docker.StateExited, SizeRW: 10},
		},
		Images:     []docker.Image{{ID: "sha256:dangling", Size: 100}},
		Volumes:    []docker.Volume{{Name: "pgdata", Size: 1000, Labels: composeLabels("blog", "db")}, {Name: anon, Anonymous: true, Size: 5}},
		Networks:   []docker.Network{{ID: "n1", Name: "empty"}},
		BuildCache: []docker.BuildCacheRecord{{ID: "b1", Size: 7}},
	}

	// Without volumes: four categories, no volume data touched.
	plain := GlobalPreview(snap, false, false)
	if len(plain) != 4 {
		t.Fatalf("got %d categories, want 4", len(plain))
	}
	for _, p := range plain {
		if p.Category == CatVolumes || p.Category == CatVolumesAll {
			t.Error("volumes are only included when explicitly asked for")
		}
	}

	// With volumes and -a: the named Compose volume is destroyed, which is the
	// data loss case the preview exists to surface.
	full := GlobalPreview(snap, true, true)
	if len(full) != 5 {
		t.Fatalf("got %d categories, want 5", len(full))
	}
	vols := full[4]
	if vols.Category != CatVolumesAll {
		t.Fatalf("last category: got %v", vols.Category)
	}
	assertNames(t, vols.Items, "pgdata", anon)
	if vols.ComposeOwned != 1 {
		t.Errorf("compose owned: got %d, want 1", vols.ComposeOwned)
	}
}

func TestPreviewSummary(t *testing.T) {
	empty := Preview{Category: CatContainers}
	if got := empty.Summary(); got != "nothing to remove" {
		t.Errorf("empty summary: got %q", got)
	}

	p := Preview{
		Category:     CatVolumesAll,
		Items:        []Item{{Name: "a"}, {Name: "b"}},
		Reclaimable:  2048,
		Unknown:      1,
		ComposeOwned: 1,
	}
	want := "2 unused volumes (named included), 2KB reclaimed (+1 of unknown size), 1 belonging to compose projects"
	if got := p.Summary(); got != want {
		t.Errorf("summary:\ngot  %q\nwant %q", got, want)
	}
}

func TestCategoryDestructive(t *testing.T) {
	for _, c := range []Category{CatVolumes, CatVolumesAll} {
		if !c.Destructive() {
			t.Errorf("%v holds data and is destructive", c)
		}
	}
	for _, c := range []Category{CatContainers, CatImages, CatImagesAll, CatNetworks, CatBuildCache} {
		if c.Destructive() {
			t.Errorf("%v does not destroy data", c)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		-1:            "-",
		0:             "0B",
		999:           "999B",
		1024:          "1KB",
		1536:          "1.5KB",
		1048576:       "1MB",
		1073741824:    "1GB",
		1099511627776: "1TB",
	}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d): got %q, want %q", in, got, want)
		}
	}
}

func TestAgeReadsAsEnglish(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{time.Minute, "1 minute ago"},
		{5 * time.Minute, "5 minutes ago"},
		{time.Hour, "1 hour ago"},
		{3 * time.Hour, "3 hours ago"},
		{24 * time.Hour, "1 day ago"},
		{72 * time.Hour, "3 days ago"},
	}
	for _, tt := range cases {
		if got := age(testNow, testNow.Add(-tt.d)); got != tt.want {
			t.Errorf("age(%s): got %q, want %q", tt.d, got, tt.want)
		}
	}
	if got := age(testNow, time.Time{}); got != "unknown age" {
		t.Errorf("a zero timestamp: got %q", got)
	}
}
