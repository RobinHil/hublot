package state

import (
	"strings"
	"testing"
)

// The messages here are real ones, copied from what the daemon and compose
// actually print. Each test names the mistake a person made.
func TestDiagnosePortAlreadyTaken(t *testing.T) {
	output := ` Container ntm-web-1 Starting
Error response from daemon: failed to set up container networking: driver failed programming external connectivity on endpoint ntm-web-1 (609ed76633a4): Bind for 0.0.0.0:8080 failed: port is already allocated`

	d, ok := Diagnose(output)
	if !ok {
		t.Fatal("a port clash is the most common failure of all and must be recognised")
	}
	if d.Port != "8080" {
		t.Errorf("port: got %q, want 8080", d.Port)
	}
	if !strings.Contains(d.Title, "8080") {
		t.Errorf("the title should name the port: %q", d.Title)
	}
	if !strings.Contains(strings.Join(d.Lines, " "), "another host port") {
		t.Errorf("the explanation must say what to do:\n%s", strings.Join(d.Lines, "\n"))
	}
}

func TestDiagnoseAddressAlreadyInUse(t *testing.T) {
	// The other wording, from the kernel rather than from the daemon.
	output := "Error starting userland proxy: listen tcp4 0.0.0.0:5432: bind: address already in use"

	d, ok := Diagnose(output)
	if !ok || d.Port != "5432" {
		t.Errorf("got %+v", d)
	}
}

func TestDiagnoseNameTaken(t *testing.T) {
	output := `Error response from daemon: Conflict. The container name "/blog-web-1" is already in use by container "3f2a1b". You have to remove (or rename) that container to be able to reuse that name.`

	d, ok := Diagnose(output)
	if !ok {
		t.Fatal("a name clash must be recognised")
	}
	if d.Name != "blog-web-1" {
		t.Errorf("name: got %q", d.Name)
	}
	if !strings.Contains(strings.Join(d.Lines, " "), "D removes it") {
		t.Errorf("the explanation should say how to clear it:\n%s", strings.Join(d.Lines, "\n"))
	}
}

func TestDiagnoseMissingImage(t *testing.T) {
	cases := []string{
		"Error response from daemon: manifest for nginx:alpin not found: manifest unknown",
		"Error response from daemon: pull access denied for privatething, repository does not exist or may require 'docker login'",
	}
	for _, output := range cases {
		d, ok := Diagnose(output)
		if !ok {
			t.Errorf("not recognised: %s", output)
			continue
		}
		if !strings.Contains(strings.Join(d.Lines, " "), "spelling") {
			t.Errorf("the explanation should mention the obvious cause:\n%s", strings.Join(d.Lines, "\n"))
		}
	}
}

func TestDiagnoseMissingVariable(t *testing.T) {
	output := `validating blog: error while interpolating services.web.environment.SECRET: required variable DB_PASSWORD is missing a value`

	d, ok := Diagnose(output)
	if !ok {
		t.Fatal("an unresolved variable must be recognised")
	}
	if !strings.Contains(d.Title, "DB_PASSWORD") {
		t.Errorf("the title should name the variable: %q", d.Title)
	}
	if !strings.Contains(strings.Join(d.Lines, " "), ".env") {
		t.Errorf("the explanation should point at the fix:\n%s", strings.Join(d.Lines, "\n"))
	}
}

func TestDiagnoseLowPortAndFullDisk(t *testing.T) {
	if d, ok := Diagnose("Error: permission denied while trying to bind port 80"); !ok ||
		!strings.Contains(d.Title, "1024") {
		t.Errorf("a privileged port: %+v", d)
	}
	if d, ok := Diagnose("write /var/lib/docker/tmp/x: no space left on device"); !ok ||
		!strings.Contains(strings.Join(d.Lines, " "), "disk view") {
		t.Errorf("a full disk: %+v", d)
	}
}

func TestDiagnoseSaysNothingWhenItKnowsNothing(t *testing.T) {
	// Inventing an explanation would be worse than the daemon's own words.
	for _, output := range []string{
		"Error response from daemon: something nobody has seen before",
		"",
		"Container blog-web-1 Started",
	} {
		if d, ok := Diagnose(output); ok {
			t.Errorf("Diagnose(%q) invented %q", output, d.Title)
		}
	}
}

