package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// LogOptions controls a log stream.
type LogOptions struct {
	// Tail is the number of trailing lines to fetch first; "all" for everything.
	Tail string
	// Follow keeps the stream open for new output.
	Follow bool
	// Since limits output to lines newer than this instant.
	Since time.Time
	// Service labels every line, used by aggregated Compose logs.
	Service string
	// Names labels the lines of a swarm service by the task they came from,
	// keyed by task id. A task missing from it keeps Service.
	Names map[string]string
}

// DefaultLogOptions is what the logs view opens with.
func DefaultLogOptions() LogOptions { return LogOptions{Tail: "500", Follow: true} }

// StreamLogs follows a container's output. Without a TTY the stream is
// multiplexed with an 8-byte frame header, so it goes through stdcopy;
// reading it raw produces garbage every few lines (AGENTS.md section 6.4).
func (c *Client) StreamLogs(ctx context.Context, id string, opts LogOptions) (<-chan LogLine, <-chan error) {
	return follow(ctx, id, opts, func() (bool, error) { return c.HasTTY(ctx, id) },
		func(apiOpts container.LogsOptions) (io.ReadCloser, error) {
			rc, err := c.api.ContainerLogs(ctx, id, apiOpts)
			if err != nil {
				return nil, fmt.Errorf("streaming logs for %s: %w", ShortID(id), err)
			}
			return rc, nil
		})
}

// StreamServiceLogs follows every task of a service, wherever it runs: the
// managers fetch the output from each node. Lines carry the task they came
// from, which opts.Names turns into a name.
func (c *Client) StreamServiceLogs(ctx context.Context, id string, tty bool, opts LogOptions) (<-chan LogLine, <-chan error) {
	return follow(ctx, id, opts, func() (bool, error) { return tty, nil },
		func(apiOpts container.LogsOptions) (io.ReadCloser, error) {
			apiOpts.Details = true
			rc, err := c.api.ServiceLogs(ctx, id, apiOpts)
			if err != nil {
				return nil, fmt.Errorf("streaming logs for service %s: %w", ShortID(id), err)
			}
			return rc, nil
		})
}

// StreamTaskLogs follows one task, on whichever node it ran.
func (c *Client) StreamTaskLogs(ctx context.Context, id string, tty bool, opts LogOptions) (<-chan LogLine, <-chan error) {
	return follow(ctx, id, opts, func() (bool, error) { return tty, nil },
		func(apiOpts container.LogsOptions) (io.ReadCloser, error) {
			apiOpts.Details = true
			rc, err := c.api.TaskLogs(ctx, id, apiOpts)
			if err != nil {
				return nil, fmt.Errorf("streaming logs for task %s: %w", ShortID(id), err)
			}
			return rc, nil
		})
}

// follow is the stream every log source shares: decide whether the output is
// multiplexed, open it, and split it into lines.
func follow(ctx context.Context, id string, opts LogOptions, hasTTY func() (bool, error),
	open func(container.LogsOptions) (io.ReadCloser, error)) (<-chan LogLine, <-chan error) {
	out := make(chan LogLine, 256)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		tty, err := hasTTY()
		if err != nil {
			if ctx.Err() == nil {
				errs <- err
			}
			return
		}

		apiOpts := container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     opts.Follow,
			Timestamps: true,
			Tail:       orDefault(opts.Tail, "all"),
		}
		if !opts.Since.IsZero() {
			apiOpts.Since = opts.Since.Format(time.RFC3339Nano)
		}

		rc, err := open(apiOpts)
		if err != nil {
			if ctx.Err() == nil {
				errs <- err
			}
			return
		}
		defer func() { _ = rc.Close() }()

		if tty {
			// A TTY stream is already a single plain byte stream.
			scanLines(ctx, rc, id, opts, "stdout", out)
			return
		}

		stdoutR, stdoutW := io.Pipe()
		stderrR, stderrW := io.Pipe()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); scanLines(ctx, stdoutR, id, opts, "stdout", out) }()
		go func() { defer wg.Done(); scanLines(ctx, stderrR, id, opts, "stderr", out) }()

		_, copyErr := stdcopy.StdCopy(stdoutW, stderrW, rc)
		_ = stdoutW.Close()
		_ = stderrW.Close()
		wg.Wait()

		if copyErr != nil && ctx.Err() == nil && copyErr != io.EOF {
			errs <- fmt.Errorf("demultiplexing logs for %s: %w", ShortID(id), copyErr)
		}
	}()

	return out, errs
}

// scanLines splits a stream into lines and publishes them, parsing the RFC3339
// timestamp the daemon prefixes when Timestamps is set.
func scanLines(ctx context.Context, r io.Reader, id string, opts LogOptions, stream string, out chan<- LogLine) {
	sc := bufio.NewScanner(r)
	// Container lines can be long; the default 64KiB limit truncates JSON logs.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		ts, text := splitTimestamp(sc.Text())
		service := opts.Service
		if task, rest, ok := splitDetails(text); ok {
			text = rest
			if name := opts.Names[task]; name != "" {
				service = name
			}
		}
		line := LogLine{ContainerID: id, Service: service, Stream: stream, At: ts, Text: text}
		select {
		case out <- line:
		case <-ctx.Done():
			return
		}
	}
}

// splitTimestamp peels off the leading timestamp the daemon adds. Lines that do
// not carry one are returned untouched.
func splitTimestamp(line string) (time.Time, string) {
	head, rest, found := strings.Cut(line, " ")
	if !found {
		return time.Time{}, line
	}
	ts, err := time.Parse(time.RFC3339Nano, head)
	if err != nil {
		return time.Time{}, line
	}
	return ts, rest
}

// splitDetails peels off the attributes swarm puts in front of each line when
// details are asked for, "com.docker.swarm.node.id=...,...task.id=... ", and
// returns the task id. A line without them is left alone.
func splitDetails(line string) (string, string, bool) {
	head, rest, found := strings.Cut(line, " ")
	if !found || !strings.HasPrefix(head, "com.docker.swarm.") {
		return "", line, false
	}
	var task string
	for _, pair := range strings.Split(head, ",") {
		if v, ok := strings.CutPrefix(pair, "com.docker.swarm.task.id="); ok {
			task = v
		}
	}
	return task, rest, true
}
