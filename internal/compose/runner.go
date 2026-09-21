package compose

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// OutputLine is one line of a running command's output.
type OutputLine struct {
	Text   string
	Stderr bool
}

// Result ends a command run.
type Result struct {
	ExitCode int
	Err      error
}

// Run executes a Compose action, streaming its output line by line. The output
// channel closes when the command ends; the result channel then carries exactly
// one value.
func (c CLI) Run(ctx context.Context, p Project, action ...string) (<-chan OutputLine, <-chan Result) {
	lines := make(chan OutputLine, 128)
	result := make(chan Result, 1)

	if !c.Available {
		close(lines)
		result <- Result{ExitCode: -1, Err: errors.New(c.Reason)}
		close(result)
		return lines, result
	}

	cmd := c.Command(ctx, p, action...)

	go func() {
		defer close(lines)
		defer close(result)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			result <- Result{ExitCode: -1, Err: fmt.Errorf("capturing stdout: %w", err)}
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			result <- Result{ExitCode: -1, Err: fmt.Errorf("capturing stderr: %w", err)}
			return
		}

		if err := cmd.Start(); err != nil {
			result <- Result{ExitCode: -1, Err: fmt.Errorf("running %s: %w", c.DisplayCommand(p, action...), err)}
			return
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); pump(ctx, stdout, false, lines) }()
		go func() { defer wg.Done(); pump(ctx, stderr, true, lines) }()
		wg.Wait()

		err = cmd.Wait()
		res := Result{}
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr):
			res.ExitCode = exitErr.ExitCode()
			// Compose already printed the reason on stderr; the task panel
			// shows it, so the error here only needs to say what failed.
			res.Err = fmt.Errorf("%s exited with code %d", action[0], res.ExitCode)
		default:
			res.ExitCode = -1
			res.Err = fmt.Errorf("running %s: %w", c.DisplayCommand(p, action...), err)
		}
		result <- res
	}()

	return lines, result
}

func pump(ctx context.Context, r io.Reader, stderr bool, out chan<- OutputLine) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for sc.Scan() {
		select {
		case out <- OutputLine{Text: sc.Text(), Stderr: stderr}:
		case <-ctx.Done():
			return
		}
	}
}

// Preflight validates that the project's YAML still resolves. Compose records
// nothing about where its variables came from, so a stack first started from a
// shell holding exported secrets can fail or come back up differently when
// restarted from here. Run this before every mutating action and surface the
// error rather than papering over it (AGENTS.md section 8.3).
func (c CLI) Preflight(ctx context.Context, p Project) error {
	if !c.Available {
		return errors.New(c.Reason)
	}
	if !p.Actionable() {
		return fmt.Errorf("project %s has no readable compose file: only direct engine actions are available", p.Name)
	}

	out, err := c.Command(ctx, p, "config", "--quiet").CombinedOutput()
	if err == nil {
		return nil
	}
	msg := trimOutput(string(out))
	if msg == "" {
		return fmt.Errorf("validating %s: %w", p.Name, err)
	}
	return fmt.Errorf("validating %s: %s", p.Name, msg)
}

// Config returns the fully resolved YAML, for the `c` key in the Compose view.
func (c CLI) Config(ctx context.Context, p Project) (string, error) {
	if !c.Available {
		return "", errors.New(c.Reason)
	}
	out, err := c.Command(ctx, p, "config").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("resolving config for %s: %s", p.Name, trimOutput(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("resolving config for %s: %w", p.Name, err)
	}
	return string(out), nil
}

// trimOutput keeps command output readable inside a modal.
func trimOutput(s string) string {
	const max = 800
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
