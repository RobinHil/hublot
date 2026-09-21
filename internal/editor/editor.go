// Package editor finds the editor to hand a file to, and builds the command
// that runs it. It knows nothing about the UI: the choosing is done above, the
// running is done by the Bubble Tea layer that can suspend the terminal.
package editor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Editor is one way of opening a file.
type Editor struct {
	// Command is the binary to run.
	Command string
	// Args go before the file name. Windowed editors need the flag that makes
	// them wait, or hublot would come back before anything had been typed.
	Args []string
	// Label is what the chooser shows.
	Label string
	// Detail says why this one might be wanted.
	Detail string
}

// Argv is the command line without the file, which is what gets remembered.
func (e Editor) Argv() []string { return append([]string{e.Command}, e.Args...) }

// String renders the editor the way it would be typed.
func (e Editor) String() string { return strings.Join(e.Argv(), " ") }

// known is the list probed for, in the order it is offered. Terminal editors
// come first: hublot is a terminal program, and an editor that opens in the
// same window is the one that fits.
var known = []Editor{
	{Command: "nvim", Label: "nvim", Detail: "neovim, in this terminal"},
	{Command: "vim", Label: "vim", Detail: "in this terminal"},
	{Command: "vi", Label: "vi", Detail: "always there, in this terminal"},
	{Command: "hx", Label: "helix", Detail: "in this terminal"},
	{Command: "micro", Label: "micro", Detail: "in this terminal"},
	{Command: "nano", Label: "nano", Detail: "in this terminal"},
	{Command: "emacs", Args: []string{"-nw"}, Label: "emacs", Detail: "in this terminal"},
	{Command: "code", Args: []string{"--wait"}, Label: "vs code", Detail: "a window, waited on"},
	{Command: "codium", Args: []string{"--wait"}, Label: "vscodium", Detail: "a window, waited on"},
	{Command: "subl", Args: []string{"--wait"}, Label: "sublime text", Detail: "a window, waited on"},
	{Command: "zed", Args: []string{"--wait"}, Label: "zed", Detail: "a window, waited on"},
	{Command: "gnome-text-editor", Label: "gnome text editor", Detail: "a window"},
	{Command: "kate", Args: []string{"--block"}, Label: "kate", Detail: "a window, waited on"},
}

// LookPath is the lookup used to decide what is installed, injected so the
// detection can be tested without depending on the machine running the tests.
type LookPath func(string) (string, error)

// Available lists the editors this machine actually has, in the order they are
// offered.
func Available(lookPath LookPath) []Editor {
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	var out []Editor
	for _, candidate := range known {
		if _, err := lookPath(candidate.Command); err == nil {
			out = append(out, candidate)
		}
	}
	return out
}

// FromEnvironment reads what the shell already says, which is the answer for
// anyone who has set it: VISUAL first, since it is the one meant for a full
// screen editor, then EDITOR.
func FromEnvironment(env func(string) string, lookPath LookPath) (Editor, bool) {
	if env == nil {
		env = os.Getenv
	}

	for _, name := range []string{"VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(env(name)); value != "" {
			if editor, err := Parse(value, lookPath); err == nil {
				editor.Detail = "from $" + name
				return editor, true
			}
		}
	}
	return Editor{}, false
}

// Parse reads a command line into an editor, checking that the binary exists
// so a typo in the configuration is caught before the screen is handed over.
func Parse(command string, lookPath LookPath) (Editor, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return Editor{}, fmt.Errorf("no editor given")
	}
	if _, err := lookPath(fields[0]); err != nil {
		return Editor{}, fmt.Errorf("%s is not on your PATH", fields[0])
	}

	return Editor{
		Command: fields[0],
		Args:    fields[1:],
		Label:   fields[0],
	}, nil
}

// Command builds the invocation for a file. The arguments are passed as a
// slice, never through a shell: a path here can come from a daemon label.
func (e Editor) Cmd(path string) *exec.Cmd {
	return exec.Command(e.Command, append(append([]string{}, e.Args...), path)...)
}
