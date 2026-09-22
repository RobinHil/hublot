package state

import (
	"strings"
	"testing"

	"github.com/RobinHil/hublot/internal/docker"
)

// sample is a container with something in every field the form shows, so a
// test can change one thing and prove the others were left alone.
func sample() docker.ContainerSpec {
	return docker.ContainerSpec{
		ID:            "0123456789abcdef",
		Name:          "blog",
		Image:         "nginx:alpine",
		Command:       []string{"nginx", "-g", "daemon off;"},
		Env:           []string{"TZ=Europe/Paris"},
		Ports:         []string{"8080:80"},
		Mounts:        []string{"/srv/blog:/usr/share/nginx/html:ro"},
		RestartPolicy: "unless-stopped",
		NanoCPUs:      1_500_000_000,
		Memory:        512 << 20,
		Running:       true,
	}
}

func TestEditPrefillRoundTrips(t *testing.T) {
	spec := sample()
	edit, err := EditFromForm(spec, EditPrefill(spec))
	if err != nil {
		t.Fatalf("the form as it was filled must be readable: %v", err)
	}
	if !edit.Empty() {
		t.Errorf("submitting an untouched form must ask for nothing: %+v", edit)
	}
}

func TestEditFromFormOnlyCarriesWhatChanged(t *testing.T) {
	spec := sample()

	cases := []struct {
		name      string
		field     string
		value     string
		recreate  bool
		inPlace   bool
		check     func(docker.Edit) bool
		wantError string
	}{
		{
			name:  "renaming is done on the container itself",
			field: "name", value: "blog-www", inPlace: true,
			check: func(e docker.Edit) bool { return e.Name != nil && *e.Name == "blog-www" },
		},
		{
			name:  "a restart policy is updated in place",
			field: "restart", value: "always", inPlace: true,
			check: func(e docker.Edit) bool { return e.RestartPolicy != nil && *e.RestartPolicy == "always" },
		},
		{
			name:  "raising a cpu limit is updated in place",
			field: "cpus", value: "2", inPlace: true,
			check: func(e docker.Edit) bool { return e.NanoCPUs != nil && *e.NanoCPUs == 2_000_000_000 },
		},
		{
			// The update endpoint reads zero as "leave this alone", so the
			// only way to actually drop a limit is a new container.
			name:  "clearing a cpu limit needs a new container",
			field: "cpus", value: "0", inPlace: true, recreate: true,
			check: func(e docker.Edit) bool { return e.NanoCPUs != nil && *e.NanoCPUs == 0 },
		},
		{
			name:  "clearing a memory limit needs a new container",
			field: "memory", value: "0", inPlace: true, recreate: true,
			check: func(e docker.Edit) bool { return e.Memory != nil && *e.Memory == 0 },
		},
		{
			name:  "an emptied limit field means no limit",
			field: "memory", value: "", inPlace: true, recreate: true,
			check: func(e docker.Edit) bool { return e.Memory != nil && *e.Memory == 0 },
		},
		{
			name:  "a memory limit is updated in place",
			field: "memory", value: "1g", inPlace: true,
			check: func(e docker.Edit) bool { return e.Memory != nil && *e.Memory == 1<<30 },
		},
		{
			name:  "another image means another container",
			field: "image", value: "nginx:1.27", recreate: true,
			check: func(e docker.Edit) bool { return e.Image != nil && *e.Image == "nginx:1.27" },
		},
		{
			name:  "a command is split the way a shell would",
			field: "command", value: `sh -c "sleep 5"`, recreate: true,
			check: func(e docker.Edit) bool {
				return e.Command != nil && len(*e.Command) == 3 && (*e.Command)[2] == "sleep 5"
			},
		},
		{
			name:  "environment variables are replaced wholesale",
			field: "env", value: "TZ=UTC, DEBUG=1", recreate: true,
			check: func(e docker.Edit) bool {
				return e.Env != nil && len(*e.Env) == 2 && (*e.Env)[1] == "DEBUG=1"
			},
		},
		{
			name:  "ports are a new port map",
			field: "ports", value: "9090:80, 9443:443/tcp", recreate: true,
			check: func(e docker.Edit) bool { return e.Ports != nil && len(*e.Ports) == 2 },
		},
		{
			name:  "binds are a new set of mounts",
			field: "mounts", value: "/srv/new:/usr/share/nginx/html:ro", recreate: true,
			check: func(e docker.Edit) bool { return e.Mounts != nil && len(*e.Mounts) == 1 },
		},
		{
			name:  "a name cannot be emptied",
			field: "name", value: "", wantError: "without a name",
		},
		{
			name:  "an image cannot be emptied",
			field: "image", value: "", wantError: "needs an image",
		},
		{
			name:  "a bare word is not an environment variable",
			field: "env", value: "TZ", wantError: "KEY=value",
		},
		{
			name:  "a mount needs somewhere to land",
			field: "mounts", value: "/srv/blog", wantError: "source:/path/inside",
		},
		{
			name:  "a size has to be one",
			field: "memory", value: "plenty", wantError: "not a size",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := EditPrefill(spec)
			values[tc.field] = tc.value

			edit, err := EditFromForm(spec, values)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("want an error naming %q, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.check(edit) {
				t.Fatalf("the change was not read back: %+v", edit)
			}
			if edit.NeedsRecreate() != tc.recreate {
				t.Errorf("NeedsRecreate = %v, want %v", edit.NeedsRecreate(), tc.recreate)
			}
			if tc.inPlace && !edit.InPlace() {
				t.Errorf("this change applies to the container as it stands")
			}
			if only := countSet(edit); only != 1 {
				t.Errorf("%d fields carried, want the one that changed", only)
			}
		})
	}
}

