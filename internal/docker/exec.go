package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	xterm "github.com/charmbracelet/x/term"
	"github.com/docker/docker/api/types/container"
)

// DefaultShellCommand picks the best shell available inside the container
// without probing it first: sh is guaranteed by any image that has a shell at
// all, and it hands over to bash when there is one.
//
// The test has to come before the exec. Written as `exec bash || exec sh`, a
// missing bash does not fall through: a failed exec is fatal to a
// non-interactive shell, so the session would die instantly on every image
// without bash, which is most of them.
var DefaultShellCommand = []string{
	"/bin/sh", "-c",
	"if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi",
}

// ExecSession is an interactive shell inside a container. It is driven by the
// UI through tea.Exec, which suspends the Bubble Tea renderer and restores the
// terminal afterwards, including on panic (AGENTS.md section 6.7).
type ExecSession struct {
	client      *Client
	containerID string
	cmd         []string
	user        string

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// ExitCode is filled once the session ends.
	ExitCode int
}

// NewExec builds an interactive session. An empty cmd means the default shell.
func (c *Client) NewExec(containerID string, cmd []string, user string) *ExecSession {
	if len(cmd) == 0 {
		cmd = DefaultShellCommand
	}
	return &ExecSession{client: c, containerID: containerID, cmd: cmd, user: user}
}

// SetStdin satisfies tea.ExecCommand.
func (s *ExecSession) SetStdin(r io.Reader) { s.stdin = r }

// SetStdout satisfies tea.ExecCommand.
func (s *ExecSession) SetStdout(w io.Writer) { s.stdout = w }

// SetStderr satisfies tea.ExecCommand.
func (s *ExecSession) SetStderr(w io.Writer) { s.stderr = w }

// Run wires the hijacked connection to the terminal and blocks until the shell
// exits. It always restores the terminal mode, including when the copy loops
// panic.
func (s *ExecSession) Run() (err error) {
	ctx := context.Background()

	if s.stdin == nil {
		s.stdin = os.Stdin
	}
	if s.stdout == nil {
		s.stdout = os.Stdout
	}
	if s.stderr == nil {
		s.stderr = os.Stderr
	}

	// Never let a panic escape with the terminal left in raw mode.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("exec session panicked: %v", r)
		}
	}()

	width, height := terminalSize(s.stdout)

	created, err := s.client.api.ContainerExecCreate(ctx, s.containerID, container.ExecOptions{
		User:         s.user,
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          s.cmd,
		ConsoleSize:  &[2]uint{uint(height), uint(width)},
	})
	if err != nil {
		return fmt.Errorf("creating exec in %s: %w", ShortID(s.containerID), err)
	}

	attached, err := s.client.api.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return fmt.Errorf("attaching to exec in %s: %w", ShortID(s.containerID), err)
	}
	defer attached.Close()

	restore, err := makeRaw(s.stdin)
	if err != nil {
		return err
	}
	defer restore()

	stopResize := s.watchResize(ctx, created.ID)
	defer stopResize()

	// Output drives the session: when the shell exits, the daemon closes the
	// connection and this returns. The stdin copy is left to die with the
	// process rather than blocked on, since a read from the terminal cannot be
	// interrupted.
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(s.stdout, attached.Reader)
		done <- copyErr
	}()
	go func() {
		defer attached.CloseWrite() //nolint:errcheck // best effort EOF to the shell
		_, _ = io.Copy(attached.Conn, s.stdin)
	}()

	if copyErr := <-done; copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return fmt.Errorf("exec session in %s: %w", ShortID(s.containerID), copyErr)
	}

	if insp, err := s.client.api.ContainerExecInspect(ctx, created.ID); err == nil {
		s.ExitCode = insp.ExitCode
	}
	return nil
}

// watchResize forwards SIGWINCH to the daemon so the remote pty follows the
// local one.
func (s *ExecSession) watchResize(ctx context.Context, execID string) func() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-sig:
				w, h := terminalSize(s.stdout)
				//nolint:errcheck // a failed resize is cosmetic
				s.client.api.ContainerExecResize(ctx, execID, container.ResizeOptions{
					Width:  uint(w),
					Height: uint(h),
				})
			case <-stop:
				return
			}
		}
	}()

	return func() {
		signal.Stop(sig)
		close(stop)
	}
}

// makeRaw puts the terminal in raw mode when stdin is one, and returns the
// restore func. Non-terminal stdin (tests, pipes) is left alone.
func makeRaw(stdin io.Reader) (func(), error) {
	f, ok := stdin.(*os.File)
	if !ok {
		return func() {}, nil
	}
	fd := f.Fd()
	if !xterm.IsTerminal(fd) {
		return func() {}, nil
	}
	state, err := xterm.MakeRaw(fd)
	if err != nil {
		return func() {}, fmt.Errorf("switching terminal to raw mode: %w", err)
	}
	return func() { _ = xterm.Restore(fd, state) }, nil
}

// terminalSize reads the current size, falling back to a sane default when the
// writer is not a terminal.
func terminalSize(w io.Writer) (width, height int) {
	if f, ok := w.(*os.File); ok {
		if cols, rows, err := xterm.GetSize(f.Fd()); err == nil && cols > 0 {
			return cols, rows
		}
	}
	return 80, 24
}
