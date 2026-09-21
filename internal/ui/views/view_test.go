package views

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RobinHil/hublot/internal/docker"
)

func TestFormatStatusFitsAColumn(t *testing.T) {
	// The daemon writes for someone reading one line; a table needs the same
	// facts in a third of the room.
	cases := []struct {
		state, status, want string
	}{
		{docker.StateRunning, "Up About an hour", "up 1h"},
		{docker.StateRunning, "Up 3 minutes", "up 3m"},
		{docker.StateRunning, "Up 2 days", "up 2d"},
		{docker.StateRunning, "Up 5 seconds", "up 5s"},
		{docker.StateRunning, "Up 10 minutes (healthy)", "up 10m ok"},
		{docker.StateExited, "Exited (0) 12 minutes ago", "exited (0) 12m"},
		{docker.StateExited, "Exited (137) About an hour ago", "exited (137) 1h"},
		{docker.StatePaused, "Up 4 hours (Paused)", "paused 4h"},
		{docker.StateCreated, "Created", "created"},
		{docker.StateRestarting, "Restarting (1) 5 seconds ago", "restarting (1) 5s"},
	}

	for _, tt := range cases {
		got := formatStatus(docker.Container{State: tt.state, Status: tt.status})
		if got != tt.want {
			t.Errorf("formatStatus(%q) = %q, want %q", tt.status, got, tt.want)
		}
		if len(got) > 18 {
			t.Errorf("formatStatus(%q) is %d characters, too long for the column", tt.status, len(got))
		}
	}
}

func TestFormatStatusFallsBackToTheState(t *testing.T) {
	// A daemon that reports nothing still has to render as something.
	if got := formatStatus(docker.Container{State: docker.StateDead}); got != "dead" {
		t.Errorf("empty status: got %q, want dead", got)
	}
}

func TestSummaryOfDropsEmptyParts(t *testing.T) {
	if got := summaryOf("a", "", "b"); got != "a · b" {
		t.Errorf("summaryOf: got %q", got)
	}
	if got := summaryOf("", ""); got != "" {
		t.Errorf("summaryOf of nothing: got %q", got)
	}
}

func TestPluralReadsAsEnglish(t *testing.T) {
	if got := plural(1, "volume"); got != "1 volume" {
		t.Errorf("got %q", got)
	}
	if got := plural(3, "volume"); got != "3 volumes" {
		t.Errorf("got %q", got)
	}
}

func TestUndoNewStackRemovesOnlyWhatItCreated(t *testing.T) {
	// Quitting the editor without writing has to leave the machine exactly as
	// it was, and touch nothing that was already there.
	parent := t.TempDir()

	dir := filepath.Join(parent, "fresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if cmd := undoNewStack(file, dir, true, true); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("undo reported a problem: %v", msg)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("a directory hublot created and then emptied must go")
	}
}

func TestUndoNewStackLeavesWhatWasAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The file was already there, so it is not ours to remove.
	undoNewStack(file, dir, false, false)
	if _, err := os.Stat(file); err != nil {
		t.Errorf("an existing file must survive: %v", err)
	}

	// A directory that already existed stays, even when the file inside it
	// was ours.
	neighbour := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(neighbour, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	undoNewStack(file, dir, true, false)
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("a directory we did not create must survive: %v", err)
	}
	if _, err := os.Stat(neighbour); err != nil {
		t.Errorf("its contents must survive: %v", err)
	}
}
