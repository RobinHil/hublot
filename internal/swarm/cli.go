package swarm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/RobinHil/hublot/internal/compose"
)

// detectTimeout bounds the startup probe, like the Compose one.
const detectTimeout = 5 * time.Second

// CLI is the docker binary, for the three things the Engine API cannot do on
// its own: deploying a stack from a compose file, and creating or re-imaging
// a service with the registry credentials only the CLI knows how to find.
type CLI struct {
	Binary    string
	Available bool
	// Reason says why it is unavailable, shown instead of failing at press
	// time.
	Reason string
}

// Detect looks for a docker CLI that knows `stack`. It never returns an error:
// an absent binary is a state the UI displays.
func Detect(ctx context.Context) CLI {
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	path, err := exec.LookPath("docker")
	if err != nil {
		return CLI{Reason: "no docker CLI in PATH: stacks cannot be deployed, and services are created and re-imaged by it"}
	}
	// `stack --help` answers without a daemon, so the probe works even when
	// the socket refuses this user.
	if err := exec.CommandContext(ctx, path, "stack", "--help").Run(); err != nil {
		return CLI{Reason: "this docker CLI has no stack command: " + err.Error()}
	}
	return CLI{Binary: "docker", Available: true}
}

// DeployArgs deploys or updates a stack. The credentials go with it so every
// node can pull a private image, and the deploy returns at once: progress is
// what the Swarm view is for.
func DeployArgs(stack string, files []string, prune bool) []string {
	args := []string{"stack", "deploy", "--detach=true", "--with-registry-auth"}
	if prune {
		args = append(args, "--prune")
	}
	for _, f := range files {
		args = append(args, "-c", f)
	}
	return append(args, stack)
}

// RemoveArgs removes a stack: its services, networks, secrets and configs.
// Volumes are never touched by it, and they live on each node anyway.
func RemoveArgs(stack string) []string { return []string{"stack", "rm", stack} }

// ConfigArgs merges and resolves stack files the way a deploy would read them.
func ConfigArgs(files []string) []string {
	args := []string{"stack", "config"}
	for _, f := range files {
		args = append(args, "-c", f)
	}
	return args
}

// UpdateImageArgs points a service at another image. Through the CLI because
// it resolves the tag to a digest, so every node runs the same build, and
// sends the credentials a private registry needs.
func UpdateImageArgs(service, image string) []string {
	return []string{"service", "update", "--detach", "--with-registry-auth", "--image", image, service}
}

// Command builds the exec.Cmd. The argument slice is passed as-is, never
// through a shell: stack names and paths are external input.
func (c CLI) Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Dir = dir
	return cmd
}

// Display renders an invocation the way a user would type it.
func (c CLI) Display(args ...string) string {
	return orDocker(c.Binary) + " " + strings.Join(args, " ")
}

func orDocker(binary string) string {
	if binary == "" {
		return "docker"
	}
	return binary
}

// Run executes a command, streaming its output.
func (c CLI) Run(ctx context.Context, dir string, args ...string) (<-chan compose.OutputLine, <-chan compose.Result) {
	if !c.Available {
		return compose.Refused(errors.New(c.Reason))
	}
	verb := strings.Join(args[:min(2, len(args))], " ")
	return compose.Stream(ctx, c.Command(ctx, dir, args...), verb, c.Display(args...))
}

// Output runs a command to completion and returns what it printed.
func (c CLI) Output(ctx context.Context, dir string, args ...string) (string, string, error) {
	if !c.Available {
		return "", "", errors.New(c.Reason)
	}
	cmd := c.Command(ctx, dir, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), stderr.String(), fmt.Errorf("%s: %s", c.Display(args...), msg)
	}
	return stdout.String(), stderr.String(), nil
}

// Preflight resolves the stack files before a deploy. `docker stack deploy`
// substitutes variables from the environment of whoever runs it and, unlike
// compose, reads no .env file; a variable nobody set comes out blank, and the
// CLI says nothing about it. A stack first deployed from a shell holding
// `export DB_PASSWORD=...` comes back with an empty password from here. The
// files are read for the variables they use, and the ones this environment
// lacks are returned for the user to accept or not: the same trap as
// AGENTS.md section 8.3, with less help from the tool.
func (c CLI) Preflight(ctx context.Context, dir string, files []string) ([]string, error) {
	if _, _, err := c.Output(ctx, dir, ConfigArgs(files)...); err != nil {
		return nil, err
	}
	var missing []string
	seen := map[string]bool{}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}
		for _, name := range MissingVariables(string(body), os.LookupEnv) {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
		}
	}
	return missing, nil
}

// variableRef matches $NAME and ${NAME...}. The doubled $$ is an escaped
// dollar and is skipped by the caller.
var variableRef = regexp.MustCompile(`\$(\{([A-Za-z_][A-Za-z0-9_]*)([^}]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)

// MissingVariables lists the variables a file uses that the environment does
// not set and that carry no default, in the order they first appear.
// ${NAME:-default} and ${NAME-default} have one; ${NAME:?message} makes the
// deploy itself fail, which needs no warning from us.
func MissingVariables(body string, lookup func(string) (string, bool)) []string {
	body = strings.ReplaceAll(body, "$$", "")
	var out []string
	seen := map[string]bool{}
	for _, m := range variableRef.FindAllStringSubmatch(body, -1) {
		name, modifier := m[2], m[3]
		if name == "" {
			name = m[4]
		}
		if strings.HasPrefix(modifier, ":-") || strings.HasPrefix(modifier, "-") ||
			strings.HasPrefix(modifier, ":?") || strings.HasPrefix(modifier, "?") {
			continue
		}
		if _, set := lookup(name); set || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