func TestPortHolderNamesTheContainer(t *testing.T) {
	containers := []Container{
		{Name: "blog-web-1", Ports: []Port{{Public: 8080}}},
		{Name: "traefik", Ports: []Port{{Public: 8082}}},
		{Name: "quiet", Ports: nil},
	}

	if got := PortHolder(containers, "8080"); got != "blog-web-1" {
		t.Errorf("got %q, want blog-web-1", got)
	}
	if got := PortHolder(containers, "9999"); got != "" {
		t.Errorf("a port nothing holds: got %q", got)
	}
}

// Captured verbatim from `docker run -d -p 8080:80` against a host port a
// container already held. The daemon words this differently from the compose
// path, and the port has to survive the endpoint id sitting in front of it.
func TestDiagnoseRealPortAllocatedMessage(t *testing.T) {
	output := "docker: Error response from daemon: failed to set up container networking: " +
		"driver failed programming external connectivity on endpoint porttest " +
		"(055db764b8606dc738d4f650e64c64d9b215c6c6491e18d1bef76630effa2e95): " +
		"Bind for 0.0.0.0:8080 failed: port is already allocated"

	d, ok := Diagnose(output)
	if !ok {
		t.Fatal("this is the commonest failure there is, it has to be recognised")
	}
	if d.Port != "8080" {
		t.Errorf("port = %q, want 8080", d.Port)
	}
}

// All four captured from `docker compose config --quiet` on files broken the
// way people break them.
func TestDiagnoseComposeConfigErrors(t *testing.T) {
	cases := []struct {
		name, output, wantTitle, wantFile string
	}{
		{
			name:      "a line indented differently from its neighbours",
			output:    "go-yaml load error in parser (while parsing a block mapping) at L2.C3-L4.C4: did not find expected key",
			wantTitle: "The compose file is not valid YAML",
		},
		{
			name:      "a misspelled key",
			output:    "validating /tmp/stack/compose.yml: services.web additional properties 'portz' not allowed",
			wantTitle: "The compose file does not describe a valid stack",
			wantFile:  "/tmp/stack/compose.yml",
		},
		{
			name:      "a value of the wrong shape",
			output:    "validating /tmp/stack/compose.yml: services.web.ports must be a array",
			wantTitle: "The compose file does not describe a valid stack",
			wantFile:  "/tmp/stack/compose.yml",
		},
		{
			name:      "a service with nothing to run",
			output:    `service "web" has neither an image nor a build context specified: invalid compose project`,
			wantTitle: "The compose file does not describe a valid stack",
		},
		{
			name:      "the top level itself wrong",
			output:    "services must be a mapping",
			wantTitle: "The compose file does not describe a valid stack",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := Diagnose(tc.output)
			if !ok {
				t.Fatal("a file that will not parse has to be explained")
			}
			if d.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", d.Title, tc.wantTitle)
			}
			if d.File != tc.wantFile {
				t.Errorf("file = %q, want %q", d.File, tc.wantFile)
			}
		})
	}
}

// A missing variable reads as a config problem too, and its own explanation is
// far more useful than "the file is wrong".
func TestDiagnoseKeepsTheVariableCaseAheadOfTheConfigOne(t *testing.T) {
	d, ok := Diagnose(`validating /tmp/stack/compose.yml: required variable DB_PASSWORD is missing a value`)
	if !ok {
		t.Fatal("expected a diagnosis")
	}
	if !strings.Contains(d.Title, "DB_PASSWORD") {
		t.Errorf("title = %q, want the variable named", d.Title)
	}
}

// Captured from `docker start` on a container that was paused. Start is the
// obvious key to reach for and the wrong one, so the answer has to name the
// right one.
func TestDiagnosePausedContainer(t *testing.T) {
	d, ok := Diagnose("Error response from daemon: cannot start a paused container, try unpause instead")
	if !ok {
		t.Fatal("expected a diagnosis")
	}
	if !strings.Contains(d.Title, "paused") {
		t.Errorf("title = %q, want it to say the container is paused", d.Title)
	}
	if !strings.Contains(strings.Join(d.Lines, " "), "P ") {
		t.Error("the key that unpauses has to be named")
	}
}
