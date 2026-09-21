package compose

import (
	"errors"
	"os"
	"testing"

	"github.com/RobinHil/hublot/internal/docker"
)

// ctr builds a container carrying Compose labels.
func ctr(name, project, service, number, state string, extra map[string]string) docker.Container {
	labels := map[string]string{
		LabelProject:     project,
		LabelService:     service,
		LabelNumber:      number,
		LabelWorkingDir:  "/srv/" + project,
		LabelConfigFiles: "/srv/" + project + "/compose.yml",
	}
	for k, v := range extra {
		labels[k] = v
	}
	return docker.Container{ID: name + "id", Name: name, State: state, Labels: labels}
}

// allFilesExist is the Stat injection used when the YAML is supposed to be there.
func allFilesExist(string) error { return nil }

// noFileExists simulates a project whose YAML was deleted.
func noFileExists(string) error { return os.ErrNotExist }

func TestBuildProjectsGroupsServicesAndReplicas(t *testing.T) {
	inv := Inventory{
		Containers: []docker.Container{
			ctr("blog-web-2", "blog", "web", "2", docker.StateRunning, nil),
			ctr("blog-web-1", "blog", "web", "1", docker.StateRunning, nil),
			ctr("blog-db-1", "blog", "db", "1", docker.StateExited, nil),
			ctr("shop-api-1", "shop", "api", "1", docker.StateRunning, nil),
			{ID: "loose", Name: "loose", State: docker.StateRunning},
		},
		Stat: allFilesExist,
	}

	projects := BuildProjects(inv)
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
	if projects[0].Name != "blog" || projects[1].Name != "shop" {
		t.Fatalf("projects must be sorted by name: %v, %v", projects[0].Name, projects[1].Name)
	}

	blog := projects[0]
	if len(blog.Services) != 2 {
		t.Fatalf("got %d services, want 2", len(blog.Services))
	}
	if blog.Services[0].Name != "db" || blog.Services[1].Name != "web" {
		t.Errorf("services must be sorted by name: %+v", blog.Services)
	}

	web := blog.Services[1]
	if got := web.Replicas(); got != 2 {
		t.Errorf("web replicas: got %d, want 2", got)
	}
	// Replicas are ordered by container-number, not by insertion order.
	if web.Containers[0].Name != "blog-web-1" {
		t.Errorf("replicas must be ordered by container-number: %s first", web.Containers[0].Name)
	}
	if got := web.Running(); got != 2 {
		t.Errorf("web running: got %d, want 2", got)
	}
	if got := blog.Services[0].State(); got != "0/1 running" {
		t.Errorf("db state: got %q", got)
	}
	if blog.WorkingDir != "/srv/blog" {
		t.Errorf("working dir: got %q", blog.WorkingDir)
	}
	if len(blog.ConfigFiles) != 1 || blog.ConfigFiles[0] != "/srv/blog/compose.yml" {
		t.Errorf("config files: got %v", blog.ConfigFiles)
	}
	if got := blog.Total(); got != 3 {
		t.Errorf("total containers: got %d, want 3", got)
	}
	if got := blog.Running(); got != 2 {
		t.Errorf("running containers: got %d, want 2", got)
	}
}

func TestBuildProjectsStoppedServiceIsMarkedStopped(t *testing.T) {
	inv := Inventory{
		Containers: []docker.Container{ctr("blog-db-1", "blog", "db", "1", docker.StateExited, nil)},
		Stat:       allFilesExist,
	}
	svc := BuildProjects(inv)[0].Services[0]
	if svc.Drift != DriftStopped {
		t.Errorf("a service with nothing running is DriftStopped, got %v", svc.Drift)
	}
	if got := svc.Drift.Marker(); got != "o" {
		t.Errorf("marker: got %q", got)
	}
}

func TestBuildProjectsSeparatesOneOffContainers(t *testing.T) {
	inv := Inventory{
		Containers: []docker.Container{
			ctr("blog-web-1", "blog", "web", "1", docker.StateRunning, nil),
			ctr("blog-migrate-run-abc", "blog", "migrate", "1", docker.StateExited,
				map[string]string{LabelOneOff: "True"}),
			// Some Compose versions write it lowercase.
			ctr("blog-seed-run-def", "blog", "seed", "1", docker.StateExited,
				map[string]string{LabelOneOff: "true"}),
		},
		Stat: allFilesExist,
	}

	p := BuildProjects(inv)[0]
	if len(p.Services) != 1 {
		t.Fatalf("one-off containers do not create services: got %d", len(p.Services))
	}
	if len(p.OneOff) != 2 {
		t.Fatalf("got %d one-off containers, want 2", len(p.OneOff))
	}
}

