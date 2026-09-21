package editor

import (
	"os/exec"
	"strings"
	"testing"
)

// only builds a lookup where just the named commands exist.
func only(names ...string) LookPath {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestAvailableOffersTerminalEditorsFirst(t *testing.T) {
	// A terminal program should suggest an editor that opens in the same
	// window before one that opens a separate one.
	got := Available(only("code", "nano", "nvim"))
	if len(got) != 3 {
		t.Fatalf("got %d editors", len(got))
	}
	if got[0].Command != "nvim" || got[1].Command != "nano" || got[2].Command != "code" {
		t.Errorf("order: %v", []string{got[0].Command, got[1].Command, got[2].Command})
	}
}

func TestAvailableFindsNothingOnABareMachine(t *testing.T) {
	if got := Available(only()); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestWindowedEditorsAreWaitedOn(t *testing.T) {
	// Without the flag, the editor returns at once and hublot would carry on
	// as though the file had been written.
	for _, editor := range Available(only("code", "codium", "subl", "zed", "kate")) {
		if len(editor.Args) == 0 {
			t.Errorf("%s opens a window and is not waited on", editor.Command)
		}
	}
}

func TestFromEnvironment(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	// VISUAL wins: it is the one meant for a full screen editor.
	got, ok := FromEnvironment(env(map[string]string{"VISUAL": "nvim", "EDITOR": "nano"}), only("nvim", "nano"))
	if !ok || got.Command != "nvim" {
		t.Errorf("VISUAL should win: %+v", got)
	}
	if !strings.Contains(got.Detail, "VISUAL") {
		t.Errorf("the source should be shown: %q", got.Detail)
	}

	got, ok = FromEnvironment(env(map[string]string{"EDITOR": "nano -w"}), only("nano"))
	if !ok || got.Command != "nano" || len(got.Args) != 1 {
		t.Errorf("arguments in EDITOR must survive: %+v", got)
	}

	// A variable naming something absent is not an answer.
	if _, ok := FromEnvironment(env(map[string]string{"EDITOR": "ed"}), only("nano")); ok {
		t.Error("an editor that is not installed must not be used")
	}
	if _, ok := FromEnvironment(env(nil), only("nano")); ok {
		t.Error("nothing set means nothing chosen")
	}
}

func TestParseRefusesWhatIsNotThere(t *testing.T) {
	if _, err := Parse("nvim", only("nvim")); err != nil {
		t.Errorf("nvim: %v", err)
	}
	if _, err := Parse("code --wait", only("code")); err != nil {
		t.Errorf("code --wait: %v", err)
	}
	// Caught here rather than by handing the terminal to something that does
	// not exist.
	if _, err := Parse("vsocde", only("code")); err == nil {
		t.Error("a typo must be refused")
	}
	if _, err := Parse("  ", only()); err == nil {
		t.Error("an empty command is not an editor")
	}
}

func TestCmdPassesThePathAsAnArgument(t *testing.T) {
	// Never through a shell: a path can come from a daemon label.
	editor := Editor{Command: "nvim"}
	cmd := editor.Cmd("/tmp/a file with spaces/compose.yml")

	if len(cmd.Args) != 2 || cmd.Args[1] != "/tmp/a file with spaces/compose.yml" {
		t.Errorf("args: %v", cmd.Args)
	}

	windowed := Editor{Command: "code", Args: []string{"--wait"}}
	if got := windowed.Cmd("/tmp/x.yml").Args; len(got) != 3 || got[1] != "--wait" {
		t.Errorf("args: %v", got)
	}
}
