package compose

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// detectTimeout bounds the startup probe: a hung docker CLI must not delay the
// whole UI coming up.
const detectTimeout = 5 * time.Second

// CLI is the detected Compose entry point. Compose is a CLI plugin, not an API,
// so every mutation goes through it (AGENTS.md section 8.2).
type CLI struct {
	// Binary is "docker" for the v2 plugin, "docker-compose" for v1.
	Binary string
	// Prefix is the sub-command turning Binary into Compose: {"compose"} for v2,
	// empty for v1.
	Prefix []string
	// Version is what the binary reported, for the status bar.
	Version string
	// Available is false when no Compose binary was found. Actions are then
	// disabled with Reason shown, rather than failing when a key is pressed
	// (AGENTS.md section 8.2).
	Available bool
	Reason    string
}

// Detect looks for Compose v2, then v1. It never returns an error: an absent
// binary is a state the UI displays, not a failure.
func Detect(ctx context.Context) CLI {
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	if out, err := exec.CommandContext(ctx, "docker", "compose", "version", "--short").Output(); err == nil {
		return CLI{
			Binary:    "docker",
			Prefix:    []string{"compose"},
			Version:   strings.TrimSpace(string(out)),
			Available: true,
		}
	}

	if out, err := exec.CommandContext(ctx, "docker-compose", "version", "--short").Output(); err == nil {
		return CLI{
			Binary:    "docker-compose",
			Version:   strings.TrimSpace(string(out)),
			Available: true,
		}
	}

	return CLI{Reason: "no docker compose plugin and no docker-compose binary found in PATH"}
}

// Args builds the full argument list for a project action. Compose's default
// output repositions the cursor, which is unreadable once captured, so every
// invocation is forced to plain output (AGENTS.md section 8.2).
func (c CLI) Args(p Project, action ...string) []string {
	args := append([]string{}, c.Prefix...)
	args = append(args, "--progress", "plain", "--ansi", "never")
	args = append(args, "--project-name", p.Name)
	for _, f := range p.ConfigFiles {
		args = append(args, "-f", f)
	}
	return append(args, action...)
}

// Command builds the exec.Cmd for an action. The argument slice is passed
// as-is: project names and paths come from daemon labels, which are external
// input and never go through a shell.
func (c CLI) Command(ctx context.Context, p Project, action ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Binary, c.Args(p, action...)...)
	// Setting the directory is what makes Compose pick up the project's .env
	// file. Variables exported in the shell that first ran `up` are invisible
	// to us (AGENTS.md section 8.3).
	cmd.Dir = p.WorkingDir
	return cmd
}

// DisplayCommand renders an invocation the way a user would type it, for the
// confirmation modal and the task panel header.
func (c CLI) DisplayCommand(p Project, action ...string) string {
	return c.Binary + " " + strings.Join(c.Args(p, action...), " ")
}