func TestBuildProjectsContainerWithoutServiceLabelIsOneOff(t *testing.T) {
	c := ctr("weird", "blog", "", "1", docker.StateRunning, nil)
	delete(c.Labels, LabelService)

	p := BuildProjects(Inventory{Containers: []docker.Container{c}, Stat: allFilesExist})[0]
	if len(p.Services) != 0 || len(p.OneOff) != 1 {
		t.Fatalf("unaddressable container belongs in OneOff: services=%d oneoff=%d", len(p.Services), len(p.OneOff))
	}
}

func TestBuildProjectsGhostProjectFromVolumesOnly(t *testing.T) {
	inv := Inventory{
		Volumes: []docker.Volume{
			{Name: "blog_pgdata", Labels: map[string]string{LabelProject: "blog"}, Size: 4096},
		},
		Networks: []docker.Network{
			{Name: "blog_default", Labels: map[string]string{LabelProject: "blog"}},
		},
		Stat: allFilesExist,
	}

	projects := BuildProjects(inv)
	if len(projects) != 1 {
		t.Fatalf("a stack is discoverable through its volumes: got %d projects", len(projects))
	}
	p := projects[0]
	if !p.Ghost {
		t.Error("a project with no containers but surviving volumes is a ghost")
	}
	// No container means no config_files label, so nothing can be resolved.
	if !p.Orphaned {
		t.Error("a ghost with no known config files is orphaned")
	}
	if len(p.Volumes) != 1 || len(p.Networks) != 1 {
		t.Errorf("volumes/networks must be attached: %d/%d", len(p.Volumes), len(p.Networks))
	}
}

func TestBuildProjectsOrphanedWhenConfigFileIsGone(t *testing.T) {
	inv := Inventory{
		Containers: []docker.Container{ctr("blog-web-1", "blog", "web", "1", docker.StateRunning, nil)},
		Stat:       noFileExists,
	}
	p := BuildProjects(inv)[0]
	if !p.Orphaned {
		t.Error("a project whose YAML is gone is orphaned")
	}
	if p.Actionable() {
		t.Error("an orphaned project is not actionable through the CLI")
	}
	if p.Ghost {
		t.Error("a project with containers is not a ghost")
	}
}

func TestBuildProjectsOneMissingFileAmongManyIsEnough(t *testing.T) {
	c := ctr("blog-web-1", "blog", "web", "1", docker.StateRunning, nil)
	c.Labels[LabelConfigFiles] = "/srv/blog/compose.yml,/srv/blog/compose.override.yml"

	missingOverride := func(path string) error {
		if path == "/srv/blog/compose.override.yml" {
			return errors.New("stat: no such file")
		}
		return nil
	}

	p := BuildProjects(Inventory{Containers: []docker.Container{c}, Stat: missingOverride})[0]
	if len(p.ConfigFiles) != 2 {
		t.Fatalf("both config files must be parsed: %v", p.ConfigFiles)
	}
	if !p.Orphaned {
		t.Error("one missing override file makes the project orphaned")
	}
}

func TestConfigHashTakenFromRunningContainers(t *testing.T) {
	inv := Inventory{
		Containers: []docker.Container{
			ctr("blog-web-1", "blog", "web", "1", docker.StateRunning,
				map[string]string{LabelConfigHash: "abc123"}),
		},
		Stat: allFilesExist,
	}
	if got := BuildProjects(inv)[0].Services[0].ConfigHash; got != "abc123" {
		t.Errorf("config hash: got %q, want abc123", got)
	}
}

func TestParseConfigFiles(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"/a/compose.yml", []string{"/a/compose.yml"}},
		{"/a/compose.yml,/a/override.yml", []string{"/a/compose.yml", "/a/override.yml"}},
		{" /a/compose.yml , /a/override.yml ", []string{"/a/compose.yml", "/a/override.yml"}},
		{",,", nil},
	}
	for _, tt := range cases {
		got := ParseConfigFiles(tt.in)
		if len(got) != len(tt.want) {
			t.Fatalf("ParseConfigFiles(%q): got %v, want %v", tt.in, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ParseConfigFiles(%q)[%d]: got %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParseDependsOn(t *testing.T) {
	got := parseDependsOn("db:service_started:true,cache:service_healthy:false")
	if len(got) != 2 || got[0] != "db" || got[1] != "cache" {
		t.Errorf("parseDependsOn: got %v", got)
	}
	if parseDependsOn("") != nil {
		t.Error("an empty label yields no dependencies")
	}
}

func TestDriftMarkers(t *testing.T) {
	cases := map[Drift]string{
		DriftNone:    "ok",
		DriftChanged: "->",
		DriftStopped: "o",
		DriftUnknown: "x",
	}
	for d, want := range cases {
		if got := d.Marker(); got != want {
			t.Errorf("marker for %v: got %q, want %q", d, got, want)
		}
	}
}
