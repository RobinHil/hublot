package compose

import (
	"errors"
	"testing"

	"github.com/RobinHil/hublot/internal/docker"
)

func TestParseHashes(t *testing.T) {
	out := "web 8f1a2b3c\ndb 99aabbcc\n\nmalformed-line\n"
	got := ParseHashes(out)
	if len(got) != 2 {
		t.Fatalf("got %d hashes, want 2: %v", len(got), got)
	}
	if got["web"] != "8f1a2b3c" || got["db"] != "99aabbcc" {
		t.Errorf("parsed hashes: %v", got)
	}
}

func TestApplyDrift(t *testing.T) {
	running := docker.Container{State: docker.StateRunning}
	stopped := docker.Container{State: docker.StateExited}

	p := Project{Services: []Service{
		{Name: "same", ConfigHash: "aaa", Containers: []docker.Container{running}},
		{Name: "changed", ConfigHash: "aaa", Containers: []docker.Container{running}},
		{Name: "stopped", ConfigHash: "aaa", Containers: []docker.Container{stopped}},
		{Name: "unhashed", Containers: []docker.Container{running}},
		{Name: "absent-from-file", ConfigHash: "aaa", Containers: []docker.Container{running}},
	}}

	ApplyDrift(&p, map[string]string{"same": "aaa", "changed": "bbb", "stopped": "aaa", "unhashed": "ccc"}, nil)

	want := map[string]Drift{
		"same":             DriftNone,
		"changed":          DriftChanged,
		"stopped":          DriftStopped,
		"unhashed":         DriftUnknown,
		"absent-from-file": DriftUnknown,
	}
	for _, s := range p.Services {
		if s.Drift != want[s.Name] {
			t.Errorf("%s: got %v, want %v", s.Name, s.Drift, want[s.Name])
		}
	}
	if !p.Drifted() {
		t.Error("a project with one changed service has drifted")
	}
}

func TestApplyDriftDegradesWhenHashingFails(t *testing.T) {
	// An old Compose without `config --hash` must not produce a wrong marker.
	p := Project{Services: []Service{
		{Name: "web", ConfigHash: "aaa", Containers: []docker.Container{{State: docker.StateRunning}}},
		{Name: "db", ConfigHash: "bbb", Containers: []docker.Container{{State: docker.StateExited}}},
	}}

	ApplyDrift(&p, nil, errors.New("unknown flag: --hash"))

	if p.Services[0].Drift != DriftUnknown {
		t.Errorf("web: got %v, want DriftUnknown", p.Services[0].Drift)
	}
	if p.Services[1].Drift != DriftStopped {
		t.Errorf("db: a stopped service stays stopped, got %v", p.Services[1].Drift)
	}
	if p.Drifted() {
		t.Error("unknown drift is not drift")
	}
}

func TestCLIArgsAlwaysForcePlainOutput(t *testing.T) {
	cli := CLI{Binary: "docker", Prefix: []string{"compose"}, Available: true}
	p := Project{
		Name:        "blog",
		WorkingDir:  "/srv/blog",
		ConfigFiles: []string{"/srv/blog/compose.yml", "/srv/blog/override.yml"},
	}

	got := cli.Args(p, "up", "-d")
	want := []string{
		"compose", "--progress", "plain", "--ansi", "never",
		"--project-name", "blog",
		"-f", "/srv/blog/compose.yml",
		"-f", "/srv/blog/override.yml",
		"up", "-d",
	}
	if len(got) != len(want) {
		t.Fatalf("args: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d]: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestCLIArgsForV1Binary(t *testing.T) {
	cli := CLI{Binary: "docker-compose", Available: true}
	got := cli.Args(Project{Name: "blog"}, "down")
	if got[0] != "--progress" {
		t.Errorf("v1 has no compose sub-command: %v", got)
	}
}

func TestCommandRunsInProjectDirectory(t *testing.T) {
	cli := CLI{Binary: "docker", Prefix: []string{"compose"}, Available: true}
	cmd := cli.Command(t.Context(), Project{Name: "blog", WorkingDir: "/srv/blog"}, "ps")
	// cmd.Dir is what makes Compose read the project's .env file.
	if cmd.Dir != "/srv/blog" {
		t.Errorf("cmd.Dir: got %q, want /srv/blog", cmd.Dir)
	}
}

func TestUnavailableCLIRefusesWork(t *testing.T) {
	cli := CLI{Reason: "no docker compose plugin found"}
	if err := cli.Preflight(t.Context(), Project{Name: "blog"}); err == nil {
		t.Error("preflight must fail when no binary was detected")
	}
	if _, err := cli.Hashes(t.Context(), Project{Name: "blog"}); err == nil {
		t.Error("hashing must fail when no binary was detected")
	}

	lines, result := cli.Run(t.Context(), Project{Name: "blog"}, "up", "-d")
	for range lines {
		t.Error("an unavailable CLI produces no output")
	}
	if res := <-result; res.Err == nil {
		t.Error("an unavailable CLI reports an error result")
	}
}

func TestPreflightRefusesOrphanedProject(t *testing.T) {
	cli := CLI{Binary: "docker", Prefix: []string{"compose"}, Available: true}
	err := cli.Preflight(t.Context(), Project{Name: "blog", Orphaned: true})
	if err == nil {
		t.Fatal("an orphaned project cannot be validated")
	}
}