// countSet counts the fields an edit actually carries, which is how a test
// proves that touching one field leaves the other eight out.
func countSet(e docker.Edit) int {
	n := 0
	for _, set := range []bool{
		e.Name != nil, e.Image != nil, e.Command != nil, e.Env != nil,
		e.Ports != nil, e.Mounts != nil, e.RestartPolicy != nil,
		e.NanoCPUs != nil, e.Memory != nil,
	} {
		if set {
			n++
		}
	}
	return n
}

func TestEditFromFormIgnoresSpacingAndKeepsEnvWithSpacesSafe(t *testing.T) {
	spec := sample()
	spec.Env = []string{"MOTD=hello world"}

	// A value with a space in it cannot be written in this syntax, which is
	// exactly why an untouched field is never applied: the container keeps the
	// variable it has instead of losing half of it.
	values := EditPrefill(spec)
	values["cpus"] = " 1.5 "

	edit, err := EditFromForm(spec, values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !edit.Empty() {
		t.Errorf("nothing was typed over, so nothing is sent: %+v", edit)
	}
}

func TestEditFromFormRefusesAnEnvItCannotExpress(t *testing.T) {
	spec := sample()

	values := EditPrefill(spec)
	values["env"] = "MOTD=hello world"

	if _, err := EditFromForm(spec, values); err == nil {
		t.Error("a value that cannot be split back must be refused, not silently cut")
	}
}

func TestJoinCommandSurvivesSplitCommand(t *testing.T) {
	cases := [][]string{
		{"nginx", "-g", "daemon off;"},
		{"sh", "-c", "while true; do echo working; sleep 5; done"},
		{"echo", `a "quoted" word`},
		nil,
	}

	for _, args := range cases {
		back, err := SplitCommand(JoinCommand(args))
		if err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if len(back) != len(args) {
			t.Fatalf("%q came back as %q", args, back)
		}
		for i := range args {
			// A double quote inside an argument is written back as a single
			// one, which is the one thing this syntax cannot carry through.
			want := strings.ReplaceAll(args[i], `"`, `'`)
			if back[i] != want {
				t.Errorf("%q came back as %q", args[i], back[i])
			}
		}
	}
}

func TestFormatLimits(t *testing.T) {
	if got := FormatCPUs(1_500_000_000); got != "1.5" {
		t.Errorf("cpus: got %q", got)
	}
	if got := FormatCPUs(0); got != "0" {
		t.Errorf("no limit reads as 0, got %q", got)
	}
	if got := FormatLimit(512 << 20); got != "512MB" {
		t.Errorf("memory: got %q", got)
	}

	// What the field shows has to parse back to the same figure, or an edit
	// nobody made would change the limit.
	for _, bytes := range []int64{512 << 20, 1 << 30, 3 << 30} {
		back, err := ParseBytes(FormatLimit(bytes))
		if err != nil || back != bytes {
			t.Errorf("%d rendered as %q came back as %d (%v)", bytes, FormatLimit(bytes), back, err)
		}
	}
}

func TestDescribeEditNamesBothSides(t *testing.T) {
	spec := sample()
	values := EditPrefill(spec)
	values["image"] = "nginx:1.27"

	edit, err := EditFromForm(spec, values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := DescribeEdit(spec, edit)
	if len(lines) != 1 {
		t.Fatalf("one change, one line: %q", lines)
	}
	if !strings.Contains(lines[0], "nginx:alpine") || !strings.Contains(lines[0], "nginx:1.27") {
		t.Errorf("a confirmation says what it replaces: %q", lines[0])
	}
}
