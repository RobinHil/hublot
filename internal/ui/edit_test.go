package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Whether an editing session did anything is decided by the file, not by how
// the editor exited: :q must change nothing, :wq must be noticed.
func TestFingerprintTellsAWriteFromAQuit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compose.yml")
	body := "services:\n  web:\n    image: nginx:alpine\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	before := fingerprint(path)

	// Opened and closed with nothing written.
	if after := fingerprint(path); after != before {
		t.Error("a file nobody wrote to must look the same")
	}

	// :wq on a template nobody edited writes the same bytes back, and whoever
	// did that has decided to keep the file. Counting it as nothing would
	// throw away exactly the stack they just asked for.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(path); after == before {
		t.Error("a write is a write, even when the bytes come back the same")
	}

	// And an edit, which changes the contents as well.
	if err := os.WriteFile(path, []byte(body+"    ports: [\"8080:80\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(path); after == before {
		t.Error("an edited file must not look the same")
	}
}

func TestFingerprintIsStableWhenNothingHappens(t *testing.T) {
	// The other half: opening an editor and closing it without writing must
	// read as nothing at all, however long it took.
	path := filepath.Join(t.TempDir(), "compose.yml")
	if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := fingerprint(path)
	time.Sleep(20 * time.Millisecond)

	if after := fingerprint(path); after != before {
		t.Errorf("a file nobody touched changed on its own:\n%s\n%s", before, after)
	}
}

func TestFingerprintOfAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gone.yml")

	absent := fingerprint(path)
	if absent == "" {
		t.Fatal("a missing file still needs an answer")
	}

	if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fingerprint(path) == absent {
		t.Error("creating a file is a change")
	}

	// And removing it again brings it back to where it started, which is what
	// cancelling a new stack does.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if fingerprint(path) != absent {
		t.Error("removing the file must return to the absent fingerprint")
	}
}
